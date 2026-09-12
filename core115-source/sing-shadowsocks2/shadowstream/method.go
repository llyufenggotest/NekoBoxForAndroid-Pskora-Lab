package shadowstream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"io"
	"net"
	"os"
	"strings"

	C "github.com/sagernet/sing-shadowsocks2/cipher"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/crypto/chacha20"
)

var MethodList = []string{
	"aes-128-ctr",
	"aes-192-ctr",
	"aes-256-ctr",
	"aes-128-cfb",
	"aes-192-cfb",
	"aes-256-cfb",
	"rc4-md5",
	"chacha20-ietf",
	"xchacha20",
}

func init() {
	C.RegisterMethod(MethodList, NewMethod)
}

// 【关键修复】对齐 Python 的 derive_key 逻辑
func pythonDeriveKey(password []byte, keyLen int) []byte {
	var d []byte
	var di []byte
	for len(d) < keyLen {
		h := md5.New()
		h.Write(di)
		h.Write(password)
		di = h.Sum(nil)
		d = append(d, di...)
	}
	return d[:keyLen]
}

// ==========================================
// 【新增】芒果协议专用 Header 构建
// ==========================================
func buildMangoHeader(suser string) []byte {
	u := []byte(suser)
	h := make([]byte, len(u)+4)
	h[0] = 0x06
	h[1] = byte(len(u))
	copy(h[2:], u)
	h[len(u)+2] = 0x00
	h[len(u)+3] = 0x00
	return h
}

type Method struct {
	keyLength          int
	saltLength         int
	encryptConstructor func(key []byte, salt []byte) (cipher.Stream, error)
	decryptConstructor func(key []byte, salt []byte) (cipher.Stream, error)
	key                []byte
	password           string // 保存原始密码
	mangoSUser         string // 【新增】保存芒果暗号
}

func NewMethod(ctx context.Context, methodName string, options C.MethodOptions) (C.Method, error) {
	// 【新增】解析密码后缀，提取芒果账号
	mangoSUser := ""
	if idx := strings.Index(options.Password, "#mg"); idx != -1 {
		mangoSUser = options.Password[idx+3:]
	}

	m := &Method{
		password:   options.Password, // 记录原始密码
		mangoSUser: mangoSUser,       // 记录暗号
	}

	switch methodName {
	case "aes-128-ctr":
		m.keyLength = 16
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
	case "aes-192-ctr":
		m.keyLength = 24
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
	case "aes-256-ctr":
		m.keyLength = 32
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCTR)
	case "aes-128-cfb":
		m.keyLength = 16
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBEncrypter)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBDecrypter)
	case "aes-192-cfb":
		m.keyLength = 24
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBEncrypter)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBDecrypter)
	case "aes-256-cfb":
		m.keyLength = 32
		m.saltLength = aes.BlockSize
		m.encryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBEncrypter)
		m.decryptConstructor = blockStream(aes.NewCipher, cipher.NewCFBDecrypter)
	case "rc4-md5":
		m.keyLength = 16
		m.saltLength = 16
		m.encryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			h := md5.New()
			h.Write(key)
			h.Write(salt)
			return rc4.NewCipher(h.Sum(nil))
		}
		m.decryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			h := md5.New()
			h.Write(key)
			h.Write(salt)
			return rc4.NewCipher(h.Sum(nil))
		}
	case "chacha20-ietf":
		m.keyLength = chacha20.KeySize
		m.saltLength = chacha20.NonceSize
		m.encryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			return chacha20.NewUnauthenticatedCipher(key, salt)
		}
		m.decryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			return chacha20.NewUnauthenticatedCipher(key, salt)
		}
	case "xchacha20":
		m.keyLength = chacha20.KeySize
		m.saltLength = chacha20.NonceSizeX
		m.encryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			return chacha20.NewUnauthenticatedCipher(key, salt)
		}
		m.decryptConstructor = func(key []byte, salt []byte) (cipher.Stream, error) {
			return chacha20.NewUnauthenticatedCipher(key, salt)
		}
	default:
		return nil, os.ErrInvalid
	}

	// 清理干扰词获取真实密码
	cleanPassword := strings.ReplaceAll(options.Password, "BLACKSTONE", "")
	if idx := strings.Index(cleanPassword, "#"); idx != -1 {
		cleanPassword = cleanPassword[:idx]
	}

	if len(options.Key) == m.keyLength {
		m.key = options.Key
	} else if len(options.Key) > 0 {
		return nil, E.New("bad key length, required ", m.keyLength, ", got ", len(options.Key))
	} else if cleanPassword != "" {
		m.key = pythonDeriveKey([]byte(cleanPassword), m.keyLength)
	} else {
		return nil, C.ErrMissingPassword
	}
	return m, nil
}

