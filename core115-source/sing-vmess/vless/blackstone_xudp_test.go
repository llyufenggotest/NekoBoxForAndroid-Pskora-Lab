package vless

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

// Exact donor NewVless synthetic Command3/XUDP captures are checked separately
// in xudp-default-fix/donor-canonical.json; literals do not reuse sing serializers.
func TestBlackstoneXUDPTwoPacketCacheAndLifecycle(t *testing.T) {
	for _, dstText := range []string{"192.0.2.1:19302", "example.com:443", "[2001:db8::1]:443"} {
		for _, extension := range []bool{false, true} {
			for _, buffered := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/E=%t/buffer=%t", dstText, extension, buffered), func(t *testing.T) {
					c := bsClient(t, extension)
					dst := M.ParseSocksaddr(dstText)
					// response addon + H outside XUDP, then two session-0 Keep frames
					raw := bsRaw(0, 2, 0x92, 0x93, 2, 0xdd, 0xee, 0, 4, 0, 0, 2, 1, 0, 3, 'b', 'i', 'g', 0, 4, 0, 0, 2, 1, 0, 1, 'R')
					pc, e := c.DialEarlyXUDPPacketConn(raw, dst)
					if e != nil {
						t.Fatal(e)
					}
					laterRaw := bsRaw()
					later, e := c.DialEarlyXUDPPacketConn(laterRaw, dst)
					if e != nil {
						t.Fatal(e)
					}
					if raw.wire.Len() != 0 || laterRaw.wire.Len() != 0 {
						t.Fatal("private initializer emitted eager empty header")
					}
					write := func(p []byte) {
						t.Helper()
						if buffered {
							b := buf.New()
							b.Resize(512, 0)
							b.Write(p)
							if e := pc.WritePacket(b, dst); e != nil {
								t.Fatal(e)
							}
						} else {
							if _, e := pc.WriteTo(p, dst); e != nil {
								t.Fatal(e)
							}
						}
					}
					write([]byte("P"))
					write([]byte("Q"))
					var addr string
					switch dstText {
					case "192.0.2.1:19302":
						addr = "4b6601c0000201"
					case "example.com:443":
						addr = "01bb020b6578616d706c652e636f6d"
					default:
						addr = "01bb0320010db8000000000000000000000001"
					}
					addrBytes, _ := hex.DecodeString(addr)
					length := 5 + len(addrBytes)
					hdr := "0000112233445566778899aabbccddeeff0003"
					if extension {
						hdr += bsMD5
					}
					first := fmt.Sprintf("%04x0000010102%s000150", length, addr)
					second := fmt.Sprintf("%04x0000020102%s000151", length, addr)
					bsAssert(t, raw.wire.Bytes(), hdr+"03aabbcc"+first+second)
					for _, want := range []string{"big", "R"} {
						b := make([]byte, 128)
						n, a, e := pc.ReadFrom(b)
						if e != nil || string(b[:n]) != want || M.SocksaddrFromNet(a) != dst {
							t.Fatalf("response/address mismatch n=%d err=%v", n, e)
						}
					}
					if _, e = later.WriteTo([]byte("P"), dst); e != nil {
						t.Fatal(e)
					}
					bsAssert(t, laterRaw.wire.Bytes(), hdr+"02ddee"+first)
				})
			}
		}
	}
	// Partial H must not publish. A zero H marker retains generation state.
	for _, reply := range [][]byte{{0, 0, 2, 0xdd}, {0, 0, 0, 0, 4, 0, 0, 2, 1, 0, 1, 'R'}} {
		c := bsClient(t, true)
		pc, e := c.DialEarlyXUDPPacketConn(&bsTestConn{r: bytes.NewReader(reply)}, bsDst())
		if e != nil {
			t.Fatal(e)
		}
		_, _, _ = pc.ReadFrom(make([]byte, 128))
		bsAssert(t, c.blackstone.snapshot(), "03aabbcc")
	}
	// First-write error is terminal, no request replay; simultaneous Close is once.
	c := bsClient(t, true)
	raw := &bsVisionRaw{response: bytes.NewReader(nil), short: true}
	pc, e := c.DialEarlyXUDPPacketConn(raw, bsDst())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pc.Write([]byte("P")); e != io.ErrShortWrite {
		t.Fatal(e)
	}
	if _, e = pc.Write([]byte("Q")); e != io.ErrShortWrite {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); pc.Close() }()
	}
	wg.Wait()
	if raw.closes.Load() != 1 {
		t.Fatalf("raw close count=%d", raw.closes.Load())
	}
}
