package vless

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-vmess"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

func testRequest(t *testing.T, privateSuffix bool) Request {
	t.Helper()
	id, err := uuid.FromString("6ac24745-6118-1ce8-57da-1aab6a9b7b56")
	if err != nil {
		t.Fatal(err)
	}
	return Request{
		UUID:        [16]byte(id),
		Command:     vmess.CommandTCP,
		Destination: M.ParseSocksaddr("example.com:443"),
		IsJuzi:      privateSuffix,
	}
}

func TestSplitPrivateSuffixIsCaseInsensitiveAndIsolated(t *testing.T) {
	base := "6ac24745-6118-1ce8-57da-1aab6a9b7b56"
	cases := []struct {
		input      string
		clean      string
		x365, juzi bool
	}{
		{base + "#juzi", base, false, true},
		{base + "#JuZi", base, false, true},
		{base + "#x365", base, true, false},
		{base + "#X365", base + "#X365", false, false},
		{base, base, false, false},
		{base + "#juzi-extra", base + "#juzi-extra", false, false},
	}
	for _, tc := range cases {
		clean, x365, juzi := splitPrivateSuffix(tc.input)
		if clean != tc.clean || x365 != tc.x365 || juzi != tc.juzi {
			t.Fatalf("%q => (%q,%v,%v), want (%q,%v,%v)", tc.input, clean, x365, juzi, tc.clean, tc.x365, tc.juzi)
		}
	}
}

func TestJuziSDKTagKnownFixture(t *testing.T) {
	id, err := uuid.FromString("6ac24745-6118-1ce8-57da-1aab6a9b7b56")
	if err != nil {
		t.Fatal(err)
	}
	tag := buildJuziSDKTag(Version, [16]byte(id))
	// Independent Python/OpenSSL parity fixture: HMAC-SHA256("hello_pidun", 00 || UUID)[:8].
	if got, want := hex.EncodeToString(tag[:]), "cadf5ab2d76ecb6c"; got != want {
		t.Fatalf("SDK tag mismatch: got %s want %s", got, want)
	}
}

func TestJuziRequestOnlyAddsEightBytesAfterUUID(t *testing.T) {
	standard := testRequest(t, false)
	juzi := testRequest(t, true)
	standardBuffer := buf.New()
	defer standardBuffer.Release()
	juziBuffer := buf.New()
	defer juziBuffer.Release()
	if err := EncodeRequest(standard, standardBuffer); err != nil {
		t.Fatal(err)
	}
	if err := EncodeRequest(juzi, juziBuffer); err != nil {
		t.Fatal(err)
	}
	if RequestLen(juzi) != RequestLen(standard)+juziSDKTagLength {
		t.Fatalf("length delta = %d", RequestLen(juzi)-RequestLen(standard))
	}
	standardBytes, juziBytes := standardBuffer.Bytes(), juziBuffer.Bytes()
	if !bytes.Equal(standardBytes[:17], juziBytes[:17]) || !bytes.Equal(standardBytes[17:], juziBytes[25:]) {
		t.Fatalf("Juzi mode changed standard bytes outside inserted tag\nstandard=%x\njuzi=%x", standardBytes, juziBytes)
	}
}

func TestX365AndStandardRemainUnchanged(t *testing.T) {
	standard := testRequest(t, false)
	x365 := standard
	x365.IsX365 = true
	if RequestLen(x365) == RequestLen(standard) {
		t.Fatal("X365 framing unexpectedly collapsed to standard framing")
	}
	buffer := buf.New()
	defer buffer.Release()
	if err := EncodeRequest(x365, buffer); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buffer.Bytes(), []byte{'X', '3', '6', '5', 0x01}) {
		t.Fatalf("X365 prefix changed: %x", buffer.Bytes())
	}
}
