// Package archive provides types and interfaces for reading RISC OS
// archive file formats.
package archive

import (
	"io"
	"time"
)

// Entry represents a single file or directory within an archive.
type Entry struct {
	Name       string
	IsDir      bool
	Load       uint32
	Exec       uint32
	Attr       uint32
	CompType   int
	CompLen    int
	OrigLen    int
	MaxBits    int
	DataOffset int64
	FileTime   time.Time
	FileType   int
	Children   []*Entry
}

// Archive provides access to entries and file data within a RISC OS
// archive.
type Archive interface {
	Entries() []*Entry
	OpenFile(e *Entry) (io.ReadCloser, error)
	Close() error
}

// WritableArchive extends Archive with mutation operations. Only the
// ArcFS format supports writing.
type WritableArchive interface {
	Archive
	AddFile(parent *Entry, name string, data []byte, load, exec, attr uint32) (*Entry, error)
	AddDir(parent *Entry, name string, load, exec, attr uint32) (*Entry, error)
	DeleteEntry(parent *Entry, target *Entry) error
	UpdateData(e *Entry, data []byte)
	Flush() error
}
