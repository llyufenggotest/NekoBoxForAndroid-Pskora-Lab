package anytls

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type recordingConn struct {
	bytes.Buffer
}

func (c *recordingConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *recordingConn) Close() error                     { return nil }
func (c *recordingConn) LocalAddr() net.Addr              { return nil }
func (c *recordingConn) RemoteAddr() net.Addr             { return nil }
func (c *recordingConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

func TestClientMetadataSettingsPreserveEmptyAndOverride(t *testing.T) {
	factory, err := newPaddingFactory(DefaultPaddingScheme)
	if err != nil {
		t.Fatal(err)
	}
	for _, metadata := range []string{"", "custom-client/2.0"} {
		client := &Client{clientMetadata: metadata}
		client.padding.Store(factory)
		server, peer := net.Pipe()
		session := newClientSession(client, server)
		peer.Close()
		server.Close()
		defer session.pending.Release()

		wire := session.pending.Bytes()
		if len(wire) < frameOverhead {
			t.Fatalf("short settings frame: %d", len(wire))
		}
		length := int(binary.BigEndian.Uint16(wire[5:7]))
		if len(wire) != frameOverhead+length {
			t.Fatalf("settings frame length=%d wire=%d", length, len(wire))
		}
		got := parseSettings(wire[frameOverhead:])[settingClient]
		if got != metadata {
			t.Fatalf("metadata=%q, want %q", got, metadata)
		}
	}
}

func TestLongUoTPayloadRemainsOneLogicalPacketAcrossAnyTLSFrames(t *testing.T) {
	const payloadLen = 0xffff
	payload := bytes.Repeat([]byte{0x5a}, payloadLen)
	uotPacket := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(uotPacket[:2], uint16(len(payload)))
	copy(uotPacket[2:], payload)

	wire := new(recordingConn)
	session := &session{
		conn:        wire,
		writeAccess: make(chan struct{}, 1),
		streams:     make(map[uint32]*stream),
		done:        make(chan struct{}),
	}
	stream := newStream(1, session)
	stream.handshakeDone.Store(true)

	n, err := stream.Write(uotPacket)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(uotPacket) {
		t.Fatalf("wrote %d bytes, want %d", n, len(uotPacket))
	}

	var rebuilt bytes.Buffer
	frameLengths := make([]int, 0, 2)
	data := wire.Bytes()
	for len(data) > 0 {
		if len(data) < frameOverhead {
			t.Fatalf("truncated frame header: %d bytes", len(data))
		}
		if data[0] != commandPSH {
			t.Fatalf("command=%d, want PSH", data[0])
		}
		if streamID := binary.BigEndian.Uint32(data[1:5]); streamID != 1 {
			t.Fatalf("stream id=%d, want 1", streamID)
		}
		length := int(binary.BigEndian.Uint16(data[5:7]))
		if length == 0 || len(data) < frameOverhead+length {
			t.Fatalf("invalid frame length=%d remaining=%d", length, len(data))
		}
		frameLengths = append(frameLengths, length)
		rebuilt.Write(data[frameOverhead : frameOverhead+length])
		data = data[frameOverhead+length:]
	}
	if len(frameLengths) != 2 || frameLengths[0] != maxFrameSize || frameLengths[1] != 2 {
		t.Fatalf("frame lengths=%v, want [%d 2]", frameLengths, maxFrameSize)
	}
	if !bytes.Equal(rebuilt.Bytes(), uotPacket) {
		t.Fatal("AnyTLS frame split changed the UoT packet byte stream")
	}
	if got := int(binary.BigEndian.Uint16(rebuilt.Bytes()[:2])); got != payloadLen {
		t.Fatalf("UoT payload length=%d, want %d", got, payloadLen)
	}
}

func TestDialContextCancellationPropagates(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Password: "ordinary-password",
		DialOut: func(ctx context.Context) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.DialContext(ctx, M.Socksaddr{Fqdn: "example.com", Port: 443})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DialContext error=%v, want context.Canceled", err)
	}
}

func TestStreamCloseUnblocksRead(t *testing.T) {
	session := &session{done: make(chan struct{}), streams: make(map[uint32]*stream), writeAccess: make(chan struct{}, 1), conn: new(recordingConn)}
	stream := newStream(1, session)
	stream.handshakeDone.Store(true)
	result := make(chan error, 1)
	go func() {
		_, err := stream.Read(make([]byte, 1))
		result <- err
	}()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Read after Close error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read remained blocked after Close")
	}
}

func TestWriteBufferLongPayloadMatchesWriteFraming(t *testing.T) {
	payload := buf.NewSize(maxFrameSize + 2)
	if _, err := payload.Write(bytes.Repeat([]byte{0x7b}, maxFrameSize+2)); err != nil {
		t.Fatal(err)
	}
	wire := new(recordingConn)
	session := &session{
		conn:        wire,
		writeAccess: make(chan struct{}, 1),
		streams:     make(map[uint32]*stream),
		done:        make(chan struct{}),
	}
	stream := newStream(7, session)
	stream.handshakeDone.Store(true)
	if err := stream.WriteBuffer(payload); err != nil {
		t.Fatal(err)
	}
	data := wire.Bytes()
	if len(data) != maxFrameSize+2+2*frameOverhead {
		t.Fatalf("wire length=%d", len(data))
	}
}
