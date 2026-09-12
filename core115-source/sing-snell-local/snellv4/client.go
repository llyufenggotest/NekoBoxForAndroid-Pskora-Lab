package snellv4

import (
	"net"
	"sync"
	"sync/atomic"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/internal/reuse"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type Client struct {
	psk     []byte
	userKey []byte
	reuse   bool
	obfs    snell.ObfsConfig
	dialer  N.Dialer
	server  M.Socksaddr

	pool      reuse.Pool[*reuseSession]
	closeIdle atomic.Bool
}

type ClientOptions struct {
	PSK      []byte
	UserKey  []byte
	Reuse    bool
	ObfsMode snell.ObfsMode
	ObfsHost string
	ObfsURI  string
	Dialer   N.Dialer
	Server   M.Socksaddr
}

func NewClient(options ClientOptions) (*Client, error) {
	if len(options.PSK) == 0 {
		return nil, snell.ErrMissingPSK
	}
	if len(options.UserKey) > 255 {
		return nil, E.New("snell: user key too long")
	}
	switch options.ObfsMode {
	case snell.ObfsModeNone, snell.ObfsModeHTTP, snell.ObfsModeTLS:
	default:
		return nil, E.New("snell: unknown obfs mode: ", int(options.ObfsMode))
	}
	client := &Client{
		psk:     options.PSK,
		userKey: options.UserKey,
		reuse:   options.Reuse,
		obfs:    snell.ObfsConfig{Mode: options.ObfsMode, Host: options.ObfsHost, URI: options.ObfsURI},
		dialer:  options.Dialer,
		server:  options.Server,
	}
	if options.Reuse {
		client.pool.Init()
	}
	return client, nil
}

func (c *Client) DialConn(conn net.Conn, destination M.Socksaddr) (net.Conn, error) {
	clientConn := &clientConn{client: c, Conn: c.obfs.ClientConn(conn), destination: destination}
	return clientConn, clientConn.writeRequest(nil)
}

func (c *Client) DialEarlyConn(conn net.Conn, destination M.Socksaddr) net.Conn {
	return &clientConn{client: c, Conn: c.obfs.ClientConn(conn), destination: destination}
}

func (c *Client) DialPacketConn(conn net.Conn) (N.NetPacketConn, error) {
	return bufio.NewNetPacketConn(&clientPacketConn{Conn: c.obfs.ClientConn(conn), client: c}), nil
}

var _ snell.Method = (*Client)(nil)

type clientConn struct {
	net.Conn
	client      *Client
	destination M.Socksaddr

	access          sync.Mutex
	readAccess      sync.Mutex
	reader          *reader
	writer          *writer
	readerReady     atomic.Bool
	writerReady     atomic.Bool
	readWaitOptions N.ReadWaitOptions
	closeWriteOnce  sync.Once
	closeWriteErr   error
}

func (c *clientConn) writeRequest(payload []byte) error {
	requestPayload := snell.Request{Command: c.command(), ClientID: c.client.userKey, Destination: c.destination}
	request := buf.NewSize(requestPayload.Len() + len(payload))
	err := requestPayload.Write(request)
	if err != nil {
		request.Release()
		return err
	}
	if len(payload) > 0 {
		common.Must1(request.Write(payload))
	}
	defer request.Release()

	recordWriter := &writer{
		upstream: c.Conn,
		psk:      c.client.psk,
	}
	_, err = recordWriter.Write(request.Bytes())
	if err != nil {
		return E.Cause(err, "write request")
	}
	c.writer = recordWriter
	c.writerReady.Store(true)
	return nil
}

func (c *clientConn) writeRequestBuffer(buffer *buf.Buffer) error {
	requestPayload := snell.Request{Command: c.command(), ClientID: c.client.userKey, Destination: c.destination}
	request := buf.With(buffer.ExtendHeader(requestPayload.Len()))
	err := requestPayload.Write(request)
	if err != nil {
		buffer.Release()
		return err
	}
	recordWriter := &writer{
		upstream: c.Conn,
		psk:      c.client.psk,
	}
	err = recordWriter.WriteBuffer(buffer)
	if err != nil {
		return E.Cause(err, "write request")
	}
	c.writer = recordWriter
	c.writerReady.Store(true)
	return nil
}

func (c *clientConn) readResponseLocked() error {
	if c.reader != nil {
		return nil
	}
	if err := c.ensureRequest(); err != nil {
		return err
	}
	recordReader := &reader{upstream: c.Conn, psk: c.client.psk}
	recordReader.InitializeReadWaiter(c.readWaitOptions)
	record, err := recordReader.ReadRecord()
	if err != nil {
		return E.Cause(err, "read reply")
	}
	cached, err := reuse.ParseReply(record)
	if err != nil {
		return err
	}
	if cached != nil {
		recordReader.SetCache(cached)
	}
	c.reader = recordReader
	c.readerReady.Store(true)
	return nil
}

