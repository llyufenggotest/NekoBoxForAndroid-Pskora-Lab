package group

import (
	"github.com/sagernet/sing-box/adapter"
	"testing"
	"time"
)

func TestRuntimeMemberHistory(t *testing.T) {
	s, leaves := fixture(t, 2)
	enableFallback(s)
	if got := s.RuntimeMemberHistory(); len(got) != 0 {
		t.Fatal(got)
	}
	stamp := time.Now()
	s.group.history.StoreURLTestHistory(leaves[0].Tag(), &adapter.URLTestHistory{Time: stamp, Delay: 17})
	s.group.history.StoreURLTestHistory("not-a-member", &adapter.URLTestHistory{Time: stamp, Delay: 999})
	got := s.RuntimeMemberHistory()
	if len(got) != 1 || got[0].Tag != leaves[0].Tag() || got[0].Delay != 17 || got[0].SampleTime != stamp.UnixMilli() {
		t.Fatal(got)
	}
	s.group.history.DeleteURLTestHistory(leaves[0].Tag())
	if len(s.RuntimeMemberHistory()) != 0 {
		t.Fatal("deleted history retained")
	}
}
