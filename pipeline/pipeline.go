// Package pipeline provides compression and encryption primitives for the
// packager archive format. It uses zstd for ultra-fast compression and
// ChaCha20-Poly1305 (AEAD) for authenticated encryption.
package pipeline

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/chacha20poly1305"
)

// Pipeline manages shared resources for compression and encryption.
// The zstd encoder/decoder and AEAD cipher are created once and reused.
type Pipeline struct {
	aead    cipher.AEAD
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

// NewPipeline initialises the encryption cipher and zstd codec instances.
// key must be exactly 32 bytes for ChaCha20-Poly1305.
func NewPipeline(key []byte) (*Pipeline, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, errors.New("key must be exactly 32 bytes")
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}

	return &Pipeline{
		aead:    aead,
		encoder: encoder,
		decoder: decoder,
	}, nil
}

// ---------------------------------------------------------------------------
// Low-level primitives
// ---------------------------------------------------------------------------

// Compress applies zstd compression to data.
func (p *Pipeline) Compress(data []byte) []byte {
	return p.encoder.EncodeAll(data, make([]byte, 0, len(data)))
}

// Decompress reverses zstd compression.
func (p *Pipeline) Decompress(data []byte) ([]byte, error) {
	return p.decoder.DecodeAll(data, nil)
}

// Encrypt applies ChaCha20-Poly1305 AEAD encryption.
// A random 12-byte nonce is prepended to the ciphertext.
func (p *Pipeline) Encrypt(data []byte) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Seal appends ciphertext to dst (nonce). This efficiently prepends the
	// nonce to the ciphertext in a single contiguous allocation.
	return p.aead.Seal(nonce, nonce, data, nil), nil
}

// Decrypt reverses Encrypt – splits the nonce, decrypts and authenticates.
func (p *Pipeline) Decrypt(data []byte) ([]byte, error) {
	nonceSize := p.aead.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("payload too short to contain nonce")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return p.aead.Open(nil, nonce, ciphertext, nil)
}

// ---------------------------------------------------------------------------
// High-level composites
// ---------------------------------------------------------------------------

// Pack compresses (if compress==true) then encrypts (if encrypt==true) rawData.
func (p *Pipeline) Pack(rawData []byte, compress, encrypt bool) ([]byte, error) {
	out := rawData
	if compress {
		out = p.Compress(out)
	}
	if encrypt {
		var err error
		out, err = p.Encrypt(out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Unpack decrypts (if encrypted==true) then decompresses (if compressed==true).
func (p *Pipeline) Unpack(payload []byte, compressed, encrypted bool) ([]byte, error) {
	out := payload
	if encrypted {
		var err error
		out, err = p.Decrypt(out)
		if err != nil {
			return nil, err
		}
	}
	if compressed {
		var err error
		out, err = p.Decompress(out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
