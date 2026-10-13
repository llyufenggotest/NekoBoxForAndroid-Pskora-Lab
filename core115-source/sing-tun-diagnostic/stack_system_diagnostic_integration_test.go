package tun

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	D "github.com/sagernet/sing-tun/diagnostic"
	"github.com/sagernet/sing-tun/gtcpip/header"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type systemDiagnosticCapture struct {
	logger.ContextLogger
	access sync.Mutex
	lines  []string
}

func newSystemDiagnosticCapture() *systemDiagnosticCapture {
	return &systemDiagnosticCapture{ContextLogger: logger.NOP()}
}

func (c *systemDiagnosticCapture) DiagnosticEnabled() bool { return true }
func (c *systemDiagnosticCapture) Debug(args ...any) {
	c.access.Lock()
	c.lines = append(c.lines, fmt.Sprint(args...))
	c.access.Unlock()
}
func (c *systemDiagnosticCapture) text() string {
	c.access.Lock()
	defer c.access.Unlock()
	return strings.Join(c.lines, "\n")
}

func newDiagnosticSystem(t *testing.T, capture *systemDiagnosticCapture) *System {
	t.Helper()
	stack, err := NewSystem(StackOptions{
		Context:    context.Background(),
		UDPTimeout: time.Hour,
		Logger:     capture,
		TunOptions: Options{
			Inet4Address: []netip.Prefix{netip.MustParsePrefix("172.19.0.1/30")},
			Inet6Address: []netip.Prefix{netip.MustParsePrefix("fd00::1/126")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return stack.(*System)
}

func TestSystemStackDiagnosticProductionWiring(t *testing.T) {
	capture := newSystemDiagnosticCapture()
	stack := newDiagnosticSystem(t, capture)
	stack.newTCPNATs()

	v4Source := netip.MustParseAddrPort("192.0.2.11:12001")
	v4Destination := netip.MustParseAddrPort("198.51.100.21:443")
	v6Source := netip.MustParseAddrPort("[2001:db8::11]:12002")
	v6Destination := netip.MustParseAddrPort("[2001:db8::21]:443")
	v4Port := stack.tcpNat4.Lookup(v4Source, v4Destination)
	v6Port := stack.tcpNat6.Lookup(v6Source, v6Destination)
	if v4Port == 0 || v6Port == 0 {
		t.Fatal("NAT allocation failed")
	}

	expiredSource := netip.MustParseAddrPort("192.0.2.12:12003")
	expiredDestination := netip.MustParseAddrPort("198.51.100.22:80")
	expiredPort := stack.tcpNat4.Lookup(expiredSource, expiredDestination)
	stack.tcpNat4.portMap[expiredPort].LastActive = time.Now().Add(-2 * time.Hour)
	stack.tcpNat4.checkTimeout()

	stack.udpNat = NewUDPNat(UDPNatOptions{Timeout: time.Hour})
	udpConn := &UDPNatConn{
		service:    stack.udpNat,
		key:        udpNatSessionKey{sourceAddr: netip.MustParseAddr("192.0.2.30"), sourcePort: 53000},
		localAddr:  M.Socksaddr{Addr: netip.MustParseAddr("192.0.2.30"), Port: 53000},
		packetChan: make(chan *N.PacketBuffer, 1),
		doneChan:   make(chan struct{}),
	}
	udpConn.cleanupEntry = &udpNatCleanupEntry{conn: udpConn, index: -1}
	stack.udpNat.cache.Add(udpConn.key, udpConn)

	stack.ResetNetwork()
	if stack.tcpNat4.LookupBack(v4Port) != nil || stack.tcpNat6.LookupBack(v6Port) != nil {
		t.Fatal("ResetNetwork did not purge TCP NATs")
	}
	if stack.udpNat.cache.Len() != 0 || !udpConn.isClosed() {
		t.Fatal("ResetNetwork did not purge UDP NAT")
	}

	text := capture.text()
	for _, event := range []D.Event{D.NATCreate, D.NATExpire, D.NATPurge} {
		if !strings.Contains(text, fmt.Sprintf("event=%d", event)) {
			t.Fatalf("missing diagnostic event %d: %s", event, text)
		}
	}
	for _, secret := range []string{"192.0.2", "198.51.100", "2001:db8", "fd00::", "172.19"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic address leak %q: %s", secret, text)
		}
	}
}

func TestSystemStackLookupMissDiagnostic(t *testing.T) {
	capture := newSystemDiagnosticCapture()
	stack := newDiagnosticSystem(t, capture)
	stack.newTCPNATs()
	stack.tcpPort = 41000
	stack.tcpPort6 = 41001

	ipv4, tcp4 := systemTCPPacket4(stack.inet4Address, netip.MustParseAddr("198.51.100.41"), stack.tcpPort, 62000)
	if _, err := stack.processIPv4TCP(ipv4, tcp4); err == nil {
		t.Fatal("expected IPv4 NAT lookup miss")
	}
	ipv6, tcp6 := systemTCPPacket6(stack.inet6Address, netip.MustParseAddr("2001:db8::41"), stack.tcpPort6, 62001)
	if _, err := stack.processIPv6TCP(ipv6, tcp6); err == nil {
		t.Fatal("expected IPv6 NAT lookup miss")
	}

	text := capture.text()
	if strings.Count(text, fmt.Sprintf("event=%d", D.NATMiss)) != 2 {
		t.Fatalf("expected IPv4 and IPv6 NATMiss events: %s", text)
	}
	for _, secret := range []string{"198.51.100", "2001:db8", "fd00::", "172.19"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic address leak %q: %s", secret, text)
		}
	}
}

func systemTCPPacket4(source, destination netip.Addr, sourcePort, destinationPort uint16) (header.IPv4, header.TCP) {
	packet := make([]byte, header.IPv4MinimumSize+header.TCPMinimumSize)
	ipHdr := header.IPv4(packet)
	ipHdr.Encode(&header.IPv4Fields{
		TotalLength: uint16(len(packet)),
		TTL:         64,
		Protocol:    uint8(header.TCPProtocolNumber),
		SrcAddr:     source,
		DstAddr:     destination,
	})
	tcpHdr := header.TCP(ipHdr.Payload())
	tcpHdr.Encode(&header.TCPFields{SrcPort: sourcePort, DstPort: destinationPort, DataOffset: header.TCPMinimumSize})
	return ipHdr, tcpHdr
}

func systemTCPPacket6(source, destination netip.Addr, sourcePort, destinationPort uint16) (header.IPv6, header.TCP) {
	packet := make([]byte, header.IPv6MinimumSize+header.TCPMinimumSize)
	ipHdr := header.IPv6(packet)
	ipHdr.Encode(&header.IPv6Fields{
		PayloadLength:     header.TCPMinimumSize,
		TransportProtocol: header.TCPProtocolNumber,
		HopLimit:          64,
		SrcAddr:           source,
		DstAddr:           destination,
	})
	tcpHdr := header.TCP(ipHdr.Payload())
	tcpHdr.Encode(&header.TCPFields{SrcPort: sourcePort, DstPort: destinationPort, DataOffset: header.TCPMinimumSize})
	return ipHdr, tcpHdr
}
