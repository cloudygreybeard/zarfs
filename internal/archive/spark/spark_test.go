package spark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudygreybeard/zarfs/internal/archive"
)

// arcfsArc is the ARM Club ArcFS distribution, packaged as a Spark
// archive. It is freely distributable and downloaded from
// https://armclub.org.uk/products/arcfs/arcfs.arc
const arcfsArc = "../../../testdata/arcfs.arc"

func testdataPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(arcfsArc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Skipf("test archive not found: %s", p)
	}
	return p
}

func TestOpenAndEntries(t *testing.T) {
	arc, err := Open(testdataPath(t), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = arc.Close() }()

	entries := arc.Entries()
	if len(entries) == 0 {
		t.Fatal("no top-level entries")
	}

	// The archive contains three top-level SparkFS directories.
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	t.Logf("top-level entries: %v", names)

	if !entries[0].IsDir {
		t.Errorf("first entry %q is not a directory", entries[0].Name)
	}
}

// wantFile describes an expected file in the test archive with known
// properties.
type wantFile struct {
	name     string
	origLen  int
	compType int
	fileType int
}

// knownFiles lists files from the arcfs.arc archive whose properties
// are verified by the test.
var knownFiles = []wantFile{
	{"!ArcFS/ArcFS,ffa", 11808, 2, 0xffa},
	{"!ArcFS/ArcFSFiler,ffa", 6880, 2, 0xffa},
	{"!ArcFS/!Help,fff", 2544, 127, 0xfff},
	{"!ArcFS/!Boot,feb", 713, 127, 0xfeb},
}

func TestDecompressKnownFiles(t *testing.T) {
	arc, err := Open(testdataPath(t), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = arc.Close() }()

	index := indexEntries(arc.Entries())

	for _, wf := range knownFiles {
		t.Run(wf.name, func(t *testing.T) {
			e, ok := index[wf.name]
			if !ok {
				t.Fatalf("entry %q not found in archive", wf.name)
			}

			if e.OrigLen != wf.origLen {
				t.Errorf("OrigLen = %d, want %d", e.OrigLen, wf.origLen)
			}
			if e.CompType != wf.compType {
				t.Errorf("CompType = %d, want %d", e.CompType, wf.compType)
			}
			if e.FileType != wf.fileType {
				t.Errorf("FileType = 0x%03x, want 0x%03x", e.FileType, wf.fileType)
			}

			rc, err := arc.OpenFile(e)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer func() { _ = rc.Close() }()

			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if len(data) != wf.origLen {
				t.Errorf("decompressed size = %d, want %d", len(data), wf.origLen)
			}
		})
	}
}

func TestDecompressAllFiles(t *testing.T) {
	arc, err := Open(testdataPath(t), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = arc.Close() }()

	index := indexEntries(arc.Entries())

	for name, e := range index {
		if e.IsDir {
			continue
		}
		t.Run(name, func(t *testing.T) {
			rc, err := arc.OpenFile(e)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer func() { _ = rc.Close() }()

			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if len(data) != e.OrigLen {
				t.Errorf("decompressed size = %d, want %d", len(data), e.OrigLen)
			}
		})
	}
}

func indexEntries(entries []*archive.Entry) map[string]*archive.Entry {
	m := make(map[string]*archive.Entry)
	var walk func([]*archive.Entry)
	walk = func(es []*archive.Entry) {
		for _, e := range es {
			m[e.Name] = e
			if e.IsDir {
				walk(e.Children)
			}
		}
	}
	walk(entries)
	return m
}
