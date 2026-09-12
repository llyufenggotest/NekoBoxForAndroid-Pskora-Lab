package snell

import (
	"context"
	"net"
	"os"

	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	HeaderVersion   = 0x04
	HeaderPlainLen  = 7
	AEADTagLen      = 16
	HeaderCipherLen = HeaderPlainLen + AEADTagLen
	SaltLen         = 16
	NonceLen        = 12
	MaxPayloadLen   = 0x3fff
)

const RequestVersion = 0x01

const (
	CommandPing      = 0x00
	CommandConnect   = 0x01
	CommandConnectV2 = 0x05
	CommandUDP       = 0x06
)

const (
	ReplyTunnel = 0x00
	ReplyPong   = 0x01
	ReplyError  = 0x02
)

const (
	UDPCommandForward = 0x01
	AddressTypeDomain = 0x03
	AddressTypeIPv4   = 0x04
	AddressTypeIPv6   = 0x06
)

var (
	ErrBadVersion            = E.New("snell: bad header version")
	ErrReservedNonZero       = E.New("snell: reserved header octet is non-zero")
	ErrPayloadTooLarge       = E.New("snell: payload length exceeds maximum")
	ErrUnsupportedCommand    = E.New("snell: unsupported command")
	ErrUnexpectedReply       = E.New("snell: unexpected reply opcode")
	ErrEmptyDomainUDPPayload = E.New("snell: empty UDP payload is not allowed for a domain destination")
	ErrMissingPSK            = E.New("snell: missing pre-shared key")
	ErrNoUsers               = E.New("snell: no users")
	ErrBadUserKey            = E.New("snell: bad user key")
	ErrDuplicateUserKey      = E.New("snell: duplicate user key")
)

type MultiUserAuthentication uint8

const (
	MultiUserAuthenticationUserKey MultiUserAuthentication = iota
	MultiUserAuthenticationPSK
)

type Method interface {
	DialConn(conn net.Conn, destination M.Socksaddr) (net.Conn, error)
	DialEarlyConn(conn net.Conn, destination M.Socksaddr) net.Conn
	DialPacketConn(conn net.Conn) (N.NetPacketConn, error)
}

// VectorisedWriteCreator is kept local so sing-snell remains compatible with
// sing versions predating common/network.VectorisedWriteCreator.
type VectorisedWriteCreator interface {
	CreateVectorisedWriter() (N.VectorisedWriter, bool)
}

type EarlyReader interface {
	NeedHandshakeForRead() bool
}

type EarlyWriter interface {
	NeedHandshakeForWrite() bool
}

type PacketBatchWriter interface {
	WritePacketBatch(buffers []*buf.Buffer, destinations []M.Socksaddr) error
}

type PacketBatchWriteCreator interface {
	CreatePacketBatchWriter() (PacketBatchWriter, bool)
}

func WritePacketBatchFallback(writer N.PacketWriter, buffers []*buf.Buffer, destinations []M.Socksaddr) error {
	if len(buffers) == 0 || len(buffers) != len(destinations) {
		buf.ReleaseMulti(buffers)
		return os.ErrInvalid
	}
	for index, buffer := range buffers {
		if err := writer.WritePacket(buffer, destinations[index]); err != nil {
			buf.ReleaseMulti(buffers[index+1:])
			return err
		}
	}
	return nil
}

// NewReadWaitBuffer backports ReadWaitOptions.NewBufferSize from newer sing.
func NewReadWaitBuffer(options N.ReadWaitOptions, bufferSize int) *buf.Buffer {
	bufferSize += options.FrontHeadroom + options.RearHeadroom
	buffer := buf.NewSize(bufferSize)
	if options.FrontHeadroom > 0 {
		buffer.Resize(options.FrontHeadroom, 0)
	}
	if options.RearHeadroom > 0 {
		buffer.Reserve(options.RearHeadroom)
	}
	return buffer
}

type Handler interface {
	N.TCPConnectionHandlerEx
	N.UDPConnectionHandlerEx
}

type Service interface {
	NewConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error
}

// snell-server v6.0.0b4: FUN_00141bc0: malformed handshakes are torn down abruptly.
type ServerError struct {
	net.Conn
	Source M.Socksaddr
	Cause  error
}

func (e *ServerError) Unwrap() error {
	return e.Cause
}

func (e *ServerError) Error() string {
	return "snell: serve " + e.Source.String() + ": " + e.Cause.Error()
}

type keepSessionKey struct{}

func ContextWithKeepSession(ctx context.Context) context.Context {
	return context.WithValue(ctx, (*keepSessionKey)(nil), true)
}

func KeepSessionFromContext(ctx context.Context) bool {
	keep, _ := ctx.Value((*keepSessionKey)(nil)).(bool)
	return keep
}