func blockStream(blockCreator func(key []byte) (cipher.Block, error), streamCreator func(block cipher.Block, iv []byte) cipher.Stream) func([]byte, []byte) (cipher.Stream, error) {
	return func(key []byte, iv []byte) (cipher.Stream, error) {
		block, err := blockCreator(key)
		if err != nil {
			return nil, err
		}
		return streamCreator(block, iv), err
	}
}

func (m *Method) DialConn(conn net.Conn, destination M.Socksaddr) (net.Conn, error) {
	ssConn := &clientConn{
		ExtendedConn: bufio.NewExtendedConn(conn),
		method:       m,
		destination:  destination,
	}
	return ssConn, common.Error(ssConn.Write(nil))
}

func (m *Method) DialEarlyConn(conn net.Conn, destination M.Socksaddr) net.Conn {
	return &clientConn{
		ExtendedConn: bufio.NewExtendedConn(conn),
		method:       m,
		destination:  destination,
	}
}

func (m *Method) DialPacketConn(conn net.Conn) N.NetPacketConn {
	return &clientPacketConn{
		ExtendedConn: bufio.NewExtendedConn(conn),
		method:       m,
	}
}

type clientConn struct {
	N.ExtendedConn
	method      *Method
	destination M.Socksaddr
	readStream  cipher.Stream
	writeStream cipher.Stream
}

func (c *clientConn) readResponse() error {
	if c.readStream != nil {
		return nil
	}

	if strings.Contains(c.method.password, "BLACKSTONE") {
		magic := make([]byte, 2)
		if _, err := io.ReadFull(c.ExtendedConn, magic); err != nil {
			return err
		}

		padLen := int(magic[1])
		if padLen > 0 {
			if _, err := io.CopyN(io.Discard, c.ExtendedConn, int64(padLen)); err != nil {
				return err
			}
		}

		if _, err := io.CopyN(io.Discard, c.ExtendedConn, 17); err != nil {
			return err
		}

		salt := make([]byte, c.method.saltLength)
		if _, err := io.ReadFull(c.ExtendedConn, salt); err != nil {
			return err
		}

		var err error
		c.readStream, err = c.method.decryptConstructor(c.method.key, salt)
		return err
	}

	saltBuffer := buf.NewSize(c.method.saltLength)
	defer saltBuffer.Release()
	_, err := saltBuffer.ReadFullFrom(c.ExtendedConn, c.method.saltLength)
	if err != nil {
		return err
	}
	c.readStream, err = c.method.decryptConstructor(c.method.key, saltBuffer.Bytes())
	return err
}

func (c *clientConn) Read(p []byte) (n int, err error) {
	if c.readStream == nil {
		err = c.readResponse()
		if err != nil {
			return
		}
	}
	n, err = c.ExtendedConn.Read(p)
	if err != nil {
		return
	}
	c.readStream.XORKeyStream(p[:n], p[:n])
	return
}

