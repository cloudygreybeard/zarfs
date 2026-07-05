// Package targz reads and writes gzip-compressed tar archives,
// implementing the archive.Archive and archive.WritableArchive
// interfaces by wrapping the tar package.
package targz

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	archivetar "github.com/cloudygreybeard/zarfs/internal/archive/tar"
)

// Archive wraps a tar.Archive with gzip compression.
type Archive struct {
	inner    *archivetar.Archive
	origPath string
	tmpPath  string
}

// Open opens a gzip-compressed tar archive at path for reading.
func Open(path string) (*Archive, error) {
	tmpPath, err := decompress(path)
	if err != nil {
		return nil, fmt.Errorf("decompressing %s: %w", path, err)
	}

	inner, err := archivetar.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	return &Archive{
		inner:    inner,
		origPath: path,
		tmpPath:  tmpPath,
	}, nil
}

// OpenRW opens a gzip-compressed tar archive at path for reading and
// writing. If the file does not exist, it is created.
func OpenRW(path string) (*Archive, error) {
	var tmpPath string
	var err error

	fi, statErr := os.Stat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}

	if statErr == nil && fi.Size() > 0 {
		tmpPath, err = decompress(path)
		if err != nil {
			return nil, fmt.Errorf("decompressing %s: %w", path, err)
		}
	} else {
		tmp, err := os.CreateTemp("", "targz-*.tar")
		if err != nil {
			return nil, err
		}
		tmpPath = tmp.Name()
		_ = tmp.Close()
	}

	inner, err := archivetar.OpenRW(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	return &Archive{
		inner:    inner,
		origPath: path,
		tmpPath:  tmpPath,
	}, nil
}

// Entries returns the top-level archive entries.
func (a *Archive) Entries() []*archive.Entry { return a.inner.Entries() }

// OpenFile returns a reader over the in-memory data for the entry.
func (a *Archive) OpenFile(e *archive.Entry) (io.ReadCloser, error) {
	return a.inner.OpenFile(e)
}

// AddFile creates a new file entry in the archive.
func (a *Archive) AddFile(parent *archive.Entry, name string, data []byte, load, exec, attr uint32) (*archive.Entry, error) {
	return a.inner.AddFile(parent, name, data, load, exec, attr)
}

// AddDir creates a new directory entry in the archive.
func (a *Archive) AddDir(parent *archive.Entry, name string, load, exec, attr uint32) (*archive.Entry, error) {
	return a.inner.AddDir(parent, name, load, exec, attr)
}

// DeleteEntry removes an entry from the archive.
func (a *Archive) DeleteEntry(parent *archive.Entry, target *archive.Entry) error {
	return a.inner.DeleteEntry(parent, target)
}

// UpdateData replaces the stored data for an existing file entry.
func (a *Archive) UpdateData(e *archive.Entry, data []byte) {
	a.inner.UpdateData(e, data)
}

// Flush writes the inner tar to the temp file, then gzip-compresses
// it back to the original path.
func (a *Archive) Flush() error {
	if err := a.inner.Flush(); err != nil {
		return err
	}
	return a.compress()
}

// Close cleans up the temporary tar file.
func (a *Archive) Close() error {
	err := a.inner.Close()
	_ = os.Remove(a.tmpPath)
	return err
}

// CreateEmpty writes a valid empty gzip-compressed tar archive to path.
func CreateEmpty(path string) error {
	if err := archivetar.CreateEmpty(path + ".tmp"); err != nil {
		return err
	}
	defer func() { _ = os.Remove(path + ".tmp") }()
	return compressFile(path+".tmp", path)
}

func compressFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(out)
	if _, err := io.Copy(gw, in); err != nil {
		_ = gw.Close()
		_ = out.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func decompress(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return "", fmt.Errorf("creating gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tmp, err := os.CreateTemp("", "targz-*.tar")
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(tmp, gr); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("decompressing: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

func (a *Archive) compress() error {
	src, err := os.Open(a.tmpPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(a.origPath)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	gw := gzip.NewWriter(dst)

	if _, err := io.Copy(gw, src); err != nil {
		_ = gw.Close()
		return fmt.Errorf("compressing: %w", err)
	}

	return gw.Close()
}
