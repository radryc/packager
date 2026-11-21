package pipeline

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// EnvelopeMagic identifies PKGRv2 envelope-encrypted payloads.
	EnvelopeMagic = "PKGRv2"

	// EnvelopeKeySize is the required 32-byte key length for DEKs and KEKs.
	EnvelopeKeySize = chacha20poly1305.KeySize
)

// ErrUnknownKey indicates that a KeyProvider does not hold the requested key ID.
var ErrUnknownKey = errors.New("unknown key ID")

// KeyProvider wraps and unwraps Data Encryption Keys (DEKs) using a Key
// Encryption Key (KEK), such as an in-memory key ring or an external KMS.
type KeyProvider interface {
	// KeyID returns the identifier of the currently active key used for wrapping.
	KeyID() string
	// WrapDEK encrypts a plaintext DEK under the provider's active key.
	WrapDEK(ctx context.Context, dek []byte) ([]byte, error)
	// UnwrapDEK decrypts a wrapped DEK using the key identified by keyID.
	UnwrapDEK(ctx context.Context, keyID string, wrappedDEK []byte) ([]byte, error)
}

// StaticKeyProvider is a single-key in-memory KeyProvider using AES-256-GCM
// for KEK wrapping.
type StaticKeyProvider struct {
	keyID string
	aead  cipher.AEAD
}

// NewStaticKeyProvider creates a StaticKeyProvider for the given keyID and
// 32-byte KEK.
func NewStaticKeyProvider(keyID string, kek []byte) (*StaticKeyProvider, error) {
	if keyID == "" || len(keyID) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: invalid key ID length %d", ErrEncryption, len(keyID))
	}
	aead, err := newAES256GCM(kek)
	if err != nil {
		return nil, err
	}
	return &StaticKeyProvider{
		keyID: keyID,
		aead:  aead,
	}, nil
}

// KeyID returns the static key identifier.
func (p *StaticKeyProvider) KeyID() string {
	return p.keyID
}

// WrapDEK encrypts dek using the static AES-256-GCM KEK with keyID bound as AAD.
func (p *StaticKeyProvider) WrapDEK(ctx context.Context, dek []byte) ([]byte, error) {
	return wrapDEKWithAEAD(ctx, p.aead, p.keyID, dek)
}

// UnwrapDEK decrypts wrappedDEK if keyID matches the static key identifier.
func (p *StaticKeyProvider) UnwrapDEK(ctx context.Context, keyID string, wrappedDEK []byte) ([]byte, error) {
	if keyID != p.keyID {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, keyID)
	}
	return unwrapDEKWithAEAD(ctx, p.aead, keyID, wrappedDEK)
}

// KeyRingProvider is a concurrency-safe in-memory KeyProvider supporting
// multiple historical key IDs for unwrapping and one active key ID for wrapping.
type KeyRingProvider struct {
	mu          sync.RWMutex
	activeKeyID string
	keys        map[string]cipher.AEAD
}

// NewKeyRingProvider initializes a KeyRingProvider with an active key ID and an
// initial map of keyID -> 32-byte KEK.
func NewKeyRingProvider(activeKeyID string, keys map[string][]byte) (*KeyRingProvider, error) {
	if activeKeyID == "" || len(activeKeyID) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: invalid active key ID length %d", ErrEncryption, len(activeKeyID))
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: key ring cannot be empty", ErrEncryption)
	}

	ciphers := make(map[string]cipher.AEAD, len(keys))
	for id, kek := range keys {
		if id == "" || len(id) > math.MaxUint16 {
			return nil, fmt.Errorf("%w: invalid key ID length %d", ErrEncryption, len(id))
		}
		aead, err := newAES256GCM(kek)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", id, err)
		}
		ciphers[id] = aead
	}

	if _, ok := ciphers[activeKeyID]; !ok {
		return nil, fmt.Errorf("%w: active key ID %q not present in key ring", ErrUnknownKey, activeKeyID)
	}

	return &KeyRingProvider{
		activeKeyID: activeKeyID,
		keys:        ciphers,
	}, nil
}

// KeyID returns the currently active key ID used for wrapping new DEKs.
func (p *KeyRingProvider) KeyID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeKeyID
}

// WrapDEK encrypts dek using the currently active key.
func (p *KeyRingProvider) WrapDEK(ctx context.Context, dek []byte) ([]byte, error) {
	p.mu.RLock()
	activeID := p.activeKeyID
	aead := p.keys[activeID]
	p.mu.RUnlock()

	if aead == nil {
		return nil, fmt.Errorf("%w: active key %q missing", ErrUnknownKey, activeID)
	}
	return wrapDEKWithAEAD(ctx, aead, activeID, dek)
}

