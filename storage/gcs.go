package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	gcStorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// GCSConfig holds configuration for creating a GCS client.
//
// Credential resolution order:
//  1. If CredentialsJSON is set, it is used directly (inline service account key).
//  2. If CredentialsFile is set, the JSON key file at that path is used.
//  3. Otherwise Application Default Credentials (ADC) are used, which covers:
//     - GOOGLE_APPLICATION_CREDENTIALS environment variable
//     - gcloud auth application-default login (local development)
//     - GCE metadata server / Workload Identity (when running in GCP)
type GCSConfig struct {
	// CredentialsFile is the path to a service account JSON key file.
	// Leave empty to use ADC or CredentialsJSON.
	CredentialsFile string
	// CredentialsJSON is the raw service account JSON key content.
	// Takes precedence over CredentialsFile.
	CredentialsJSON []byte
}

// GCSReader implements ObjectReader for objects stored in Google Cloud Storage.
// It translates ReadAt calls into GCS range-read requests using the
// cloud.google.com/go/storage client library.
type GCSReader struct {
	client *gcStorage.Client // non-nil when created via NewGCSReaderFromConfig
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

// NewGCSClient creates a *storage.Client from the given GCSConfig.
// When explicit credentials are provided they are used directly; otherwise
// Application Default Credentials are used, which work both inside GCP
// (metadata server, Workload Identity) and outside (env var, gcloud CLI).
func NewGCSClient(ctx context.Context, cfg GCSConfig) (*gcStorage.Client, error) {
	var opts []option.ClientOption

	switch {
	case len(cfg.CredentialsJSON) > 0:
		opts = append(opts, option.WithCredentialsJSON(cfg.CredentialsJSON))
	case cfg.CredentialsFile != "":
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := gcStorage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcs: create client: %w", err)
	}
	return client, nil
}

// NewGCSReaderFromConfig creates an ObjectReader for the given GCS object using
// the provided GCSConfig. This is a convenience wrapper that creates the GCS
// client and reader in one step. The caller must call Close to release the
// underlying client.
func NewGCSReaderFromConfig(ctx context.Context, cfg GCSConfig, bucket, object string, opts ...GCSOption) (*GCSReader, error) {
	client, err := NewGCSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	r := &GCSReader{
		client: client,
		bucket: client.Bucket(bucket),
		object: object,
		ctx:    ctx,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
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

// Close releases the underlying GCS client if one was created via
// NewGCSReaderFromConfig. For readers created with NewGCSReader this is a no-op.
func (r *GCSReader) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
