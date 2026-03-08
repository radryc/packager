package packager

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"

	"github.com/radryc/packager/pipeline"
	"github.com/radryc/packager/storage"
)

// ArchiveReader provides O(1) random-access to individual files within a
// sealed archive. It reads the footer and index on open, then fetches only
// the exact byte range needed for each requested file.
type ArchiveReader struct {
	store    storage.ObjectReader
	pipeline *pipeline.Pipeline
	index    *PathIndex
}

// openConfig holds optional parameters for OpenArchive.
type openConfig struct {
	indexSize int64
}

// OpenOption configures optional behaviour when opening an archive.
type OpenOption func(*openConfig)

// WithIndexSize provides a pre-cached index size, eliminating the need to
// fetch the 8-byte footer from storage. This is useful when the index size
// has been recorded in a database alongside the object key.
func WithIndexSize(size int64) OpenOption {
	return func(c *openConfig) { c.indexSize = size }
}

// OpenArchive reads and decrypts the master index from the archive stored in
// store. The footer (last 8 bytes) is read first to locate the index, unless
// WithIndexSize is provided. The index is always treated as compressed and
// encrypted.
func OpenArchive(store storage.ObjectReader, p *pipeline.Pipeline, opts ...OpenOption) (*ArchiveReader, error) {
	config := &openConfig{}
	for _, opt := range opts {
		opt(config)
	}

	totalSize, err := store.Size()
	if err != nil {
		return nil, err
	}
	if totalSize < 8 {
		return nil, errors.New("invalid archive: too small to contain footer")
	}

	indexSize := config.indexSize
	if indexSize <= 0 {
		footer := make([]byte, 8)
		if _, err := store.ReadAt(footer, totalSize-8); err != nil {
			return nil, err
		}
		indexSize = int64(binary.LittleEndian.Uint64(footer))
	}

	if indexSize <= 0 || indexSize > totalSize-8 {
		return nil, errors.New("invalid archive: index size out of range")
	}

	indexOffset := totalSize - 8 - indexSize
	encryptedIndex := make([]byte, indexSize)
	if _, err := store.ReadAt(encryptedIndex, indexOffset); err != nil {
		return nil, err
	}

	// Index is always compressed + encrypted
	rawIndex, err := p.Unpack(encryptedIndex, true, true)
	if err != nil {
		return nil, err
	}

	index := NewPathIndex()
	if err := json.Unmarshal(rawIndex, index); err != nil {
		return nil, err
	}

	return &ArchiveReader{store: store, pipeline: p, index: index}, nil
}

// GetFile retrieves and unpacks a single file from the archive.
// It returns the raw file data and the associated FileEntry metadata.
// Returns os.ErrNotExist if the file is not in the index or has been deleted.
func (ar *ArchiveReader) GetFile(filepath string) ([]byte, *FileEntry, error) {
	entry, exists := ar.index.Get(filepath)
	if !exists || entry.IsDeleted {
		return nil, nil, os.ErrNotExist
	}

	encryptedData := make([]byte, entry.Size)
	if _, err := ar.store.ReadAt(encryptedData, entry.Offset); err != nil {
		return nil, nil, err
	}

	rawData, err := ar.pipeline.Unpack(encryptedData, entry.IsCompressed, entry.IsEncrypted)
	if err != nil {
		return nil, nil, err
	}

	return rawData, &entry, nil
}

// GetEntry returns the metadata for a file without fetching its data.
// Returns (nil, false) if the path is not in the index or has been deleted.
func (ar *ArchiveReader) GetEntry(filepath string) (*FileEntry, bool) {
	entry, exists := ar.index.Get(filepath)
	if !exists || entry.IsDeleted {
		return nil, false
	}
	return &entry, true
}

// ListFiles returns a sorted list of all file paths in the archive,
// excluding deleted entries.
func (ar *ArchiveReader) ListFiles() []string {
	all := ar.index.List()
	live := make([]string, 0, len(all))
	for _, p := range all {
		entry, ok := ar.index.Get(p)
		if ok && !entry.IsDeleted {
			live = append(live, p)
		}
	}
	return live
}

// Index returns a flat map copy of the full index. Modifying the returned
// map does not affect the archive.
func (ar *ArchiveReader) Index() map[string]FileEntry {
	return ar.index.ToMap()
}

// Close releases the underlying storage handle.
func (ar *ArchiveReader) Close() error {
	return ar.store.Close()
}
