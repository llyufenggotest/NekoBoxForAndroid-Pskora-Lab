package vless

import (
	"testing"
)

func TestPureStrictIdentity(t *testing.T) {
	for _, id := range []string{"bad#pure", "00000000000000000000000000000000#pure", "00000000-0000-0000-0000-000000000000#juzi#pure", "00000000-0000-0000-0000-000000000000#pure#sl"} {
		if _, err := NewClient(id, "", nil); err == nil {
			t.Errorf("accepted invalid Pure identity")
		}
	}
	for _, suffix := range []string{"#pure", "#PURE", "#PuRe"} {
		if _, err := NewClient("00000000-0000-0000-0000-000000000000"+suffix, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewClient("00000000-0000-0000-0000-000000000000#pure", FlowVision, nil); err == nil {
		t.Error("Pure accepted Vision")
	}
}
