package storage

import (
	"context"
	"testing"
)

func TestNewGCSClientDefaultCredentials(t *testing.T) {
	// With no explicit credentials the client uses ADC.
	// This will fail when no default credentials are configured.
	client, err := NewGCSClient(context.Background(), GCSConfig{})
	if err != nil {
		t.Skipf("skipping: no default credentials available: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	client.Close()
}

func TestNewGCSClientWithCredentialsJSON(t *testing.T) {
	// Provide a minimal (invalid) service account JSON to test the code path.
	// The client is created successfully; authentication fails on first API call.
	fakeJSON := []byte(`{
		"type": "service_account",
		"project_id": "test",
		"private_key_id": "1",
		"private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MhgHcTz6sE2I2yPB\naFDrBz9vFqU6xqXKk1s8A+rLhCPl0MZaQfT+qPH7Go1nAPsTFoG7RBfHSsBeFBn\nhx0VwUu1EpMCpBSI52nMlKSiB5OGERdl7GlVgu0BKOQ30E5gTNxCPx1SHmUPVC3\nbtZ5DDl6mYpw7K/gGDz8ED4GLoYaR8tMeRRzPCTzpMYXqa5r0Yp0PVFnp8MPvwkT\nHWAaH8/eTPrvFY4IjJZ87g3zX3K+g8UWXIQJJbTBkNwV3aHsGkaMDgWR0ZXEQC+d\nY8MWNCO3p5RBkBR1yE5q5PdWOHqEUWDCiMbR8QIDAQABAKCAQEAlA/8CLoJt+i2n\nFEWnWzP6LQpHRNpGpmmAul5E7IgAcWcfKb7zj7gBIY2n/B0/vg7d/9r0j+JBfmYN\nhBQOh/TZ7dOu1AJwBFm9A7AL5CIibSOCvn5H/4JaR1AFcm7b8lQXV3aIEFZ4pFwd\nxPsVdRLKQ4eHEd3RMv0EB3dFjVD8GLgBbN3bLhfkHg6Vr2YEQf/CZfavpiPNtsdg\nhBzp0I46I6CPVKGHIGmzG8I2JOXCe5cEB20LqQs3TAWqmi1VEoD+5M5nJ8VQ8zDi\nF/Bx2KN/CqvDxOBH3b8Fxz7F9dMqeNFoH7dMe1uMoJ5G1F+vW1bdHNGSL+PZcA9\ng8PZpQKBgQD1mQ+QHR9VLAaLBntCjB8EMtZtLuYPOP8LOp+0UjcA0tMihtDPEOT2\n4IHNTPbNy+v6AJC2kSzFu3EXBRCWiNfdiYkH9TOxSJh0cBGgW8l4PR/qXCqQ1OQA\nRq8tnRYhVULXWYV+AqUbWyRKjjBLE2S2ckTSImhTMsmKbBfBN5l9WwKBgQDa8T2f\nFBNp/P4k+zr7Q0Mg6e3Fqp6tTsm4Gy0D1VPJGsMkv7JE+F+0E0HF8GE4y1DNPSP\ny/NhBYVJJLFXR59JE3TiAP6QE8UcJd0Rv6bNWmsvFSN/fCB3E7pOkPrHOP3Slt9F\nYaxG0zQDg5JiFd3b5WJChGXz/F/PP9N3FQ1b+wKBgQCxfRLLqYTNQHMptuqOb8V5\nG+sYJD24d+45IOB7rvAhHF0866hj/hMcbDE3JxPkvEI7Wul5jv7GEQ0LMEVzm/4s\nlkFOKlpFE6DVZsGJ5g6HzfIOC8WsJCBBzJhVi1GVj3rWT1niAPJn1RvjP1tEE2+y\nFwFjbBhBNtVDHl4WZQCxUwKBgQCjzV5Y3Hy5rKzUi7pDBpLOoGb+K/zGs5bJPVsp\nFJGZoLYfdRX6bCUbO/Bp5bGECMqHhsiE6vFGeDsIq6qs7B/zFmEfoYmJIQe/cFi1\nqwj9AWb/NAdPhw6Z0J8M7VG5sPF4MjkB6B0NzB+x5nYZpxM0OTBr3Bwro1KIWw2y\n/OKERwKBgQCUthU1b8RtIg7Cv9fGPGMJRT/fw3WYe1JKsi0j6lkJVtl/YlQz6Mao\nLT8hR7C0JMGkIBIyLqL0vCC1y9z/cUO6rDs8uMi5CBEC3mH5N+AFSBnHFBtoLHVd\nS8X3BqKVIte+P7GxIr7a96JL5x2b8p3jJL5lqXWiF/wJKMb/clOJAg==\n-----END RSA PRIVATE KEY-----\n",
		"client_email": "test@test.iam.gserviceaccount.com",
		"client_id": "123",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`)

	client, err := NewGCSClient(context.Background(), GCSConfig{
		CredentialsJSON: fakeJSON,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	client.Close()
}

func TestNewGCSReaderFromConfigCreatesReader(t *testing.T) {
	reader, err := NewGCSReaderFromConfig(context.Background(), GCSConfig{}, "test-bucket", "test-object")
	if err != nil {
		t.Skipf("skipping: no default credentials available: %v", err)
	}
	if reader == nil {
		t.Fatal("expected non-nil reader")
	}
	if reader.object != "test-object" {
		t.Errorf("object = %q, want %q", reader.object, "test-object")
	}
	// Close should release the internal client without error.
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewGCSReaderCloseIsNoOp(t *testing.T) {
	// A reader created via NewGCSReader (no internal client) should have
	// a no-op Close.
	r := NewGCSReader(nil, "obj")
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
