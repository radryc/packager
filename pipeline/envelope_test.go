package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestEnvelopeRoundTripStaticProvider(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticKeyProvider("k1", testKey())
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	aad := []byte("archive=logs/2026-11-18.pack;tenant=acme")
	plaintext := []byte("sensitive payload protected by PKGRv2 envelope encryption")

	env, err := EncryptEnvelope(ctx, provider, aad, plaintext)
	if err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	if !bytes.HasPrefix(env, []byte(EnvelopeMagic)) {
		t.Fatalf("expected PKGRv2 magic prefix, got %q", env[:min(len(env), 6)])
	}
	if bytes.Contains(env, plaintext) {
		t.Fatal("envelope must not contain plaintext")
	}

	got, err := DecryptEnvelope(ctx, provider, aad, env)
	if err != nil {
		t.Fatalf("DecryptEnvelope: %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEnvelopeEmptyPlaintextAndNilAAD(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticKeyProvider("k-empty", testKey())
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	env, err := EncryptEnvelope(ctx, provider, nil, []byte{})
	if err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}
	got, err := DecryptEnvelope(ctx, provider, nil, env)
	if err != nil {
		t.Fatalf("DecryptEnvelope: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(got))
	}
}

func TestEnvelopeKeyRotation(t *testing.T) {
	ctx := context.Background()
	k1 := testKey()
	k2 := testKey()

	ring, err := NewKeyRingProvider("k1", map[string][]byte{"k1": k1})
	if err != nil {
		t.Fatalf("NewKeyRingProvider: %v", err)
	}

	aad := []byte("entry=001")
	msg1 := []byte("encrypted under k1")
	env1, err := EncryptEnvelope(ctx, ring, aad, msg1)
	if err != nil {
		t.Fatalf("EncryptEnvelope(k1): %v", err)
	}
	if gotID := extractEnvelopeKeyID(t, env1); gotID != "k1" {
		t.Fatalf("env1 keyID = %q, want %q", gotID, "k1")
	}

	// Rotate active key from k1 -> k2.
	if err := ring.Rotate("k2", k2); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if ring.KeyID() != "k2" {
		t.Fatalf("ring.KeyID() = %q, want %q", ring.KeyID(), "k2")
	}

	msg2 := []byte("encrypted under k2")
	env2, err := EncryptEnvelope(ctx, ring, aad, msg2)
	if err != nil {
		t.Fatalf("EncryptEnvelope(k2): %v", err)
	}
	if gotID := extractEnvelopeKeyID(t, env2); gotID != "k2" {
		t.Fatalf("env2 keyID = %q, want %q", gotID, "k2")
	}

	// Historical k1 envelope must still decrypt after rotation.
	got1, err := DecryptEnvelope(ctx, ring, aad, env1)
	if err != nil {
		t.Fatalf("DecryptEnvelope(env1 after rotation): %v", err)
	}
	if !bytes.Equal(msg1, got1) {
		t.Fatalf("env1 plaintext mismatch: got %q, want %q", got1, msg1)
	}

	// New k2 envelope must decrypt.
	got2, err := DecryptEnvelope(ctx, ring, aad, env2)
	if err != nil {
		t.Fatalf("DecryptEnvelope(env2): %v", err)
	}
	if !bytes.Equal(msg2, got2) {
		t.Fatalf("env2 plaintext mismatch: got %q, want %q", got2, msg2)
	}

	// A provider without k1 must fail to decrypt env1 with ErrUnknownKey.
	onlyK2, err := NewStaticKeyProvider("k2", k2)
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}
	_, err = DecryptEnvelope(ctx, onlyK2, aad, env1)
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("expected ErrUnknownKey for env1 on k2-only provider, got %v", err)
	}
}

func TestEnvelopeAADTamperingDetection(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticKeyProvider("k1", testKey())
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	env, err := EncryptEnvelope(ctx, provider, []byte("expected-aad"), []byte("top secret"))
	if err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}

	_, err = DecryptEnvelope(ctx, provider, []byte("wrong-aad"), env)
	if !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption on mismatched AAD, got %v", err)
	}

	_, err = DecryptEnvelope(ctx, provider, nil, env)
	if !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption on nil AAD, got %v", err)
	}
}

func TestEnvelopeCiphertextTamperingDetection(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticKeyProvider("k1", testKey())
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	aad := []byte("meta")
	env, err := EncryptEnvelope(ctx, provider, aad, []byte("tamper test payload"))
	if err != nil {
		t.Fatalf("EncryptEnvelope: %v", err)
	}

	// Flip a byte in the sealed ciphertext.
	tamperedCT := bytes.Clone(env)
	tamperedCT[len(tamperedCT)-1] ^= 0xFF
	if _, err := DecryptEnvelope(ctx, provider, aad, tamperedCT); !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption for tampered ciphertext, got %v", err)
	}

	// Flip a byte inside the wrapped DEK.
	// Header: 6B magic + 2B keyIDLen + 2B ("k1") + 2B wrappedDEKLen -> wrappedDEK starts at offset 12.
	tamperedDEK := bytes.Clone(env)
	tamperedDEK[12] ^= 0x01
	if _, err := DecryptEnvelope(ctx, provider, aad, tamperedDEK); !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption for tampered wrappedDEK, got %v", err)
	}

	// Truncated envelope must fail cleanly.
	if _, err := DecryptEnvelope(ctx, provider, aad, env[:len(EnvelopeMagic)+3]); !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption for truncated envelope, got %v", err)
	}
}

func TestEnvelopeLegacyV1Fallback(t *testing.T) {
	ctx := context.Background()
	legacyKey := testKey()
	p, err := NewPipeline(legacyKey)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	plaintext := []byte("legacy v1 encrypted entry")
	v1Ciphertext, err := p.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Pipeline.Encrypt: %v", err)
	}

	provider, err := NewStaticKeyProvider("k2", testKey())
	if err != nil {
		t.Fatalf("NewStaticKeyProvider: %v", err)
	}

	// Without a legacy key configured, v1 payload must fail.
	if _, err := DecryptEnvelope(ctx, provider, nil, v1Ciphertext); !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected ErrDecryption without legacy key, got %v", err)
	}

	// With WithLegacyKey, v1 payload decrypts transparently.
	got, err := DecryptEnvelope(ctx, provider, []byte("ignored-for-v1"), v1Ciphertext, WithLegacyKey(legacyKey))
	if err != nil {
		t.Fatalf("DecryptEnvelope(WithLegacyKey): %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("legacy v1 fallback mismatch: got %q, want %q", got, plaintext)
	}

	// With WithLegacyPipeline, v1 payload also decrypts transparently.
	got, err = DecryptEnvelope(ctx, provider, nil, v1Ciphertext, WithLegacyPipeline(p))
	if err != nil {
		t.Fatalf("DecryptEnvelope(WithLegacyPipeline): %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("legacy v1 pipeline fallback mismatch: got %q, want %q", got, plaintext)
	}
}

func extractEnvelopeKeyID(t *testing.T, env []byte) string {
	t.Helper()
	if len(env) < len(EnvelopeMagic)+2 {
		t.Fatalf("envelope too short: %d", len(env))
	}
	keyIDLen := int(binary.BigEndian.Uint16(env[len(EnvelopeMagic) : len(EnvelopeMagic)+2]))
	start := len(EnvelopeMagic) + 2
	if len(env) < start+keyIDLen {
		t.Fatalf("envelope truncated before keyID end")
	}
	return string(env[start : start+keyIDLen])
}
