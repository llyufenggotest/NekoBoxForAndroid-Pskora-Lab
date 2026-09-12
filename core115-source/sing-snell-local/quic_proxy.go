package snell

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

const (
	QUICProxySessionIdleTimeout = 60 * time.Second
	quicProxyMaxPlaintext       = 1417
)

func IsQUICLooking(first byte) bool {
	return first >= 0xc0 || first >= 0x40 && first <= 0x7f
}

func IsQUICInitial(packet []byte) bool {
	if len(packet) < 5 || packet[0]&0xc0 != 0xc0 {
		return false
	}
	version := binary.BigEndian.Uint32(packet[1:5])
	if version == 0 {
		return false
	}
	packetType := packet[0] & 0x30
	// RFC 9369 assigns type 1 to Initial packets; QUIC v1 and compatible
	// draft versions assign type 0.
	if version == 0x6b3343cf {
		return packetType == 0x10
	}
	return packetType == 0x00
}

type QUICProxyPacketConn struct {
	conn        net.Conn
	destination M.Socksaddr
	psk         []byte
	userKey     []byte
}

func NewQUICProxyPacketConn(conn net.Conn, psk []byte, userKey []byte, destination M.Socksaddr, initialPayload []byte) (*QUICProxyPacketConn, error) {
	if len(initialPayload) == 0 {
		return nil, errors.New("snell quic proxy: initial payload is required")
	}
	packetConn := &QUICProxyPacketConn{
		conn:        conn,
		destination: destination,
		psk:         append([]byte(nil), psk...),
		userKey:     append([]byte(nil), userKey...),
	}
	if _, err := packetConn.sendInitFrame(initialPayload); err != nil {
		return nil, err
	}
	return packetConn, nil
}

func (c *QUICProxyPacketConn) sendInitFrame(payload []byte) (int, error) {
	plain, err := encodeQUICProxyInit(c.userKey, c.destination, payload)
	if err != nil {
		return 0, err
	}
	frame, err := encodeQUICProxyFrame(c.psk, plain)
	if err != nil {
		return 0, err
	}
	if _, err = c.conn.Write(frame); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *QUICProxyPacketConn) WriteTo(payload []byte, _ net.Addr) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	// A later Initial may arrive after the server's source-address context has
	// expired while the local UDP socket is still alive. A fresh init is valid
	// both as a duplicate for a live context and as a replacement for an expired
	// one, so it avoids sending an unrecoverable raw Initial without context.
	if IsQUICInitial(payload) {
		return c.sendInitFrame(payload)
	}
	if IsQUICLooking(payload[0]) {
		if _, err := c.conn.Write(payload); err != nil {
			return 0, err
		}
		return len(payload), nil
	}
	return c.sendInitFrame(payload)
}

func (c *QUICProxyPacketConn) ReadFrom(payload []byte) (int, net.Addr, error) {
	readLen, err := c.conn.Read(payload)
	return readLen, c.destination, err
}

func (c *QUICProxyPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	readLen, err := c.conn.Read(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(readLen)
	return c.destination, nil
}

func (c *QUICProxyPacketConn) WritePacket(buffer *buf.Buffer, _ M.Socksaddr) error {
	defer buffer.Release()
	_, err := c.WriteTo(buffer.Bytes(), nil)
	return err
}

func (c *QUICProxyPacketConn) Close() error                       { return c.conn.Close() }
func (c *QUICProxyPacketConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *QUICProxyPacketConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *QUICProxyPacketConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *QUICProxyPacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

type QUICProxySession struct {
	psk          []byte
	target       M.Socksaddr
	applyContext func(context.Context) context.Context
	lastActive   time.Time
	access       sync.Mutex
}

func NewQUICProxySession(psk []byte, target M.Socksaddr, applyContext func(context.Context) context.Context) *QUICProxySession {
	return &QUICProxySession{
		psk:          append([]byte(nil), psk...),
		target:       target,
		applyContext: applyContext,
		lastActive:   time.Now(),
	}
}

func (s *QUICProxySession) Target() M.Socksaddr { return s.target }

func (s *QUICProxySession) Context(ctx context.Context) context.Context {
	if s.applyContext != nil {
		return s.applyContext(ctx)
	}
	return ctx
}

func (s *QUICProxySession) Touch() {
	s.access.Lock()
	s.lastActive = time.Now()
	s.access.Unlock()
}

func (s *QUICProxySession) IsExpired() bool {
	s.access.Lock()
	defer s.access.Unlock()
	return time.Since(s.lastActive) > QUICProxySessionIdleTimeout
}

func (s *QUICProxySession) DecodeDuplicateInit(data []byte) ([]byte, error) {
	_, _, payload, err := DecodeQUICProxyInit(s.psk, data)
	return payload, err
}

func DecodeQUICProxyInit(psk []byte, data []byte) (M.Socksaddr, []byte, []byte, error) {
	plain, err := decodeQUICProxyFrame(psk, data)
	if err != nil {
		return M.Socksaddr{}, nil, nil, err
	}
	return parseQUICProxyInit(plain)
}

func encodeQUICProxyFrame(psk []byte, plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("snell quic proxy: empty init payload")
	}
	salt := make([]byte, SaltLen)
	for {
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
		if !IsQUICLooking(salt[0]) {
			break
		}
	}
	aead, err := NewAEAD(DeriveKey(psk, salt))
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceLen)
	frame := make([]byte, 0, len(salt)+len(plain)+(len(plain)+quicProxyMaxPlaintext-1)/quicProxyMaxPlaintext*(HeaderCipherLen+AEADTagLen))
	frame = append(frame, salt...)
	for len(plain) > 0 {
		payloadLen := min(len(plain), quicProxyMaxPlaintext)
		header := make([]byte, HeaderPlainLen)
		header[0] = HeaderVersion
		binary.BigEndian.PutUint16(header[5:7], uint16(payloadLen))
		frame = aead.Seal(frame, nonce, header, nil)
		IncreaseNonce(nonce)
		frame = aead.Seal(frame, nonce, plain[:payloadLen], nil)
		IncreaseNonce(nonce)
		plain = plain[payloadLen:]
	}
	return frame, nil
}

