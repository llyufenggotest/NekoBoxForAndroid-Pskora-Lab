package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	D "github.com/sagernet/sing-tun/diagnostic"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/uot"

	"github.com/sagernet/sing-box/protocol/shadowsocks"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.XHttpOutboundOptions](registry, "xhttp", NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	ctx       context.Context
	router    adapter.Router
	logger    logger.ContextLogger
	dialer    N.Dialer
	nodeName  string
	xToken    string
	nodeID    string
	uotClient *uot.Client

	initMu     sync.Mutex
	isInit     bool
	initErr    error
	bestCfg    RealConfig
	ssOutbound adapter.Outbound
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.XHttpOutboundOptions) (adapter.Outbound, error) {
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}

	targetName := options.Name
	if targetName == "" {
		targetName = tag
	}

	ob := &Outbound{
		Adapter:  outbound.NewAdapterWithDialerOptions("xhttp", tag, options.Network.Build(), options.DialerOptions),
		ctx:      ctx,
		router:   router,
		logger:   logger,
		dialer:   outboundDialer,
		nodeName: targetName,
		xToken:   options.Password,
		nodeID:   options.NodeID,
	}

	uotOptions := common.PtrValueOrDefault(options.UDPOverTCP)
	ob.uotClient = &uot.Client{
		Dialer:  (*xhttpDialer)(ob),
		Version: uotOptions.Version,
	}

	return ob, nil
}

func (h *Outbound) pingRace(ctx context.Context, configs []RealConfig) (RealConfig, error) {
	if len(configs) == 1 {
		return configs[0], nil
	}

	type raceResult struct {
		cfg RealConfig
		err error
	}
	resCh := make(chan raceResult, len(configs))
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for _, cfg := range configs {
		wg.Add(1)
		go func(c RealConfig) {
			defer wg.Done()
			addr := M.ParseSocksaddr(net.JoinHostPort(c.Server, c.Port))
			dialCtx, dialCancel := context.WithTimeout(raceCtx, 3*time.Second)
			defer dialCancel()
			conn, err := h.dialer.DialContext(dialCtx, N.NetworkTCP, addr)
			if err == nil {
				conn.Close()
				cancel()
			}
			resCh <- raceResult{cfg: c, err: err}
		}(cfg)
	}

	go func() { wg.Wait(); close(resCh) }()

	var firstErr error
	for res := range resCh {
		if res.err == nil {
			return res.cfg, nil
		}
		if firstErr == nil {
			firstErr = res.err
		}
	}
	return RealConfig{}, fmt.Errorf("all nodes failed tcp ping: %v", firstErr)
}

func (h *Outbound) lazyInit(ctx context.Context) error {
	h.initMu.Lock()
	defer h.initMu.Unlock()

	if h.isInit {
		return h.initErr
	}

	h.logger.InfoContext(ctx, "[xhttp] 🌟 节点首次发包，通过特权直连拉取最新配置...")

	apiCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	realConfigs, err := FetchDynamicConfig(apiCtx, h.dialer, h.nodeName, h.nodeID, h.xToken)
	if err != nil {
		h.logger.ErrorContext(ctx, "[xhttp] ❌ 动态配置获取失败: ", err)
		return err
	}

	best, err := h.pingRace(apiCtx, realConfigs)
	if err != nil {
		h.logger.ErrorContext(ctx, "[xhttp] ❌ 节点测速全部失败: ", err)
		return err
	}

	h.bestCfg = best
	if best.Type == "ss" {
		h.logger.InfoContext(ctx, "[xhttp] ⚡ 动态配置为 SS2022，正在初始化底层核心...")
		portInt, _ := strconv.ParseUint(best.Port, 10, 16)
		ssOpts := option.ShadowsocksOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     best.Server,
				ServerPort: uint16(portInt),
			},
			Method:   best.Cipher,
			Password: best.Password + "#BLACKSTONE",
		}
		h.ssOutbound, h.initErr = shadowsocks.NewOutbound(h.ctx, h.router, h.logger, h.Tag()+"_ss", ssOpts)
		if h.initErr != nil {
			return h.initErr
		}
	} else {
		h.logger.InfoContext(ctx, fmt.Sprintf("[xhttp] ⚡ 锁定优选 XHTTP 节点 -> %s:%s", best.Server, best.Port))
	}

	h.isInit = true
	h.initErr = nil
	return nil
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	D.Emit(h.logger, D.Record{Event: D.Start, Generation: D.Generation(), Value: 2})
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination

	if err := h.lazyInit(ctx); err != nil {
		return nil, err
	}

	if h.ssOutbound != nil {
		return h.ssOutbound.DialContext(ctx, network, destination)
	}

	switch N.NetworkName(network) {
	case N.NetworkTCP:
		return (*xhttpDialer)(h).DialContext(ctx, network, destination)
	case N.NetworkUDP:
		return h.uotClient.DialContext(ctx, network, destination)
	}
	return nil, E.Extend(N.ErrUnknownNetwork, network)
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if err := h.lazyInit(ctx); err != nil {
		return nil, err
	}
	if h.ssOutbound != nil {
		return h.ssOutbound.ListenPacket(ctx, destination)
	}
	return h.uotClient.ListenPacket(ctx, destination)
}

