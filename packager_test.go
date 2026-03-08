package packager

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/radryc/packager/pipeline"
	"github.com/radryc/packager/storage"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestWriteReadRoundTrip(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	// ---------- Write archive ----------
	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)

	files := []struct {
		path string
		data []byte
		opts AddFileOptions
	}{
		{
			path: "src/main.go",
			data: []byte("package main\n\nfunc main() { println(\"hello\") }\n"),
			opts: AddFileOptions{Permission: 0644, OwnerUID: 1000, Encrypt: true},
		},
		{
			path: "docs/readme.txt",
			data: []byte("This is the readme for the project.\nIt has multiple lines.\n"),
			opts: AddFileOptions{Permission: 0644, OwnerUID: 1000, Encrypt: true},
		},
		{
			// PNG file: should auto-detect as pre-compressed → skip zstd
			path: "assets/logo.png",
			data: append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAB}, 100)...),
			opts: AddFileOptions{Permission: 0444, OwnerUID: 0, Encrypt: true},
		},
		{
			// Unencrypted JSON config
			path: "config/settings.json",
			data: []byte(`{"port": 8080, "debug": false, "db": "postgres://localhost/app"}`),
			opts: AddFileOptions{Permission: 0600, OwnerUID: 0, Encrypt: false},
		},
	}

	for _, f := range files {
		if err := w.AddFile(f.path, f.data, f.opts); err != nil {
			t.Fatalf("AddFile(%q): %v", f.path, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// ---------- Read archive ----------
	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	defer ar.Close()

	// Verify every file round-trips correctly
	for _, f := range files {
		data, entry, err := ar.GetFile(f.path)
		if err != nil {
			t.Fatalf("GetFile(%q): %v", f.path, err)
		}
		if !bytes.Equal(data, f.data) {
			t.Errorf("GetFile(%q): data mismatch (got %d bytes, want %d)", f.path, len(data), len(f.data))
		}

		// Check metadata
		if entry.Permission != f.opts.Permission {
			t.Errorf("%q: permission = %o, want %o", f.path, entry.Permission, f.opts.Permission)
		}
		if entry.OwnerUID != f.opts.OwnerUID {
			t.Errorf("%q: ownerUID = %d, want %d", f.path, entry.OwnerUID, f.opts.OwnerUID)
		}
		if entry.IsEncrypted != f.opts.Encrypt {
			t.Errorf("%q: isEncrypted = %v, want %v", f.path, entry.IsEncrypted, f.opts.Encrypt)
		}
	}

	// PNG should NOT be compressed (auto-detected)
	pngEntry, ok := ar.GetEntry("assets/logo.png")
	if !ok {
		t.Fatal("PNG entry not found")
	}
	if pngEntry.IsCompressed {
		t.Error("PNG should have IsCompressed=false (auto-detected as pre-compressed)")
	}

	// Text files SHOULD be compressed
	goEntry, ok := ar.GetEntry("src/main.go")
	if !ok {
		t.Fatal("Go entry not found")
	}
	if !goEntry.IsCompressed {
		t.Error("Go source should have IsCompressed=true")
	}

	// Unencrypted config
	cfgEntry, ok := ar.GetEntry("config/settings.json")
	if !ok {
		t.Fatal("JSON config entry not found")
	}
	if cfgEntry.IsEncrypted {
		t.Error("JSON config should have IsEncrypted=false")
	}
}

func TestListFiles(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)

	paths := []string{"z/last.txt", "a/first.txt", "m/middle.txt"}
	opts := DefaultAddFileOptions()
	for _, path := range paths {
		if err := w.AddFile(path, []byte("content"), opts); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	listed := ar.ListFiles()
	if len(listed) != 3 {
		t.Fatalf("ListFiles: got %d, want 3", len(listed))
	}
	// Should be sorted
	if listed[0] != "a/first.txt" || listed[1] != "m/middle.txt" || listed[2] != "z/last.txt" {
		t.Errorf("ListFiles not sorted: %v", listed)
	}
}

func TestGetFileNotFound(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)
	if err := w.AddFile("exists.txt", []byte("data"), DefaultAddFileOptions()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	_, _, err = ar.GetFile("does-not-exist.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestWithIndexSizeOption(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)
	if err := w.AddFile("test.txt", []byte("hello"), DefaultAddFileOptions()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	archiveBytes := buf.Bytes()
	totalSize := int64(len(archiveBytes))

	// Read the footer to get the index size (so we can pass it via option)
	footerBytes := archiveBytes[totalSize-8:]
	indexSize := int64(footerBytes[0]) | int64(footerBytes[1])<<8 | int64(footerBytes[2])<<16 | int64(footerBytes[3])<<24 |
		int64(footerBytes[4])<<32 | int64(footerBytes[5])<<40 | int64(footerBytes[6])<<48 | int64(footerBytes[7])<<56

	reader := storage.NewMemReader(archiveBytes)
	ar, err := OpenArchive(reader, p, WithIndexSize(indexSize))
	if err != nil {
		t.Fatalf("OpenArchive with WithIndexSize: %v", err)
	}
	defer ar.Close()

	data, _, err := ar.GetFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", data, "hello")
	}
}

func TestForceCompress(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)

	// Force compress a PNG (normally skipped)
	forceTrue := true
	pngData := append([]byte{0x89, 0x50, 0x4E, 0x47}, bytes.Repeat([]byte{0x00}, 50)...)
	opts := AddFileOptions{Permission: 0644, Encrypt: true, ForceCompress: &forceTrue}
	if err := w.AddFile("forced.png", pngData, opts); err != nil {
		t.Fatal(err)
	}

	// Force NO compression on a text file (normally compressed)
	forceFalse := false
	opts2 := AddFileOptions{Permission: 0644, Encrypt: true, ForceCompress: &forceFalse}
	if err := w.AddFile("uncompressed.txt", []byte("lots of text"), opts2); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	pngEntry, ok := ar.GetEntry("forced.png")
	if !ok {
		t.Fatal("forced.png not found")
	}
	if !pngEntry.IsCompressed {
		t.Error("forced.png should be compressed (ForceCompress=true)")
	}

	txtEntry, ok := ar.GetEntry("uncompressed.txt")
	if !ok {
		t.Fatal("uncompressed.txt not found")
	}
	if txtEntry.IsCompressed {
		t.Error("uncompressed.txt should NOT be compressed (ForceCompress=false)")
	}

	// Verify data integrity
	data, _, err := ar.GetFile("forced.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pngData) {
		t.Error("forced.png data mismatch")
	}

	data, _, err = ar.GetFile("uncompressed.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "lots of text" {
		t.Error("uncompressed.txt data mismatch")
	}
}

func TestEmptyArchive(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatalf("OpenArchive on empty: %v", err)
	}
	defer ar.Close()

	if files := ar.ListFiles(); len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestIndexCopy(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)
	if err := w.AddFile("a.txt", []byte("aaa"), DefaultAddFileOptions()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	idx := ar.Index()
	// Mutating the copy should not affect the reader
	delete(idx, "a.txt")

	_, ok := ar.GetEntry("a.txt")
	if !ok {
		t.Error("GetEntry should still find a.txt after external index mutation")
	}
}

func TestDeleteFile(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)

	opts := DefaultAddFileOptions()
	if err := w.AddFile("keep.txt", []byte("keeper"), opts); err != nil {
		t.Fatal(err)
	}
	if err := w.AddFile("remove.txt", []byte("gone"), opts); err != nil {
		t.Fatal(err)
	}

	// Mark remove.txt as deleted
	w.Delete("remove.txt")

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	// Deleted file should not be retrievable
	_, _, err = ar.GetFile("remove.txt")
	if err == nil {
		t.Fatal("expected error for deleted file")
	}

	// Deleted file should not appear in GetEntry
	_, ok := ar.GetEntry("remove.txt")
	if ok {
		t.Error("GetEntry should return false for deleted file")
	}

	// Deleted file should not appear in ListFiles
	listed := ar.ListFiles()
	for _, f := range listed {
		if f == "remove.txt" {
			t.Error("deleted file should not appear in ListFiles")
		}
	}
	if len(listed) != 1 || listed[0] != "keep.txt" {
		t.Errorf("expected [keep.txt], got %v", listed)
	}

	// Non-deleted file still works
	data, _, err := ar.GetFile("keep.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keeper" {
		t.Errorf("got %q, want %q", data, "keeper")
	}
}

func TestDeleteOnlyFile(t *testing.T) {
	key := testKey(t)
	p, err := pipeline.NewPipeline(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := NewArchiveWriter(&buf, p)

	// Delete a file that was never added (pure tombstone)
	w.Delete("phantom.txt")

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	reader := storage.NewMemReader(buf.Bytes())
	ar, err := OpenArchive(reader, p)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()

	if files := ar.ListFiles(); len(files) != 0 {
		t.Errorf("expected 0 listed files, got %v", files)
	}

	_, _, err = ar.GetFile("phantom.txt")
	if err == nil {
		t.Fatal("expected error for deleted phantom file")
	}
}
