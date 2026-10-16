package xhttp

import (
	"bytes"
	"crypto/md5"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
)

// The shared serializer accepts an empty domain as an invalid Socksaddr and
// skips its port. A private packet parser must reject it, not publish auth or
// deliver the remaining port bytes as payload.
func TestPacketRejectsEmptySourceWithoutCachePublication(t *testing.T) {
	client, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	key := md5.Sum([]byte("synthetic"))
	frame := wireServer(key[:], []byte{9}, []byte{3, 0, 0, 53, 'x'})
	raw := &memoryPacket{read: frame, actual: len(frame)}
	pc := client.PacketConn(raw, M.ParseSocksaddr("127.0.0.1:1"))
	if n, _, err := pc.ReadFrom(make([]byte, 10)); err == nil || n != 0 {
		t.Fatalf("empty source accepted: n=%d err=%v", n, err)
	}
	if !bytes.Equal(client.snapshot(), []byte{1, 2}) {
		t.Fatal("invalid source published auth")
	}
}

func TestUOTRejectsEmptySourceAndLatchesReadFailure(t *testing.T) {
	// The next two bytes would be misread as payload length without validation.
	raw := &wireMemoryConn{input: bytes.NewReader([]byte{2, 0, 0, 0})}
	pc, _ := NewWireUOT(raw, 2, M.ParseSocksaddr("192.0.2.1:53"))
	_, _, first := pc.ReadFrom(make([]byte, 10))
	if first == nil {
		t.Fatal("empty UOT source accepted")
	}
	if _, _, next := pc.ReadFrom(make([]byte, 10)); next != first {
		t.Fatal("UOT parser resumed after invalid source")
	}
}