func (h *Outbound) InterfaceUpdated() {}
func (h *Outbound) Close() error      { return nil }

var _ N.Dialer = (*xhttpDialer)(nil)

type xhttpDialer Outbound

func (h *xhttpDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	addr := M.ParseSocksaddr(net.JoinHostPort(h.bestCfg.Server, h.bestCfg.Port))
	rawConn, err := h.dialer.DialContext(ctx, N.NetworkTCP, addr)
	if err != nil {
		return nil, err
	}

	if tcpConn, ok := rawConn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	passParts := strings.Split(h.bestCfg.Password, ":")
	if len(passParts) != 2 {
		rawConn.Close()
		return nil, errors.New("invalid xhttp password format")
	}
	part1Str := strings.TrimSpace(passParts[0])
	part2Hex := passParts[1]
	decodedHmacKey, _ := hex.DecodeString(part2Hex)
	hmacLen := len(decodedHmacKey)
	handshakeLen := 128 + 16 + hmacLen + 1 + 16
	ivLenOffset := 144 + hmacLen

	clientHandshake := make([]byte, handshakeLen)
	decodedTcp, _ := hex.DecodeString(h.bestCfg.TcpFake)

	copyLen := len(decodedTcp)
	if copyLen > 128 {
		copyLen = 128
	}
	copy(clientHandshake[0:copyLen], decodedTcp[:copyLen])
	if copyLen < 128 {
		_, _ = rand.Read(clientHandshake[copyLen:128])
	}

	tokenMD5 := md5.Sum([]byte(part1Str + "do not hack this protocol please"))
	copy(clientHandshake[128:144], tokenMD5[:])
	copy(clientHandshake[144:144+hmacLen], decodedHmacKey)

	clientHandshake[ivLenOffset] = 0x10
	clientWriteIV := clientHandshake[ivLenOffset+1 : handshakeLen]
	_, _ = rand.Read(clientWriteIV)

	aesKey := md5.Sum([]byte(part1Str))
	block, _ := aes.NewCipher(aesKey[:])
	encryptStream := cipher.NewCTR(block, clientWriteIV)

	var destBuf bytes.Buffer
	if destination.IsFqdn() {
		destBuf.WriteByte(0x03)
		destBuf.WriteByte(byte(len(destination.Fqdn)))
		destBuf.WriteString(destination.Fqdn)
	} else if destination.IsIPv4() {
		destBuf.WriteByte(0x01)
		destBuf.Write(destination.Addr.AsSlice())
	} else {
		destBuf.WriteByte(0x04)
		destBuf.Write(destination.Addr.AsSlice())
	}
	binary.Write(&destBuf, binary.BigEndian, destination.Port)

	addrPayload := destBuf.Bytes()
	encryptedAddr := make([]byte, len(addrPayload))
	encryptStream.XORKeyStream(encryptedAddr, addrPayload)

	firstPayload := append(clientHandshake, encryptedAddr...)

	rawConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := rawConn.Write(firstPayload); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("write payload failed: %v", err)
	}
	rawConn.SetWriteDeadline(time.Time{})

	// 🔥 这里绝不阻塞等待读取服务器响应！
	// 立刻把连接交给核心，让核心可以发真实 TLS 载荷触发服务器回应。
	// 解密逻辑全权交给 conn.go 里的 Read 方法处理。
	return NewXHttpConn(rawConn, encryptStream, block, ivLenOffset), nil
}

func (h *xhttpDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, E.New("not implemented")
}
