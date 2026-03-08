package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Reader implements ObjectReader for objects stored in Amazon S3.
// It translates ReadAt calls into S3 GetObject requests with byte-range
// headers, minimising bandwidth usage.
type S3Reader struct {
	client *s3.Client
	bucket string
	key    string
	ctx    context.Context

	// size is lazily fetched via HeadObject and cached.
	sizeOnce sync.Once
	size     int64
	sizeErr  error
}

// S3Option configures optional behaviour for the S3 reader.
type S3Option func(*S3Reader)

// WithS3Context sets the context used for all S3 API calls.
// Defaults to context.Background() if not provided.
func WithS3Context(ctx context.Context) S3Option {
	return func(r *S3Reader) { r.ctx = ctx }
}

// NewS3Reader creates an ObjectReader backed by the given S3 object.
// The client should already be configured with credentials and region.
func NewS3Reader(client *s3.Client, bucket, key string, opts ...S3Option) *S3Reader {
	r := &S3Reader{
		client: client,
		bucket: bucket,
		key:    key,
		ctx:    context.Background(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ReadAt reads len(p) bytes from the S3 object starting at byte offset off.
// Each call issues a single GetObject request with a Range header.
func (r *S3Reader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1)

	resp, err := r.client.GetObject(r.ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.key),
		Range:  aws.String(rangeHeader),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 GetObject: %w", err)
	}
	defer resp.Body.Close()

	n, err := io.ReadFull(resp.Body, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		// Partial read — the object may be smaller than requested range end.
		return n, io.EOF
	}
	return n, err
}

// Size returns the total size of the S3 object in bytes.
// The value is fetched once via HeadObject and cached.
func (r *S3Reader) Size() (int64, error) {
	r.sizeOnce.Do(func() {
		resp, err := r.client.HeadObject(r.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(r.bucket),
			Key:    aws.String(r.key),
		})
		if err != nil {
			r.sizeErr = fmt.Errorf("s3 HeadObject: %w", err)
			return
		}
		if resp.ContentLength != nil {
			r.size = *resp.ContentLength
		}
	})
	return r.size, r.sizeErr
}

// Close is a no-op for S3 (no persistent connection to release).
func (r *S3Reader) Close() error {
	return nil
}
