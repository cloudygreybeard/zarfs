package arcfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/riscos"
)

type dirRecord struct {
	comptype byte
	name     [11]byte
	origlen  uint32
	load     uint32
	exec     uint32
	packed   uint32
	complen  uint32
	infoWord uint32
}

// OpenRW opens an ArcFS archive at path for reading and writing.
func OpenRW(path string, passwd []byte) (*Archive, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	a := &Archive{
		f:         f,
		passwd:    passwd,
		readWrite: true,
		entryRec:  make(map[*archive.Entry]dirRecord),
		newData:   make(map[*archive.Entry][]byte),
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	a.fileSize = fi.Size()

	if a.fileSize == 0 {
		a.dataStart = arcfsHeaderSize
		a.dirty = true
		return a, nil
	}

	if err := a.parse(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return a, nil
}

// AddFile creates a new uncompressed file entry in the archive.
// The parent parameter determines where in the entry tree the file is
// placed: nil means top-level, otherwise it is appended to
// parent.Children.
func (a *Archive) AddFile(parent *archive.Entry, name string, data []byte, load, exec, attr uint32) (*archive.Entry, error) {
	if !a.readWrite {
		return nil, fmt.Errorf("archive not opened for writing")
	}
	if len(name) > 10 {
		return nil, fmt.Errorf("name %q exceeds 10 characters", name)
	}

	var nameBuf [11]byte
	copy(nameBuf[:], name)

	rec := dirRecord{
		comptype: ctStore,
		name:     nameBuf,
		origlen:  uint32(len(data)),
		load:     load,
		exec:     exec,
		packed:   attr,
		complen:  uint32(len(data)),
	}

	translated := riscos.TranslateFilename(name)
	e := &archive.Entry{
		Name:     riscos.AppendFileType(translated, load),
		IsDir:    false,
		Load:     load,
		Exec:     exec,
		Attr:     attr,
		CompType: ctStore,
		CompLen:  len(data),
		OrigLen:  len(data),
		FileTime: riscos.FileTime(load, exec),
		FileType: riscos.FileType(load),
	}

	a.entryRec[e] = rec
	a.newData[e] = append([]byte(nil), data...)

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
	if len(name) > 10 {
		return nil, fmt.Errorf("name %q exceeds 10 characters", name)
	}

	var nameBuf [11]byte
	copy(nameBuf[:], name)

	rec := dirRecord{
		comptype: ctStore,
		name:     nameBuf,
		origlen:  0xFFFFFFFF,
		load:     load,
		exec:     exec,
		packed:   attr,
		complen:  0xFFFFFFFF,
		infoWord: 0x80000000,
	}

	translated := riscos.TranslateFilename(name)
	e := &archive.Entry{
		Name:     riscos.AppendFileType(translated, load),
		IsDir:    true,
		Load:     load,
		Exec:     exec,
		Attr:     attr,
		CompType: ctStore,
		CompLen:  int(0xFFFFFFFF),
		OrigLen:  int(0xFFFFFFFF),
		FileTime: riscos.FileTime(load, exec),
		FileType: riscos.FileType(load),
	}

	a.entryRec[e] = rec
	if parent != nil {
		parent.Children = append(parent.Children, e)
	} else {
		a.entries = append(a.entries, e)
	}

	a.dirty = true
	return e, nil
}

// DeleteEntry removes an entry from the archive. The entry is removed
// from its parent's Children slice and its data is discarded on the
// next Flush.
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
			delete(a.entryRec, target)
			delete(a.newData, target)
			a.dirty = true
			return nil
		}
	}
	return fmt.Errorf("entry not found in parent")
}

