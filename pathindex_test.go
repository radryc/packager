package packager

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestPathIndexPutGet(t *testing.T) {
	pi := NewPathIndex()

	entry := FileEntry{Offset: 0, Size: 100, Permission: 0644, OwnerUID: 1000, IsEncrypted: true, IsCompressed: true}
	pi.Put("a/b/c/file.go", entry)

	got, ok := pi.Get("a/b/c/file.go")
	if !ok {
		t.Fatal("expected to find a/b/c/file.go")
	}
	if got.Offset != 0 || got.Size != 100 || got.Permission != 0644 {
		t.Errorf("unexpected entry: %+v", got)
	}

	// Not found
	_, ok = pi.Get("nonexistent.txt")
	if ok {
		t.Error("expected not found for nonexistent path")
	}
}

func TestPathIndexRootFiles(t *testing.T) {
	pi := NewPathIndex()

	pi.Put("readme.md", FileEntry{Offset: 0, Size: 50})
	pi.Put("LICENSE", FileEntry{Offset: 50, Size: 30})

	got, ok := pi.Get("readme.md")
	if !ok || got.Size != 50 {
		t.Errorf("root file lookup failed: ok=%v, entry=%+v", ok, got)
	}

	if pi.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", pi.Len())
	}
}

func TestPathIndexReplace(t *testing.T) {
	pi := NewPathIndex()

	pi.Put("dir/file.txt", FileEntry{Size: 100})
	pi.Put("dir/file.txt", FileEntry{Size: 200})

	got, ok := pi.Get("dir/file.txt")
	if !ok || got.Size != 200 {
		t.Errorf("replacement failed: ok=%v, size=%d", ok, got.Size)
	}
	if pi.Len() != 1 {
		t.Errorf("replace should not increase count: got %d", pi.Len())
	}
}

func TestPathIndexPrefixDedup(t *testing.T) {
	pi := NewPathIndex()

	// Add 100 files in the same deep directory
	for i := 0; i < 100; i++ {
		dir := "very/long/deeply/nested/directory/structure/"
		name := fmt.Sprintf("file_%03d.go", i)
		pi.Put(dir+name, FileEntry{Offset: int64(i)})
	}

	// Only 1 unique directory should exist
	if len(pi.dirs) != 1 {
		t.Errorf("expected 1 unique dir, got %d", len(pi.dirs))
	}
	if pi.dirs[0] != "very/long/deeply/nested/directory/structure" {
		t.Errorf("unexpected dir: %q", pi.dirs[0])
	}
	if pi.Len() != 100 {
		t.Errorf("expected 100 entries, got %d", pi.Len())
	}
}

func TestPathIndexList(t *testing.T) {
	pi := NewPathIndex()

	pi.Put("z/last.txt", FileEntry{})
	pi.Put("a/first.txt", FileEntry{})
	pi.Put("root.txt", FileEntry{})

	listed := pi.List()
	if len(listed) != 3 {
		t.Fatalf("expected 3, got %d", len(listed))
	}
	expected := []string{"a/first.txt", "root.txt", "z/last.txt"}
	for i, e := range expected {
		if listed[i] != e {
			t.Errorf("list[%d] = %q, want %q", i, listed[i], e)
		}
	}
}

func TestPathIndexJSONRoundTrip(t *testing.T) {
	pi := NewPathIndex()
	pi.Put("src/main.go", FileEntry{Offset: 0, Size: 100, Permission: 0644, OwnerUID: 1000, IsEncrypted: true, IsCompressed: true})
	pi.Put("src/util.go", FileEntry{Offset: 100, Size: 50, Permission: 0644, OwnerUID: 1000, IsEncrypted: true, IsCompressed: true})
	pi.Put("README.md", FileEntry{Offset: 150, Size: 200, Permission: 0444, OwnerUID: 0, IsEncrypted: false, IsCompressed: true})

	data, err := json.Marshal(pi)
	if err != nil {
		t.Fatal(err)
	}

	// Verify prefix-table structure
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["d"]; !ok {
		t.Error("expected 'd' (dirs) key in JSON")
	}
	if _, ok := raw["f"]; !ok {
		t.Error("expected 'f' (files) key in JSON")
	}

	// Unmarshal back
	pi2 := NewPathIndex()
	if err := json.Unmarshal(data, pi2); err != nil {
		t.Fatal(err)
	}

	if pi2.Len() != pi.Len() {
		t.Fatalf("len mismatch: got %d, want %d", pi2.Len(), pi.Len())
	}

	// Verify all entries round-trip
	for _, path := range pi.List() {
		orig, _ := pi.Get(path)
		got, ok := pi2.Get(path)
		if !ok {
			t.Errorf("missing after round-trip: %q", path)
			continue
		}
		if orig != got {
			t.Errorf("%q: entry mismatch\n  orig: %+v\n  got:  %+v", path, orig, got)
		}
	}
}

func TestPathIndexToMap(t *testing.T) {
	pi := NewPathIndex()
	pi.Put("a/b.txt", FileEntry{Size: 10})
	pi.Put("c.txt", FileEntry{Size: 20})

	m := pi.ToMap()
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["a/b.txt"].Size != 10 {
		t.Error("a/b.txt size mismatch")
	}
	if m["c.txt"].Size != 20 {
		t.Error("c.txt size mismatch")
	}
}

func TestPathIndexEmptyJSON(t *testing.T) {
	pi := NewPathIndex()

	data, err := json.Marshal(pi)
	if err != nil {
		t.Fatal(err)
	}

	pi2 := NewPathIndex()
	if err := json.Unmarshal(data, pi2); err != nil {
		t.Fatal(err)
	}
	if pi2.Len() != 0 {
		t.Errorf("expected 0 entries, got %d", pi2.Len())
	}
}
