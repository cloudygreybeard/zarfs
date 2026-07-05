// Package tar reads and writes standard tar archives, implementing
// the archive.Archive and archive.WritableArchive interfaces.
package tar

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	gotar "archive/tar"

	"github.com/cloudygreybeard/zarfs/internal/archive"
)


// Archive implements archive.Archive and archive.WritableArchive for
// tar files.
type Archive struct {
	path      string
	entries   []*archive.Entry
	fileData  map[*archive.Entry][]byte
	readWrite bool
	dirty     bool
}

// Open opens a tar archive at path for reading.
func Open(path string) (*Archive, error) {
	a := &Archive{
		path:     path,
		fileData: make(map[*archive.Entry][]byte),
	}
	if err := a.parse(); err != nil {
		return nil, err
	}
	return a, nil
}

// OpenRW opens a tar archive at path for reading and writing. If the
// file does not exist, it is created.
func OpenRW(path string) (*Archive, error) {
	a := &Archive{
		path:      path,
		fileData:  make(map[*archive.Entry][]byte),
		readWrite: true,
	}

	fi, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && fi.Size() > 0 {
		if err := a.parse(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Entries returns the top-level archive entries.
func (a *Archive) Entries() []*archive.Entry { return a.entries }

// Close is a no-op for tar archives since all data is held in memory.
func (a *Archive) Close() error { return nil }

// OpenFile returns a reader over the in-memory data for the entry.
func (a *Archive) OpenFile(e *archive.Entry) (io.ReadCloser, error) {
	data, ok := a.fileData[e]
	if !ok {
		return nil, fmt.Errorf("no data for entry %q", e.Name)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// AddFile creates a new file entry in the archive.
func (a *Archive) AddFile(parent *archive.Entry, name string, data []byte, load, exec, attr uint32) (*archive.Entry, error) {
	if !a.readWrite {
		return nil, fmt.Errorf("archive not opened for writing")
	}

	e := &archive.Entry{
		Name:     name,
		Load:     load,
		Exec:     exec,
		Attr:     attr,
		OrigLen:  len(data),
		CompLen:  len(data),
		FileTime: time.Now(),
		FileType: -1,
	}
	if load != 0 || exec != 0 {
		e.FileType = int((load >> 8) & 0xfff)
	}

	a.fileData[e] = append([]byte(nil), data...)

	if parent != nil {
		parent.Children = append(parent.Children, e)
	} else {
		a.entries = append(a.entries, e)
	}

	a.dirty = true
	return e, nil
}

// AddDir creates a new directory entry in the archive.
func (a *Archive) AddDir(parent *archive.Entry, name string, load, exec, attr uint32) (*archive.Entry, error) {
	if !a.readWrite {
		return nil, fmt.Errorf("archive not opened for writing")
	}

	e := &archive.Entry{
		Name:     name,
		IsDir:    true,
		Load:     load,
		Exec:     exec,
		Attr:     attr,
		FileTime: time.Now(),
		FileType: -1,
	}
	if load != 0 || exec != 0 {
		e.FileType = int((load >> 8) & 0xfff)
	}

	if parent != nil {
		parent.Children = append(parent.Children, e)
	} else {
		a.entries = append(a.entries, e)
	}

	a.dirty = true
	return e, nil
}

// DeleteEntry removes an entry from the archive.
func (a *Archive) DeleteEntry(parent *archive.Entry, target *archive.Entry) error {
	if !a.readWrite {
		return fmt.Errorf("archive not opened for writing")
	}

	var siblings *[]*archive.Entry
	if parent != nil {
		siblings = &parent.Children
	} else {
		siblings = &a.entries
	}

	for i, e := range *siblings {
		if e == target {
			*siblings = append((*siblings)[:i], (*siblings)[i+1:]...)
			delete(a.fileData, target)
			a.dirty = true
			return nil
		}
	}
	return fmt.Errorf("entry not found in parent")
}

// UpdateData replaces the stored data for an existing file entry.
func (a *Archive) UpdateData(e *archive.Entry, data []byte) {
	a.fileData[e] = append([]byte(nil), data...)
	e.OrigLen = len(data)
	e.CompLen = len(data)
	a.dirty = true
}

// Flush rewrites the entire tar file from the in-memory entry tree.
func (a *Archive) Flush() error {
	if !a.dirty {
		return nil
	}

	f, err := os.Create(a.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	tw := gotar.NewWriter(f)

	var walk func([]*archive.Entry, string) error
	walk = func(entries []*archive.Entry, prefix string) error {
		for _, e := range entries {
			fullPath := prefix + e.Name
			if err := a.writeEntry(tw, e, fullPath); err != nil {
				return err
			}
			if e.IsDir {
				if err := walk(e.Children, fullPath+"/"); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(a.entries, ""); err != nil {
		_ = tw.Close()
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}

	a.dirty = false
	return nil
}

// CreateEmpty writes a valid empty tar archive to path.
func CreateEmpty(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	tw := gotar.NewWriter(f)
	if err := tw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (a *Archive) writeEntry(tw *gotar.Writer, e *archive.Entry, fullPath string) error {
	hdr := &gotar.Header{
		Name:    fullPath,
		ModTime: e.FileTime,
	}

	if e.IsDir {
		if !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		hdr.Typeflag = gotar.TypeDir
		hdr.Mode = 0o755
	} else {
		hdr.Typeflag = gotar.TypeReg
		hdr.Mode = 0o644
		data := a.fileData[e]
		hdr.Size = int64(len(data))
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("writing header for %s: %w", fullPath, err)
	}

	if !e.IsDir {
		data := a.fileData[e]
		if _, err := tw.Write(data); err != nil {
			return fmt.Errorf("writing data for %s: %w", fullPath, err)
		}
	}

	return nil
}

func (a *Archive) parse() error {
	f, err := os.Open(a.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	tr := gotar.NewReader(f)
	dirMap := make(map[string]*archive.Entry)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		name := path.Clean(hdr.Name)
		name = strings.TrimPrefix(name, "./")
		name = strings.TrimSuffix(name, "/")
		if name == "." || name == "" {
			continue
		}

		isDir := hdr.Typeflag == gotar.TypeDir

		var data []byte
		if !isDir {
			data, err = io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("reading data for %s: %w", name, err)
			}
		}

		e := &archive.Entry{
			Name:     path.Base(name),
			IsDir:    isDir,
			OrigLen:  len(data),
			CompLen:  len(data),
			FileTime: hdr.ModTime,
			FileType: -1,
		}

		if !isDir {
			a.fileData[e] = data
		}

		parentPath := path.Dir(name)
		if parentPath == "." {
			a.entries = append(a.entries, e)
		} else {
			parent := a.ensureParentDirs(dirMap, parentPath, hdr.ModTime)
			parent.Children = append(parent.Children, e)
		}

		if isDir {
			dirMap[name] = e
		}
	}

	return nil
}

// ensureParentDirs creates intermediate directory entries as needed
// and returns the immediate parent for the given directory path.
func (a *Archive) ensureParentDirs(dirMap map[string]*archive.Entry, dirPath string, modTime time.Time) *archive.Entry {
	if e, ok := dirMap[dirPath]; ok {
		return e
	}

	parts := strings.Split(dirPath, "/")
	var current *archive.Entry
	built := ""

	for _, part := range parts {
		if built == "" {
			built = part
		} else {
			built = built + "/" + part
		}

		if e, ok := dirMap[built]; ok {
			current = e
			continue
		}

		e := &archive.Entry{
			Name:     part,
			IsDir:    true,
			FileTime: modTime,
			FileType: -1,
		}
		dirMap[built] = e

		if current != nil {
			current.Children = append(current.Children, e)
		} else {
			a.entries = append(a.entries, e)
		}
		current = e
	}

	return current
}

