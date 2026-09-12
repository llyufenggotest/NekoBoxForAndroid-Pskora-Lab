package neko_log

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriterSubprocess(t *testing.T) {
	path := os.Getenv("NEKO_TEST_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}
	w := &logWriter{file: f, writers: []io.Writer{f}}
	w.Write([]byte("before"))
	fmt.Println("ready")
	bufio.NewReader(os.Stdin).ReadString('\n')
	w.Write([]byte("after"))
	f.Close()
	os.Exit(0)
}

func TestCrossProcessOldDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWriterSubprocess$")
	cmd.Env = append(os.Environ(), "NEKO_TEST_LOG="+path)
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("ready %q %v", line, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := &logWriter{file: f, writers: []io.Writer{f}}
	if err = w.Reconfigure(true, true); err != nil {
		t.Fatal(err)
	}
	input.Write([]byte("continue\n"))
	input.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "after" {
		t.Fatalf("old descriptor after clear: %q", b)
	}
}

func TestConcurrentGateAndClear(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "log"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := &logWriter{file: f, writers: []io.Writer{f}}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				w.Write([]byte("x"))
			}
		}()
	}
	for j := 0; j < 100; j++ {
		if err := w.Reconfigure(j%2 == 0, true); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if err := w.Reconfigure(true, true); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("final"))
	if b, _ := os.ReadFile(f.Name()); string(b) != "final" {
		t.Fatalf("%q", b)
	}
}