// UnwrapDEK decrypts wrappedDEK using any key ID present in the ring.
func (p *KeyRingProvider) UnwrapDEK(ctx context.Context, keyID string, wrappedDEK []byte) ([]byte, error) {
	p.mu.RLock()
	aead := p.keys[keyID]
	p.mu.RUnlock()

	if aead == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, keyID)
	}
	return unwrapDEKWithAEAD(ctx, aead, keyID, wrappedDEK)
}

// Rotate adds newKEK under newKeyID (if not already present) and promotes
// newKeyID to be the active wrapping key while retaining all previous keys for
// unwrapping historical envelopes.
func (p *KeyRingProvider) Rotate(newKeyID string, newKEK []byte) error {
	if newKeyID == "" || len(newKeyID) > math.MaxUint16 {
		return fmt.Errorf("%w: invalid key ID length %d", ErrEncryption, len(newKeyID))
	}
	aead, err := newAES256GCM(newKEK)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys[newKeyID] = aead
	p.activeKeyID = newKeyID
	return nil
}

// AddKey registers a historical or staged key without changing the active key.
func (p *KeyRingProvider) AddKey(keyID string, kek []byte) error {
	if keyID == "" || len(keyID) > math.MaxUint16 {
		return fmt.Errorf("%w: invalid key ID length %d", ErrEncryption, len(keyID))
	}
	aead, err := newAES256GCM(kek)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys[keyID] = aead
	return nil
}

// EnvelopeOption configures optional behavior for DecryptEnvelope.
type EnvelopeOption func(*envelopeConfig)

type envelopeConfig struct {
	legacyKey      []byte
	legacyPipeline *Pipeline
}

// WithLegacyKey configures a 32-byte v1 ChaCha20-Poly1305 key for automatic
// fallback when DecryptEnvelope encounters a payload without the PKGRv2 magic.
func WithLegacyKey(key []byte) EnvelopeOption {
	return func(c *envelopeConfig) {
		if len(key) > 0 {
			cp := make([]byte, len(key))
			copy(cp, key)
			c.legacyKey = cp
		}
	}
}

// WithLegacyPipeline configures an existing v1 Pipeline for automatic fallback
// when DecryptEnvelope encounters a payload without the PKGRv2 magic.
func WithLegacyPipeline(p *Pipeline) EnvelopeOption {
	return func(c *envelopeConfig) {
		c.legacyPipeline = p
	}
}

// EncryptEnvelope generates a fresh random 32-byte DEK, wraps it using provider,
// and encrypts plaintext with ChaCha20-Poly1305 binding keyID || aad as AEAD
// associated data. The returned bytes use the PKGRv2 binary envelope format:
//
//	magic (6B "PKGRv2") | keyIDLen (2B BE) | keyID | wrappedDEKLen (2B BE) | wrappedDEK | nonce (12B) | ciphertext
func EncryptEnvelope(ctx context.Context, provider KeyProvider, aad, plaintext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("%w: nil key provider", ErrEncryption)
	}

	keyID := provider.KeyID()
	if keyID == "" || len(keyID) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: invalid key ID length %d", ErrEncryption, len(keyID))
	}

	dek := make([]byte, EnvelopeKeySize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}
	defer clear(dek)

	wrappedDEK, err := provider.WrapDEK(ctx, dek)
	if err != nil {
		return nil, err
	}
	if len(wrappedDEK) == 0 || len(wrappedDEK) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: invalid wrapped DEK length %d", ErrEncryption, len(wrappedDEK))
	}

	aead, err := chacha20poly1305.New(dek)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}

	headerLen := len(EnvelopeMagic) + 2 + len(keyID) + 2 + len(wrappedDEK) + len(nonce)
	out := make([]byte, headerLen, headerLen+len(plaintext)+aead.Overhead())

	pos := 0
	pos += copy(out[pos:], EnvelopeMagic)
	binary.BigEndian.PutUint16(out[pos:pos+2], uint16(len(keyID)))
	pos += 2
	pos += copy(out[pos:], keyID)
	binary.BigEndian.PutUint16(out[pos:pos+2], uint16(len(wrappedDEK)))
	pos += 2
	pos += copy(out[pos:], wrappedDEK)
	copy(out[pos:], nonce)

	boundAAD := buildEnvelopeAAD(keyID, aad)
	return aead.Seal(out, nonce, plaintext, boundAAD), nil
}

