package group

import (
    "testing"
    "time"

    "github.com/sagernet/sing-box/adapter"
)

// Advances a 300-second schedule deterministically; no wall-clock wait.
func TestPreferredAutoHistoryTwoIntervalsClockModel(t *testing.T) {
    s, leaves := fixture(t, 1)
    enableFallback(s)
    interval := 300 * time.Second
    now := time.Unix(1000, 0)
    due := func(history *adapter.URLTestHistory) bool {
        return history == nil || now.Sub(history.Time) >= interval
    }
    if !due(nil) { t.Fatal("initial batch not due") }
    first := &adapter.URLTestHistory{Time: now, Delay: 41}
    s.group.history.StoreURLTestHistory(leaves[0].Tag(), first)
    if got := s.RuntimeMemberHistory(); len(got) != 1 || got[0].Delay != 41 || got[0].SampleTime != now.UnixMilli() {
        t.Fatalf("first snapshot=%+v", got)
    }
    now = now.Add(interval - time.Millisecond)
    if due(first) { t.Fatal("second batch ran early") }
    now = now.Add(time.Millisecond)
    if !due(first) { t.Fatal("second 300-second batch not due") }
    second := &adapter.URLTestHistory{Time: now, Delay: 73}
    s.group.history.StoreURLTestHistory(leaves[0].Tag(), second)
    got := s.RuntimeMemberHistory()
    if len(got) != 1 || got[0].Delay != 73 || got[0].SampleTime != now.UnixMilli() || got[0].Source != "auto_urltest" {
        t.Fatalf("second snapshot=%+v", got)
    }
    t.Log("300s clock model produced two distinct automatic history batches")
}
