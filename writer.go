package packager

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/radryc/packager/pipeline"
)

// AddFileOptions controls per-file behaviour when adding a file to an archive.
type AddFileOptions struct {
	// Permission stores Unix permission bits (e.g. 0644, 0755).
	Permission uint32
	// OwnerUID is the numeric user ID of the file owner.
	OwnerUID int
	// Encrypt controls whether the file data is AEAD-encrypted.
	// Defaults to true when using the zero value.
	Encrypt bool
	// ForceCompress overrides automatic pre-compressed detection.
	// nil  → auto-detect (skip compression for already-compressed formats).
	// true → always compress.  false → never compress.
	ForceCompress *bool
}

// DefaultAddFileOptions returns sensible defaults: 0644 perms, UID 0,
// encryption enabled, auto-detect compression.
func DefaultAddFileOptions() AddFileOptions {
	return AddFileOptions{
		Permission: 0644,
		OwnerUID:   0,
		Encrypt:    true,
	}
}

// ArchiveWriter builds a WORM archive sequentially. Files are packed and
// appended one at a time. Call Close to finalise the archive (writes the
// encrypted master index and 8-byte footer).
type ArchiveWriter struct {
	writer        io.Writer
	pipeline      *pipeline.Pipeline
	index         *PathIndex
	currentOffset int64
}

// NewArchiveWriter creates a new archive writer that writes to w.
func NewArchiveWriter(w io.Writer, p *pipeline.Pipeline) *ArchiveWriter {
	return &ArchiveWriter{
		writer:        w,
		pipeline:      p,
		index:         NewPathIndex(),
		currentOffset: 0,
	}
}

// AddFile compresses, optionally encrypts, and appends a file to the archive.
// The filepath (full path including directories) is used as the index key.
// Compression is automatically skipped for files that are already in a
// compressed format (detected by extension and magic bytes) unless overridden
// via opts.ForceCompress.
func (aw *ArchiveWriter) AddFile(filepath string, rawData []byte, opts AddFileOptions) error {
	// Determine whether to compress
	compress := true
	if opts.ForceCompress != nil {
		compress = *opts.ForceCompress
	} else {
		if IsPreCompressed(filepath, rawData) {
			compress = false
		}
	}

	// Pack (compress + encrypt based on flags)
	packedData, err := aw.pipeline.Pack(rawData, compress, opts.Encrypt)
	if err != nil {
		return fmt.Errorf("pack %q: %w", filepath, err)
	}

	n, err := aw.writer.Write(packedData)
	if err != nil {
		return fmt.Errorf("write %q: %w", filepath, err)
	}

	aw.index.Put(filepath, FileEntry{
		Offset:       aw.currentOffset,
		Size:         int64(n),
		Permission:   opts.Permission,
		OwnerUID:     opts.OwnerUID,
		IsEncrypted:  opts.Encrypt,
		IsCompressed: compress,
	})

	aw.currentOffset += int64(n)
	return nil
}

// Close finalises the archive by writing the encrypted master index and
// the 8-byte little-endian footer. The master index is always compressed
// and encrypted regardless of per-file settings.
func (aw *ArchiveWriter) Close() error {
	// Serialise the index to JSON
	indexBytes, err := json.Marshal(aw.index)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	// The index is always compressed + encrypted
	packedIndex, err := aw.pipeline.Pack(indexBytes, true, true)
	if err != nil {
		return fmt.Errorf("pack index: %w", err)
	}

	indexSize, err := aw.writer.Write(packedIndex)
	if err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Write the 8-byte LE footer containing the packed index size
	footer := make([]byte, 8)
	binary.LittleEndian.PutUint64(footer, uint64(indexSize))
	if _, err := aw.writer.Write(footer); err != nil {
		return fmt.Errorf("write footer: %w", err)
	}

	return nil
}