// Flush rewrites the entire archive from in-memory state.
func (a *Archive) Flush() error {
	if !a.dirty {
		return nil
	}

	type fileBlob struct {
		entry *archive.Entry
		data  []byte
	}
	var files []fileBlob
	var records []dirRecord
	recordMap := make(map[*archive.Entry]int)

	var walk func([]*archive.Entry) error
	walk = func(entries []*archive.Entry) error {
		for _, e := range entries {
			rec, ok := a.entryRec[e]
			if !ok {
				rec = a.entryToRecord(e)
			}
			idx := len(records)
			records = append(records, rec)
			recordMap[e] = idx

			if e.IsDir {
				if err := walk(e.Children); err != nil {
					return err
				}
				records = append(records, dirRecord{comptype: ctEnd})
			} else {
				data, err := a.readEntryData(e)
				if err != nil {
					return fmt.Errorf("reading data for %s: %w", e.Name, err)
				}
				files = append(files, fileBlob{entry: e, data: data})
			}
		}
		return nil
	}
	if err := walk(a.entries); err != nil {
		return err
	}

	headerLen := uint32(len(records) * 36)
	newDataStart := uint32(arcfsHeaderSize) + headerLen

	offset := uint32(0)
	for i := range files {
		idx := recordMap[files[i].entry]
		records[idx].infoWord = offset
		records[idx].complen = uint32(len(files[i].data))
		records[idx].origlen = uint32(len(files[i].data))
		offset += uint32(len(files[i].data))
	}

	if _, err := a.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := a.f.Truncate(0); err != nil {
		return err
	}

	if err := a.writeHeader(headerLen, newDataStart); err != nil {
		return err
	}

	for _, rec := range records {
		if err := a.writeRecord(rec); err != nil {
			return err
		}
	}

	for _, fb := range files {
		if _, err := a.f.Write(fb.data); err != nil {
			return err
		}
	}

	a.dataStart = int64(newDataStart)
	a.dirty = false

	for i := range files {
		idx := recordMap[files[i].entry]
		files[i].entry.DataOffset = int64(records[idx].infoWord) + a.dataStart
		files[i].entry.CompType = int(records[idx].comptype)
		files[i].entry.CompLen = int(records[idx].complen)
		files[i].entry.OrigLen = int(records[idx].origlen)
	}

	a.entryRec = make(map[*archive.Entry]dirRecord)
	for e, idx := range recordMap {
		a.entryRec[e] = records[idx]
	}
	a.newData = make(map[*archive.Entry][]byte)

	fi, err := a.f.Stat()
	if err == nil {
		a.fileSize = fi.Size()
	}

	return nil
}

func (a *Archive) readEntryData(e *archive.Entry) ([]byte, error) {
	if data, ok := a.newData[e]; ok {
		return data, nil
	}
	if e.CompLen <= 0 {
		return nil, nil
	}
	data := make([]byte, e.CompLen)
	if _, err := a.f.ReadAt(data, e.DataOffset); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *Archive) entryToRecord(e *archive.Entry) dirRecord {
	var nameBuf [11]byte
	copy(nameBuf[:], e.Name)

	rec := dirRecord{
		comptype: byte(e.CompType),
		name:     nameBuf,
		origlen:  uint32(e.OrigLen),
		load:     e.Load,
		exec:     e.Exec,
		packed:   e.Attr | (uint32(e.MaxBits) << 8),
		complen:  uint32(e.CompLen),
	}
	if e.IsDir {
		rec.origlen = 0xFFFFFFFF
		rec.complen = 0xFFFFFFFF
		rec.infoWord = 0x80000000
	}
	return rec
}

func (a *Archive) writeHeader(headerLen, dataStart uint32) error {
	if _, err := a.f.Write([]byte("Archive\x00")); err != nil {
		return err
	}
	for _, v := range []uint32{headerLen, dataStart, 0, 0, 0} {
		if err := binary.Write(a.f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	reserved := make([]byte, 68)
	_, err := a.f.Write(reserved)
	return err
}

func (a *Archive) writeRecord(rec dirRecord) error {
	if _, err := a.f.Write([]byte{rec.comptype}); err != nil {
		return err
	}
	if _, err := a.f.Write(rec.name[:]); err != nil {
		return err
	}
	for _, v := range []uint32{rec.origlen, rec.load, rec.exec, rec.packed, rec.complen, rec.infoWord} {
		if err := binary.Write(a.f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return nil
}

// UpdateData replaces the stored data for an existing entry. The
// updated data is written on the next Flush.
func (a *Archive) UpdateData(e *archive.Entry, data []byte) {
	a.newData[e] = append([]byte(nil), data...)
	e.OrigLen = len(data)
	e.CompLen = len(data)
	e.CompType = ctStore
	if rec, ok := a.entryRec[e]; ok {
		rec.comptype = ctStore
		rec.origlen = uint32(len(data))
		rec.complen = uint32(len(data))
		a.entryRec[e] = rec
	}
	a.dirty = true
}

