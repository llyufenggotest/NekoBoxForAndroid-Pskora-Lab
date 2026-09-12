package anytls

import (
	"errors"
	"io"
	"testing"
)

func TestPrivateLocalCloseEOF(t *testing.T) {
	s := newStream(1, &session{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("local Read = %v", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, io.EOF) {
		t.Fatalf("local Write = %v", err)
	}
}
