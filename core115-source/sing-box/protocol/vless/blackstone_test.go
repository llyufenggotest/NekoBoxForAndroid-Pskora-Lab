package vless

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

// Independent synthetic oracle: standard header, full MD5(UUID16), opaque H,
// payload. No production serializer/digest/parser is used for expected bytes.
const bsID = "00112233-4455-6677-8899-aabbccddeeff"
const bsStandard = "0000112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d"
const bsDigest = "6e8311168ee16d6aa1aa48c64145003c"

type bsRecordingConn struct {
	net.Conn
	bytes.Buffer
	response io.Reader
}

func (c *bsRecordingConn) Write(p []byte) (int, error) { return c.Buffer.Write(p) }
func (c *bsRecordingConn) Read(p []byte) (int, error)  { return c.response.Read(p) }
func (c *bsRecordingConn) Close() error                { return nil }

func TestBlackstoneProductionConstructorWire(t *testing.T) {
	var options option.VLESSOutboundOptions
	if err := json.Unmarshal([]byte(`{"server":"127.0.0.1","server_port":443,"uuid":"`+bsID+`#blackstone","e":true,"h":"03aabbcc"}`), &options); err != nil {
		t.Fatal(err)
	}
	created, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "synthetic", options)
	if err != nil {
		t.Fatal(err)
	}
	raw := &bsRecordingConn{response: bytes.NewReader([]byte{0, 0, 0, 'R'})}
	conn, err := created.(*Outbound).client.DialEarlyConn(raw, M.ParseSocksaddr("example.com:443"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write([]byte("P")); err != nil {
		t.Fatal(err)
	}
	want := bsStandard + bsDigest + "03aabbcc50"
	if hex.EncodeToString(raw.Bytes()) != want {
		t.Fatalf("production wire mismatch: got %x want %s", raw.Bytes(), want)
	}
	var p [1]byte
	if _, err = io.ReadFull(conn, p[:]); err != nil || p[0] != 'R' {
		t.Fatalf("private response not consumed: %x %v", p, err)
	}
}
