package xhttp

import (
	"crypto/cipher"
	"io"
	"net"
	"sync"
)

// XHttpConn is the private Blackstone stream, unrelated to VLESS network:xhttp.
// The embedded socket retains deadline, address and Close ownership.
type XHttpConn struct {
	net.Conn
	encryptStream, decryptStream cipher.Stream
	block                        cipher.Block
	ivLenOffset                  int // retained only for source compatibility with the old constructor
	handshakeDone                bool
	readMu, writeMu              sync.Mutex
	readErr, writeErr            error
	client                       *WireClient
	auth, destination            []byte
}

func (c *XHttpConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.encryptStream == nil {
		header, stream, err := c.client.uplink(c.auth, c.client.tcpFake)
		if err != nil {
			c.writeErr = err
			return 0, err
		}
		encrypted := make([]byte, len(c.destination))
		stream.XORKeyStream(encrypted, c.destination)
		if err = c.writeFrame(append(header, encrypted...)); err != nil {
			return 0, err
		}
		c.encryptStream = stream
	}
	if len(p) == 0 {
		return 0, nil
	}
	encrypted := make([]byte, len(p))
	c.encryptStream.XORKeyStream(encrypted, p)
	if err := c.writeFrame(encrypted); err != nil {
		return 0, err
	}
	return len(p), nil
}
func (c *XHttpConn) writeFrame(p []byte) error {
	n, err := c.Conn.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.writeErr = err
		_ = c.Conn.Close()
	}
	return err
}
func (c *XHttpConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.readErr != nil {
		return 0, c.readErr
	}
	if !c.handshakeDone {
		// The legacy constructor lacks token material; new integrations MUST use
		// WireClient.StreamConn so the downlink token is verified.
		client := c.client
		if client == nil {
			client = &WireClient{block: c.block}
		}
		stream, auth, err := client.downlink(c.Conn, c.client != nil)
		if err != nil {
			c.readErr = err
			return 0, err
		}
		c.decryptStream = stream
		c.handshakeDone = true
		if c.client != nil {
			c.client.publish(auth)
		}
	}
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.decryptStream.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

// NewXHttpConn preserves the old constructor for downstream source compatibility.
// It cannot validate a token it was never given. Prefer WireClient.StreamConn.
func NewXHttpConn(conn net.Conn, enc cipher.Stream, block cipher.Block, ivLen int) *XHttpConn {
	return &XHttpConn{Conn: conn, encryptStream: enc, block: block, ivLenOffset: ivLen}
}

var _ net.Conn = (*XHttpConn)(nil)
