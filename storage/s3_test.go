package storage

import (
	"context"
	"testing"
)

func TestNewS3ClientRequiresRegion(t *testing.T) {
	_, err := NewS3Client(context.Background(), S3Config{})
	if err == nil {
		t.Fatal("expected error when Region is empty")
	}
}

func TestNewS3ClientStaticCredentials(t *testing.T) {
	cfg := S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Endpoint:        "http://localhost:9000",
		UsePathStyle:    true,
	}
	client, err := NewS3Client(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewS3ClientDefaultCredentials(t *testing.T) {
	cfg := S3Config{
		Region: "eu-west-1",
	}
	// Default credential chain won't fail at client creation time —
	// it fails lazily on first API call when no credentials are found.
	client, err := NewS3Client(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewS3ReaderFromConfigRequiresRegion(t *testing.T) {
	_, err := NewS3ReaderFromConfig(context.Background(), S3Config{}, "bucket", "key")
	if err == nil {
		t.Fatal("expected error when Region is empty")
	}
}

func TestNewS3ReaderFromConfigCreatesReader(t *testing.T) {
	cfg := S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Endpoint:        "http://localhost:9000",
		UsePathStyle:    true,
	}
	reader, err := NewS3ReaderFromConfig(context.Background(), cfg, "test-bucket", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reader == nil {
		t.Fatal("expected non-nil reader")
	}
	if reader.bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", reader.bucket, "test-bucket")
	}
	if reader.key != "test-key" {
		t.Errorf("key = %q, want %q", reader.key, "test-key")
	}
}

func TestNewS3ReaderFromConfigWithSessionToken(t *testing.T) {
	cfg := S3Config{
		Region:          "us-west-2",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "FwoGZXIvYXdzEBYaDNiq",
	}
	client, err := NewS3Client(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
