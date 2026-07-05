// Package spark reads Spark (and SparkFS) archive files.
package spark

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

// Compression type constants.
const (
	ctEndDir      = 0x00
	ctNotComp     = 0x01
	ctNotComp2    = 0x02
	ctPack        = 0x03
	ctPackSqueeze = 0x04
	ctCrunch      = 0x08
	ctSquash      = 0x09
	ctComp        = 0x7f

	archpackBit = 0x80
	startByte   = 0x1a
)

// Archive implements archive.Archive for Spark files.
type Archive struct {
	f       *os.File
	entries []*archive.Entry
	passwd  []byte
}

// Open opens a Spark archive at path. Pass nil for passwd if none.
func Open(path string, passwd []byte) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	a := &Archive{f: f, passwd: passwd}
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
	gr := compress.NewGarbleReader(lr, a.passwd)

	switch e.CompType {
	case ctNotComp, ctNotComp2:
		return io.NopCloser(gr), nil
	case ctComp:
		return io.NopCloser(compress.NewNcompressReader(gr, compress.Compress, 0)), nil
	case ctPack:
		return io.NopCloser(compress.NewPackReader(gr)), nil
	case ctPackSqueeze:
		return io.NopCloser(compress.NewPackReader(compress.NewHuffReader(gr))), nil
	case ctCrunch:
		return io.NopCloser(compress.NewPackReader(
			compress.NewNcompressReader(gr, compress.Crunch, e.MaxBits))), nil
	case ctSquash:
		return io.NopCloser(compress.NewNcompressReader(gr, compress.Squash, 0)), nil
	default:
		return nil, fmt.Errorf("unsupported spark compression type 0x%02x", e.CompType)
	}
}

func (a *Archive) read16() (uint16, error) {
	var v uint16
	if err := binary.Read(a.f, binary.LittleEndian, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func (a *Archive) read32() (uint32, error) {
	var v uint32
	if err := binary.Read(a.f, binary.LittleEndian, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func (a *Archive) parse() error {
	var hdr [2]byte
	if _, err := io.ReadFull(a.f, hdr[:]); err != nil {
		return fmt.Errorf("reading spark header: %w", err)
	}
	if hdr[0] != startByte || hdr[1]&0x80 == 0 {
		return fmt.Errorf("invalid spark file")
	}

	var curDir string
	// dirStack tracks the directory Entry at each nesting level so
	// children can be attached.
	var dirStack []*archive.Entry
	offset := int64(1)

	for {
		if _, err := a.f.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seeking to entry at offset %d: %w", offset, err)
		}

		b := make([]byte, 1)
		if n, err := a.f.Read(b); n == 0 || err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("reading comptype at offset %d: %w", offset, err)
		}
		comptype := int(b[0])

		if comptype&^archpackBit == 0 {
			idx := strings.LastIndex(curDir, "/")
			if idx > -1 {
				curDir = curDir[:idx]
			} else {
				if curDir == "" {
					break
				}
				curDir = ""
			}
			if len(dirStack) > 0 {
				dirStack = dirStack[:len(dirStack)-1]
			}
			offset += 2
			continue
		}

		nameBuf := make([]byte, 13)
		if _, err := io.ReadFull(a.f, nameBuf); err != nil {
			return fmt.Errorf("reading entry name at offset %d: %w", offset, err)
		}
		nul := 0
		for nul < len(nameBuf) {
			if nameBuf[nul] < ' ' || nameBuf[nul] > '~' {
				break
			}
			nul++
		}
		name := string(nameBuf[:nul])
		if name == "" {
			break
		}

		var localName string
		translated := riscos.TranslateFilename(name)
		if curDir != "" {
			localName = curDir + "/" + translated
		} else {
			localName = translated
		}

		complen, err := a.read32()
		if err != nil {
			return fmt.Errorf("reading complen for %s: %w", name, err)
		}
		if _, err := a.read16(); err != nil {
			return fmt.Errorf("reading date for %s: %w", name, err)
		}
		if _, err := a.read16(); err != nil {
			return fmt.Errorf("reading time for %s: %w", name, err)
		}
		if _, err := a.read16(); err != nil {
			return fmt.Errorf("reading crc for %s: %w", name, err)
		}

		var origlen uint32
		if comptype&^archpackBit > ctNotComp {
			origlen, err = a.read32()
			if err != nil {
				return fmt.Errorf("reading origlen for %s: %w", name, err)
			}
		} else {
			origlen = complen
		}

		var load, exec, attr uint32
		if comptype&archpackBit != 0 {
			load, err = a.read32()
			if err != nil {
				return fmt.Errorf("reading load for %s: %w", name, err)
			}
			exec, err = a.read32()
			if err != nil {
				return fmt.Errorf("reading exec for %s: %w", name, err)
			}
			attr, err = a.read32()
			if err != nil {
				return fmt.Errorf("reading attr for %s: %w", name, err)
			}
		}
		comptype &^= archpackBit

		dataPos, _ := a.f.Seek(0, io.SeekCurrent)
		isDir := riscos.IsDirectory(load)

		e := &archive.Entry{
			Name:       riscos.AppendFileType(localName, load),
			IsDir:      isDir,
			Load:       load,
			Exec:       exec,
			Attr:       attr,
			CompType:   comptype,
			CompLen:    int(complen),
			OrigLen:    int(origlen),
			DataOffset: dataPos,
			FileTime:   riscos.FileTime(load, exec),
			FileType:   riscos.FileType(load),
		}

		if isDir {
			if curDir != "" {
				curDir = curDir + "/" + name
			} else {
				curDir = name
			}
			if len(dirStack) > 0 {
				parent := dirStack[len(dirStack)-1]
				parent.Children = append(parent.Children, e)
			} else {
				a.entries = append(a.entries, e)
			}
			dirStack = append(dirStack, e)
			offset = dataPos + 1
		} else {
			if len(dirStack) > 0 {
				parent := dirStack[len(dirStack)-1]
				parent.Children = append(parent.Children, e)
			} else {
				a.entries = append(a.entries, e)
			}
			offset = dataPos + int64(complen) + 1
		}
	}
	return nil
}