// DecryptEnvelope parses a PKGRv2 envelope, unwraps the DEK via provider, and
// decrypts the payload while verifying keyID || aad. If ciphertext does not
// begin with the PKGRv2 magic and a legacy v1 key or Pipeline is configured via
// options, it falls back to v1 ChaCha20-Poly1305 decryption.
func DecryptEnvelope(ctx context.Context, provider KeyProvider, aad, ciphertext []byte, opts ...EnvelopeOption) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var cfg envelopeConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	defer clear(cfg.legacyKey)

	if len(ciphertext) < len(EnvelopeMagic) || !bytes.Equal(ciphertext[:len(EnvelopeMagic)], []byte(EnvelopeMagic)) {
		return decryptLegacyV1(ciphertext, &cfg)
	}

	if provider == nil {
		return nil, fmt.Errorf("%w: nil key provider", ErrDecryption)
	}

	pos := len(EnvelopeMagic)
	if len(ciphertext) < pos+2 {
		return nil, fmt.Errorf("%w: truncated envelope keyID length", ErrDecryption)
	}
	keyIDLen := int(binary.BigEndian.Uint16(ciphertext[pos : pos+2]))
	pos += 2
	if keyIDLen == 0 || len(ciphertext) < pos+keyIDLen+2 {
		return nil, fmt.Errorf("%w: invalid or truncated envelope keyID", ErrDecryption)
	}
	keyID := string(ciphertext[pos : pos+keyIDLen])
	pos += keyIDLen

	wrappedDEKLen := int(binary.BigEndian.Uint16(ciphertext[pos : pos+2]))
	pos += 2
	if wrappedDEKLen == 0 || len(ciphertext) < pos+wrappedDEKLen+chacha20poly1305.NonceSize {
		return nil, fmt.Errorf("%w: invalid or truncated envelope wrappedDEK", ErrDecryption)
	}
	wrappedDEK := ciphertext[pos : pos+wrappedDEKLen]
	pos += wrappedDEKLen

	nonce := ciphertext[pos : pos+chacha20poly1305.NonceSize]
	sealed := ciphertext[pos+chacha20poly1305.NonceSize:]

	dek, err := provider.UnwrapDEK(ctx, keyID, wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	if len(dek) != EnvelopeKeySize {
		return nil, fmt.Errorf("%w: unwrapped DEK has invalid length %d", ErrDecryption, len(dek))
	}

	aead, err := chacha20poly1305.New(dek)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
	}

	boundAAD := buildEnvelopeAAD(keyID, aad)
	plaintext, err := aead.Open(nil, nonce, sealed, boundAAD)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
	}
	return plaintext, nil
}

func decryptLegacyV1(ciphertext []byte, cfg *envelopeConfig) ([]byte, error) {
	if cfg.legacyPipeline != nil {
		return cfg.legacyPipeline.Decrypt(ciphertext)
	}
	if cfg.legacyKey != nil {
		if len(cfg.legacyKey) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("%w: legacy key must be %d bytes", ErrDecryption, chacha20poly1305.KeySize)
		}
		aead, err := chacha20poly1305.New(cfg.legacyKey)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
		}
		nonceSize := aead.NonceSize()
		if len(ciphertext) < nonceSize {
			return nil, fmt.Errorf("%w: payload too short to contain nonce", ErrDecryption)
		}
		out, err := aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: invalid envelope magic and no legacy key configured", ErrDecryption)
}

func newAES256GCM(kek []byte) (cipher.AEAD, error) {
	if len(kek) != EnvelopeKeySize {
		return nil, fmt.Errorf("%w: KEK must be exactly %d bytes", ErrEncryption, EnvelopeKeySize)
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}
	return aead, nil
}

func wrapDEKWithAEAD(ctx context.Context, aead cipher.AEAD, keyID string, dek []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(dek) == 0 {
		return nil, fmt.Errorf("%w: DEK cannot be empty", ErrEncryption)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}
	return aead.Seal(nonce, nonce, dek, []byte(keyID)), nil
}

func unwrapDEKWithAEAD(ctx context.Context, aead cipher.AEAD, keyID string, wrappedDEK []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(wrappedDEK) < nonceSize+aead.Overhead() {
		return nil, fmt.Errorf("%w: wrapped DEK too short", ErrDecryption)
	}
	nonce, ct := wrappedDEK[:nonceSize], wrappedDEK[nonceSize:]
	dek, err := aead.Open(nil, nonce, ct, []byte(keyID))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
	}
	return dek, nil
}

// buildEnvelopeAAD domain-separates keyID and aad so boundary shifts between
// keyID and aad cannot collide.
func buildEnvelopeAAD(keyID string, aad []byte) []byte {
	out := make([]byte, 2+len(keyID)+len(aad))
	binary.BigEndian.PutUint16(out[:2], uint16(len(keyID)))
	copy(out[2:], keyID)
	copy(out[2+len(keyID):], aad)
	return out
}
