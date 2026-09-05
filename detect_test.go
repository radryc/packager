package packager

import "testing"

func TestIsPreCompressedByExtension(t *testing.T) {
	compressed := []string{
		"archive.zip", "photo.PNG", "image.jpg", "anim.gif",
		"video.mp4", "sound.mp3", "font.woff2", "data.gz",
		"backup.rar", "archive.7z", "movie.mkv",
	}
	for _, name := range compressed {
		if !IsPreCompressed(name, nil) {
			t.Errorf("expected %q to be detected as pre-compressed", name)
		}
	}
}

func TestIsPreCompressedByMagicBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"PNG magic", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}},
		{"JPEG magic", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}},
		{"GIF magic", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}},
		{"ZIP magic", []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00}},
		{"gzip magic", []byte{0x1F, 0x8B, 0x08, 0x00, 0x00, 0x00}},
		{"zstd magic", []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x00}},
		{"MP4 ftyp", []byte{0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70}},
		{"MP3 ID3", []byte{0x49, 0x44, 0x33, 0x04, 0x00, 0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a generic extension so only magic bytes trigger detection
			if !IsPreCompressed("noext", tt.data) {
				t.Errorf("expected magic-byte detection for %s", tt.name)
			}
		})
	}
}

func TestIsNotPreCompressed(t *testing.T) {
	notCompressed := []struct {
		name string
		data []byte
	}{
		{"text file", []byte("hello world, this is plain text")},
		{"go source", []byte("package main\n\nfunc main() {}")},
		{"json", []byte(`{"key": "value"}`)},
		{"empty", []byte{}},
	}
	for _, tt := range notCompressed {
		t.Run(tt.name, func(t *testing.T) {
			filename := "file.txt"
			if tt.name == "go source" {
				filename = "main.go"
			} else if tt.name == "json" {
				filename = "data.json"
			}
			if IsPreCompressed(filename, tt.data) {
				t.Errorf("expected %q to NOT be detected as pre-compressed", tt.name)
			}
		})
	}
}
