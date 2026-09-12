package neko_log

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReconfigurePreservesOldWriterAndToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neko.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var console bytes.Buffer
	w := &logWriter{writers: []io.Writer{&console, f}, file: f}
	if _, err = w.Write([]byte("old")); err != nil {
		t.Fatal(err)
	}
	if err = w.Reconfigure(false, true); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hidden"))
	if b, _ := os.ReadFile(path); len(b) != 0 {
		t.Fatalf("disabled/clear: %q", b)
	}
	if err = w.Reconfigure(true, false); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("new"))
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Fatalf("old writer stranded: %q", b)
	}
	if console.String() != "oldnew" {
		t.Fatalf("console: %q", console.String())
	}
}

func TestReconfigureReportsClosedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	w := &logWriter{file: f}
	if err = w.Reconfigure(false, true); err == nil {
		t.Fatal("clear falsely succeeded")
	}
}
