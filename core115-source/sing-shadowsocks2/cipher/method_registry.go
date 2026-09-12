package cipher

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var methodRegistry map[string]MethodCreator

func RegisterMethod(methods []string, creator MethodCreator) {
	if methodRegistry == nil {
		methodRegistry = make(map[string]MethodCreator)
	}
	for _, method := range methods {
		methodRegistry[method] = creator
	}
}

func CreateMethod(ctx context.Context, methodName string, options MethodOptions) (Method, error) {
	if methodRegistry == nil {
		methodRegistry = make(map[string]MethodCreator)
	}
	creator, ok := methodRegistry[methodName]
	if !ok {
		return nil, E.New("unknown method: ", methodName)
	}

	pwd := options.Password
	upperPwd := strings.ToUpper(pwd)
	var actualToken string
	isViewTurboNode := false

	// 识别专线节点标记
	if strings.HasSuffix(upperPwd, "#VT") {
		isViewTurboNode = true
		actualToken = pwd[:len(pwd)-3]
	} else if strings.HasSuffix(upperPwd, "VT") {
		if len(pwd) == 34 {
			isViewTurboNode = true
			actualToken = pwd[:len(pwd)-2]
		}
	}

	if isViewTurboNode {
		// 💥 强制使用官方底层硬编码密码走原生 MD5 流程
		options.Password = "password"
		options.Key = nil
	}

	origMethod, err := creator(ctx, methodName, options)
	if err != nil {
		return nil, err
	}

	if isViewTurboNode {
		return &viewTurboMethodWrapper{
			origMethod: origMethod,
			token:      actualToken,
		}, nil
	}

	return origMethod, nil
}

type viewTurboMethodWrapper struct {
	origMethod Method
	token      string
}

func (m *viewTurboMethodWrapper) DialConn(conn net.Conn, destination M.Socksaddr) (net.Conn, error) {
	wrappedConn := wrapViewTurboConn(conn, m.token)
	return m.origMethod.DialConn(wrappedConn, destination)
}

func (m *viewTurboMethodWrapper) DialEarlyConn(conn net.Conn, destination M.Socksaddr) net.Conn {
	wrappedConn := wrapViewTurboConn(conn, m.token)
	return m.origMethod.DialEarlyConn(wrappedConn, destination)
}

func (m *viewTurboMethodWrapper) DialPacketConn(conn net.Conn) N.NetPacketConn {
	return m.origMethod.DialPacketConn(conn)
}

// ---------------------------------------------------------
// 底层劫持核心：高性能 Read 缓存，精准 Salt 取反
// ---------------------------------------------------------
type viewTurboConn struct {
	net.Conn
	token      string
	firstWrite bool
	firstRead  bool
	leftover   []byte // 💥 性能优化：存放“大口读”时多出来的真实数据
}

func wrapViewTurboConn(c net.Conn, token string) net.Conn {
	return &viewTurboConn{
		Conn:       c,
		token:      token,
		firstWrite: true,
		firstRead:  true,
		leftover:   nil,
	}
}

func randomString(min, max int) string {
	length := min
	if max > min {
		length = rand.Intn(max-min+1) + min
	}
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// 💥 发包劫持：完美伪装官方的 TCP 分片特征 (带长度混淆，防指纹识别)
func (c *viewTurboConn) Write(b []byte) (int, error) {
	if c.firstWrite {
		if len(b) >= 32 {
			c.firstWrite = false
			rand.Seed(time.Now().UnixNano())

			// 🛡️ 修复核心 1: 恢复假头的长度随机性！(例如 20~25 字节随机字母 + 4字节换行)
			// 这样第一个包的总长度会在 24~29 字节之间不断跳动，彻底摧毁固定长度指纹！
			fakeHeader := randomString(20, 25) + "\r\n\r\n"
			_, err := c.Conn.Write([]byte(fakeHeader))
			if err != nil {
				return 0, err
			}

			// 官方特征 2: 发送严格的 60 字节 Token 块 (这个必须固定，因为是解密密钥)
			tokenBlock := make([]byte, 60)
			copy(tokenBlock, []byte(randomString(60, 60)))
			tokenBytes := []byte(c.token)
			copy(tokenBlock, tokenBytes)
			if len(tokenBytes) < 60 {
				tokenBlock[len(tokenBytes)] = ':'
			}
			tokenBlock[1] = ^tokenBlock[1] // Token 第二字节取反
			_, err = c.Conn.Write(tokenBlock)
			if err != nil {
				return 0, err
			}

			// 官方特征 3: 发送严格的 32 字节 Salt 盐值 (首字节精准取反)
			saltBlock := make([]byte, 32)
			copy(saltBlock, b[:32])
			saltBlock[0] = ^saltBlock[0]
			_, err = c.Conn.Write(saltBlock)
			if err != nil {
				return 0, err
			}

			// 🛡️ 修复核心 4: 强制 TCP 切片！模拟目标地址小包
			rem := b[32:]
			if len(rem) > 0 {
				// 动态切片：50~80 字节之间跳动
				splitSize := rand.Intn(30) + 50
				if len(rem) < splitSize {
					splitSize = len(rem)
				}

				// 先发第一刀（伪装成独立的 Address Chunk 小包）
				_, err = c.Conn.Write(rem[:splitSize])
				if err != nil {
					return 0, err
				}

				// 再发剩下的真实大块数据
				if len(rem) > splitSize {
					_, err = c.Conn.Write(rem[splitSize:])
					if err != nil {
						return 0, err
					}
				}
			}

			// 欺骗上层引擎：告诉它数据全发完了
			return len(b), nil
		}
	}
	
	// 后续数据正常透传
	return c.Conn.Write(b)
}

// 💥 收包劫持：带缓存的高速剥离逻辑
func (c *viewTurboConn) Read(b []byte) (int, error) {
	if c.firstRead {
		c.firstRead = false
		
		// 性能优化：不再 1 字节地读，而是 1024 字节大口吞咽
		buf := make([]byte, 1024)
		var receivedData []byte
		
		for {
			n, err := c.Conn.Read(buf)
			if err != nil {
				return 0, err
			}
			receivedData = append(receivedData, buf[:n]...)
			
			// 寻找假 HTTP 头的结束标志
			idx := bytes.Index(receivedData, []byte("\r\n\r\n"))
			if idx != -1 {
				endOfHeader := idx + 4
				// 如果读多了，把多出来的真实加密数据存入 leftover
				if len(receivedData) > endOfHeader {
					c.leftover = make([]byte, len(receivedData)-endOfHeader)
					copy(c.leftover, receivedData[endOfHeader:])
				}
				break
			}
			// 保护逻辑：防止恶意长头爆内存
			if len(receivedData) > 4096 {
				return 0, io.ErrUnexpectedEOF
			}
		}
	}
	
	// 优先消耗存钱罐（leftover）里的数据
	if len(c.leftover) > 0 {
		n := copy(b, c.leftover)
		c.leftover = c.leftover[n:]
		return n, nil
	}
	
	// 恢复正常透传读取
	return c.Conn.Read(b)
}