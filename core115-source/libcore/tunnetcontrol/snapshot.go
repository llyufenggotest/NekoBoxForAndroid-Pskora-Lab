package tunnetcontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const snapshotSchema = 1

type Snapshot struct {
	SchemaVersion   int             `json:"schema_version"`
	ClientID        string          `json:"client_id"`
	ActiveEntryNode string          `json:"active_entry_node"`
	SelectedHost    string          `json:"selected_host_slug"`
	ECHConfig       SnapshotECH     `json:"ech_config"`
	EntryNodes      []SnapshotEntry `json:"entry_nodes"`
	Hosts           []SnapshotHost  `json:"hosts"`
	ValidatedRoute  string          `json:"validated_route_ip"`
	XHTTPAuthority  string          `json:"xhttp_authority"`
	XHTTPPath       string          `json:"xhttp_path"`
}

type SnapshotECH struct {
	ConfigList string `json:"config_list"`
	ExpiresAt  int64  `json:"expires_at"`
}

type SnapshotEntry struct {
	Name       string             `json:"name"`
	IPv4       []string           `json:"ipv4"`
	FrontProxy SnapshotFrontProxy `json:"front_proxy"`
}

type SnapshotFrontProxy struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
}

type SnapshotHost struct {
	Slug               string  `json:"slug"`
	Online             bool    `json:"online"`
	LoadPercent        float64 `json:"load_percent,omitempty"`
	Authority          string  `json:"authority"`
	Domain             string  `json:"domain,omitempty"`
	VLESSEncryptionKey string  `json:"vless_encryption_key"`
}

func WriteSnapshotAtomic(path string, snapshot *Snapshot, now time.Time) error {
	if err := validateSnapshot(snapshot, now); err != nil {
		return err
	}
	content, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal TunNet snapshot: %w", err)
	}
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create TunNet snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".tunnet-snapshot-*")
	if err != nil {
		return fmt.Errorf("create TunNet snapshot temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(content)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write TunNet snapshot: %w", err)
	}
	if err = replaceSnapshotFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace TunNet snapshot: %w", err)
	}
	return nil
}

func validateSnapshot(snapshot *Snapshot, now time.Time) error {
	if snapshot == nil || snapshot.SchemaVersion != snapshotSchema {
		return errors.New("invalid TunNet snapshot schema")
	}
	if !clientIDPattern.MatchString(strings.ToLower(snapshot.ClientID)) {
		return errors.New("invalid TunNet snapshot client ID")
	}
	if snapshot.ActiveEntryNode == "" || snapshot.SelectedHost == "" {
		return errors.New("TunNet snapshot selection is incomplete")
	}
	if snapshot.ECHConfig.ConfigList == "" || snapshot.ECHConfig.ExpiresAt <= now.UnixMilli() {
		return errors.New("TunNet snapshot ECH is missing or expired")
	}
	if net.ParseIP(snapshot.ValidatedRoute) == nil {
		return errors.New("invalid TunNet snapshot route")
	}
	if !validAuthority(snapshot.XHTTPAuthority) || snapshot.XHTTPPath == "" || !strings.HasPrefix(snapshot.XHTTPPath, "/") {
		return errors.New("invalid TunNet snapshot XHTTP target")
	}
	entryFound := false
	for _, entry := range snapshot.EntryNodes {
		if entry.Name != snapshot.ActiveEntryNode {
			continue
		}
		parsed, err := url.Parse(entry.FrontProxy.Endpoint)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("invalid TunNet snapshot front proxy")
		}
		if entry.FrontProxy.Headers["Host"] == "" || entry.FrontProxy.Headers["X-T5-Auth"] == "" {
			return errors.New("TunNet snapshot front proxy headers are incomplete")
		}
		entryFound = true
		break
	}
	if !entryFound {
		return errors.New("TunNet snapshot active entry is missing")
	}
	hostFound := false
	for _, host := range snapshot.Hosts {
		if !strings.EqualFold(host.Slug, snapshot.SelectedHost) {
			continue
		}
		if !host.Online || !validAuthority(firstValue(host.Authority, host.Domain)) || host.VLESSEncryptionKey == "" {
			return errors.New("TunNet snapshot selected host is unusable")
		}
		hostFound = true
		break
	}
	if !hostFound {
		return errors.New("TunNet snapshot selected host is missing")
	}
	return nil
}

func replaceSnapshotFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	// Windows does not replace an existing destination with Rename. Keep the
	// previous file recoverable until the new file has been installed.
	backup := destination + ".previous"
	_ = os.Remove(backup)
	if err := os.Rename(destination, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		_ = os.Rename(backup, destination)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func validAuthority(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\\/\r\n")
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
