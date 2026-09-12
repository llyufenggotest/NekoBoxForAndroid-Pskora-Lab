package tunnetcontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type batchCatalog struct {
	root       string
	bootstrap  json.RawMessage
	synced     json.RawMessage
	ech        ECHConfig
	identity   *Identity
	routeProbe RouteProber
	mu         sync.Mutex
	paths      map[string]struct{}
	closed     bool
}

type cachedProbeResult struct {
	latency time.Duration
	err     error
	ready   chan struct{}
}

type cachedRouteProber struct {
	prober RouteProber
	mu     sync.Mutex
	cache  map[string]*cachedProbeResult
}

func newCachedRouteProber(prober RouteProber) *cachedRouteProber {
	return &cachedRouteProber{prober: prober, cache: make(map[string]*cachedProbeResult)}
}

func (p *cachedRouteProber) Probe(ctx context.Context, address string) (time.Duration, error) {
	p.mu.Lock()
	result, exists := p.cache[address]
	if !exists {
		result = &cachedProbeResult{ready: make(chan struct{})}
		p.cache[address] = result
		p.mu.Unlock()
		result.latency, result.err = p.prober.Probe(ctx, address)
		close(result.ready)
		return result.latency, result.err
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-result.ready:
		return result.latency, result.err
	}
}

type IngressProbeResult struct {
	Address       string `json:"address"`
	Port          int    `json:"port"`
	LatencyMillis int64  `json:"latency_ms"`
}

var batchRegistry = struct {
	sync.Mutex
	catalogs map[string]*batchCatalog
}{catalogs: make(map[string]*batchCatalog)}

func randomBatchToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate TunNet batch token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func registerBatch(catalog *batchCatalog) (string, error) {
	if catalog == nil || catalog.root == "" || catalog.identity == nil || catalog.routeProbe == nil {
		return "", errors.New("incomplete TunNet batch catalog")
	}
	if err := os.MkdirAll(catalog.root, 0o700); err != nil {
		return "", fmt.Errorf("create TunNet batch directory: %w", err)
	}
	catalog.paths = make(map[string]struct{})
	for {
		token, err := randomBatchToken()
		if err != nil {
			return "", err
		}
		batchRegistry.Lock()
		if _, exists := batchRegistry.catalogs[token]; !exists {
			batchRegistry.catalogs[token] = catalog
			batchRegistry.Unlock()
			return token, nil
		}
		batchRegistry.Unlock()
	}
}

func lookupBatch(token string) (*batchCatalog, error) {
	batchRegistry.Lock()
	catalog := batchRegistry.catalogs[token]
	batchRegistry.Unlock()
	if catalog == nil {
		return nil, errors.New("unknown or released TunNet batch token")
	}
	return catalog, nil
}

// PrepareBatch acquires the rotating control catalog once and returns an opaque,
// process-private token. The token contains no identity or catalog material.
func prepareBatchWithOptions(ctx context.Context, endpoint, noBackupDirectory, appVersion string, options SyncOptions) (string, error) {
	syncMu.Lock()
	defer syncMu.Unlock()
	root := filepath.Join(noBackupDirectory, "tunnet")
	options.Endpoint = endpoint
	options.IdentityPath = filepath.Join(root, "identity.json")
	options.AppVersion = appVersion
	acquired, err := acquireCatalog(ctx, options)
	if err != nil {
		return "", err
	}
	return registerBatch(&batchCatalog{
		root: filepath.Join(root, "batch"), bootstrap: acquired.bootstrap, synced: acquired.synced,
		ech: acquired.ech, identity: acquired.identity, routeProbe: newCachedRouteProber(TCPRouteProber{}),
	})
}

func PrepareBatch(endpoint, noBackupDirectory, appVersion string) (string, error) {
	return prepareBatchWithOptions(context.Background(), endpoint, noBackupDirectory, appVersion, SyncOptions{OpenResponse: HPKEResponseOpener{}})
}

