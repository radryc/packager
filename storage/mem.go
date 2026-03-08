package storage

import (
	"bytes"
	"errors"
)

// MemReader implements ObjectReader over an in-memory byte slice.
// Useful for testing and for archives that fit comfortably in RAM.
type MemReader struct {
	data   []byte
	closed bool
}

// NewMemReader wraps data as a read-only ObjectReader.
func NewMemReader(data []byte) *MemReader {
	return &MemReader{data: data}
}

// ReadAt reads len(p) bytes starting at byte offset off.
func (m *MemReader) ReadAt(p []byte, off int64) (int, error) {
	if m.closed {
		return 0, errors.New("reader is closed")
	}
	if off < 0 || off >= int64(len(m.data)) {
		return 0, errors.New("offset out of range")
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, errors.New("short read")
	}
	return n, nil
}

// Size returns the length of the underlying data.
func (m *MemReader) Size() (int64, error) {
	if m.closed {
		return 0, errors.New("reader is closed")
	}
	return int64(len(m.data)), nil
}

// Close marks the reader as closed.
func (m *MemReader) Close() error {
	m.closed = true
	return nil
}

// Bytes returns the underlying byte slice (for inspection in tests).
func (m *MemReader) Bytes() []byte {
	return m.data
}

// NewMemReaderFromBuffer wraps a bytes.Buffer as a read-only ObjectReader.
func NewMemReaderFromBuffer(buf *bytes.Buffer) *MemReader {
	return &MemReader{data: buf.Bytes()}
}