func (c *clientConn) Write(p []byte) (n int, err error) {
	if c.writeStream == nil {
		if strings.Contains(c.method.password, "BLACKSTONE") {
			// (略过你原有的 BLACKSTONE 处理逻辑)
			cleanPass := strings.ReplaceAll(c.method.password, "BLACKSTONE", "")
			if idx := strings.Index(cleanPass, "#"); idx != -1 {
				cleanPass = cleanPass[:idx]
			}
			token := md5.Sum([]byte(cleanPass + "_onesocks"))

			salt := make([]byte, c.method.saltLength)
			common.Must1(io.ReadFull(rand.Reader, salt))

			header := make([]byte, 35)
			header[0] = 0x01
			header[1] = 0x00
			copy(header[2:18], token[:])
			header[18] = 0x10
			copy(header[19:35], salt)

			if _, err := c.ExtendedConn.Write(header); err != nil {
				return 0, err
			}

			c.writeStream, err = c.method.encryptConstructor(c.method.key, salt)
			if err != nil {
				return 0, err
			}

			addrLen := M.SocksaddrSerializer.AddrPortLen(c.destination)
			buffer := buf.NewSize(addrLen + len(p))
			defer buffer.Release()

			err = M.SocksaddrSerializer.WriteAddrPort(buffer, c.destination)
			if err != nil {
				return 0, err
			}
			if len(p) > 0 {
				common.Must1(buffer.Write(p))
			}

			c.writeStream.XORKeyStream(buffer.Bytes(), buffer.Bytes())
			_, err = c.ExtendedConn.Write(buffer.Bytes())
			if err == nil {
				n = len(p)
			}
			return
		}

		// ========================================================
		// 原版逻辑 + 芒果协议头注入
		// ========================================================
		mangoLen := 0
		var mHeader []byte
		if c.method.mangoSUser != "" {
			mHeader = buildMangoHeader(c.method.mangoSUser)
			mangoLen = len(mHeader)
		}

		buffer := buf.NewSize(c.method.saltLength + mangoLen + M.SocksaddrSerializer.AddrPortLen(c.destination) + len(p))
		defer buffer.Release()
		
		// 1. 写入明文 IV
		buffer.WriteRandom(c.method.saltLength)
		
		// 2. 在地址之前，写入芒果暗号 (将参与接下来的加密)
		if mangoLen > 0 {
			buffer.Write(mHeader)
		}
		
		// 3. 写入目标地址
		err = M.SocksaddrSerializer.WriteAddrPort(buffer, c.destination)
		if err != nil {
			return
		}
		
		// 4. 写入 payload
		if len(p) > 0 {
			common.Must1(buffer.Write(p))
		}
		
		// 5. 初始化 AES 引擎
		c.writeStream, err = c.method.encryptConstructor(c.method.key, buffer.To(c.method.saltLength))
		if err != nil {
			return
		}
		
		// 6. 加密缓冲区 (跳过开头的 IV，加密后面的所有内容: 头 + 地址 + 数据)
		c.writeStream.XORKeyStream(buffer.From(c.method.saltLength), buffer.From(c.method.saltLength))
		_, err = c.ExtendedConn.Write(buffer.Bytes())
		if err == nil {
			n = len(p)
		}
		return
	}
	c.writeStream.XORKeyStream(p, p)
	return c.ExtendedConn.Write(p)
}

func (c *clientConn) ReadBuffer(buffer *buf.Buffer) error {
	if c.readStream == nil {
		err := c.readResponse()
		if err != nil {
			return err
		}
	}
	err := c.ExtendedConn.ReadBuffer(buffer)
	if err != nil {
		return err
	}
	c.readStream.XORKeyStream(buffer.Bytes(), buffer.Bytes())
	return nil
}

func (c *clientConn) WriteBuffer(buffer *buf.Buffer) error {
	if c.writeStream == nil {
		if strings.Contains(c.method.password, "BLACKSTONE") {
			// (略过黑石逻辑...)
			cleanPass := strings.ReplaceAll(c.method.password, "BLACKSTONE", "")
			if idx := strings.Index(cleanPass, "#"); idx != -1 {
				cleanPass = cleanPass[:idx]
			}
			token := md5.Sum([]byte(cleanPass + "_onesocks"))

			salt := make([]byte, c.method.saltLength)
			common.Must1(io.ReadFull(rand.Reader, salt))

			header := make([]byte, 35)
			header[0] = 0x01
			header[1] = 0x00
			copy(header[2:18], token[:])
			header[18] = 0x10
			copy(header[19:35], salt)

			if _, err := c.ExtendedConn.Write(header); err != nil {
				return err
			}

			var err error
			c.writeStream, err = c.method.encryptConstructor(c.method.key, salt)
			if err != nil {
				return err
			}

			addrLen := M.SocksaddrSerializer.AddrPortLen(c.destination)
			addrHeader := buf.With(buffer.ExtendHeader(addrLen))
			err = M.SocksaddrSerializer.WriteAddrPort(addrHeader, c.destination)
			if err != nil {
				return err
			}

			c.writeStream.XORKeyStream(buffer.Bytes(), buffer.Bytes())
			return c.ExtendedConn.WriteBuffer(buffer)
		}

		// ========================================================
		// 原版 WriteBuffer + 芒果协议头注入
		// ========================================================
		mangoLen := 0
		var mHeader []byte
		if c.method.mangoSUser != "" {
			mHeader = buildMangoHeader(c.method.mangoSUser)
			mangoLen = len(mHeader)
		}

		header := buf.With(buffer.ExtendHeader(c.method.saltLength + mangoLen + M.SocksaddrSerializer.AddrPortLen(c.destination)))
		header.WriteRandom(c.method.saltLength)
		
		// 注入芒果暗号
		if mangoLen > 0 {
			header.Write(mHeader)
		}
		
		err := M.SocksaddrSerializer.WriteAddrPort(header, c.destination)
		if err != nil {
			return err
		}
		c.writeStream, err = c.method.encryptConstructor(c.method.key, header.To(c.method.saltLength))
		if err != nil {
			return err
		}
		c.writeStream.XORKeyStream(buffer.From(c.method.saltLength), buffer.From(c.method.saltLength))
	} else {
		c.writeStream.XORKeyStream(buffer.Bytes(), buffer.Bytes())
	}
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *clientConn) FrontHeadroom() int {
	if c.writeStream == nil {
		if strings.Contains(c.method.password, "BLACKSTONE") {
			return M.SocksaddrSerializer.AddrPortLen(c.destination)
		}
		// 分配缓冲区时必须加上芒果头的长度，防止内存越界
		mangoLen := 0
		if c.method.mangoSUser != "" {
			mangoLen = len(c.method.mangoSUser) + 4
		}
		return c.method.saltLength + mangoLen + M.SocksaddrSerializer.AddrPortLen(c.destination)
	}
	return 0
}

