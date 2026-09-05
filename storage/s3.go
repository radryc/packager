package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds configuration for creating an S3 client.
//
// Credential resolution order:
//  1. If AccessKeyID and SecretAccessKey are set, static credentials are used.
//  2. Otherwise the default AWS credential chain is used, which covers:
//     - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//     - Shared credentials file (~/.aws/credentials)
//     - IAM role for EC2 / ECS task role / EKS IRSA (when running in AWS)
//
// Set Endpoint for S3-compatible services (MinIO, Ceph, etc.).
type S3Config struct {
	// Region is the AWS region (e.g. "us-east-1"). Required.
	Region string
	// Endpoint overrides the default S3 endpoint URL.
	// Use this for S3-compatible services like MinIO.
	Endpoint string
	// AccessKeyID for static credentials. Leave empty to use the default chain.
	AccessKeyID string
	// SecretAccessKey for static credentials.
	SecretAccessKey string
	// SessionToken is an optional session token for temporary credentials.
	SessionToken string
	// UsePathStyle forces path-style addressing (bucket in path, not subdomain).
	// Required for most S3-compatible services.
	UsePathStyle bool
}

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

// NewS3Client creates an *s3.Client from the given S3Config.
// When static credentials are provided they are used directly; otherwise
// the default AWS credential chain is used (env vars, shared config,
// IAM role, etc.) which works both inside and outside of AWS.
func NewS3Client(ctx context.Context, cfg S3Config) (*s3.Client, error) {
	if cfg.Region == "" {
		return nil, errors.New("s3: Region is required")
	}

	var optFns []func(*config.LoadOptions) error
	optFns = append(optFns, config.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		optFns = append(optFns, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	var s3OptFns []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3OptFns = append(s3OptFns, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}
	if cfg.UsePathStyle {
		s3OptFns = append(s3OptFns, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(awsCfg, s3OptFns...), nil
}

// NewS3ReaderFromConfig creates an ObjectReader for the given S3 object using
// the provided S3Config. This is a convenience wrapper that creates the S3
// client and reader in one step.
func NewS3ReaderFromConfig(ctx context.Context, cfg S3Config, bucket, key string, opts ...S3Option) (*S3Reader, error) {
	client, err := NewS3Client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewS3Reader(client, bucket, key, append([]S3Option{WithS3Context(ctx)}, opts...)...), nil
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
