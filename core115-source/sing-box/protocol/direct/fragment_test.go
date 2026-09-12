package direct

import (
	"bytes"
	"context"
	"github.com/sagernet/sing-box/option"
	"net"
	"testing"
	"time"
)

type fragmentRecorder struct {
	net.Conn
	writes [][]byte
}

func (c *fragmentRecorder) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}
func TestLegacyFragmentRecords(t *testing.T) {
	f, err := parseFragment(&option.Fragment{Length: "2", Interval: "0"})
	if err != nil {
		t.Fatal(err)
	}
	r := new(fragmentRecorder)
	c := newFragmentConn(context.Background(), r, f)
	p := []byte{22, 3, 3, 0, 5, 1, 2, 3, 4, 5, 23, 3, 3, 0, 1, 9}
	n, err := c.Write(p)
	if err != nil || n != len(p) {
		t.Fatalf("%d %v", n, err)
	}
	want := [][]byte{{22, 3, 3, 0, 2, 1, 2}, {22, 3, 3, 0, 2, 3, 4}, {22, 3, 3, 0, 1, 5}, {23, 3, 3, 0, 1, 9}}
	if len(r.writes) != len(want) {
		t.Fatalf("writes=%v", r.writes)
	}
	for i := range want {
		if !bytes.Equal(r.writes[i], want[i]) {
			t.Fatalf("record=%v", r.writes[i])
		}
	}
	c.Write(p)
	if !bytes.Equal(r.writes[4], p) {
		t.Fatal("fragmented twice")
	}
}
func TestLegacyFragmentValidation(t *testing.T) {
	for _, s := range []string{"0", "-1", "1-2-3", "65536", "x"} {
		if _, err := parseFragment(&option.Fragment{Length: s, Interval: "0"}); err == nil {
			t.Fatal(s)
		}
	}
}
func TestLegacyFragmentIncompleteAndCancellation(t *testing.T) {
	f, _ := parseFragment(&option.Fragment{Length: "2", Interval: "1000"})
	r := new(fragmentRecorder)
	c := newFragmentConn(context.Background(), r, f)
	p := []byte{22, 3, 3, 0, 5, 1}
	if n, e := c.Write(p); n != len(p) || e != nil || !bytes.Equal(r.writes[0], p) {
		t.Fatal(n, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = newFragmentConn(ctx, r, f)
	start := time.Now()
	if _, err := c.Write([]byte{22, 3, 3, 0, 4, 1, 2, 3, 4}); err != context.Canceled {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation ignored")
	}
}
