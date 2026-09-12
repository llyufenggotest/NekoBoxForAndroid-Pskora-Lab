package oppa

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
)

const fixturePassword = "0123456789abcdef0123456789abcdef"

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestTCPIPv4Fixture(t *testing.T) {
	actual, err := buildStreamHeader(fixturePassword, commandTCP, M.Socksaddr{Addr: netip.MustParseAddr("8.8.8.8"), Port: 853})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte(fixturePassword), mustHex(t, "0101080808080355")...)
	if !bytes.Equal(expected, actual) {
		t.Fatalf("fixture mismatch\nwant %x\n got %x", expected, actual)
	}
}

func TestTCPDomainFixture(t *testing.T) {
	actual, err := buildStreamHeader(fixturePassword, commandTCP, M.Socksaddr{Fqdn: "mtalk.google.com", Port: 5228})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte(fixturePassword), mustHex(t, "0103106d74616c6b2e676f6f676c652e636f6d146c")...)
	if !bytes.Equal(expected, actual) {
		t.Fatalf("fixture mismatch\nwant %x\n got %x", expected, actual)
	}
}

func TestUDPSessionFixture(t *testing.T) {
	actual, err := buildSessionHeader(fixturePassword, commandUDP)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte(fixturePassword), commandUDP)
	if !bytes.Equal(expected, actual) {
		t.Fatalf("fixture mismatch\nwant %x\n got %x", expected, actual)
	}
}

func TestUDPFrameLayout(t *testing.T) {
	source := M.Socksaddr{Addr: netip.MustParseAddr("10.111.222.1"), Port: 58092}
	destination := M.Socksaddr{Addr: netip.MustParseAddr("8.8.8.8"), Port: 443}
	body, err := appendAddress(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	body, err = appendAddress(body, destination)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, bytes.Repeat([]byte{0x42}, 113)...)
	frame := append([]byte{0x00, 0x7f}, body...)
	if len(frame) != 129 {
		t.Fatalf("unexpected frame length: %d", len(frame))
	}
	if got := hex.EncodeToString(frame[:16]); got != "007f010a6fde01e2ec010808080801bb" {
		t.Fatalf("unexpected frame prefix: %s", got)
	}
}

func TestPasswordLengthCompatibility(t *testing.T) {
	if header, err := buildSessionHeader("future-token", commandTCP); err != nil || string(header) != "future-token\x01" {
		t.Fatalf("variable-length password compatibility failed: %x %v", header, err)
	}
	if _, err := buildSessionHeader("", commandTCP); err == nil {
		t.Fatal("expected empty password error")
	}
	if _, err := buildSessionHeader(string(bytes.Repeat([]byte{'x'}, 4097)), commandTCP); err == nil {
		t.Fatal("expected oversized password error")
	}
}
