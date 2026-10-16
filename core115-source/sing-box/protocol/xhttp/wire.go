package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

type WireConfig struct{ Password, Cipher, TCPFake, UDPFake string }

// WireClient belongs to exactly one immutable selected configuration generation.
// Never reuse this cache across refreshes, even if the password is unchanged.
type WireClient struct {
	block            cipher.Block
	token            [16]byte
	tcpFake, udpFake []byte
	mu               sync.Mutex
	auth             []byte
}

func NewWireClient(config WireConfig) (*WireClient, error) {
	parts := strings.Split(config.Password, ":")
	if len(parts) < 2 {
		return nil, errors.New("blackstone: password requires auth hex")
	}
	if len(parts[1]) > 512 {
		return nil, errors.New("blackstone: auth blob too large")
	}
	auth, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("blackstone: malformed auth hex")
	}
	if len(config.TCPFake) > 256 || len(config.UDPFake) > 256 {
		return nil, errors.New("blackstone: invalid fake prefix")
	}
	tcp, err := hex.DecodeString(config.TCPFake)
	if err != nil {
		return nil, errors.New("blackstone: invalid TCP fake prefix")
	}
	udp, err := hex.DecodeString(config.UDPFake)
	if err != nil {
		return nil, errors.New("blackstone: invalid UDP fake prefix")
	}
	size := 16
	switch strings.ToUpper(config.Cipher) {
	case "", "AES-128-CTR":
	case "AES-192-CTR":
		size = 24
	case "AES-256-CTR":
		size = 32
	case "DUMMY":
		return nil, errors.New("blackstone: DUMMY destination serialization unverified; unsupported")
	default:
		return nil, errors.New("blackstone: unsupported cipher")
	}
	var key, last []byte
	for len(key) < size {
		sum := md5.Sum(append(append([]byte(nil), last...), []byte(parts[0])...))
		last = sum[:]
		key = append(key, last...)
	}
	block, err := aes.NewCipher(key[:size])
	if err != nil {
		return nil, err
	}
	return &WireClient{block: block, token: md5.Sum([]byte(parts[0] + "do not hack this protocol please")), tcpFake: tcp, udpFake: udp, auth: auth}, nil
}
func (c *WireClient) snapshot() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.auth...)
}
func (c *WireClient) publish(auth []byte) {
	c.mu.Lock()
	c.auth = append([]byte(nil), auth...)
	c.mu.Unlock()
}
func (c *WireClient) uplink(auth, prefix []byte) ([]byte, cipher.Stream, error) {
	header := make([]byte, 161+len(auth))
	copy(header, prefix)
	if _, err := rand.Read(header[len(prefix):128]); err != nil {
		return nil, nil, err
	}
	copy(header[128:144], c.token[:])
	copy(header[144:], auth)
	off := 144 + len(auth)
	header[off] = 16
	iv := header[off+1:]
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, err
	}
	return header, cipher.NewCTR(c.block, iv), nil
}
func (c *WireClient) downlink(r io.Reader, verify bool) (cipher.Stream, []byte, error) {
	fixed := make([]byte, 145)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, nil, err
	}
	if verify && subtle.ConstantTimeCompare(fixed[128:144], c.token[:]) != 1 {
		return nil, nil, errors.New("blackstone: token mismatch")
	}
	auth := make([]byte, 1+int(fixed[144]))
	auth[0] = fixed[144]
	if _, err := io.ReadFull(r, auth[1:]); err != nil {
		return nil, nil, err
	}
	var n [1]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, nil, err
	}
	if n[0] != 16 {
		return nil, nil, errors.New("blackstone: invalid IV length")
	}
	var iv [16]byte
	if _, err := io.ReadFull(r, iv[:]); err != nil {
		return nil, nil, err
	}
	return cipher.NewCTR(c.block, iv[:]), auth, nil
}

// StreamConn serializes the already selected destination using sing's metadata.
// Callers must preserve routing/host overrides before passing destination here.
func (c *WireClient) StreamConn(conn net.Conn, destination M.Socksaddr) (*XHttpConn, error) {
	address, err := wireAddress(destination, false)
	if err != nil {
		return nil, err
	}
	return &XHttpConn{Conn: conn, client: c, auth: c.snapshot(), destination: address}, nil
}

// OpenStream emits only the request header/address; it never waits for a server
// reply, so application TLS payload can trigger the response. The caller owns
// dialing and the returned socket after this bounded initialization completes.
func (c *WireClient) OpenStream(ctx context.Context, raw net.Conn, destination M.Socksaddr) (*XHttpConn, error) {
	fail := func(err error) (*XHttpConn, error) { _ = raw.Close(); return nil, err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	stream, err := c.StreamConn(raw, destination)
	if err != nil {
		return fail(err)
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = raw.SetWriteDeadline(deadline); err == nil {
		_, err = stream.Write(nil)
	}
	stopped := stop()
	if err != nil || !stopped || ctx.Err() != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if err == nil {
			err = context.Canceled
		}
		return fail(err)
	}
	if err = raw.SetWriteDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	return stream, nil
}
func wireAddress(addr net.Addr, uot bool) ([]byte, error) {
	if addr == nil {
		return nil, errors.New("blackstone: missing address")
	}
	destination := M.SocksaddrFromNet(addr)
	if destination.Addr.IsValid() {
		destination.Addr = destination.Addr.Unmap()
	}
	if destination.IsFqdn() {
		if len(destination.Fqdn) > 255 {
			return nil, errors.New("blackstone: invalid domain length")
		}
	} else if !destination.Addr.IsValid() {
		return nil, errors.New("blackstone: invalid address")
	}
	var b bytes.Buffer
	if err := M.SocksaddrSerializer.WriteAddrPort(&b, destination); err != nil {
		return nil, err
	}
	result := b.Bytes()
	if uot {
		switch result[0] {
		case 1:
			result[0] = 0
		case 4:
			result[0] = 1
		case 3:
			result[0] = 2
		}
	}
	return result, nil
}
