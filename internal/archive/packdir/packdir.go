// Package packdir reads RISC OS PackDir archive files.
package packdir

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/compress"
	"github.com/cloudygreybeard/zarfs/internal/riscos"
)

const (
	ctNotComp = 0x01
	ctLZW     = 0x07

	rootDirOffset int64 = 9
	maxNestDepth  int   = 1000
)

// Archive implements archive.Archive for PackDir files.
type Archive struct {
	f       *os.File
	entries []*archive.Entry
	lzwBits int
}

// Open opens a PackDir archive at path.
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	a := &Archive{f: f}
	if err := a.parse(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return a, nil
}

// Entries returns the top-level archive entries.
func (a *Archive) Entries() []*archive.Entry { return a.entries }

// Close releases the underlying file.
func (a *Archive) Close() error { return a.f.Close() }

// OpenFile returns a reader that decompresses the entry's data.
func (a *Archive) OpenFile(e *archive.Entry) (io.ReadCloser, error) {
	if _, err := a.f.Seek(e.DataOffset, io.SeekStart); err != nil {
		return nil, err
	}
	lr := io.LimitReader(a.f, int64(e.CompLen))

	switch e.CompType {
	case ctNotComp:
		return io.NopCloser(lr), nil
	case ctLZW:
		return io.NopCloser(compress.NewLZWZooReader(lr, e.MaxBits)), nil
	default:
		return nil, fmt.Errorf("unsupported packdir compression type 0x%02x", e.CompType)
	}
}

func (a *Archive) readString() (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		if _, err := a.f.Read(b); err != nil {
			if len(buf) > 0 {
				return string(buf), nil
			}
			return "", err
		}
		if b[0] == 0 {
			return string(buf), nil
		}
		buf = append(buf, b[0])
	}
}

func (a *Archive) read32() (uint32, error) {
	var v uint32
	if err := binary.Read(a.f, binary.LittleEndian, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func (a *Archive) parse() error {
	hdr, err := a.readString()
	if err != nil || hdr != "PACK" {
		return fmt.Errorf("invalid packdir header")
	}
	bits, err := a.read32()
	if err != nil {
		return err
	}
	a.lzwBits = int(bits) + 12

	var curDir string
	var dirStack []*archive.Entry
	var dirEntries []int
	dirIdx := -1

	offset, _ := a.f.Seek(0, io.SeekCurrent)
	first := true

	for {
		isRoot := offset == rootDirOffset
		if _, err := a.f.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seeking to offset %d: %w", offset, err)
		}

		name, err := a.readString()
		if err != nil || name == "" {
			break
		}

		if isRoot {
			i := strings.LastIndex(name, ".")
			if i == -1 {
				i = strings.LastIndex(name, ":")
			}
			if i != -1 {
				name = name[i+1:]
			}
		}

		load, err := a.read32()
		if err != nil {
			return fmt.Errorf("reading load for %s: %w", name, err)
		}
		exec, err := a.read32()
		if err != nil {
			return fmt.Errorf("reading exec for %s: %w", name, err)
		}
		n, err := a.read32()
		if err != nil {
			return fmt.Errorf("reading n for %s: %w", name, err)
		}
		attr, err := a.read32()
		if err != nil {
			return fmt.Errorf("reading attr for %s: %w", name, err)
		}

		var entryType uint32
		if isRoot {
			entryType = 1
		} else {
			entryType, err = a.read32()
			if err != nil {
				return fmt.Errorf("reading entryType for %s: %w", name, err)
			}
		}

		var localName string
		if curDir != "" {
			localName = curDir + "/" + strings.ReplaceAll(name, "/", ".")
		} else {
			localName = name
		}

		if entryType == 1 {
			e := &archive.Entry{
				Name:     localName,
				IsDir:    true,
				Load:     load,
				Exec:     exec,
				Attr:     attr,
				FileTime: riscos.FileTime(load, exec),
				FileType: riscos.FileType(load),
			}

			if curDir == "" {
				curDir = name
			} else {
				curDir = curDir + "/" + name
			}

			if dirIdx >= 0 {
				dirEntries[dirIdx]--
			}
			dirIdx++
			if dirIdx >= maxNestDepth {
				return fmt.Errorf("directory nesting exceeds maximum depth of %d", maxNestDepth)
			}
			if dirIdx >= len(dirEntries) {
				dirEntries = append(dirEntries, 0)
			}
			dirEntries[dirIdx] = int(n)

			if len(dirStack) > 0 {
				parent := dirStack[len(dirStack)-1]
				parent.Children = append(parent.Children, e)
			} else {
				a.entries = append(a.entries, e)
			}
			dirStack = append(dirStack, e)

			offset, _ = a.f.Seek(0, io.SeekCurrent)
		} else {
			origlen := int(n)
			complen, err := a.read32()
			if err != nil {
				return fmt.Errorf("reading complen for %s: %w", name, err)
			}

			var comptype int
			cl := int(complen)
			if int32(complen) == -1 {
				comptype = ctNotComp
				cl = origlen
			} else {
				comptype = ctLZW
			}

			dataPos, _ := a.f.Seek(0, io.SeekCurrent)

			e := &archive.Entry{
				Name:       riscos.AppendFileType(localName, load),
				Load:       load,
				Exec:       exec,
				Attr:       attr,
				CompType:   comptype,
				CompLen:    cl,
				OrigLen:    origlen,
				MaxBits:    a.lzwBits,
				DataOffset: dataPos,
				FileTime:   riscos.FileTime(load, exec),
				FileType:   riscos.FileType(load),
			}

			if dirIdx >= 0 {
				dirEntries[dirIdx]--
			}

			if len(dirStack) > 0 {
				parent := dirStack[len(dirStack)-1]
				parent.Children = append(parent.Children, e)
			} else {
				a.entries = append(a.entries, e)
			}

			if int32(complen) == -1 {
				offset = dataPos + int64(origlen)
			} else {
				offset = dataPos + int64(complen)
			}
		}

		for dirIdx >= 0 && dirEntries[dirIdx] == 0 {
			i := strings.LastIndex(curDir, "/")
			if i != -1 {
				curDir = curDir[:i]
			} else {
				curDir = ""
			}
			dirIdx--
			if len(dirStack) > 0 {
				dirStack = dirStack[:len(dirStack)-1]
			}
		}

		if first && dirEntries[0] <= 0 {
			break
		}
		first = false
		if dirIdx < 0 {
			break
		}
	}
	return nil
}
