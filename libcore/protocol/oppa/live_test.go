package oppa

import (
	"context"
	stdtls "crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestLiveUDPWithUnspecifiedSource(t *testing.T) {
	if os.Getenv("OPPA_LIVE") != "1" {
		t.Skip("set OPPA_LIVE=1 with OPPA_SERVER and OPPA_PASSWORD")
	}
	server, password := os.Getenv("OPPA_SERVER"), os.Getenv("OPPA_PASSWORD")
	if server == "" || password == "" {
		t.Fatal("missing live Oppa configuration")
	}
	raw, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	conn := stdtls.Client(raw, &stdtls.Config{InsecureSkipVerify: true, MinVersion: stdtls.VersionTLS13, MaxVersion: stdtls.VersionTLS13})
	if err = conn.HandshakeContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	header, err := buildSessionHeader(password, commandUDP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write(header); err != nil {
		t.Fatal(err)
	}
	query := dnsQuery(0x71a5, "example.com")
	body, err := appendAddress(nil, M.Socksaddr{Addr: netip.IPv4Unspecified()})
	if err != nil {
		t.Fatal(err)
	}
	body, err = appendAddress(body, M.Socksaddr{Addr: netip.MustParseAddr("8.8.8.8"), Port: 53})
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, query...)
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(body)))
	frame = append(frame, body...)
	if _, err = conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var size [2]byte
	if _, err = io.ReadFull(conn, size[:]); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err = io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	reader := &countingReader{data: response}
	if _, err = parseAddress(reader); err != nil {
		t.Fatal(err)
	}
	if _, err = parseAddress(reader); err != nil {
		t.Fatal(err)
	}
	payload := response[reader.offset:]
	if len(payload) < 12 || binary.BigEndian.Uint16(payload[:2]) != 0x71a5 || payload[3]&0x80 == 0 {
		t.Fatalf("invalid DNS response: %x", payload)
	}
}

type countingReader struct {
	data   []byte
	offset int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func dnsQuery(id uint16, domain string) []byte {
	query := make([]byte, 12)
	binary.BigEndian.PutUint16(query[0:2], id)
	binary.BigEndian.PutUint16(query[2:4], 0x0100)
	binary.BigEndian.PutUint16(query[4:6], 1)
	for _, label := range splitLabels(domain) {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	return append(query, 0, 0, 1, 0, 1)
}

func splitLabels(domain string) [][]byte {
	var labels [][]byte
	start := 0
	for i := 0; i <= len(domain); i++ {
		if i == len(domain) || domain[i] == '.' {
			labels = append(labels, []byte(domain[start:i]))
			start = i + 1
		}
	}
	return labels
}
