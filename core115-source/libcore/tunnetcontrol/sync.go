package tunnetcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var syncMu sync.Mutex

type SyncOptions struct {
	Endpoint      string
	IdentityPath  string
	SnapshotPath  string
	AppVersion    string
	Timeout       time.Duration
	PollInterval  time.Duration
	HTTPClient    *http.Client
	OpenResponse  ResponseOpener
	DataAuthority string
	EntryNode     string
	HostSlug      string
	BuildSnapshot func(bootstrap, syncResponse json.RawMessage, echConfig ECHConfig, identity *Identity) (*Snapshot, error)
}

type acquiredCatalog struct {
	bootstrap json.RawMessage
	synced    json.RawMessage
	ech       ECHConfig
	identity  *Identity
}

func acquireCatalog(ctx context.Context, options SyncOptions) (*acquiredCatalog, error) {
	if options.IdentityPath == "" || options.AppVersion == "" {
		return nil, errors.New("incomplete TunNet catalog options")
	}
	identity, err := LoadOrCreateIdentity(options.IdentityPath)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse TunNet endpoint: %w", err)
	}
	client := &Client{Endpoint: endpoint, HTTPClient: options.HTTPClient, Identity: identity, Opener: options.OpenResponse}
	if client.Opener == nil {
		client.Opener = HPKEResponseOpener{}
	}
	if options.Timeout <= 0 {
		options.Timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	machine := AccessMachine{Client: client, AppVersion: options.AppVersion, Platform: "android", HTTPClient: options.HTTPClient, PollInterval: options.PollInterval}
	bootstrapResponse, err := machine.BootstrapAndAwaitReady(ctx, nil)
	if err != nil {
		return nil, err
	}
	bootstrap, err := json.Marshal(bootstrapResponse)
	if err != nil {
		return nil, fmt.Errorf("marshal TunNet ready bootstrap: %w", err)
	}
	syncBody, err := marshalIdentityRequest(identity.ClientID, "android", options.AppVersion)
	if err != nil {
		return nil, err
	}
	var syncResponse json.RawMessage
	if err = client.Call(ctx, "sync", json.RawMessage(syncBody), &syncResponse); err != nil {
		return nil, fmt.Errorf("TunNet sync: %w", err)
	}
	echConfig, err := FetchECHConfigDNS(ctx, options.HTTPClient, echQueryName, time.Now())
	if err != nil {
		return nil, err
	}
	return &acquiredCatalog{bootstrap: bootstrap, synced: syncResponse, ech: echConfig, identity: identity}, nil
}

