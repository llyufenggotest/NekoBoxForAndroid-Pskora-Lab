package xhttp

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	M "github.com/sagernet/sing/common/metadata"
	"os"
	"path/filepath"
	"testing"
)

// Optional external evidence export contains only these synthetic vectors.
func TestWireExternalOracleFrames(t *testing.T) {
	dir := os.Getenv("BLACKSTONE_WIRE_EVIDENCE")
	if dir == "" {
		dir = t.TempDir()
	}
	type vector struct{ Mode, Key, Frame, Plain string }
	var vectors []vector
	for _, v := range []struct{ method, key string }{{"aes-128-ctr", "5f06b85e1619b2b8b56f7b758a0a310a"}, {"aes-192-ctr", "5f06b85e1619b2b8b56f7b758a0a310a737fb43703acf936"}, {"aes-256-ctr", "5f06b85e1619b2b8b56f7b758a0a310a737fb43703acf936c6280204ae5f6506"}} {
		c, err := NewWireClient(WireConfig{Password: "synthetic:0102", Cipher: v.method, TCPFake: "face", UDPFake: "abcd"})
		if err != nil {
			t.Fatal(err)
		}
		raw := &wireMemoryConn{}
		conn, err := c.StreamConn(raw, M.ParseSocksaddr("route.example:443"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Write([]byte("independent-python-oracle")); err != nil {
			t.Fatal(err)
		}
		address := append([]byte{3, 13}, []byte("route.example")...)
		address = append(address, 1, 187)
		plain := append(address, []byte("independent-python-oracle")...)
		vectors = append(vectors, vector{v.method + "/TCP", v.key, hex.EncodeToString(raw.Bytes()), hex.EncodeToString(plain)})
		packet := &memoryPacket{}
		pc := c.PacketConn(packet, M.ParseSocksaddr("127.0.0.1:1"))
		for range 2 {
			if _, err = pc.WriteTo([]byte("datagram"), M.ParseSocksaddr("192.0.2.1:53")); err != nil {
				t.Fatal(err)
			}
		}
		if bytes.Equal(packet.writes[0][147:163], packet.writes[1][147:163]) {
			t.Fatal("IV reused")
		}
		for _, frame := range packet.writes {
			vectors = append(vectors, vector{v.method + "/UDP", v.key, hex.EncodeToString(frame), hex.EncodeToString([]byte{1, 192, 0, 2, 1, 0, 53, 'd', 'a', 't', 'a', 'g', 'r', 'a', 'm'})})
		}
	}
	data, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "synthetic-frames.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
