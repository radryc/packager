package packager

import (
	"path/filepath"
	"strings"
)

// compressedExtensions lists file extensions that are already compressed.
// Adding a file with one of these extensions will automatically skip zstd
// compression (unless explicitly overridden via AddFileOptions.ForceCompress).
var compressedExtensions = map[string]bool{
	// Archives
	".zip": true, ".gz": true, ".bz2": true, ".xz": true,
	".zst": true, ".rar": true, ".7z": true, ".lz4": true,
	".lzma": true, ".tar.gz": true, ".tgz": true,
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".heic": true,
	// Audio
	".mp3": true, ".aac": true, ".ogg": true, ".flac": true,
	".opus": true, ".wma": true,
	// Video
	".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
	".webm": true, ".wmv": true,
	// Fonts
	".woff": true, ".woff2": true,
	// Other
	".br": true, // Brotli
}

// magicSignatures maps byte prefixes to a description (unused but kept for
// clarity). The presence of a matching prefix indicates the data is already
// in a compressed or otherwise non-compressible format.
type magicSig struct {
	offset int
	magic  []byte
}

var magicSignatures = []magicSig{
	{0, []byte{0x50, 0x4B}},                   // ZIP / DOCX / XLSX / JAR
	{0, []byte{0x89, 0x50, 0x4E, 0x47}},       // PNG
	{0, []byte{0x47, 0x49, 0x46, 0x38}},       // GIF87a / GIF89a
	{0, []byte{0xFF, 0xD8, 0xFF}},             // JPEG
	{0, []byte{0x1F, 0x8B}},                   // gzip
	{0, []byte{0x42, 0x5A}},                   // bzip2
	{0, []byte{0x28, 0xB5, 0x2F, 0xFD}},       // zstd
	{0, []byte{0xFD, 0x37, 0x7A, 0x58, 0x5A}}, // xz
	{0, []byte{0x52, 0x61, 0x72, 0x21}},       // RAR
	{0, []byte{0x37, 0x7A, 0xBC, 0xAF}},       // 7z
	{0, []byte{0x04, 0x22, 0x4D, 0x18}},       // LZ4
	{0, []byte{0x52, 0x49, 0x46, 0x46}},       // RIFF (WebP, AVI)
	{0, []byte{0x4F, 0x67, 0x67, 0x53}},       // OGG
	{0, []byte{0x66, 0x4C, 0x61, 0x43}},       // FLAC
	{4, []byte{0x66, 0x74, 0x79, 0x70}},       // MP4/MOV (ftyp at offset 4)
	{0, []byte{0x49, 0x44, 0x33}},             // MP3 (ID3 tag)
	{0, []byte{0xFF, 0xFB}},                   // MP3 (frame sync)
	{0, []byte{0xFF, 0xF3}},                   // MP3 (frame sync variant)
	{0, []byte{0xFF, 0xF2}},                   // MP3 (frame sync variant)
	{0, []byte{0x77, 0x4F, 0x46, 0x46}},       // WOFF
	{0, []byte{0x77, 0x4F, 0x46, 0x32}},       // WOFF2
	{0, []byte{0x1A, 0x45, 0xDF, 0xA3}},       // Matroska/WebM
}

// IsPreCompressed returns true if the file is detected as already compressed
// (or otherwise non-compressible), based on its extension and/or magic bytes.
// Either check matching is sufficient — this catches both correctly-named and
// mis-named/extensionless files.
func IsPreCompressed(filename string, data []byte) bool {
	// 1. Extension-based check
	ext := strings.ToLower(filepath.Ext(filename))
	if compressedExtensions[ext] {
		return true
	}

	// 2. Magic-byte check
	for _, sig := range magicSignatures {
		end := sig.offset + len(sig.magic)
		if len(data) >= end {
			match := true
			for i, b := range sig.magic {
				if data[sig.offset+i] != b {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}

	return false
}
