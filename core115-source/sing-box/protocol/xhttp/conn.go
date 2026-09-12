package xhttp

import (
	"crypto/cipher"
	"errors"
	"io"
	"net"
	"sync"
)

type XHttpConn struct {
	net.Conn
	encryptStream cipher.Stream
	decryptStream cipher.Stream
	block         cipher.Block
	ivLenOffset   int
	handshakeDone bool
	readMu        sync.Mutex
}

func (c *XHttpConn) Write(b []byte) (n int, err error) {
	buf := make([]byte, len(b))
	c.encryptStream.XORKeyStream(buf, b)
	return c.Conn.Write(buf)
}

func (c *XHttpConn) Read(b []byte) (n int, err error) {
	c.readMu.Lock()
	if !c.handshakeDone {
		// 🔥 延迟到第一次真实读取时，才去读取服务器的握手响应，完美解决死锁！
		baseHeader := make([]byte, c.ivLenOffset)
		if _, err := io.ReadFull(c.Conn, baseHeader); err != nil {
			c.readMu.Unlock()
			return 0, err
		}

		// 处理服务器的随机 Padding
		markerBuf := make([]byte, 1)
		var shift int
		for {
			if _, err := io.ReadFull(c.Conn, markerBuf); err != nil {
				c.readMu.Unlock()
				return 0, err
			}
			if markerBuf[0] == 0x10 {
				break
			}
			shift++
			if shift > 10 {
				c.readMu.Unlock()
				return 0, errors.New("IV offset padding too large")
			}
		}

		// 读取服务器下发的 16 字节 IV
		serverReadIV := make([]byte, 16)
		if _, err := io.ReadFull(c.Conn, serverReadIV); err != nil {
			c.readMu.Unlock()
			return 0, err
		}

		// 初始化解密流
		c.decryptStream = cipher.NewCTR(c.block, serverReadIV)
		c.handshakeDone = true
	}
	c.readMu.Unlock()

	// 正式读取数据并解密
	n, err = c.Conn.Read(b)
	if n > 0 {
		c.decryptStream.XORKeyStream(b[:n], b[:n])
	}
	return n, err
}

func NewXHttpConn(conn net.Conn, enc cipher.Stream, block cipher.Block, ivLen int) *XHttpConn {
	return &XHttpConn{
		Conn:          conn,
		encryptStream: enc,
		block:         block,
		ivLenOffset:   ivLen,
		handshakeDone: false,
	}
}