// SyncToSnapshot completes the signed control/access flow and installs a validated
// snapshot. It is safe to expose through gomobile: inputs and outputs contain no
// private key material.
func SyncToSnapshot(ctx context.Context, options SyncOptions) error {
	syncMu.Lock()
	defer syncMu.Unlock()

	if options.IdentityPath == "" || options.SnapshotPath == "" || options.AppVersion == "" {
		return errors.New("incomplete TunNet sync options")
	}
	identity, err := LoadOrCreateIdentity(options.IdentityPath)
	if err != nil {
		return err
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil {
		return fmt.Errorf("parse TunNet endpoint: %w", err)
	}
	client := &Client{
		Endpoint:   endpoint,
		HTTPClient: options.HTTPClient,
		Identity:   identity,
		Opener:     options.OpenResponse,
	}
	if client.Opener == nil {
		client.Opener = HPKEResponseOpener{}
	}
	if options.Timeout <= 0 {
		options.Timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	machine := AccessMachine{
		Client:       client,
		AppVersion:   options.AppVersion,
		Platform:     "android",
		HTTPClient:   options.HTTPClient,
		PollInterval: options.PollInterval,
	}
	bootstrapResponse, err := machine.BootstrapAndAwaitReady(ctx, nil)
	if err != nil {
		return err
	}
	bootstrap, err := json.Marshal(bootstrapResponse)
	if err != nil {
		return fmt.Errorf("marshal TunNet ready bootstrap: %w", err)
	}

	syncBody, err := marshalIdentityRequest(identity.ClientID, "android", options.AppVersion)
	if err != nil {
		return err
	}
	var syncResponse json.RawMessage
	if err = client.Call(ctx, "sync", json.RawMessage(syncBody), &syncResponse); err != nil {
		return fmt.Errorf("TunNet sync: %w", err)
	}
	authorityResponse := syncResponse
	if options.DataAuthority == "" {
		var synced syncWire
		var ready syncWire
		if json.Unmarshal(syncResponse, &synced) != nil || synced.Runtime == nil ||
			json.Unmarshal(bootstrap, &ready) != nil || ready.Runtime == nil {
			return errors.New("invalid TunNet runtime for authority merge")
		}
		if len(synced.Runtime.Network.RootDomains) == 0 {
			synced.Runtime.Network = ready.Runtime.Network
		}
		authorityResponse, err = json.Marshal(synced)
		if err != nil {
			return fmt.Errorf("merge TunNet authority metadata: %w", err)
		}
	}
	authority, err := ResolveDataAuthority(authorityResponse, options.DataAuthority, options.HostSlug)
	if err != nil {
		return fmt.Errorf("select TunNet data authority: %w", err)
	}
	echConfig, err := FetchECHConfigDNS(ctx, options.HTTPClient, echQueryName, time.Now())
	if err != nil {
		return err
	}
	mapper := options.BuildSnapshot
	if mapper == nil {
		mapper = func(bootstrap, syncResponse json.RawMessage, echConfig ECHConfig, identity *Identity) (*Snapshot, error) {
			previousEntry := ""
			if previous, readErr := os.ReadFile(options.SnapshotPath); readErr == nil {
				var existing Snapshot
				if json.Unmarshal(previous, &existing) == nil && strings.EqualFold(existing.ClientID, identity.ClientID) {
					previousEntry = existing.ActiveEntryNode
				}
			}
			return MapSnapshotWithOptions(ctx, bootstrap, syncResponse, echConfig, identity, MapOptions{
				DataAuthority: authority, PreviousEntryNode: previousEntry,
				RequestedEntryNode: options.EntryNode, RequestedHostSlug: options.HostSlug,
			})
		}
	}
	snapshot, err := mapper(bootstrap, syncResponse, echConfig, identity)
	if err != nil {
		return fmt.Errorf("build TunNet snapshot: %w", err)
	}
	return WriteSnapshotAtomic(options.SnapshotPath, snapshot, time.Now())
}

func parseAccessRaw(response []byte) (Access, error) {
	var bootstrap BootstrapResponse
	if err := json.Unmarshal(response, &bootstrap); err != nil {
		return Access{}, fmt.Errorf("decode TunNet bootstrap: %w", err)
	}
	if bootstrap.SchemaVersion != 2 {
		return Access{}, fmt.Errorf("unsupported TunNet bootstrap schema: %d", bootstrap.SchemaVersion)
	}
	return bootstrap.Access, nil
}

type identityRequest struct {
	ClientID   string `json:"client_id"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

func marshalIdentityRequest(clientID, platform, appVersion string) ([]byte, error) {
	clientID = strings.ToLower(clientID)
	if !clientIDPattern.MatchString(clientID) || strings.TrimSpace(platform) == "" || strings.TrimSpace(appVersion) == "" {
		return nil, errors.New("invalid TunNet identity request")
	}
	return json.Marshal(identityRequest{ClientID: clientID, Platform: platform, AppVersion: appVersion})
}

// Sync is the gomobile-facing entry point used before constructing a #TunNet
// sing-box outbound. The protocol mapper remains fail-closed until its exact
// runtime response schema is installed.
func Sync(endpoint, noBackupDirectory, appVersion, dataAuthority, entryNode, hostSlug string) error {
	root := filepath.Join(noBackupDirectory, "tunnet")
	return SyncToSnapshot(context.Background(), SyncOptions{
		Endpoint:      endpoint,
		IdentityPath:  filepath.Join(root, "identity.json"),
		SnapshotPath:  filepath.Join(root, "snapshot.json"),
		AppVersion:    appVersion,
		DataAuthority: dataAuthority,
		EntryNode:     entryNode,
		HostSlug:      hostSlug,
		HTTPClient:    nil,
		OpenResponse:  HPKEResponseOpener{},
	})
}

func SnapshotExists(noBackupDirectory string) bool {
	information, err := os.Stat(filepath.Join(noBackupDirectory, "tunnet", "snapshot.json"))
	return err == nil && information.Mode().IsRegular() && information.Size() > 0
}