func (c *clientConn) NeedHandshake() bool {
	return c.writeStream == nil
}

func (c *clientConn) Upstream() any {
	return c.ExtendedConn
}

type clientPacketConn struct {
	N.ExtendedConn
	method *Method
}

func (c *clientPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	err = c.ReadBuffer(buffer)
	if err != nil {
		return
	}
	stream, err := c.method.decryptConstructor(c.method.key, buffer.To(c.method.saltLength))
	if err != nil {
		return
	}
	stream.XORKeyStream(buffer.From(c.method.saltLength), buffer.From(c.method.saltLength))
	buffer.Advance(c.method.saltLength)
	destination, err = M.SocksaddrSerializer.ReadAddrPort(buffer)
	if err != nil {
		return
	}
	return destination.Unwrap(), nil
}

func (c *clientPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	mangoLen := 0
	var mHeader []byte
	if c.method.mangoSUser != "" {
		mHeader = buildMangoHeader(c.method.mangoSUser)
		mangoLen = len(mHeader)
	}

	header := buf.With(buffer.ExtendHeader(c.method.saltLength + mangoLen + M.SocksaddrSerializer.AddrPortLen(destination)))
	header.WriteRandom(c.method.saltLength)
	
	if mangoLen > 0 {
		header.Write(mHeader)
	}
	
	err := M.SocksaddrSerializer.WriteAddrPort(header, destination)
	if err != nil {
		return err
	}
	stream, err := c.method.encryptConstructor(c.method.key, buffer.To(c.method.saltLength))
	if err != nil {
		return err
	}
	stream.XORKeyStream(buffer.From(c.method.saltLength), buffer.From(c.method.saltLength))
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *clientPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, err = c.ExtendedConn.Read(p)
	if err != nil {
		return
	}
	stream, err := c.method.decryptConstructor(c.method.key, p[:c.method.saltLength])
	if err != nil {
		return
	}
	buffer := buf.As(p[c.method.saltLength:n])
	stream.XORKeyStream(buffer.Bytes(), buffer.Bytes())
	destination, err := M.SocksaddrSerializer.ReadAddrPort(buffer)
	if err != nil {
		return
	}
	if destination.IsFqdn() {
		addr = destination
	} else {
		addr = destination.UDPAddr()
	}
	n = copy(p, buffer.Bytes())
	return
}

func (c *clientPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	destination := M.SocksaddrFromNet(addr)

	mangoLen := 0
	var mHeader []byte
	if c.method.mangoSUser != "" {
		mHeader = buildMangoHeader(c.method.mangoSUser)
		mangoLen = len(mHeader)
	}

	buffer := buf.NewSize(c.method.saltLength + mangoLen + M.SocksaddrSerializer.AddrPortLen(destination) + len(p))
	defer buffer.Release()
	buffer.WriteRandom(c.method.saltLength)
	
	if mangoLen > 0 {
		buffer.Write(mHeader)
	}
	
	err = M.SocksaddrSerializer.WriteAddrPort(buffer, destination)
	if err != nil {
		return
	}
	_, err = buffer.Write(p)
	if err != nil {
		return
	}
	stream, err := c.method.encryptConstructor(c.method.key, buffer.To(c.method.saltLength))
	if err != nil {
		return
	}
	// 整体加密 (地址 + 芒果头 + 真实数据)
	stream.XORKeyStream(buffer.From(c.method.saltLength), buffer.From(c.method.saltLength))
	
	_, err = c.ExtendedConn.Write(buffer.Bytes())
	if err == nil {
		n = len(p)
	}
	return
}

func (c *clientPacketConn) FrontHeadroom() int {
	mangoLen := 0
	if c.method.mangoSUser != "" {
		mangoLen = len(c.method.mangoSUser) + 4
	}
	return c.method.saltLength + mangoLen + M.MaxSocksaddrLength
}

func (c *clientPacketConn) Upstream() any {
	return c.ExtendedConn
}