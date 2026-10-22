//go:build with_gvisor

package tun

import (
	"bytes"
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-tun/gtcpip/header"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type mixedPacketHandler struct {
	Handler
	done    chan error
	payload []byte
	once    sync.Once
}

func (h *mixedPacketHandler) JudgeFlow(uint8, netip.AddrPort, netip.AddrPort, []byte) FlowVerdict {
	return FlowVerdict{Action: ActionAccept}
}
func (h *mixedPacketHandler) NewPacketConnectionEx(_ context.Context, c N.PacketConn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	b := buf.New()
	defer b.Release()
	d, e := c.ReadPacket(b)
	if e == nil && !bytes.Equal(b.Bytes(), h.payload) {
		e = context.Canceled
	}
	if e == nil {
		e = c.WritePacket(buf.As([]byte("mixed-reply")), d)
	}
	h.done <- e
}
func TestMixedSuccessfulUDPDataPath(t *testing.T) {
	f := newMixedFixture(t, "")
	h := &mixedPacketHandler{done: make(chan error, 1), payload: []byte("mixed-payload")}
	f.m.handler = h
	if e := f.start(); e != nil {
		t.Fatal(e)
	}
	source := netip.MustParseAddrPort("198.18.0.2:12345")
	destination := netip.MustParseAddrPort("203.0.113.1:443")
	packet := make([]byte, header.IPv4MinimumSize+header.UDPMinimumSize+len(h.payload))
	goEncodeUDPPacket(packet, 4, source, destination, h.payload, 1)
	f.tun.packets <- packet
	select {
	case e := <-h.done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("real Mixed UDP forwarder did not deliver payload")
	}
	select {
	case response := <-f.tun.writes:
		ip := header.IPv4(response)
		if ip.SourceAddr() != destination.Addr() || ip.DestinationAddr() != source.Addr() {
			t.Fatal("reply addresses changed")
		}
		udp := header.UDP(ip.Payload())
		if udp.SourcePort() != destination.Port() || udp.DestinationPort() != source.Port() || !bytes.Equal(udp.Payload(), []byte("mixed-reply")) {
			t.Fatal("reply data path changed")
		}
	case <-time.After(time.Second):
		t.Fatal("real gVisor UDP reply not written to memory TUN")
	}
	for i := 0; i < 3; i++ {
		if e := f.m.Close(); e != nil {
			t.Fatal(e)
		}
	}
	f.assertStackClosed()
	f.assertSystemClosed()
	f.tun.Close()
	mixedAwait(t, f.tun.readDone, "external TUN owner retirement")
	f.awaitWorkers("(*Mixed).packetLoop", 0)
}
