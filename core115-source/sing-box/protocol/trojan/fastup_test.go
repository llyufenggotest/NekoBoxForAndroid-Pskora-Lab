package trojan

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/sagernet/sing-box/option"
	transport "github.com/sagernet/sing-box/transport/trojan"
)

func TestDeriveOutboundPasswordStandardUnchanged(t *testing.T) {
	const password = "ordinary-trojan-password"
	derived, fastup := deriveOutboundPassword(password, "ignored-for-standard")
	if fastup {
		t.Fatal("ordinary password was classified as Fastup")
	}
	if derived != password {
		t.Fatalf("standard password changed: %q", derived)
	}
	if got, want := transport.Key(derived), transport.Key(password); got != want {
		t.Fatal("standard Trojan key changed")
	}
}

func TestDeriveOutboundPasswordFastupFixture(t *testing.T) {
	const password = "00000000-0000-0000-0000-000000000000#fastup"
	derived, fastup := deriveOutboundPassword(password, "")
	if !fastup {
		t.Fatal("Fastup suffix was not detected")
	}
	const expectedMaterial = "97e4190babc93555ce26e1a1e0d1e8ff"
	if derived != expectedMaterial {
		t.Fatalf("derived material mismatch: got %q want %q", derived, expectedMaterial)
	}
	key := sha256.Sum224([]byte(expectedMaterial))
	var expectedKey [transport.KeyLength]byte
	hex.Encode(expectedKey[:], key[:])
	if got := transport.Key(derived); got != expectedKey {
		t.Fatal("Fastup Trojan key mismatch")
	}
}

func TestPrepareOutboundOptionsFastupForcesH2Mux(t *testing.T) {
	options := option.TrojanOutboundOptions{Password: "synthetic#fastup"}
	derived, prepared := prepareOutboundOptions(options)
	if !derived {
		t.Fatal("Fastup suffix was not detected")
	}
	if prepared.Multiplex == nil || !prepared.Multiplex.Enabled || prepared.Multiplex.Protocol != "h2mux" {
		t.Fatalf("Fastup h2mux not forced: %#v", prepared.Multiplex)
	}
}

func TestPrepareOutboundOptionsFastupUsesExplicitMpw(t *testing.T) {
	options := option.TrojanOutboundOptions{Password: "synthetic#fastup", Mpw: "rotated-mpw"}
	derived, prepared := prepareOutboundOptions(options)
	if !derived {
		t.Fatal("Fastup suffix was not detected")
	}
	digest := md5.Sum([]byte("synthetic" + options.Mpw))
	if prepared.Password != hex.EncodeToString(digest[:]) {
		t.Fatal("explicit mpw was not used")
	}
}

func TestPrepareOutboundOptionsStandardPreservesMux(t *testing.T) {
	options := option.TrojanOutboundOptions{Password: "ordinary", Mpw: "must-be-ignored"}
	options.Multiplex = &option.OutboundMultiplexOptions{Enabled: false, Protocol: "smux"}
	derived, prepared := prepareOutboundOptions(options)
	if derived {
		t.Fatal("standard Trojan was classified as Fastup")
	}
	if prepared.Multiplex != options.Multiplex {
		t.Fatal("standard Trojan multiplex options changed")
	}
	if prepared.Password != options.Password {
		t.Fatal("standard Trojan password changed when mpw was present")
	}
}

func TestDeriveOutboundPasswordOnlyMatchesSuffix(t *testing.T) {
	const password = "ordinary#fastup-not-a-suffix"
	derived, fastup := deriveOutboundPassword(password, "ignored")
	if fastup || derived != password {
		t.Fatal("non-suffix marker changed standard Trojan")
	}
}
