package neko_log

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/matsuridayo/libneko/neko_common"
	"github.com/matsuridayo/libneko/syscallw"
)

var LogWriter *logWriter

// Only used before SetupLog; live changes must use Reconfigure.
var LogWriterDisable = false
var TruncateOnStart = true
var NB4AGuiLogWriter io.Writer

func SetupLog(maxSize int, path string) error {
	if LogWriter != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	w := &logWriter{file: f, disabled: LogWriterDisable}
	if TruncateOnStart {
		err = w.withFileLock(func() error {
			stat, err := f.Stat()
			if err != nil {
				return err
			}
			if stat.Size() <= int64(maxSize) {
				return nil
			}
			old := make([]byte, maxSize)
			if _, err = f.ReadAt(old, stat.Size()-int64(maxSize)); err != nil {
				return err
			}
			if err = w.truncateFile(); err != nil {
				return err
			}
			_, err = f.Write(old)
			return err
		})
		if err != nil {
			f.Close()
			return err
		}
	}
	if neko_common.RunMode == neko_common.RunMode_NekoBoxForAndroid {
		if err = w.redirectStderr(!w.disabled); err != nil {
			f.Close()
			return err
		}
		w.writers = []io.Writer{NB4AGuiLogWriter, f}
	} else {
		w.writers = []io.Writer{os.Stdout, f}
	}
	LogWriter = w
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(w)
	return nil
}

type logWriter struct {
	mu       sync.Mutex
	writers  []io.Writer
	file     *os.File
	disabled bool
}

// The same append-only inode is retained, including across process boundaries.
// Every file write and truncate uses flock; never unlink or replace the log.
func (w *logWriter) withFileLock(fn func() error) (err error) {
	if w.file == nil {
		return errors.New("log file is not initialized")
	}
	fd := int(w.file.Fd())
	if err = syscallw.Flock(fd, syscallw.LOCK_EX); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, syscallw.Flock(fd, syscallw.LOCK_UN)) }()
	return fn()
}

func (w *logWriter) truncateFile() error {
	if runtime.GOOS != "windows" {
		return w.file.Truncate(0)
	}
	// Windows append-only handles cannot truncate. Open the same path without
	// append, verify identity, and never unlink the live writer's inode.
	f, err := os.OpenFile(w.file.Name(), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	before, err := w.file.Stat()
	if err != nil {
		return err
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("log file replaced")
	}
	return f.Truncate(0)
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disabled {
		return len(p), nil
	}
	for _, out := range w.writers {
		if out == nil {
			continue
		}
		var n int
		var err error
		if out == w.file {
			err = w.withFileLock(func() error { n, err = out.Write(p); return err })
		} else {
			n, err = out.Write(p)
		}
		if err != nil {
			return n, err
		}
		if n != len(p) {
			return n, io.ErrShortWrite
		}
	}
	return len(p), nil
}

// redirectStderr gates raw native writes too; flock only protects cooperative writers.
func (w *logWriter) redirectStderr(enabled bool) error {
	if neko_common.RunMode != neko_common.RunMode_NekoBoxForAndroid {
		return nil
	}
	if enabled {
		return syscallw.Dup3(int(w.file.Fd()), int(os.Stderr.Fd()), 0)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	return syscallw.Dup3(int(null.Fd()), int(os.Stderr.Fd()), 0)
}

// Reconfigure serializes the process-local gate with in-flight writers and
// checks cross-process flock/truncate errors. On failure the gate is unchanged.
func (w *logWriter) Reconfigure(enabled, clear bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.redirectStderr(false); err != nil {
		return err
	}
	if clear {
		if err := w.withFileLock(func() error { return w.truncateFile() }); err != nil {
			_ = w.redirectStderr(!w.disabled)
			return err
		}
	}
	if err := w.redirectStderr(enabled); err != nil {
		return err
	}
	w.disabled = !enabled
	return nil
}

func Reconfigure(enabled, clear bool) error {
	if LogWriter == nil {
		return errors.New("log writer is not initialized")
	}
	return LogWriter.Reconfigure(enabled, clear)
}

func (w *logWriter) Truncate() { _ = w.ReconfigureClear() }
func (w *logWriter) ReconfigureClear() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.withFileLock(func() error { return w.truncateFile() })
}
func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disabled = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
