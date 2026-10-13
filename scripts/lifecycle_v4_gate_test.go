package main

import "testing"

func scanFixture(t *testing.T, source string) (map[string]startFunction, []issue) {
	t.Helper()
	starts, issues, err := scanStarts(sourceTree{files: map[string][]byte{"fixture.go": []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	return starts, issues
}

func hasIssueKind(issues []issue, kind string) bool {
	for _, current := range issues {
		if current.Kind == kind {
			return true
		}
	}
	return false
}

func TestGateRejectsUnreachableScopeRegistration(t *testing.T) {
	starts, issues := scanFixture(t, `package fixture

type component struct{}

func (c *component) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if false {
		scope.Add(c.Close)
	}
	return nil
}
`)
	start := starts["fixture.go::component"]
	if len(start.Registrations) != 0 {
		t.Fatalf("unreachable registration counted as cleanup: %#v", start.Registrations)
	}
	if !hasIssueKind(issues, "unreachable_scope_add") {
		t.Fatalf("missing unreachable_scope_add issue: %#v", issues)
	}
}

func TestGateRejectsSemanticNoopCleanup(t *testing.T) {
	fixtures := map[string]string{
		"unreachable close": `func() error {
			if false { return c.Close() }
			_ = c
			return nil
		}`,
		"zero error variable": `func() error {
			var err error
			return err
		}`,
	}
	for name, cleanup := range fixtures {
		t.Run(name, func(t *testing.T) {
			_, issues := scanFixture(t, `package fixture

type component struct{}

func (c *component) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	scope.Add(`+cleanup+`)
	return nil
}
`)
			if !hasIssueKind(issues, "semantic_noop_scope_add") {
				t.Fatalf("missing semantic_noop_scope_add issue: %#v", issues)
			}
		})
	}
}

func TestGateRejectsUnapprovedWaitHelper(t *testing.T) {
	_, issues := scanFixture(t, `package fixture

type component struct{}

func (c *component) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	scope.Add(waitForDone(make(chan struct{})))
	return nil
}
`)
	if !hasIssueKind(issues, "semantic_noop_scope_add") {
		t.Fatalf("unapproved helper bypassed the gate: %#v", issues)
	}
}

func TestGateAcceptsReachableCleanupCall(t *testing.T) {
	starts, issues := scanFixture(t, `package fixture

type component struct{}

func (c *component) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	scope.Add(func() error { return c.Close() })
	return nil
}
`)
	start := starts["fixture.go::component"]
	if len(start.Registrations) != 1 {
		t.Fatalf("reachable registration missing: %#v", start.Registrations)
	}
	if hasIssueKind(issues, "semantic_noop_scope_add") || hasIssueKind(issues, "unreachable_scope_add") {
		t.Fatalf("reachable cleanup rejected: %#v", issues)
	}
}
