package oppa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const TypeOppa = "oppa"

const (
	commandTCP byte = 0x01
	commandUDP byte = 0x02
)

type Options struct {
	option.DialerOptions
	option.ServerOptions
	Password      string `json:"password,omitempty"`
	PreConnect    int    `json:"pre_connect,omitempty"`
	PinCertSHA256 string `json:"pin_cert_sha256,omitempty"`
	option.OutboundTLSOptionsContainer
}

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[Options](registry, TypeOppa, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	logger     logger.ContextLogger
	dialer     N.Dialer
	serverAddr M.Socksaddr
	password   string
	tlsConfig  tls.Config
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	passwordLength := len([]byte(options.Password))
	if passwordLength == 0 || passwordLength > 4096 {
		return nil, E.New("Oppa password length must be between 1 and 4096 bytes")
	}
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, C.ErrTLSRequired
	}
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tls.NewSTDClient(ctx, options.Server, *options.TLS)
	if err != nil {
		return nil, err
	}
	if options.PinCertSHA256 != "" {
		pin, err := decodeHash(options.PinCertSHA256)
		if err != nil {
			return nil, E.Cause(err, "decode Oppa certificate pin")
		}
		stdConfig, _ := tlsConfig.Config()
		stdConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if !bytes.Equal(pin, certChainHash(rawCerts)) {
				return E.New("Oppa peer certificate hash mismatch")
			}
			return nil
		}
		stdConfig.InsecureSkipVerify = true
	}
	return &Outbound{
		Adapter:    outbound.NewAdapterWithDialerOptions(TypeOppa, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		logger:     logger,
		dialer:     outboundDialer,
		serverAddr: options.ServerOptions.Build(),
		password:   options.Password,
		tlsConfig:  tlsConfig,
	}, nil
}

func (o *Outbound) dialTLS(ctx context.Context) (net.Conn, error) {
	conn, err := o.dialer.DialContext(ctx, N.NetworkTCP, o.serverAddr)
	if err != nil {
		return nil, err
	}
	tlsConn, err := tls.ClientHandshake(ctx, conn, o.tlsConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		o.logger.InfoContext(ctx, "outbound connection to ", destination)
		conn, err := o.dialTLS(ctx)
		if err != nil {
			return nil, err
		}
		header, err := buildStreamHeader(o.password, commandTCP, destination)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if _, err = conn.Write(header); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	case N.NetworkUDP:
		packetConn, err := o.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(packetConn, destination), nil
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	o.logger.InfoContext(ctx, "outbound packet connection to ", destination)
	conn, err := o.dialTLS(ctx)
	if err != nil {
		return nil, err
	}
	header, _ := buildSessionHeader(o.password, commandUDP)
	if _, err = conn.Write(header); err != nil {
		conn.Close()
		return nil, err
	}
	return &packetConn{Conn: conn}, nil
}

func buildSessionHeader(password string, command byte) ([]byte, error) {
	passwordLength := len([]byte(password))
	if passwordLength == 0 || passwordLength > 4096 {
		return nil, E.New("Oppa password length must be between 1 and 4096 bytes")
	}
	return append(append([]byte(nil), []byte(password)...), command), nil
}

func buildStreamHeader(password string, command byte, destination M.Socksaddr) ([]byte, error) {
	header, err := buildSessionHeader(password, command)
	if err != nil {
		return nil, err
	}
	return appendAddress(header, destination)
}

func appendAddress(buffer []byte, address M.Socksaddr) ([]byte, error) {
	if address.IsFqdn() {
		domain := []byte(address.Fqdn)
		if len(domain) == 0 || len(domain) > 255 {
			return nil, E.New("invalid Oppa domain length")
		}
		buffer = append(buffer, 0x03, byte(len(domain)))
		buffer = append(buffer, domain...)
	} else if address.Addr.Is4() {
		buffer = append(buffer, 0x01)
		ip := address.Addr.As4()
		buffer = append(buffer, ip[:]...)
	} else if address.Addr.Is6() {
		buffer = append(buffer, 0x04)
		ip := address.Addr.As16()
		buffer = append(buffer, ip[:]...)
	} else {
		return nil, E.New("invalid Oppa address")
	}
	return binary.BigEndian.AppendUint16(buffer, address.Port), nil
}

func parseAddress(reader io.Reader) (M.Socksaddr, error) {
	var kind [1]byte
	if _, err := io.ReadFull(reader, kind[:]); err != nil {
		return M.Socksaddr{}, err
	}
	switch kind[0] {
	case 0x01:
		var raw [6]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return M.Socksaddr{}, err
		}
		addr, ok := netip.AddrFromSlice(raw[:4])
		if !ok {
			return M.Socksaddr{}, E.New("invalid Oppa IPv4 address")
		}
		return M.Socksaddr{Addr: addr, Port: binary.BigEndian.Uint16(raw[4:])}, nil
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return M.Socksaddr{}, err
		}
		raw := make([]byte, int(size[0])+2)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return M.Socksaddr{}, err
		}
		return M.Socksaddr{Fqdn: string(raw[:len(raw)-2]), Port: binary.BigEndian.Uint16(raw[len(raw)-2:])}, nil
	case 0x04:
		var raw [18]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return M.Socksaddr{}, err
		}
		addr, ok := netip.AddrFromSlice(raw[:16])
		if !ok {
			return M.Socksaddr{}, E.New("invalid Oppa IPv6 address")
		}
		return M.Socksaddr{Addr: addr, Port: binary.BigEndian.Uint16(raw[16:])}, nil
	default:
		return M.Socksaddr{}, E.New("unsupported Oppa address type: ", kind[0])
	}
}

