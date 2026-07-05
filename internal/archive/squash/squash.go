// Package squash reads RISC OS Squash single-file archives.
package squash

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/compress"
	"github.com/cloudygreybeard/zarfs/internal/riscos"
)

// Archive implements archive.Archive for Squash files.
type Archive struct {
	f       *os.File
	entries []*archive.Entry
}

// Open opens a Squash archive at path.
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	a := &Archive{f: f}
	if err := a.parse(path); err != nil {
		_ = f.Close()
		return nil, err
	}
	return a, nil
}

// Entries returns the archive entries (always one file).
func (a *Archive) Entries() []*archive.Entry { return a.entries }

// Close releases the underlying file.
func (a *Archive) Close() error { return a.f.Close() }

// OpenFile returns a reader that decompresses the entry's data.
// Squash files contain a Unix compress stream.
func (a *Archive) OpenFile(e *archive.Entry) (io.ReadCloser, error) {
	if _, err := a.f.Seek(e.DataOffset, io.SeekStart); err != nil {
		return nil, err
	}
	lr := io.LimitReader(a.f, int64(e.CompLen))
	return io.NopCloser(compress.NewNcompressReader(lr, compress.UnixCompress, 0)), nil
}

func (a *Archive) parse(path string) error {
	var magic [4]byte
	if _, err := io.ReadFull(a.f, magic[:]); err != nil {
		return fmt.Errorf("reading squash header: %w", err)
	}
	if string(magic[:]) != "SQSH" {
		return fmt.Errorf("invalid squash magic")
	}

	var origlen, load, exec, skip uint32
	if err := binary.Read(a.f, binary.LittleEndian, &origlen); err != nil {
		return fmt.Errorf("reading squash origlen: %w", err)
	}
	if err := binary.Read(a.f, binary.LittleEndian, &load); err != nil {
		return fmt.Errorf("reading squash load: %w", err)
	}
	if err := binary.Read(a.f, binary.LittleEndian, &exec); err != nil {
		return fmt.Errorf("reading squash exec: %w", err)
	}
	if err := binary.Read(a.f, binary.LittleEndian, &skip); err != nil {
		return fmt.Errorf("reading squash reserved: %w", err)
	}

	dataPos, _ := a.f.Seek(0, io.SeekCurrent)
	fi, err := a.f.Stat()
	if err != nil {
		return err
	}
	complen := fi.Size() - dataPos

	basename := filepath.Base(path)
	name := basename
	for _, suffix := range []string{",fca", ".fca"} {
		if strings.HasSuffix(name, suffix) {
			name = name[:len(name)-4]
			break
		}
	}

	e := &archive.Entry{
		Name:       riscos.AppendFileType(name, load),
		Load:       load,
		Exec:       exec,
		CompLen:    int(complen),
		OrigLen:    int(origlen),
		DataOffset: dataPos,
		FileTime:   riscos.FileTime(load, exec),
		FileType:   riscos.FileType(load),
	}
	a.entries = append(a.entries, e)
	return nil
}
