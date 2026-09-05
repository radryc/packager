package packager

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Compile-time check: nameIdx must exist and have the same length as byDir/dirs
// after Put operations (verified via internal state access in the _test package).


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

// TestPathIndexNameIdxConsistency verifies that nameIdx stays in sync with
// byDir across Put (add), Put (replace), and JSON round-trip operations.
func TestPathIndexNameIdxConsistency(t *testing.T) {
	pi := NewPathIndex()

	// Add 1 000 files in the same directory.
	const N = 1000
	for i := 0; i < N; i++ {
		pi.Put(fmt.Sprintf("bigdir/file_%04d.go", i), FileEntry{Offset: int64(i)})
	}

	// nameIdx must be allocated and length must match dirs.
	if len(pi.nameIdx) != len(pi.dirs) {
		t.Fatalf("nameIdx len %d != dirs len %d", len(pi.nameIdx), len(pi.dirs))
	}
	// The single directory should have N entries in its name map.
	if len(pi.nameIdx[0]) != N {
		t.Errorf("nameIdx[0] has %d entries, want %d", len(pi.nameIdx[0]), N)
	}

	// All entries must be retrievable and point at the correct slice index.
	for i := 0; i < N; i++ {
		name := fmt.Sprintf("file_%04d.go", i)
		idx, ok := pi.nameIdx[0][name]
		if !ok {
			t.Fatalf("nameIdx missing %q", name)
		}
		if pi.byDir[0][idx].name != name {
			t.Errorf("nameIdx[0][%q]=%d points at wrong entry %q", name, idx, pi.byDir[0][idx].name)
		}
	}

	// Replace half the entries; nameIdx indices must remain valid.
	for i := 0; i < N/2; i++ {
		pi.Put(fmt.Sprintf("bigdir/file_%04d.go", i), FileEntry{Offset: int64(i + N)})
	}
	if pi.Len() != N {
		t.Errorf("Len after replace: got %d, want %d", pi.Len(), N)
	}
	for i := 0; i < N/2; i++ {
		path := fmt.Sprintf("bigdir/file_%04d.go", i)
		entry, ok := pi.Get(path)
		if !ok {
			t.Fatalf("Get(%q) not found after replace", path)
		}
		if entry.Offset != int64(i+N) {
			t.Errorf("Get(%q).Offset = %d, want %d", path, entry.Offset, i+N)
		}
	}

	// JSON round-trip must rebuild nameIdx correctly.
	data, err := json.Marshal(pi)
	if err != nil {
		t.Fatal(err)
	}
	pi2 := NewPathIndex()
	if err := json.Unmarshal(data, pi2); err != nil {
		t.Fatal(err)
	}
	if len(pi2.nameIdx) != len(pi2.dirs) {
		t.Fatalf("post-unmarshal nameIdx len %d != dirs len %d", len(pi2.nameIdx), len(pi2.dirs))
	}
	if len(pi2.nameIdx[0]) != N {
		t.Errorf("post-unmarshal nameIdx[0] has %d entries, want %d", len(pi2.nameIdx[0]), N)
	}
}

// BenchmarkPathIndexPutLargeDir measures Put throughput when all files land
// in the same directory (worst case for the old O(n) linear scan).
func BenchmarkPathIndexPutLargeDir(b *testing.B) {
	pi := NewPathIndex()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pi.Put(fmt.Sprintf("bigdir/file_%08d.go", i), FileEntry{Offset: int64(i)})
	}
}

// BenchmarkPathIndexGetLargeDir measures Get throughput after inserting many
// files in a single directory.
func BenchmarkPathIndexGetLargeDir(b *testing.B) {
	const N = 100_000
	pi := NewPathIndex()
	for i := 0; i < N; i++ {
		pi.Put(fmt.Sprintf("bigdir/file_%08d.go", i), FileEntry{Offset: int64(i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pi.Get(fmt.Sprintf("bigdir/file_%08d.go", i%N))
	}
}
