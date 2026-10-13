// Command lifecycle_v4_gate compares alpha.12 lifecycle cleanup structure and
// rejects obvious unreachable/no-op registrations. It is an auxiliary gate,
// not semantic proof; production failure-injection tests remain authoritative.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var requiredMissingPaths = []string{
	"protocol/masque/client.go",
	"protocol/masque/server.go",
	"protocol/tailscale/tailcat_inbound.go",
	"protocol/tailscale/tailcat_outbound.go",
}

type sourceTree struct {
	GitDir string
	Ref    string
	Root   string
	Prefix string
	files  map[string][]byte
}

type registration struct {
	Expression string `json:"expression"`
	Line       int    `json:"line"`
	Occurrence int    `json:"occurrence"`
}

type startFunction struct {
	Path          string         `json:"path"`
	Receiver      string         `json:"receiver"`
	Line          int            `json:"line"`
	HasScope      bool           `json:"has_scope"`
	Registrations []registration `json:"registrations"`
}

type aggregateExemption struct {
	Path                  string `json:"path"`
	OfficialResource      string `json:"official_resource"`
	EquivalentCleanup     string `json:"equivalent_cleanup"`
	Reason                string `json:"reason"`
	ProductionFailureTest string `json:"production_failure_test"`
}

type issue struct {
	Path       string `json:"path"`
	Receiver   string `json:"receiver,omitempty"`
	Kind       string `json:"kind"`
	Expression string `json:"expression,omitempty"`
	Line       int    `json:"line,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type absentProof struct {
	Path            string   `json:"path"`
	FileAbsent      bool     `json:"file_absent"`
	RegistryTerms   []string `json:"registry_terms"`
	RegistryMatches []string `json:"registry_matches"`
	Proved          bool     `json:"proved"`
}

type report struct {
	OfficialRef                    string                   `json:"official_ref"`
	MigrationRef                   string                   `json:"migration_ref"`
	OfficialStartCount             int                      `json:"official_start_count"`
	MigrationStartCount            int                      `json:"migration_start_count"`
	OfficialScopeAddCount          int                      `json:"official_scope_add_count"`
	MigrationScopeAddCount         int                      `json:"migration_scope_add_count"`
	MappedScopeAddCount            int                      `json:"mapped_scope_add_count"`
	AggregateExemptionCount        int                      `json:"aggregate_exemption_count"`
	OldSignatureCount              int                      `json:"old_signature_count"`
	MissingRegistrationCount       int                      `json:"missing_registration_count"`
	UnexpectedMissingPathCount     int                      `json:"unexpected_missing_path_count"`
	MissingPathAllowlist           []string                 `json:"missing_path_allowlist"`
	MissingPathAllowlistExact      bool                     `json:"missing_path_allowlist_exact"`
	MissingPathProofs              []absentProof            `json:"missing_path_proofs"`
	MissingPathProofFailureCount   int                      `json:"missing_path_proof_failure_count"`
	ASTNoopCleanupCount            int                      `json:"ast_noop_cleanup_count"`
	TextNoopCleanupCount           int                      `json:"text_noop_cleanup_count"`
	InvalidAggregateExemptionCount int                      `json:"invalid_aggregate_exemption_count"`
	Issues                         []issue                  `json:"issues"`
	OfficialFunctions              map[string]startFunction `json:"official_functions,omitempty"`
	MigrationFunctions             map[string]startFunction `json:"migration_functions,omitempty"`
	Passed                         bool                     `json:"passed"`
}

func main() {
	var officialGit, officialRef, migrationGit, migrationRef, migrationRoot, prefix, missingPathFile, aggregateFile, jsonOut string
	flag.StringVar(&officialGit, "official-git", "", "official repository used through git objects")
	flag.StringVar(&officialRef, "official-ref", "v1.15.0-alpha.12", "official git ref")
	flag.StringVar(&migrationGit, "migration-git", "", "migration repository used through git objects")
	flag.StringVar(&migrationRef, "migration-ref", "", "migration git ref (empty reads the worktree)")
	flag.StringVar(&migrationRoot, "migration-root", "core115-source/sing-box", "migration worktree root")
	flag.StringVar(&prefix, "migration-prefix", "core115-source/sing-box", "path prefix inside migration git objects")
	flag.StringVar(&missingPathFile, "missing-path-allowlist", "scripts/lifecycle_v4_missing_paths.json", "exact missing path allowlist")
	flag.StringVar(&aggregateFile, "aggregate-allowlist", "scripts/lifecycle_v4_aggregate_close_allowlist.json", "aggregate cleanup equivalence allowlist")
	flag.StringVar(&jsonOut, "json-out", "", "write JSON report")
	flag.Parse()

	if officialGit == "" {
		fatal(errors.New("--official-git is required"))
	}
	official := sourceTree{GitDir: officialGit, Ref: officialRef}
	migration := sourceTree{GitDir: migrationGit, Ref: migrationRef, Root: migrationRoot, Prefix: prefix}
	if migrationRef != "" && migrationGit == "" {
		migration.GitDir = "."
	}
	if err := official.load(); err != nil {
		fatal(err)
	}
	if err := migration.load(); err != nil {
		fatal(err)
	}

	missingPaths, err := readStringList(missingPathFile)
	if err != nil {
		fatal(err)
	}
	exemptions, exemptionIssues, err := readAggregateExemptions(aggregateFile, migration)
	if err != nil {
		fatal(err)
	}

	officialStarts, _, err := scanStarts(official)
	if err != nil {
		fatal(err)
	}
	migrationStarts, migrationNoops, err := scanStarts(migration)
	if err != nil {
		fatal(err)
	}
	textNoops := scanTextNoops(migration)

	r := report{
		OfficialRef: officialRef, MigrationRef: migrationRef,
		OfficialStartCount: len(officialStarts), MigrationStartCount: len(migrationStarts),
		OfficialScopeAddCount: registrationCount(officialStarts), MigrationScopeAddCount: registrationCount(migrationStarts),
		AggregateExemptionCount: len(exemptions), MissingPathAllowlist: missingPaths,
		MissingPathAllowlistExact: equalStrings(missingPaths, requiredMissingPaths),
		ASTNoopCleanupCount:       len(migrationNoops), TextNoopCleanupCount: len(textNoops),
		InvalidAggregateExemptionCount: len(exemptionIssues),
		OfficialFunctions:              officialStarts, MigrationFunctions: migrationStarts,
	}
	r.Issues = append(r.Issues, exemptionIssues...)
	if !r.MissingPathAllowlistExact {
		r.Issues = append(r.Issues, issue{Kind: "missing_path_allowlist_not_exact", Detail: fmt.Sprintf("want exactly %v", requiredMissingPaths)})
	}
	allowedMissing := make(map[string]bool)
	for _, path := range missingPaths {
		allowedMissing[path] = true
	}

	for _, actual := range migrationStarts {
		if !actual.HasScope {
			r.OldSignatureCount++
			r.Issues = append(r.Issues, issue{Path: actual.Path, Receiver: actual.Receiver, Kind: "old_start_signature", Line: actual.Line})
		}
	}

	usedExemptions := make(map[int]bool)
	for key, expected := range officialStarts {
		actual, ok := migrationStarts[key]
		if !ok {
			if allowedMissing[expected.Path] {
				continue
			}
			r.UnexpectedMissingPathCount++
			r.Issues = append(r.Issues, issue{Path: expected.Path, Receiver: expected.Receiver, Kind: "missing_start_function"})
			continue
		}
		actualByExpression := make(map[string][]registration)
		for _, add := range actual.Registrations {
			actualByExpression[add.Expression] = append(actualByExpression[add.Expression], add)
		}
		actualByCategory := make(map[string][]registration)
		for _, add := range actual.Registrations {
			if category := cleanupCategory(actual.Path, add.Expression); category != "" {
				actualByCategory[category] = append(actualByCategory[category], add)
			}
		}
		used := make(map[string]int)
		usedCategory := make(map[string]int)
		for _, add := range expected.Registrations {
			index := used[add.Expression]
			if index < len(actualByExpression[add.Expression]) {
				used[add.Expression]++
				r.MappedScopeAddCount++
				continue
			}
			if category := cleanupCategory(expected.Path, add.Expression); category != "" {
				categoryIndex := usedCategory[category]
				if categoryIndex < len(actualByCategory[category]) {
					usedCategory[category]++
					r.MappedScopeAddCount++
					continue
				}
			}
			if exemptionIndex := matchExemption(exemptions, usedExemptions, expected.Path, add.Expression, actual.Registrations); exemptionIndex >= 0 {
				usedExemptions[exemptionIndex] = true
				r.MappedScopeAddCount++
				continue
			}
			r.MissingRegistrationCount++
			r.Issues = append(r.Issues, issue{Path: expected.Path, Receiver: expected.Receiver, Kind: "missing_scope_add", Expression: add.Expression, Line: add.Line, Detail: fmt.Sprintf("official occurrence %d is not mapped", add.Occurrence)})
		}
	}

	for index, exemption := range exemptions {
		if usedExemptions[index] {
			continue
		}
		r.InvalidAggregateExemptionCount++
		r.Issues = append(r.Issues, issue{Path: exemption.Path, Kind: "unused_aggregate_exemption", Expression: exemption.OfficialResource, Detail: "entry did not map exactly one missing official scope.Add expression"})
	}

	for _, path := range requiredMissingPaths {
		terms := []string{"masque"}
		if strings.Contains(path, "tailcat") {
			terms = []string{"tailcat"}
		}
		proof := proveAbsent(migration, path, terms)
		r.MissingPathProofs = append(r.MissingPathProofs, proof)
		if !proof.Proved {
			r.MissingPathProofFailureCount++
			r.Issues = append(r.Issues, issue{Path: path, Kind: "missing_path_not_absent_or_unregistered", Detail: strings.Join(proof.RegistryMatches, "; ")})
		}
	}
	for _, hit := range migrationNoops {
		r.Issues = append(r.Issues, hit)
	}
	for _, hit := range textNoops {
		r.Issues = append(r.Issues, hit)
	}

	r.Passed = r.MissingPathAllowlistExact && r.OldSignatureCount == 0 && r.MissingRegistrationCount == 0 && r.UnexpectedMissingPathCount == 0 && r.MissingPathProofFailureCount == 0 && r.ASTNoopCleanupCount == 0 && r.TextNoopCleanupCount == 0 && r.InvalidAggregateExemptionCount == 0
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	_, _ = os.Stdout.Write(encoded)
	if jsonOut != "" {
		if err := os.MkdirAll(filepath.Dir(jsonOut), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(jsonOut, encoded, 0o644); err != nil {
			fatal(err)
		}
	}
	if !r.Passed {
		os.Exit(1)
	}
}

func (t *sourceTree) load() error {
	t.files = make(map[string][]byte)
	if t.Ref != "" {
		args := []string{"-C", t.GitDir, "ls-tree", "-r", "--name-only", t.Ref}
		if t.Prefix != "" {
			args = append(args, "--", t.Prefix)
		}
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			return fmt.Errorf("list %s: %w", t.Ref, err)
		}
		for _, objectPath := range strings.Fields(string(out)) {
			if !strings.HasSuffix(objectPath, ".go") {
				continue
			}
			rel := objectPath
			if t.Prefix != "" {
				rel = strings.TrimPrefix(strings.TrimPrefix(objectPath, t.Prefix), "/")
			}
			content, err := exec.Command("git", "-C", t.GitDir, "show", t.Ref+":"+filepath.ToSlash(objectPath)).Output()
			if err != nil {
				return fmt.Errorf("read %s:%s: %w", t.Ref, objectPath, err)
			}
			t.files[filepath.ToSlash(rel)] = content
		}
		return nil
	}
	return filepath.WalkDir(t.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(t.Root, path)
		if err != nil {
			return err
		}
		t.files[filepath.ToSlash(rel)] = content
		return nil
	})
}

func scanStarts(tree sourceTree) (map[string]startFunction, []issue, error) {
	result := make(map[string]startFunction)
	var noops []issue
	paths := sortedKeys(tree.files)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, tree.files[path], parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		parents := parentMap(file)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isScopeAdd(call) || len(call.Args) != 1 {
				return true
			}
			kind := ""
			switch {
			case !isStaticallyReachable(call, parents):
				kind = "unreachable_scope_add"
			case isNoopCleanup(call.Args[0]):
				kind = "ast_noop_scope_add"
			case !cleanupHasReachableEffect(path, call.Args[0]):
				kind = "semantic_noop_scope_add"
			}
			if kind != "" {
				noops = append(noops, issue{Path: path, Kind: kind, Line: fset.Position(call.Pos()).Line, Expression: nodeString(fset, call.Args[0])})
			}
			return true
		})
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Start" || fn.Body == nil || !hasStartStage(fn.Type.Params, file.Name.Name) || !returnsError(fn.Type.Results) {
				continue
			}
			receiver := receiverName(fn.Recv.List[0].Type)
			key := path + "::" + receiver
			if _, exists := result[key]; exists {
				return nil, nil, fmt.Errorf("duplicate lifecycle Start %s", key)
			}
			start := startFunction{Path: path, Receiver: receiver, Line: fset.Position(fn.Pos()).Line, HasScope: hasScope(fn.Type.Params)}
			counts := make(map[string]int)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isScopeAdd(call) || len(call.Args) != 1 || !isStaticallyReachable(call, parents) || !cleanupHasReachableEffect(path, call.Args[0]) {
					return true
				}
				expression := nodeString(fset, call.Args[0])
				counts[expression]++
				start.Registrations = append(start.Registrations, registration{Expression: expression, Line: fset.Position(call.Pos()).Line, Occurrence: counts[expression]})
				return true
			})
			result[key] = start
		}
	}
	return result, noops, nil
}

func hasStartStage(fields *ast.FieldList, packageName string) bool {
	if fields == nil {
		return false
	}
	for index, field := range fields.List {
		if index != 0 {
			return false
		}
		selector, ok := field.Type.(*ast.SelectorExpr)
		if ok {
			if id, ok := selector.X.(*ast.Ident); ok && id.Name == "adapter" && selector.Sel.Name == "StartStage" {
				return true
			}
		}
		if id, ok := field.Type.(*ast.Ident); ok && id.Name == "StartStage" && packageName == "adapter" {
			return true
		}
	}
	return false
}
func hasScope(fields *ast.FieldList) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		selector, ok := star.X.(*ast.SelectorExpr)
		if ok {
			if id, ok := selector.X.(*ast.Ident); ok && id.Name == "adapter" && selector.Sel.Name == "Scope" {
				return true
			}
		}
		if id, ok := star.X.(*ast.Ident); ok && id.Name == "Scope" {
			return true
		}
	}
	return false
}
func returnsError(fields *ast.FieldList) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if id, ok := field.Type.(*ast.Ident); ok && id.Name == "error" {
			return true
		}
	}
	return false
}
func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return nodeString(token.NewFileSet(), expr)
}
func isScopeAdd(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "scope"
}
func parentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func isStaticallyReachable(node ast.Node, parents map[ast.Node]ast.Node) bool {
	for current := node; current != nil; current = parents[current] {
		parent := parents[current]
		ifStmt, ok := parent.(*ast.IfStmt)
		if !ok || !isFalseExpr(ifStmt.Cond) {
			continue
		}
		if current != ifStmt.Else {
			return false
		}
	}
	return true
}

func isFalseExpr(expr ast.Expr) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return isFalseExpr(value.X)
	case *ast.Ident:
		return value.Name == "false"
	}
	return false
}

func cleanupHasReachableEffect(path string, expr ast.Expr) bool {
	if call, ok := expr.(*ast.CallExpr); ok {
		return isWaitForDoneCall(path, call)
	}
	if _, ok := expr.(*ast.SelectorExpr); ok {
		return true
	}
	fn, ok := expr.(*ast.FuncLit)
	if !ok {
		// Named cleanup functions are checked by production failure tests; the
		// structural gate can only prove inline cleanup bodies.
		return true
	}
	parents := parentMap(fn.Body)
	hasEffect := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if hasEffect || node == nil || !isStaticallyReachable(node, parents) {
			return !hasEffect
		}
		switch current := node.(type) {
		case *ast.CallExpr:
			hasEffect = true
			return false
		case *ast.SendStmt, *ast.IncDecStmt:
			hasEffect = true
			return false
		case *ast.UnaryExpr:
			if current.Op == token.ARROW {
				hasEffect = true
				return false
			}
		case *ast.AssignStmt:
			for _, left := range current.Lhs {
				if id, ok := left.(*ast.Ident); ok && id.Name == "_" {
					continue
				}
				hasEffect = true
				return false
			}
		}
		return true
	})
	return hasEffect
}

func cleanupCategory(path, expression string) string {
	if path == "service/origin_ca/service.go" && (expression == "waitForDone(done)" || expression == "func()error{<-donereturnnil}") {
		return "wait_for_done"
	}
	return ""
}

func isWaitForDoneCall(path string, call *ast.CallExpr) bool {
	if path != "service/origin_ca/service.go" {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "waitForDone" && len(call.Args) == 1
}

func isNoopCleanup(expr ast.Expr) bool {
	fn, ok := expr.(*ast.FuncLit)
	if !ok || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	id, ok := ret.Results[0].(*ast.Ident)
	return ok && id.Name == "nil"
}
func nodeString(fset *token.FileSet, node any) string {
	var output bytes.Buffer
	if err := format.Node(&output, fset, node); err != nil {
		return fmt.Sprintf("<format error: %v>", err)
	}
	return strings.Join(strings.Fields(output.String()), "")
}

var textNoopPattern = regexp.MustCompile(`(?s)scope\s*\.\s*Add\s*\(\s*func\s*\(\s*\)\s*error\s*\{\s*return\s+nil\s*\}\s*\)`)

func scanTextNoops(tree sourceTree) []issue {
	var result []issue
	for _, path := range sortedKeys(tree.files) {
		for _, location := range textNoopPattern.FindAllIndex(tree.files[path], -1) {
			line := bytes.Count(tree.files[path][:location[0]], []byte("\n")) + 1
			result = append(result, issue{Path: path, Kind: "text_noop_scope_add", Line: line, Expression: string(tree.files[path][location[0]:location[1]])})
		}
	}
	return result
}

func proveAbsent(tree sourceTree, path string, terms []string) absentProof {
	proof := absentProof{Path: path, FileAbsent: tree.files[path] == nil, RegistryTerms: terms}
	for file, content := range tree.files {
		base := filepath.Base(file)
		if !strings.HasPrefix(file, "include/") && base != "registry.go" && !strings.Contains(file, "/registry/") {
			continue
		}
		lower := strings.ToLower(string(content))
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				proof.RegistryMatches = append(proof.RegistryMatches, file+": contains "+term)
				break
			}
		}
	}
	sort.Strings(proof.RegistryMatches)
	proof.Proved = proof.FileAbsent && len(proof.RegistryMatches) == 0
	return proof
}

func readStringList(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf}), &values); err != nil {
		return nil, err
	}
	return values, nil
}
func readAggregateExemptions(path string, migration sourceTree) ([]aggregateExemption, []issue, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var values []aggregateExemption
	if err := json.Unmarshal(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf}), &values); err != nil {
		return nil, nil, err
	}
	var issues []issue
	seen := make(map[string]bool)
	for _, value := range values {
		key := value.Path + "\x00" + value.OfficialResource
		valid := value.Path != "" && value.OfficialResource != "" && value.EquivalentCleanup != "" && value.Reason != "" && value.ProductionFailureTest != "" && !seen[key]
		seen[key] = true
		parts := strings.SplitN(value.ProductionFailureTest, "::", 2)
		if len(parts) != 2 || migration.files[parts[0]] == nil || !bytes.Contains(migration.files[parts[0]], []byte(parts[1])) {
			valid = false
		}
		if !valid {
			issues = append(issues, issue{Path: value.Path, Kind: "invalid_aggregate_exemption", Expression: value.OfficialResource, Detail: "all fields are required, entries must be unique, and production_failure_test must be path::TestName present in migration sources"})
		}
	}
	return values, issues, nil
}
func matchExemption(values []aggregateExemption, used map[int]bool, path, official string, actual []registration) int {
	for index, value := range values {
		if used[index] || value.Path != path || value.OfficialResource != official {
			continue
		}
		for _, add := range actual {
			if add.Expression == value.EquivalentCleanup {
				return index
			}
		}
	}
	return -1
}
func registrationCount(values map[string]startFunction) int {
	total := 0
	for _, value := range values {
		total += len(value.Registrations)
	}
	return total
}
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "lifecycle_v4_gate:", err); os.Exit(2) }
