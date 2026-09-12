package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type capture struct {
	enabled bool
	mu      sync.Mutex
	lines   []string
}

func (c *capture) DiagnosticEnabled() bool { return c.enabled }
func (c *capture) Debug(a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprint(a...))
}
func TestPrivacyGateAndLimits(t *testing.T) {
	emitted.Store(0)
	c := &capture{}
	ctx, f := New(context.Background(), c, 1, 1, 123, 1, true, 1)
	if f != nil || From(ctx) != nil || emitted.Load() != 0 {
		t.Fatal("off allocated")
	}
	Emit(c, Record{Event: Start})
	if len(c.lines) != 0 {
		t.Fatal("off logged")
	}
	c.enabled = true
	_, f = New(ctx, c, 1, 1, 123, 1, true, 1)
	f.IO(true, 9, errors.New("https://SECRET:UUID@domain.invalid/payload"))
	for i := 0; i < 100; i++ {
		f.Event(Record{Event: GotConn})
	}
	if len(c.lines) != 24 {
		t.Fatal(len(c.lines))
	}
	if strings.Contains(strings.Join(c.lines, ""), "SECRET") {
		t.Fatal("leak")
	}
	for i := uint64(0); i < Limit*2; i++ {
		Emit(c, Record{Event: NATMiss})
	}
	if uint64(len(c.lines)) != Limit {
		t.Fatal(len(c.lines))
	}
	c.enabled = false
	Emit(c, Record{})
	if uint64(len(c.lines)) != Limit {
		t.Fatal("gate")
	}
}

func TestClassifyPackagesUsesRuntimeIdentity(t *testing.T) {
	cases := []struct {
		packages []string
		app, sub uint8
	}{
		{[]string{"org.telegram.messenger"}, AppTelegram, SubAppTelegramOfficial},
		{[]string{"tw.nekomimi.nekogram"}, AppTelegram, SubAppTelegramNekogram},
		{[]string{"com.android.chrome"}, AppChrome, SubAppUnknown},
		{[]string{"org.telegram.messenger.beta"}, AppUnknown, SubAppUnknown},
	}
	for _, tc := range cases {
		app, sub := ClassifyPackages(tc.packages)
		if app != tc.app || sub != tc.sub {
			t.Fatalf("packages=%v got=%d/%d want=%d/%d", tc.packages, app, sub, tc.app, tc.sub)
		}
	}
}

func TestTUNCorrelationKeepsRuntimeUIDWhenPackageUnknown(t *testing.T) {
	emitted.Store(0)
	c := &capture{enabled: true}
	ctx := BeginTUN(context.Background(), c, 38428, []string{""}, 10480, true)
	_, f := New(ctx, c, 0, 0, 0, 0, false, 1)
	if f.App != AppUnknown || f.SubApp != SubAppUnknown || f.UID != 10480 || f.Source != 38428 || !f.OwnerKnown {
		t.Fatalf("runtime owner discarded: %+v", f)
	}
	if got := strings.Join(c.lines, "\n"); !strings.Contains(got, "DTRACE3 schema=3") || !strings.Contains(got, "owner=true") || !strings.Contains(got, "uid=10480") {
		t.Fatalf("schema/owner identity missing: %s", got)
	}
}

func TestUnknownOwnerIsExplicitAndDoesNotInventUID(t *testing.T) {
	emitted.Store(0)
	c := &capture{enabled: true}
	ctx := BeginTUN(context.Background(), c, 38429, nil, 0, false)
	_, f := New(ctx, c, 0, 0, 0, 0, false, 1)
	if f != nil || len(c.lines) != 0 {
		t.Fatalf("ownerless flow emitted: flow=%+v lines=%v", f, c.lines)
	}
}

func TestTUNCorrelationConcurrentPortReuse(t *testing.T) {
	emitted.Store(0)
	seq.Store(0)
	generation.Store(9)
	c := &capture{enabled: true}
	const source = 42424
	var wg sync.WaitGroup
	for _, tc := range []struct {
		pkg string
		uid int32
		sub uint8
	}{
		{"org.telegram.messenger", 10537, SubAppTelegramOfficial},
		{"tw.nekomimi.nekogram", 10480, SubAppTelegramNekogram},
	} {
		wg.Add(1)
		go func(pkg string, uid int32, sub uint8) {
			defer wg.Done()
			ctx := BeginTUN(context.Background(), c, source, []string{pkg}, uid, true)
			_, f := New(ctx, c, 0, 0, 0, 0, false, 1)
			f.Event(Record{Event: GotConn, Reused: true})
			if f.Source != source || f.UID != uid || f.SubApp != sub || f.Gen != 9 || !f.OwnerKnown {
				t.Errorf("correlation crossed: %+v", f)
			}
		}(tc.pkg, tc.uid, tc.sub)
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.lines) != 6 {
		t.Fatalf("lines=%d %v", len(c.lines), c.lines)
	}
	for _, uid := range []string{"uid=10537", "uid=10480"} {
		seen := map[string]bool{}
		for _, line := range c.lines {
			if strings.Contains(line, uid) && strings.Contains(line, "sport=42424") {
				for _, part := range strings.Fields(line) {
					if strings.HasPrefix(part, "flow=") {
						seen[part] = true
					}
				}
			}
		}
		if len(seen) != 1 {
			t.Fatalf("%s flows=%v lines=%v", uid, seen, c.lines)
		}
	}
}
