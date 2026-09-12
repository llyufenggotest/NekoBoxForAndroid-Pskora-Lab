package configcompat

import (
	"context"
	"encoding/json"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"os"
	"path/filepath"
	"testing"
)

func TestAndroidFixtureAdapterInventory(t *testing.T) {
	files, err := filepath.Glob("../../evidence/android-config-tests/fixtures/*.json")
	if err != nil || len(files) != 50 {
		t.Fatalf("fixture count %d %v", len(files), err)
	}
	results := []map[string]string{}
	for _, f := range files {
		r := map[string]string{"file": filepath.Base(f), "stage": "adapted-schema"}
		b, err := os.ReadFile(f)
		if err == nil {
			b, err = Convert(b, nil, nil)
		}
		if err == nil {
			var o option.Options
			err = o.UnmarshalJSONContext(include.Context(context.Background()), b)
		}
		if err != nil {
			r["error"] = err.Error()
			r["stage"] = "blocked"
		}
		results = append(results, r)
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	if err = os.WriteFile("../../evidence/android-config-tests/adapter-inventory.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r["stage"] == "blocked" {
			t.Log(r["file"], r["error"])
		}
	}
	t.Log("inventory only, blocked records are NOT compatibility successes")
}
