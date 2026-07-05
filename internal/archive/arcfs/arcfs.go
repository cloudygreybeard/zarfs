// Package arcfs reads ArcFS archive files.
package arcfs

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

// Compression type constants for ArcFS.
const (
	ctStore    = 0x82
	ctPack     = 0x83
	ctCrunch   = 0x88
	ctCompress = 0xff
	ctEnd      = 0x00
	ctDeleted  = 0x01
)

const arcfsHeaderSize = 96

// Archive implements archive.Archive for ArcFS files.
type Archive struct {
	f         *os.File
	entries   []*archive.Entry
	passwd    []byte
	dataStart int64

	readWrite  bool
	dirty      bool
	fileSize   int64
	dirRecords []dirRecord
	entryRec   map[*archive.Entry]dirRecord
	newData    map[*archive.Entry][]byte
}

// Open opens an ArcFS archive at path.
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
	case ctStore:
		return io.NopCloser(gr), nil
	case ctCompress:
		return io.NopCloser(compress.NewNcompressReader(gr, compress.Compress, e.MaxBits)), nil
	case ctPack:
		return io.NopCloser(compress.NewPackReader(gr)), nil
	case ctCrunch:
		return io.NopCloser(compress.NewPackReader(
			compress.NewNcompressReader(gr, compress.Crunch, e.MaxBits))), nil
	default:
		return nil, fmt.Errorf("unsupported arcfs compression type 0x%02x", e.CompType)
	}
}

func (a *Archive) parse() error {
	magic := make([]byte, 8)
	if _, err := io.ReadFull(a.f, magic); err != nil {
		return fmt.Errorf("reading arcfs header: %w", err)
	}
	if string(magic) != "Archive\x00" {
		return fmt.Errorf("invalid arcfs magic")
	}

	var headerLen, dataStart, version uint32
	if err := binary.Read(a.f, binary.LittleEndian, &headerLen); err != nil {
		return err
	}
	if err := binary.Read(a.f, binary.LittleEndian, &dataStart); err != nil {
		return err
	}
	if err := binary.Read(a.f, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version > 40 {
		return fmt.Errorf("arcfs version %d too high", version)
	}
	a.dataStart = int64(dataStart)

	// Skip remaining 76 bytes of the 88-byte header (rwVersion, arcFormat, 17 reserved).
	if _, err := a.f.Seek(76, io.SeekCurrent); err != nil {
		return err
	}

	numEntries := int(headerLen) / 36
	var curDir string
	var dirStack []*archive.Entry

	for i := 0; i < numEntries; i++ {
		var ctByte [1]byte
		if _, err := a.f.Read(ctByte[:]); err != nil {
			return fmt.Errorf("reading entry %d comptype: %w", i, err)
		}
		comptype := int(ctByte[0])

		var nameBuf [11]byte
		if _, err := io.ReadFull(a.f, nameBuf[:]); err != nil {
			return fmt.Errorf("reading entry %d name: %w", i, err)
		}
		nul := 0
		for nul < len(nameBuf) {
			if nameBuf[nul] == 0 {
				break
			}
			nul++
		}
		name := string(nameBuf[:nul])

		var origlen, load, exec, packed, complen, infoWord uint32
		if err := binary.Read(a.f, binary.LittleEndian, &origlen); err != nil {
			return fmt.Errorf("reading entry %d origlen: %w", i, err)
		}
		if err := binary.Read(a.f, binary.LittleEndian, &load); err != nil {
			return fmt.Errorf("reading entry %d load: %w", i, err)
		}
		if err := binary.Read(a.f, binary.LittleEndian, &exec); err != nil {
			return fmt.Errorf("reading entry %d exec: %w", i, err)
		}
		if err := binary.Read(a.f, binary.LittleEndian, &packed); err != nil {
			return fmt.Errorf("reading entry %d packed: %w", i, err)
		}
		if err := binary.Read(a.f, binary.LittleEndian, &complen); err != nil {
			return fmt.Errorf("reading entry %d complen: %w", i, err)
		}
		if err := binary.Read(a.f, binary.LittleEndian, &infoWord); err != nil {
			return fmt.Errorf("reading entry %d infoWord: %w", i, err)
		}

		rec := dirRecord{
			comptype: byte(comptype),
			name:     nameBuf,
			origlen:  origlen,
			load:     load,
			exec:     exec,
			packed:   packed,
			complen:  complen,
			infoWord: infoWord,
		}
		a.dirRecords = append(a.dirRecords, rec)

		attr := packed & 0xff
		maxbits := int((packed >> 8) & 0xff)
		dataOffset := int64(infoWord&0x7fffffff) + a.dataStart
		isDir := infoWord&0x80000000 != 0 && origlen == 0xffffffff && complen == 0xffffffff

		if comptype == ctEnd {
			idx := strings.LastIndex(curDir, "/")
			if idx > -1 {
				curDir = curDir[:idx]
			} else {
				curDir = ""
			}
			if len(dirStack) > 0 {
				dirStack = dirStack[:len(dirStack)-1]
			}
			continue
		}
		if comptype == ctDeleted {
			continue
		}

		var localName string
		translated := riscos.TranslateFilename(name)
		if curDir != "" {
			localName = curDir + "/" + translated
		} else {
			localName = translated
		}

		e := &archive.Entry{
			Name:       riscos.AppendFileType(localName, load),
			IsDir:      isDir,
			Load:       load,
			Exec:       exec,
			Attr:       attr,
			CompType:   comptype,
			CompLen:    int(complen),
			OrigLen:    int(origlen),
			MaxBits:    maxbits,
			DataOffset: dataOffset,
			FileTime:   riscos.FileTime(load, exec),
			FileType:   riscos.FileType(load),
		}

		if a.entryRec != nil {
			a.entryRec[e] = rec
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
		} else {
			if len(dirStack) > 0 {
				parent := dirStack[len(dirStack)-1]
				parent.Children = append(parent.Children, e)
			} else {
				a.entries = append(a.entries, e)
			}
		}
	}
	return nil
}
