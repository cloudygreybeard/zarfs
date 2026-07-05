package targz

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudygreybeard/zarfs/internal/archive"
)

func TestCreateAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.tar.gz")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}

	arc, err := OpenRW(path)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	content := []byte("compressed hello")
	_, err = arc.AddFile(nil, "hello.txt", content, 0, 0, 0)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}

	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].OrigLen != len(content) {
		t.Errorf("size = %d, want %d", entries[0].OrigLen, len(content))
	}

	rc, err := arc2.OpenFile(entries[0])
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content = %q, want %q", data, content)
	}
}

func TestRoundTripWithDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirs.tar.gz")
	arc, err := OpenRW(path)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	dir, _ := arc.AddDir(nil, "sub", 0, 0, 0)
	if _, err := arc.AddFile(dir, "inner.txt", []byte("nested"), 0, 0, 0); err != nil {
		t.Fatalf("AddFile inner: %v", err)
	}
	if _, err := arc.AddFile(nil, "outer.txt", []byte("top-level"), 0, 0, 0); err != nil {
		t.Fatalf("AddFile outer: %v", err)
	}

	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d top-level entries, want 2", len(entries))
	}

	totalFiles := countFiles(entries)
	if totalFiles != 2 {
		t.Errorf("total files = %d, want 2", totalFiles)
	}
}

func countFiles(entries []*archive.Entry) int {
	n := 0
	for _, e := range entries {
		if !e.IsDir {
			n++
		}
		n += countFiles(e.Children)
	}
	return n
}
