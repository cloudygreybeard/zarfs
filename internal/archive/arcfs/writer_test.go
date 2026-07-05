package arcfs

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.arc")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	arc, err := OpenRW(path, nil)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	content := []byte("Hello, RISC OS!")
	e, err := arc.AddFile(nil, "greeting", content, 0xffffff33, 0x7f142a00, 0x13)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}

	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open (re-read): %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	_ = e
	got := entries[0]
	if got.OrigLen != len(content) {
		t.Errorf("OrigLen = %d, want %d", got.OrigLen, len(content))
	}

	rc, err := arc2.OpenFile(got)
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

func TestCreateDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.arc")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	arc, err := OpenRW(path, nil)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	dir, err := arc.AddDir(nil, "mydir", 0xfffddc00, 0, 0x13)
	if err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	if !dir.IsDir {
		t.Error("directory entry is not marked as dir")
	}

	_, err = arc.AddFile(dir, "inner", []byte("nested content"), 0xffffff33, 0x7f142a00, 0x13)
	if err != nil {
		t.Fatalf("AddFile (nested): %v", err)
	}

	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open (re-read): %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d top-level entries, want 1", len(entries))
	}
	if !entries[0].IsDir {
		t.Fatal("top entry not a directory")
	}
	if len(entries[0].Children) != 1 {
		t.Fatalf("directory has %d children, want 1", len(entries[0].Children))
	}

	child := entries[0].Children[0]
	rc, err := arc2.OpenFile(child)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("content = %q, want %q", data, "nested content")
	}
}

func TestDeleteEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.arc")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	arc, err := OpenRW(path, nil)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	e1, _ := arc.AddFile(nil, "keep", []byte("keep this"), 0xffffff33, 0x7f142a00, 0x13)
	e2, _ := arc.AddFile(nil, "remove", []byte("delete this"), 0xffffff33, 0x7f142a00, 0x13)
	_ = e1

	if err := arc.DeleteEntry(nil, e2); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open (re-read): %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].OrigLen != len([]byte("keep this")) {
		t.Errorf("wrong entry survived deletion")
	}
}

func TestUpdateData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.arc")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	arc, err := OpenRW(path, nil)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	e, _ := arc.AddFile(nil, "mutable", []byte("original"), 0xffffff33, 0x7f142a00, 0x13)
	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush (initial): %v", err)
	}

	arc.UpdateData(e, []byte("modified content"))
	if err := arc.Flush(); err != nil {
		t.Fatalf("Flush (update): %v", err)
	}
	_ = arc.Close()

	arc2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open (re-read): %v", err)
	}
	defer func() { _ = arc2.Close() }()

	entries := arc2.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
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
	if string(data) != "modified content" {
		t.Errorf("content = %q, want %q", data, "modified content")
	}
}
