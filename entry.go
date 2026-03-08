package packager

// FileType distinguishes regular files, directories, and symlinks.
type FileType uint8

const (
	FileTypeRegular FileType = 0 // default zero value = regular file (backwards compat)
	FileTypeDir     FileType = 1
	FileTypeSymlink FileType = 2
)

// FileEntry stores the location and metadata for a single file inside the archive.
type FileEntry struct {
	// Offset is the byte offset of the packed data block in the archive.
	Offset int64 `json:"o"`
	// Size is the byte length of the packed data block.
	Size int64 `json:"s"`
	// Permission stores Unix permission bits (e.g. 0644).
	Permission uint32 `json:"p"`
	// OwnerUID is the numeric user ID of the file owner.
	OwnerUID int `json:"u"`
	// IsEncrypted indicates whether AEAD encryption was applied to this block.
	IsEncrypted bool `json:"e"`
	// IsCompressed indicates whether zstd compression was applied to this block.
	IsCompressed bool `json:"c"`
	// IsDeleted marks the entry as a tombstone (logically deleted file).
	IsDeleted bool `json:"del,omitempty"`
	// FileType indicates whether this entry is a regular file, directory, or symlink.
	FileType FileType `json:"t,omitempty"`
}
