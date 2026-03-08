package packager

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
)

// PathIndex stores file entries with directory-prefix deduplication.
//
// Instead of storing full paths as map keys — O(N × avg_path_len) memory —
// unique directory prefixes are stored once in a shared table and each entry
// retains only its basename. For N files across D unique directories the
// path memory drops to O(D × avg_dir_len + N × avg_basename_len).
//
// Example: 100 000 files in 500 directories with avg 50-byte dir prefixes
// and avg 12-byte basenames stores ~25 KB of dir strings + ~1.2 MB of
// basenames rather than ~6.2 MB of full paths.
type PathIndex struct {
	dirs      []string          // unique dir paths; index = dirID
	dirLookup map[string]uint32 // dir string → dirID (O(1) lookup)
	byDir     [][]baseEntry     // byDir[dirID] = files in that directory
	count     int               // total number of file entries
}

// baseEntry holds a single file within a directory.
type baseEntry struct {
	name  string    // basename only (e.g. "file.go")
	entry FileEntry // full metadata + archive location
}

// NewPathIndex creates an empty index.
func NewPathIndex() *PathIndex {
	return &PathIndex{
		dirLookup: make(map[string]uint32),
	}
}

// splitPath separates a full path into (directory, basename).
//
//	"a/b/file.go" → ("a/b", "file.go")
//	"file.go"     → ("", "file.go")
func splitPath(fullPath string) (dir, name string) {
	dir = path.Dir(fullPath)
	name = path.Base(fullPath)
	if dir == "." {
		dir = ""
	}
	return
}

// joinPath reconstructs a full path from dir + basename.
func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// Put adds or replaces a file entry in the index.
func (pi *PathIndex) Put(fullPath string, entry FileEntry) {
	dir, name := splitPath(fullPath)

	dirID, exists := pi.dirLookup[dir]
	if !exists {
		dirID = uint32(len(pi.dirs))
		pi.dirs = append(pi.dirs, dir)
		pi.byDir = append(pi.byDir, nil)
		pi.dirLookup[dir] = dirID
	}

	// Replace existing entry with the same basename if present.
	files := pi.byDir[dirID]
	for i, f := range files {
		if f.name == name {
			pi.byDir[dirID][i].entry = entry
			return
		}
	}

	pi.byDir[dirID] = append(files, baseEntry{name: name, entry: entry})
	pi.count++
}

// Get retrieves a file entry by full path. Returns false if not found.
func (pi *PathIndex) Get(fullPath string) (FileEntry, bool) {
	dir, name := splitPath(fullPath)

	dirID, exists := pi.dirLookup[dir]
	if !exists {
		return FileEntry{}, false
	}
	for _, f := range pi.byDir[dirID] {
		if f.name == name {
			return f.entry, true
		}
	}
	return FileEntry{}, false
}

// Len returns the total number of files in the index.
func (pi *PathIndex) Len() int {
	return pi.count
}

// List returns a sorted slice of all full file paths.
func (pi *PathIndex) List() []string {
	paths := make([]string, 0, pi.count)
	for dirID, files := range pi.byDir {
		dir := pi.dirs[dirID]
		for _, f := range files {
			paths = append(paths, joinPath(dir, f.name))
		}
	}
	sort.Strings(paths)
	return paths
}

// ForEach calls fn for every entry. Iteration order is not guaranteed.
func (pi *PathIndex) ForEach(fn func(fullPath string, entry FileEntry)) {
	for dirID, files := range pi.byDir {
		dir := pi.dirs[dirID]
		for _, f := range files {
			fn(joinPath(dir, f.name), f.entry)
		}
	}
}

// ToMap returns a flat map[string]FileEntry copy of the index.
// Useful for callers that need a simple map view.
func (pi *PathIndex) ToMap() map[string]FileEntry {
	m := make(map[string]FileEntry, pi.count)
	pi.ForEach(func(fullPath string, entry FileEntry) {
		m[fullPath] = entry
	})
	return m
}

// ---------------------------------------------------------------------------
// Serialisation — prefix-table format
// ---------------------------------------------------------------------------
//
// On-disk layout (JSON before zstd+AEAD):
//
//	{
//	  "d": ["dir/a", "dir/b", ...],
//	  "f": [
//	    {"d":0,"n":"file.go","o":0,"s":123,"p":420,"u":1000,"e":true,"c":true},
//	    ...
//	  ]
//	}
//
// Directory strings appear once in the "d" array. Each file references its
// directory by index, storing only the basename. This makes the pre-compression
// JSON significantly smaller when many files share common directory prefixes.

type serializedIndex struct {
	Dirs  []string         `json:"d"`
	Files []serializedFile `json:"f"`
}

type serializedFile struct {
	DirID        uint32 `json:"d"`
	Name         string `json:"n"`
	Offset       int64  `json:"o"`
	Size         int64  `json:"s"`
	Permission   uint32 `json:"p"`
	OwnerUID     int    `json:"u"`
	IsEncrypted  bool   `json:"e,omitempty"`
	IsCompressed bool   `json:"c,omitempty"`
	IsDeleted    bool   `json:"del,omitempty"`
}

// MarshalJSON serialises the PathIndex using the prefix-table format.
func (pi *PathIndex) MarshalJSON() ([]byte, error) {
	si := serializedIndex{
		Dirs:  pi.dirs,
		Files: make([]serializedFile, 0, pi.count),
	}
	if si.Dirs == nil {
		si.Dirs = []string{}
	}
	for dirID, files := range pi.byDir {
		for _, f := range files {
			si.Files = append(si.Files, serializedFile{
				DirID:        uint32(dirID),
				Name:         f.name,
				Offset:       f.entry.Offset,
				Size:         f.entry.Size,
				Permission:   f.entry.Permission,
				OwnerUID:     f.entry.OwnerUID,
				IsEncrypted:  f.entry.IsEncrypted,
				IsCompressed: f.entry.IsCompressed,
				IsDeleted:    f.entry.IsDeleted,
			})
		}
	}
	return json.Marshal(si)
}

// UnmarshalJSON deserialises from the prefix-table format.
func (pi *PathIndex) UnmarshalJSON(data []byte) error {
	var si serializedIndex
	if err := json.Unmarshal(data, &si); err != nil {
		return err
	}

	pi.dirs = si.Dirs
	pi.dirLookup = make(map[string]uint32, len(si.Dirs))
	pi.byDir = make([][]baseEntry, len(si.Dirs))
	pi.count = 0

	for i, dir := range si.Dirs {
		pi.dirLookup[dir] = uint32(i)
	}

	for _, f := range si.Files {
		if int(f.DirID) >= len(pi.dirs) {
			return fmt.Errorf("invalid dir ID %d (only %d dirs)", f.DirID, len(pi.dirs))
		}
		pi.byDir[f.DirID] = append(pi.byDir[f.DirID], baseEntry{
			name: f.Name,
			entry: FileEntry{
				Offset:       f.Offset,
				Size:         f.Size,
				Permission:   f.Permission,
				OwnerUID:     f.OwnerUID,
				IsEncrypted:  f.IsEncrypted,
				IsCompressed: f.IsCompressed,
				IsDeleted:    f.IsDeleted,
			},
		})
		pi.count++
	}

	return nil
}
