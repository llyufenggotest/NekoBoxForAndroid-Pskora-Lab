package snellv4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type exporterPipeDialer struct {
	client net.Conn
}

func (d *exporterPipeDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return d.client, nil
}

func (d *exporterPipeDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unsupported")
}

var _ N.Dialer = (*exporterPipeDialer)(nil)

func TestClientUsesPerSessionExporter(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	exporter := bytes.Repeat([]byte{0x42}, IdentityExporterLength)
	calls := 0
	client, err := NewClient(ClientOptions{
		PSK: []byte("password"), Dialer: &exporterPipeDialer{client: clientSide},
		Server: M.ParseSocksaddr("127.0.0.1:443"), RequireExporter: true,
		ExporterFromConn: func(conn net.Conn) ([]byte, error) {
			calls++
			if conn != clientSide {
				t.Fatal("exporter callback received a different session")
			}
			return exporter, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := client.DialContext(context.Background(), M.ParseSocksaddr("example.com:80"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	done := make(chan error, 1)
	go func() { _, err := conn.Write([]byte("x")); done <- err }()
	wire := make([]byte, snellSaltLen+len(identityWireMagicV2)+IdentityHeaderLength+IdentityAuthTagLength)
	if _, err = io.ReadFull(serverSide, wire); err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, serverSide)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not complete")
	}
	if calls != 1 {
		t.Fatalf("exporter callback calls = %d, want 1", calls)
	}
	if got := string(wire[snellSaltLen : snellSaltLen+len(identityWireMagicV2)]); got != identityWireMagicV2 {
		t.Fatalf("identity magic = %q", got)
	}
	identityEnd := snellSaltLen + len(identityWireMagicV2) + IdentityHeaderLength
	want, err := IdentityV2AuthTag([]byte("password"), exporter, wire[:snellSaltLen])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire[identityEnd:identityEnd+IdentityAuthTagLength], want) {
		t.Fatal("identity tag was not derived from the session exporter")
	}
}

func TestClientFailsClosedWithoutValidExporter(t *testing.T) {
	_, err := NewClient(ClientOptions{PSK: []byte("password"), RequireExporter: true})
	if err == nil {
		t.Fatal("required exporter callback was accepted as nil")
	}
	client, err := NewClient(ClientOptions{
		PSK: []byte("password"), RequireExporter: true,
		ExporterFromConn: func(net.Conn) ([]byte, error) { return []byte{1}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if _, err = client.DialConn(left, M.ParseSocksaddr("example.com:80")); err == nil {
		t.Fatal("invalid exporter length was silently accepted")
	}
}
