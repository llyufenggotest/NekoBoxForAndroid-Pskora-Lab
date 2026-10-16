package vless

import (
	"encoding/hex"
	M "github.com/sagernet/sing/common/metadata"
	"testing"
)

// This file runs unchanged in exact baseline and candidate archives.
func TestLegacyLiteralIsolation(t *testing.T) {
	for _, tc := range []struct{ name, suffix, wire string }{
		{"standard", "", "0000112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d50"},
		{"pure", "#pure", "a100112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d50"},
		{"juzi", "#juzi", "0000112233445566778899aabbccddeeff8c2cdb151f9c8f59000101bb020b6578616d706c652e636f6d50"},
		{"x365", "#x365", "58333635010100112233445566778899aabbccddeeff01bb020b6578616d706c652e636f6d50"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient("00112233-4455-6677-8899-aabbccddeeff"+tc.suffix, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			raw := &pureRecordingConn{}
			conn, err := c.DialEarlyConn(raw, M.ParseSocksaddr("example.com:443"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Write([]byte("P")); err != nil {
				t.Fatal(err)
			}
			var all []byte
			for _, w := range raw.writes {
				all = append(all, w...)
			}
			if hex.EncodeToString(all) != tc.wire {
				t.Fatalf("literal mismatch: got %x want %s", all, tc.wire)
			}
		})
	}
}
