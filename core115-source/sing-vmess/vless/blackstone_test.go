package vless

import (
	"bytes"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

const bsTestID = "00112233-4455-6677-8899-aabbccddeeff"
const bsTCP = "0000112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d"
const bsUDP = "0000112233445566778899aabbccddeeff000201bb020b6578616d706c652e636f6d"
const bsMD5 = "6e8311168ee16d6aa1aa48c64145003c"

type bsTestConn struct {
	net.Conn
	wire  bytes.Buffer
	r     io.Reader
	short bool
}

func (c *bsTestConn) Write(p []byte) (int, error) {
	if c.short {
		return len(p) - 1, nil
	}
	return c.wire.Write(p)
}
func (c *bsTestConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *bsTestConn) Close() error               { return nil }
func bsClient(t *testing.T, e bool) *Client {
	t.Helper()
	c, err := NewClientWithBlackstoneOptions(bsTestID+"#blackstone", "", nil, e, "03aabbcc")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func bsAssert(t *testing.T, got []byte, want string) {
	t.Helper()
	if hex.EncodeToString(got) != want {
		t.Fatalf("wire got %x want %s", got, want)
	}
}
func bsRaw(response ...byte) *bsTestConn { return &bsTestConn{r: bytes.NewReader(response)} }
func bsDst() M.Socksaddr                 { return M.ParseSocksaddr("example.com:443") }

func TestBlackstoneLegacyIgnoresEH(t *testing.T) {
	for _, suffix := range []string{"", "#juzi", "#x365", "#pure"} {
		a, e := NewClient(bsTestID+suffix, "", nil)
		if e != nil {
			t.Fatal(e)
		}
		b, e := NewClientWithBlackstoneOptions(bsTestID+suffix, "", nil, true, "not-hex-is-ignored")
		if e != nil {
			t.Fatal(e)
		}
		ar, br := bsRaw(), bsRaw()
		ac, e := a.DialEarlyConn(ar, bsDst())
		if e != nil {
			t.Fatal(e)
		}
		bc, e := b.DialEarlyConn(br, bsDst())
		if e != nil {
			t.Fatal(e)
		}
		_, _ = ac.Write([]byte("P"))
		_, _ = bc.Write([]byte("P"))
		if !bytes.Equal(ar.wire.Bytes(), br.wire.Bytes()) {
			t.Fatal("E/H selected private wire without exact selector")
		}
	}
	// Initial H has no locally invented structure, length prefix, or small cap.
	for _, h := range []string{"00", "ffff", string(bytes.Repeat([]byte{'a'}, 514))} {
		if _, e := NewClientWithBlackstoneOptions(bsTestID+"#blackstone", "", nil, false, h); e != nil {
			t.Fatal(e)
		}
	}
}

func TestBlackstoneDestinationWireMatrix(t *testing.T) {
	for _, e := range []bool{false, true} {
		for _, tc := range []struct{ dst, header string }{
			{"1.2.3.4:80", "0000112233445566778899aabbccddeeff000100500101020304"},
			{"[2001:db8::1]:443", "0000112233445566778899aabbccddeeff000101bb0320010db8000000000000000000000001"},
		} {
			c := bsClient(t, e)
			r := bsRaw()
			conn, err := c.DialEarlyConn(r, M.ParseSocksaddr(tc.dst))
			if err != nil {
				t.Fatal(err)
			}
			_, err = conn.Write([]byte("P"))
			if err != nil {
				t.Fatal(err)
			}
			want := tc.header
			if e {
				want += bsMD5
			}
			bsAssert(t, r.wire.Bytes(), want+"03aabbcc50")
		}
	}
}

func TestBlackstonePacketWire(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "packet"}[buffered], func(t *testing.T) {
			c := bsClient(t, true)
			raw := bsRaw(0, 0, 2, 0xdd, 0xee, 0, 1, 'R')
			pc, err := c.DialEarlyPacketConn(raw, bsDst())
			if err != nil {
				t.Fatal(err)
			}
			if buffered {
				b := buf.New()
				b.Resize(2, 0)
				b.WriteByte('P')
				err = pc.WritePacket(b, bsDst())
			} else {
				_, err = pc.Write([]byte("P"))
			}
			if err != nil {
				t.Fatal(err)
			}
			bsAssert(t, raw.wire.Bytes(), bsUDP+bsMD5+"03aabbcc000150")
			var p [1]byte
			if _, err = pc.Read(p[:]); err != nil || p[0] != 'R' {
				t.Fatalf("response %x %v", p, err)
			}
			bsAssert(t, c.blackstone.snapshot(), "02ddee")
		})
	}
}
func TestBlackstoneCurrentHAtFirstWriteAndGenerationIsolation(t *testing.T) {
	old := bsClient(t, false)
	current := bsClient(t, false)
	lazyRaw := bsRaw()
	lazy, _ := old.DialEarlyConn(lazyRaw, bsDst())
	// Stream response updates only its owning generation, including a late old response.
	updater, _ := old.DialEarlyConn(bsRaw(0, 2, 9, 8, 2, 0x11, 0x22, 'R'), bsDst())
	var p [1]byte
	if _, err := updater.Read(p[:]); err != nil || p[0] != 'R' {
		t.Fatal(err)
	}
	if _, err := lazy.Write([]byte("P")); err != nil {
		t.Fatal(err)
	}
	bsAssert(t, lazyRaw.wire.Bytes(), bsTCP+"02112250")
	bsAssert(t, current.blackstone.snapshot(), "03aabbcc")
	// Atomic whole-cache snapshots across independent connections.
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				u, _ := old.DialEarlyConn(bsRaw(0, 0, 3, 1, 2, 3, 'R'), bsDst())
				var q [1]byte
				_, e := u.Read(q[:])
				if e != nil {
					t.Error(e)
				}
			} else {
				r := bsRaw()
				c, _ := old.DialEarlyConn(r, bsDst())
				_, e := c.Write(nil)
				if e != nil {
					t.Error(e)
				}
				h := hex.EncodeToString(r.wire.Bytes())
				if h != bsTCP+"021122" && h != bsTCP+"03010203" {
					t.Errorf("torn snapshot %s", h)
				}
			}
		}(i)
	}
	wg.Wait()
	bsAssert(t, current.blackstone.snapshot(), "03aabbcc")
}
func TestBlackstoneBufferWireAndResponse(t *testing.T) {
	c := bsClient(t, true)
	raw := bsRaw(0, 1, 9, 2, 0xcc, 0xdd, 'R')
	conn, _ := c.DialEarlyConn(raw, bsDst())
	v := conn.(*Conn)
	b := buf.New()
	b.WriteByte('P')
	if err := v.WriteBuffer(b); err != nil {
		t.Fatal(err)
	}
	bsAssert(t, raw.wire.Bytes(), bsTCP+bsMD5+"03aabbcc50")
	b = buf.New()
	defer b.Release()
	if err := v.ReadBuffer(b); err != nil {
		t.Fatal(err)
	}
	if string(b.Bytes()) != "R" {
		t.Fatal("response auth leaked")
	}
	bsAssert(t, c.blackstone.snapshot(), "02ccdd")
}
func TestBlackstoneResponseFailureDoesNotPublish(t *testing.T) {
	for _, response := range [][]byte{{0}, {1, 0, 1, 2}, {0, 2, 9}, {0, 0, 3, 1, 2}} {
		c := bsClient(t, false)
		conn, _ := c.DialEarlyConn(bsRaw(response...), bsDst())
		var p [1]byte
		if _, err := conn.Read(p[:]); err == nil {
			t.Fatal("invalid response accepted")
		}
		bsAssert(t, c.blackstone.snapshot(), "03aabbcc")
	}
	c := bsClient(t, false)
	conn, _ := c.DialEarlyConn(bsRaw(0, 0, 0, 'R'), bsDst())
	var p [1]byte
	if _, err := conn.Read(p[:]); err != nil || p[0] != 'R' {
		t.Fatal(err)
	}
	bsAssert(t, c.blackstone.snapshot(), "03aabbcc")
}
func TestBlackstoneShortWrite(t *testing.T) {
	c := bsClient(t, true)
	r := bsRaw()
	r.short = true
	conn, _ := c.DialEarlyConn(r, bsDst())
	if _, err := conn.Write([]byte("P")); err != io.ErrShortWrite {
		t.Fatalf("short write error: %v", err)
	}
}
func TestBlackstoneSelectorAndConstructor(t *testing.T) {
	for _, id := range []string{bsTestID, "00112233445566778899aabbccddeeff"} {
		if _, err := NewClientWithBlackstoneOptions(id+"#blackstone", "", nil, false, "01"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{" " + bsTestID + "#blackstone", bsTestID + "#blackstone\u00a0", bsTestID + "#BLACKSTONE", bsTestID + "#blackstone#blackstone", bsTestID + "#pure#blackstone", bsTestID + "#blackstone#juzi", bsTestID + "#x365#blackstone", "00112233-4455-6677-8899-aabbcc-ddeef#blackstone"} {
		if _, err := NewClientWithBlackstoneOptions(id, "", nil, true, "01"); err == nil {
			t.Fatal("malformed private selector accepted")
		}
	}
	if _, err := NewClient(bsTestID+"#blackstone", "", nil); err == nil {
		t.Fatal("legacy constructor silently selected Blackstone")
	}
	for _, h := range []string{"", "x", "xyz", "00 "} {
		if _, err := NewClientWithBlackstoneOptions(bsTestID+"#blackstone", "", nil, true, h); err == nil {
			t.Fatal("invalid H accepted")
		}
	}
}
