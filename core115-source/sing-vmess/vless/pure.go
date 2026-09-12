package vless

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"strings"
)

// PureIdentity reserves any #pure token, so mixed/malformed modes never fall back to UUIDv5.
func PureIdentity(id string) (key [16]byte, enabled bool, err error) {
	lower := strings.ToLower(id)
	if !strings.Contains(lower, "#pure") {
		return
	}
	enabled = true
	if !strings.HasSuffix(lower, "#pure") {
		err = fmt.Errorf("Pure: invalid or mixed suffix")
		return
	}
	raw := id[:len(id)-5]
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		err = fmt.Errorf("Pure: canonical UUID required")
		return
	}
	var b []byte
	b, err = hex.DecodeString(strings.ReplaceAll(raw, "-", ""))
	if err != nil || len(b) != 16 {
		err = fmt.Errorf("Pure: canonical UUID required")
		return
	}
	copy(key[:], b)
	return
}

func pureHeader(key [16]byte, dst M.Socksaddr) ([]byte, error) {
	if dst.Port == 0 {
		return nil, fmt.Errorf("Pure: invalid destination port")
	}
	b := append([]byte{0xa1}, key[:]...)
	b = append(b, 0, 1)
	b = binary.BigEndian.AppendUint16(b, dst.Port)
	if dst.Addr.Is4() {
		a := dst.Addr.As4()
		return append(append(b, 1), a[:]...), nil
	}
	if dst.Addr.IsValid() {
		return nil, fmt.Errorf("Pure: IPv6 destination unsupported")
	}
	if len(dst.Fqdn) == 0 || len(dst.Fqdn) > 255 {
		return nil, fmt.Errorf("Pure: invalid destination domain")
	}
	return append(append(b, 2, byte(len(dst.Fqdn))), []byte(dst.Fqdn)...), nil
}

// Pure operates on the existing upgraded WS transport. One Write is one message.
func (c *Client) dialPure(conn net.Conn, dst M.Socksaddr) (net.Conn, error) {
	b, err := pureHeader(c.key, dst)
	if err != nil {
		conn.Close()
		return nil, err
	}
	n, err := conn.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &pureConn{Conn: conn}, nil
}

type pureConn struct {
	net.Conn
	ready       bool
	responseErr error
}

func (c *pureConn) NeedAdditionalReadDeadline() bool { return true }

func (c *pureConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	if !c.ready {
		var h [2]byte
		_, err := io.ReadFull(c.Conn, h[:])
		if err == nil && h != [2]byte{0xa1, 0} {
			err = fmt.Errorf("Pure response prefix invalid")
		}
		if err != nil {
			c.responseErr = err
			return 0, err
		}
		c.ready = true
	}
	return c.Conn.Read(p)
}