type packetConn struct {
	net.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
}

func (c *packetConn) ReadFrom(buffer []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var sizeBytes [2]byte
	if _, err := io.ReadFull(c.Conn, sizeBytes[:]); err != nil {
		return 0, nil, err
	}
	size := int(binary.BigEndian.Uint16(sizeBytes[:]))
	if size < 4 || size > 65535 {
		return 0, nil, E.New("invalid Oppa UDP frame length: ", size)
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(c.Conn, frame); err != nil {
		return 0, nil, err
	}
	reader := bytes.NewReader(frame)
	_, err := parseAddress(reader)
	if err != nil {
		return 0, nil, err
	}
	destination, err := parseAddress(reader)
	if err != nil {
		return 0, nil, err
	}
	if reader.Len() > len(buffer) {
		return 0, nil, io.ErrShortBuffer
	}
	n, _ := reader.Read(buffer)
	return n, destination.UDPAddr(), nil
}

func (c *packetConn) WriteTo(payload []byte, address net.Addr) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	destination := M.SocksaddrFromNet(address).Unwrap()
	body, err := appendAddress(nil, M.Socksaddr{Addr: netip.IPv4Unspecified()})
	if err != nil {
		return 0, err
	}
	body, err = appendAddress(body, destination)
	if err != nil {
		return 0, err
	}
	body = append(body, payload...)
	if len(body) > 65535 {
		return 0, E.New("Oppa UDP payload too large")
	}
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(body)))
	frame = append(frame, body...)
	if _, err = c.Conn.Write(frame); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *packetConn) LocalAddr() net.Addr                { return c.Conn.LocalAddr() }
func (c *packetConn) SetDeadline(t time.Time) error      { return c.Conn.SetDeadline(t) }
func (c *packetConn) SetReadDeadline(t time.Time) error  { return c.Conn.SetReadDeadline(t) }
func (c *packetConn) SetWriteDeadline(t time.Time) error { return c.Conn.SetWriteDeadline(t) }

func decodeHash(raw string) ([]byte, error) {
	clean := bytes.ReplaceAll([]byte(raw), []byte(":"), nil)
	if decoded, err := hex.DecodeString(string(clean)); err == nil && len(decoded) == sha256.Size {
		return decoded, nil
	}
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(raw); err == nil && len(decoded) == sha256.Size {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid SHA-256 pin")
}

func certChainHash(rawCerts [][]byte) []byte {
	var chain []byte
	for _, cert := range rawCerts {
		digest := sha256.Sum256(cert)
		if chain == nil {
			chain = digest[:]
		} else {
			next := sha256.Sum256(append(chain, digest[:]...))
			chain = next[:]
		}
	}
	return chain
}
