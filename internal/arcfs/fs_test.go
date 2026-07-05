package arcfs

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

const testArchive = "../../testdata/arcfs.arc"

func testdataPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(testArchive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Skipf("test archive not found: %s", p)
	}
	return p
}

func TestOpenAndBrowse(t *testing.T) {
	afs, err := Open(testdataPath(t), nil, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = afs.Close() }()

	root := afs.GetInode(RootInodeID)
	if root == nil {
		t.Fatal("root inode is nil")
	}
	if !root.IsDir() {
		t.Fatal("root is not a directory")
	}
	if len(root.ChildIDs) == 0 {
		t.Fatal("root has no children")
	}

	children, err := afs.Children(RootInodeID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	t.Logf("root has %d children", len(children))
	for _, c := range children {
		t.Logf("  %s (dir=%v)", c.Name, c.IsDir())
	}
}

func TestLookupAndRead(t *testing.T) {
	afs, err := Open(testdataPath(t), nil, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = afs.Close() }()

	// Navigate to !ArcFS,ddc/!ArcFS/!Help,fff
	arcfsDir, err := afs.Lookup(RootInodeID, "!ArcFS,ddc")
	if err != nil {
		t.Fatalf("Lookup !ArcFS,ddc: %v", err)
	}
	if !arcfsDir.IsDir() {
		t.Fatal("!ArcFS,ddc is not a directory")
	}

	appDir, err := afs.Lookup(arcfsDir.ID, "!ArcFS")
	if err != nil {
		t.Fatalf("Lookup !ArcFS: %v", err)
	}

	helpFile, err := afs.Lookup(appDir.ID, "!Help,fff")
	if err != nil {
		t.Fatalf("Lookup !Help,fff: %v", err)
	}

	hid, err := afs.OpenFile(helpFile.ID)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer afs.ReleaseHandle(hid)

	buf := make([]byte, 4096)
	n, err := afs.ReadHandle(hid, 0, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadHandle: %v", err)
	}
	if n != 2544 {
		t.Errorf("read %d bytes, want 2544", n)
	}

	attr, err := afs.GetAttr(helpFile.ID)
	if err != nil {
		t.Fatalf("GetAttr: %v", err)
	}
	if attr.Size != 2544 {
		t.Errorf("attr.Size = %d, want 2544", attr.Size)
	}
	if attr.Mode != 0o444 {
		t.Errorf("attr.Mode = %o, want 444", attr.Mode)
	}
}

func TestReadAllFiles(t *testing.T) {
	afs, err := Open(testdataPath(t), nil, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = afs.Close() }()

	var walkAndRead func(uint64)
	walkAndRead = func(parentID uint64) {
		children, err := afs.Children(parentID)
		if err != nil {
			t.Fatalf("Children(%d): %v", parentID, err)
		}
		for _, child := range children {
			if child.IsDir() {
				walkAndRead(child.ID)
				continue
			}
			t.Run(child.Name, func(t *testing.T) {
				hid, err := afs.OpenFile(child.ID)
				if err != nil {
					t.Fatalf("OpenFile: %v", err)
				}
				defer afs.ReleaseHandle(hid)

				attr, _ := afs.GetAttr(child.ID)
				buf := make([]byte, attr.Size+1)
				n, err := afs.ReadHandle(hid, 0, buf)
				if err != nil && err != io.EOF {
					t.Fatalf("ReadHandle: %v", err)
				}
				if uint64(n) != attr.Size {
					t.Errorf("read %d bytes, want %d", n, attr.Size)
				}
			})
		}
	}

	walkAndRead(RootInodeID)
}
