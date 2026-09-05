package pipeline

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

func TestNewPipelineInvalidKey(t *testing.T) {
	_, err := NewPipeline([]byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestCompressDecompress(t *testing.T) {
	p, err := NewPipeline(testKey())
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("hello world, this is a test of the compression pipeline")
	compressed := p.Compress(original)
	decompressed, err := p.Decompress(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, decompressed) {
		t.Fatalf("round-trip failed: got %q", decompressed)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	p, err := NewPipeline(testKey())
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("secret data that must be protected")
	encrypted, err := p.Encrypt(original)
	if err != nil {
		t.Fatal(err)
	}
	// Encrypted data must differ from original
	if bytes.Equal(original, encrypted) {
		t.Fatal("encrypted data is identical to original")
	}
	decrypted, err := p.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, decrypted) {
		t.Fatalf("round-trip failed: got %q", decrypted)
	}
}

func TestDecryptTamperedData(t *testing.T) {
	p, err := NewPipeline(testKey())
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := p.Encrypt([]byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the ciphertext
	encrypted[len(encrypted)-1] ^= 0xFF
	_, err = p.Decrypt(encrypted)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestPackUnpackAllModes(t *testing.T) {
	p, err := NewPipeline(testKey())
	if err != nil {
		t.Fatal(err)
	}

	original := []byte("the quick brown fox jumps over the lazy dog, repeatedly and at length to ensure good compression ratios")

	modes := []struct {
		name     string
		compress bool
		encrypt  bool
	}{
		{"compress+encrypt", true, true},
		{"compress-only", true, false},
		{"encrypt-only", false, true},
		{"neither", false, false},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			packed, err := p.Pack(original, m.compress, m.encrypt)
			if err != nil {
				t.Fatalf("Pack: %v", err)
			}
			unpacked, err := p.Unpack(packed, m.compress, m.encrypt)
			if err != nil {
				t.Fatalf("Unpack: %v", err)
			}
			if !bytes.Equal(original, unpacked) {
				t.Fatalf("round-trip failed")
			}
		})
	}
}

func TestDecryptShortPayload(t *testing.T) {
	p, err := NewPipeline(testKey())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Decrypt([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for short payload")
	}
}