func decodeQUICProxyFrame(psk []byte, data []byte) ([]byte, error) {
	if len(data) < SaltLen+HeaderCipherLen {
		return nil, errors.New("snell quic proxy: datagram too short")
	}
	salt := data[:SaltLen]
	aead, err := NewAEAD(DeriveKey(psk, salt))
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceLen)
	data = data[SaltLen:]
	var plain []byte
	for len(data) > 0 {
		if len(data) < HeaderCipherLen {
			return nil, errors.New("snell quic proxy: truncated frame header")
		}
		headerCipher := append([]byte(nil), data[:HeaderCipherLen]...)
		header, openErr := aead.Open(headerCipher[:0], nonce, headerCipher, nil)
		if openErr != nil || len(header) != HeaderPlainLen || header[0] != HeaderVersion {
			return nil, errors.New("snell quic proxy: header authentication failed")
		}
		IncreaseNonce(nonce)
		if binary.BigEndian.Uint16(header[3:5]) != 0 {
			return nil, errors.New("snell quic proxy: unexpected padding")
		}
		payloadLen := int(binary.BigEndian.Uint16(header[5:7]))
		frameLen := HeaderCipherLen + payloadLen + AEADTagLen
		if payloadLen == 0 || len(data) < frameLen {
			return nil, errors.New("snell quic proxy: invalid payload length")
		}
		bodyCipher := append([]byte(nil), data[HeaderCipherLen:frameLen]...)
		body, openErr := aead.Open(bodyCipher[:0], nonce, bodyCipher, nil)
		if openErr != nil {
			return nil, errors.New("snell quic proxy: payload authentication failed")
		}
		IncreaseNonce(nonce)
		plain = append(plain, body...)
		data = data[frameLen:]
	}
	return plain, nil
}

func encodeQUICProxyInit(userKey []byte, destination M.Socksaddr, payload []byte) ([]byte, error) {
	if len(userKey) > 255 {
		return nil, errors.New("snell quic proxy: user key too long")
	}
	host := destination.AddrString()
	if len(host) == 0 || len(host) > 255 {
		return nil, errors.New("snell quic proxy: invalid target host")
	}
	plain := make([]byte, 0, 2+len(userKey)+1+len(host)+2+len(payload))
	plain = append(plain, RequestVersion, byte(len(userKey)))
	plain = append(plain, userKey...)
	plain = append(plain, byte(len(host)))
	plain = append(plain, host...)
	plain = binary.BigEndian.AppendUint16(plain, destination.Port)
	plain = append(plain, payload...)
	return plain, nil
}

func parseQUICProxyInit(plain []byte) (M.Socksaddr, []byte, []byte, error) {
	if len(plain) < 5 || plain[0] != RequestVersion {
		return M.Socksaddr{}, nil, nil, errors.New("snell quic proxy: invalid init request")
	}
	index := 1
	userKeyLen := int(plain[index])
	index++
	if index+userKeyLen+3 > len(plain) {
		return M.Socksaddr{}, nil, nil, errors.New("snell quic proxy: truncated user key")
	}
	userKey := append([]byte(nil), plain[index:index+userKeyLen]...)
	index += userKeyLen
	hostLen := int(plain[index])
	index++
	if index+hostLen+2 > len(plain) {
		return M.Socksaddr{}, nil, nil, errors.New("snell quic proxy: truncated target")
	}
	host := string(plain[index : index+hostLen])
	index += hostLen
	port := binary.BigEndian.Uint16(plain[index : index+2])
	index += 2
	target := M.ParseSocksaddrHostPort(host, port)
	if !target.IsValid() || port == 0 {
		return M.Socksaddr{}, nil, nil, errors.New("snell quic proxy: invalid target")
	}
	return target, userKey, plain[index:], nil
}
