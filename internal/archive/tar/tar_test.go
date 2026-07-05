package tar

import (
	"io"
	"path/filepath"
	"testing"
)

func TestCreateAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.tar")
	if err := CreateEmpty(path); err != nil {
		t.Fatalf("CreateEmpty: %v", err)
	}

	arc, err := OpenRW(path)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	content := []byte("hello from zarfs")
	_, err = arc.AddFile(nil, "greeting.txt", content, 0, 0, 0)
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
	if entries[0].Name != "greeting.txt" {
		t.Errorf("name = %q, want %q", entries[0].Name, "greeting.txt")
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

func TestNestedDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested.tar")
	arc, err := OpenRW(path)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	dir, err := arc.AddDir(nil, "docs", 0, 0, 0)
	if err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	_, err = arc.AddFile(dir, "readme.txt", []byte("read me"), 0, 0, 0)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	_, err = arc.AddFile(nil, "root.txt", []byte("at root"), 0, 0, 0)
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
	if len(entries) != 2 {
		t.Fatalf("got %d top-level entries, want 2", len(entries))
	}

	var dirEntry, fileEntry = entries[0], entries[1]
	if !dirEntry.IsDir {
		dirEntry, fileEntry = entries[1], entries[0]
	}

	if !dirEntry.IsDir || dirEntry.Name != "docs" {
		t.Errorf("dir entry: name=%q isDir=%v", dirEntry.Name, dirEntry.IsDir)
	}
	if len(dirEntry.Children) != 1 {
		t.Fatalf("dir has %d children, want 1", len(dirEntry.Children))
	}
	if dirEntry.Children[0].Name != "readme.txt" {
		t.Errorf("child name = %q, want %q", dirEntry.Children[0].Name, "readme.txt")
	}
	if fileEntry.IsDir || fileEntry.Name != "root.txt" {
		t.Errorf("file entry: name=%q isDir=%v", fileEntry.Name, fileEntry.IsDir)
	}
}

func TestDeleteAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mutable.tar")
	arc, err := OpenRW(path)
	if err != nil {
		t.Fatalf("OpenRW: %v", err)
	}

	keep, _ := arc.AddFile(nil, "keep.txt", []byte("keep"), 0, 0, 0)
	remove, _ := arc.AddFile(nil, "remove.txt", []byte("gone"), 0, 0, 0)
	_ = keep

	if err := arc.DeleteEntry(nil, remove); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	arc.UpdateData(keep, []byte("updated content"))

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

	rc, err := arc2.OpenFile(entries[0])
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "updated content" {
		t.Errorf("content = %q, want %q", data, "updated content")
	}
}
