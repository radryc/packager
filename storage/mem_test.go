package storage

import (
	"errors"
	"io"
	"testing"
)

func TestMemReaderReadAtEOFContract(t *testing.T) {
	data := []byte("hello")
	r := NewMemReader(data)

	// Read past end: must get io.EOF, not a custom error.
	buf := make([]byte, 10)
	n, err := r.ReadAt(buf, 0)
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF for short read, got %v", err)
	}
}

func TestMemReaderReadAtExact(t *testing.T) {
	data := []byte("hello")
	r := NewMemReader(data)

	buf := make([]byte, 5)
	n, err := r.ReadAt(buf, 0)
	if n != 5 || err != nil {
		t.Errorf("exact read: got n=%d err=%v, want n=5 err=nil", n, err)
	}
}

func TestMemReaderReadAtClosedError(t *testing.T) {
	r := NewMemReader([]byte("hello"))
	_ = r.Close()
	_, err := r.ReadAt(make([]byte, 1), 0)
	if err == nil {
		t.Error("expected error reading from closed MemReader")
	}
}

func TestMemReaderSizeClosedError(t *testing.T) {
	r := NewMemReader([]byte("hello"))
	_ = r.Close()
	_, err := r.Size()
	if err == nil {
		t.Error("expected error calling Size on closed MemReader")
	}
}
