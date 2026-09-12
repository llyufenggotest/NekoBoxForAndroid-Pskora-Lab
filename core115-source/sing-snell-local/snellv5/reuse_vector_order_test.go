package snellv5

import (
	"bytes"
	"net"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/sagernet/sing-snell/internal/reuse"
	"github.com/sagernet/sing/common/buf"
	N "github.com/sagernet/sing/common/network"
)

func TestServerReuseVectorisedResponseCompletesBeforeCloseWrite(t *testing.T) { //nolint:paralleltest // Changes process-wide GOMAXPROCS.
	previousMaxProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousMaxProcs) })

	partA := []byte("first-vector-buffer")
	partB := []byte("second-vector-buffer")
	recordWriter := &orderedRecordWriter{
		secondBufferPending: make(chan struct{}),
		allowSecondBuffer:   make(chan struct{}),
	}
	conn := &serverReuseConn[struct{}]{
		session: &serverReuseSession[struct{}]{writer: recordWriter},
	}
	vectorisedWriter := &serverReuseVectorisedWriter[struct{}]{conn: conn}

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- vectorisedWriter.WriteVectorised([]*buf.Buffer{
			newServerWriteBuffer(partA),
			newServerWriteBuffer(partB),
		})
	}()
	<-recordWriter.secondBufferPending

	closeAttempted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeAttempted)
		closeDone <- conn.CloseWrite()
	}()
	<-closeAttempted
	// With one P, yield until CloseWrite either completes (old behavior) or
	// blocks behind the vectorised response lock (fixed behavior).
	runtime.Gosched()
	close(recordWriter.allowSecondBuffer)
	if err := <-writeDone; err != nil {
		t.Fatal("write vectorised:", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal("close write:", err)
	}

	expected := []orderedWrite{
		{kind: "payload", payload: partA},
		{kind: "payload", payload: partB},
		{kind: "eof"},
	}
	if actual := recordWriter.snapshot(); !reflect.DeepEqual(expected, actual) {
		t.Fatalf("unexpected response write order: got %#v, want %#v", actual, expected)
	}
}

func newServerWriteBuffer(payload []byte) *buf.Buffer {
	buffer := buf.NewSize(1 + len(payload))
	buffer.Resize(1, 0)
	_, _ = buffer.Write(payload)
	return buffer
}

type orderedWrite struct {
	kind    string
	payload []byte
}

type orderedRecordWriter struct {
	reuse.RecordWriter
	access              sync.Mutex
	writes              []orderedWrite
	secondBufferPending chan struct{}
	allowSecondBuffer   chan struct{}
}

func (w *orderedRecordWriter) WriteBuffer(buffer *buf.Buffer) error {
	defer buffer.Release()
	payload := bytes.Clone(buffer.Bytes()[1:])
	w.append(orderedWrite{kind: "payload", payload: payload})
	return nil
}

func (w *orderedRecordWriter) CreateVectorisedWriterFor(N.VectorisedWriter) N.VectorisedWriter {
	close(w.secondBufferPending)
	<-w.allowSecondBuffer
	return orderedVectorisedWriter(func(buffers []*buf.Buffer) error {
		defer buf.ReleaseMulti(buffers)
		for _, buffer := range buffers {
			w.append(orderedWrite{kind: "payload", payload: bytes.Clone(buffer.Bytes())})
		}
		return nil
	})
}

func (w *orderedRecordWriter) WriteZeroChunk() error {
	w.append(orderedWrite{kind: "eof"})
	return nil
}

func (w *orderedRecordWriter) append(write orderedWrite) {
	w.access.Lock()
	w.writes = append(w.writes, write)
	w.access.Unlock()
}

func (w *orderedRecordWriter) snapshot() []orderedWrite {
	w.access.Lock()
	defer w.access.Unlock()
	return append([]orderedWrite(nil), w.writes...)
}

type orderedVectorisedWriter func(buffers []*buf.Buffer) error

func (w orderedVectorisedWriter) WriteVectorised(buffers []*buf.Buffer) error {
	return w(buffers)
}

var (
	_ N.VectorisedWriter = orderedVectorisedWriter(nil)
	_ reuse.RecordWriter = (*orderedRecordWriter)(nil)
	_ net.Conn           = (*serverReuseConn[struct{}])(nil)
)
