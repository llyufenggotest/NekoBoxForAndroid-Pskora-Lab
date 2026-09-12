package androidmonitor

import (
	"errors"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"net/netip"
	"sync"
	"testing"
)

func newEpochMonitor(t *testing.T) (*Monitor, *snapshotManager, *[]int) {
	t.Helper()
	ifaceA := control.Interface{Name: "ccmni1", Index: 10, Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/24")}}
	ifaceB := control.Interface{Name: "wlan0", Index: 11, Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}}
	nm := &snapshotManager{
		fakeManager: fakeManager{finder: fakeFinder{interfaces: map[int]*control.Interface{10: &ifaceA, 11: &ifaceB}}},
		snapshot:    []adapter.NetworkInterface{{Interface: ifaceA, DNSServers: []string{"1.1.1.1"}}},
	}
	m := New(&fakePlatform{}, func() adapter.NetworkManager { return nm }, logger.NOP())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	got := &[]int{}
	m.RegisterCallback(func(intf *control.Interface, _ int) {
		if intf == nil {
			*got = append(*got, -1)
		} else {
			*got = append(*got, intf.Index)
		}
	})
	return m, nm, got
}

func TestOfflineEpochRecoveryDeliveryMatrix(t *testing.T) {
	m, nm, got := newEpochMonitor(t)
	defer m.Close()
	emit := func(name string, index int32) { m.UpdateDefaultInterface(name, index, false, false) }

	// Ordinary duplicates stay deduplicated. Empty repeats deliver offline once.
	emit("ccmni1", 10)
	emit("ccmni1", 10)
	emit("", -1)
	emit("", -1)
	emit("", -1)
	// lost -> same must be delivered once, then ordinary duplicate suppression resumes.
	emit("ccmni1", 10)
	emit("ccmni1", 10)
	// A second identical call cycle is a distinct recovery epoch.
	emit("", -1)
	emit("ccmni1", 10)
	// lost -> changed is delivered once without an extra duplicate.
	emit("", -1)
	nm.snapshot[0].Interface = *nm.finder.(fakeFinder).interfaces[11]
	emit("wlan0", 11)
	emit("wlan0", 11)
	// Direct A -> B remains a normal real change.
	nm.snapshot[0].Interface = *nm.finder.(fakeFinder).interfaces[10]
	emit("ccmni1", 10)

	want := []int{10, -1, 10, -1, 10, -1, 11, 10}
	if len(*got) != len(want) {
		t.Fatalf("events=%v want=%v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("events=%v want=%v", *got, want)
		}
	}
}

func TestOfflineEnumerationFailureStillOpensRecoveryEpoch(t *testing.T) {
	m, nm, got := newEpochMonitor(t)
	defer m.Close()
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	nm.updateErr = errors.New("no interfaces while offline")
	m.UpdateDefaultInterface("", -1, false, false)
	nm.updateErr = nil
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	want := []int{10, -1, 10}
	if len(*got) != len(want) {
		t.Fatalf("offline enumeration events=%v want=%v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("offline enumeration events=%v want=%v", *got, want)
		}
	}
}

func TestOfflineEpochEnumerationFailureDoesNotConsumeRecovery(t *testing.T) {
	m, nm, got := newEpochMonitor(t)
	defer m.Close()
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	m.UpdateDefaultInterface("", -1, false, false)
	nm.updateErr = errors.New("stale enumeration")
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	nm.updateErr = nil
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	want := []int{10, -1, 10}
	if len(*got) != len(want) {
		t.Fatalf("late enumeration events=%v want=%v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("late enumeration events=%v want=%v", *got, want)
		}
	}
}

func TestOfflineEpochConcurrentRecoveryDeliveredExactlyOnce(t *testing.T) {
	m, _, got := newEpochMonitor(t)
	defer m.Close()
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	m.UpdateDefaultInterface("", -1, false, false)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.UpdateDefaultInterface("ccmni1", 10, false, false) }()
	}
	wg.Wait()
	want := []int{10, -1, 10}
	if len(*got) != len(want) {
		t.Fatalf("concurrent events=%v want=%v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("concurrent events=%v want=%v", *got, want)
		}
	}
}

func TestRapidOfflineOnlineEpochsEachRecoverOnce(t *testing.T) {
	m, _, got := newEpochMonitor(t)
	defer m.Close()
	for i := 0; i < 3; i++ {
		m.UpdateDefaultInterface("ccmni1", 10, false, false)
		m.UpdateDefaultInterface("", -1, false, false)
	}
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	want := []int{10, -1, 10, -1, 10, -1, 10}
	if len(*got) != len(want) {
		t.Fatalf("rapid events=%v want=%v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("rapid events=%v want=%v", *got, want)
		}
	}
}