func (catalog *batchCatalog) writeSnapshot(path string, snapshot *Snapshot) error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if catalog.closed {
		return errors.New("released TunNet batch token")
	}
	if _, exists := catalog.paths[path]; exists {
		return errors.New("TunNet batch snapshot path already materialized")
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("TunNet batch snapshot path already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect TunNet batch snapshot path: %w", err)
	}
	if err := WriteSnapshotAtomic(path, snapshot, time.Now()); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o400); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("seal TunNet batch snapshot: %w", err)
	}
	catalog.paths[path] = struct{}{}
	return nil
}

// MaterializeBatch writes a validated, immutable snapshot for one selector and
// returns its unique path. Catalog acquisition and route probes are reused.
func MaterializeBatchSnapshot(token, entryNode, hostSlug, snapshotPath string) error {
	catalog, err := lookupBatch(token)
	if err != nil {
		return err
	}
	if snapshotPath == "" || strings.TrimSpace(entryNode) != entryNode || strings.TrimSpace(hostSlug) != hostSlug {
		return errors.New("invalid TunNet batch materialization options")
	}
	snapshot, err := MapSnapshotWithOptions(context.Background(), catalog.bootstrap, catalog.synced, catalog.ech, catalog.identity, MapOptions{
		RequestedEntryNode: entryNode, RequestedHostSlug: hostSlug, RouteProber: catalog.routeProbe,
	})
	if err != nil {
		return fmt.Errorf("build TunNet batch snapshot: %w", err)
	}
	return catalog.writeSnapshot(snapshotPath, snapshot)
}

func MaterializeBatch(token, dataAuthority, entryNode, hostSlug string) (string, error) {
	catalog, err := lookupBatch(token)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(entryNode) != entryNode || strings.TrimSpace(hostSlug) != hostSlug {
		return "", errors.New("invalid TunNet batch selector")
	}
	snapshot, err := MapSnapshotWithOptions(context.Background(), catalog.bootstrap, catalog.synced, catalog.ech, catalog.identity, MapOptions{
		DataAuthority: dataAuthority, RequestedEntryNode: entryNode, RequestedHostSlug: hostSlug, RouteProber: catalog.routeProbe,
	})
	if err != nil {
		return "", fmt.Errorf("build TunNet batch snapshot: %w", err)
	}
	name, err := randomBatchToken()
	if err != nil {
		return "", err
	}
	path := filepath.Join(catalog.root, name+".json")
	if err = catalog.writeSnapshot(path, snapshot); err != nil {
		return "", err
	}
	return path, nil
}

// ResolveBatchIngress probes an entry's TCP ingress and returns a JSON result
// suitable for gomobile callers.
func ResolveBatchIngress(token, entryNode string) (string, error) {
	catalog, err := lookupBatch(token)
	if err != nil {
		return "", err
	}
	var wire syncWire
	if err = json.Unmarshal(catalog.synced, &wire); err != nil || wire.Runtime == nil {
		return "", errors.New("invalid TunNet batch runtime")
	}
	var candidates []string
	for _, entry := range wire.Runtime.EntryNodes {
		if entry.Name == entryNode {
			candidates = entry.IPv4
			break
		}
	}
	if candidates == nil {
		return "", errors.New("requested TunNet entry is absent from batch")
	}
	ip, err := SelectRouteIP(context.Background(), candidates, catalog.routeProbe)
	if err != nil {
		return "", err
	}
	latency, err := catalog.routeProbe.Probe(context.Background(), ip)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(IngressProbeResult{Address: ip, Port: 443, LatencyMillis: latency.Milliseconds()})
	if err != nil {
		return "", fmt.Errorf("encode TunNet ingress probe: %w", err)
	}
	return string(encoded), nil
}

// ReleaseBatch invalidates a token and removes all snapshots it materialized.
func ReleaseBatch(token string) error {
	batchRegistry.Lock()
	catalog := batchRegistry.catalogs[token]
	delete(batchRegistry.catalogs, token)
	batchRegistry.Unlock()
	if catalog == nil {
		return errors.New("unknown or released TunNet batch token")
	}
	catalog.mu.Lock()
	catalog.closed = true
	paths := make([]string, 0, len(catalog.paths))
	for path := range catalog.paths {
		paths = append(paths, path)
	}
	catalog.mu.Unlock()
	var firstErr error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("remove TunNet batch snapshots: %w", firstErr)
	}
	return nil
}