func (c *clientConn) ensureRequest() error {
	c.access.Lock()
	defer c.access.Unlock()
	if c.writer != nil {
		return nil
	}
	return c.writeRequest(nil)
}

func (c *clientConn) Read(p []byte) (int, error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	err := c.readResponseLocked()
	if err != nil {
		return 0, err
	}
	return c.reader.Read(p)
}

func (c *clientConn) ReadBuffer(buffer *buf.Buffer) error {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	err := c.readResponseLocked()
	if err != nil {
		return err
	}
	return c.reader.ReadBuffer(buffer)
}

func (c *clientConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, c.ensureRequest()
	}
	c.access.Lock()
	if c.writer != nil {
		writer := c.writer
		c.access.Unlock()
		return writer.Write(p)
	}
	defer c.access.Unlock()
	err := c.writeRequest(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *clientConn) WriteBuffer(buffer *buf.Buffer) error {
	c.access.Lock()
	if c.writer != nil {
		writer := c.writer
		c.access.Unlock()
		return writer.WriteBuffer(buffer)
	}
	defer c.access.Unlock()
	return c.writeRequestBuffer(buffer)
}

func (c *clientConn) CreateVectorisedWriter() (N.VectorisedWriter, bool) {
	upstreamWriter, created := bufio.CreateVectorisedWriter(c.Conn)
	if !created {
		return nil, false
	}
	return &clientVectorisedWriter{conn: c, upstream: upstreamWriter}, true
}

type clientVectorisedWriter struct {
	conn     *clientConn
	upstream N.VectorisedWriter
}

func (w *clientVectorisedWriter) WriteVectorised(buffers []*buf.Buffer) error {
	conn := w.conn
	conn.access.Lock()
	if conn.writer != nil {
		recordWriter := conn.writer
		conn.access.Unlock()
		return recordWriter.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers)
	}
	for index, buffer := range buffers {
		if buffer.IsEmpty() {
			buffer.Release()
			continue
		}
		err := conn.writeRequestBuffer(buffer)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index+1:])
			return err
		}
		if index+1 < len(buffers) {
			recordWriter := conn.writer
			conn.access.Unlock()
			return recordWriter.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers[index+1:])
		}
		conn.access.Unlock()
		return nil
	}
	conn.access.Unlock()
	return nil
}

func (c *clientConn) CloseWrite() error {
	c.closeWriteOnce.Do(func() {
		c.access.Lock()
		defer c.access.Unlock()
		if c.writer == nil {
			c.closeWriteErr = c.writeRequest(nil)
			if c.closeWriteErr != nil {
				return
			}
		}
		if c.client.reuse {
			c.closeWriteErr = c.writer.WriteZeroChunk()
		} else {
			c.closeWriteErr = N.CloseWrite(c.Conn)
		}
	})
	return c.closeWriteErr
}

func (c *clientConn) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	c.readWaitOptions = options
	if c.reader != nil {
		c.reader.InitializeReadWaiter(options)
	}
	return false
}

func (c *clientConn) WaitReadBuffer() (*buf.Buffer, error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	err := c.readResponseLocked()
	if err != nil {
		return nil, err
	}
	return c.reader.WaitReadBuffer()
}

func (c *clientConn) CreateReadWaiter() (N.ReadWaiter, bool) {
	return c, true
}

func (c *clientConn) FrontHeadroom() int {
	requestPayload := snell.Request{Command: c.command(), ClientID: c.client.userKey, Destination: c.destination}
	return requestPayload.Len() + snell.SaltLen + snell.HeaderCipherLen + maxInitialPaddingLen
}

func (c *clientConn) command() byte {
	if c.client.reuse {
		return snell.CommandConnectV2
	}
	return snell.CommandConnect
}

func (c *clientConn) RearHeadroom() int {
	return snell.AEADTagLen
}

func (c *clientConn) WriterMTU() int {
	return maxPayload
}

func (c *clientConn) NeedHandshakeForRead() bool {
	return !c.readerReady.Load()
}

func (c *clientConn) NeedHandshakeForWrite() bool {
	return !c.writerReady.Load()
}

func (c *clientConn) NeedAdditionalReadDeadline() bool {
	return !c.readerReady.Load()
}

func (c *clientConn) Upstream() any {
	return c.Conn
}

func (c *clientConn) RemoteAddr() net.Addr {
	return c.destination.TCPAddr()
}

var (
	_ N.ExtendedConn               = (*clientConn)(nil)
	_ N.ReadWaiter                 = (*clientConn)(nil)
	_ N.ReadWaitCreator            = (*clientConn)(nil)
	_ snell.VectorisedWriteCreator = (*clientConn)(nil)
	_ N.VectorisedWriter           = (*clientVectorisedWriter)(nil)
	_ snell.EarlyReader            = (*clientConn)(nil)
	_ snell.EarlyWriter            = (*clientConn)(nil)
	_ N.WriteCloser                = (*clientConn)(nil)
)
