package storage

import "os"

// LocalFileReader implements ObjectReader for a local file on disk.
type LocalFileReader struct {
	file *os.File
}

// NewLocalFileReader opens the file at path for reading.
func NewLocalFileReader(path string) (*LocalFileReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &LocalFileReader{file: f}, nil
}

// ReadAt reads len(p) bytes from the file starting at byte offset off.
func (l *LocalFileReader) ReadAt(p []byte, off int64) (int, error) {
	return l.file.ReadAt(p, off)
}

// Size returns the total file size in bytes.
func (l *LocalFileReader) Size() (int64, error) {
	info, err := l.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Close closes the underlying file handle.
func (l *LocalFileReader) Close() error {
	return l.file.Close()
}
