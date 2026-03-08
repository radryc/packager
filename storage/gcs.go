package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	gcStorage "cloud.google.com/go/storage"
)

// GCSReader implements ObjectReader for objects stored in Google Cloud Storage.
// It translates ReadAt calls into GCS range-read requests using the
// cloud.google.com/go/storage client library.
type GCSReader struct {
	bucket *gcStorage.BucketHandle
	object string
	ctx    context.Context

	// size is lazily fetched via ObjectAttrs and cached.
	sizeOnce sync.Once
	size     int64
	sizeErr  error
}

// GCSOption configures optional behaviour for the GCS reader.
type GCSOption func(*GCSReader)

// WithGCSContext sets the context used for all GCS API calls.
// Defaults to context.Background() if not provided.
func WithGCSContext(ctx context.Context) GCSOption {
	return func(r *GCSReader) { r.ctx = ctx }
}

// NewGCSReader creates an ObjectReader backed by the given GCS object.
// The bucket handle should already have been obtained from a storage.Client.
func NewGCSReader(bucket *gcStorage.BucketHandle, object string, opts ...GCSOption) *GCSReader {
	r := &GCSReader{
		bucket: bucket,
		object: object,
		ctx:    context.Background(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ReadAt reads len(p) bytes from the GCS object starting at byte offset off.
// Each call opens a range-limited reader on the object.
func (r *GCSReader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	length := int64(len(p))
	rc, err := r.bucket.Object(r.object).NewRangeReader(r.ctx, off, length)
	if err != nil {
		return 0, fmt.Errorf("gcs NewRangeReader: %w", err)
	}
	defer rc.Close()

	n, err := io.ReadFull(rc, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, io.EOF
	}
	return n, err
}

// Size returns the total size of the GCS object in bytes.
// The value is fetched once via Object.Attrs and cached.
func (r *GCSReader) Size() (int64, error) {
	r.sizeOnce.Do(func() {
		attrs, err := r.bucket.Object(r.object).Attrs(r.ctx)
		if err != nil {
			r.sizeErr = fmt.Errorf("gcs Attrs: %w", err)
			return
		}
		r.size = attrs.Size
	})
	return r.size, r.sizeErr
}

// Close is a no-op for GCS (no persistent connection to release).
func (r *GCSReader) Close() error {
	return nil
}
