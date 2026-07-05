// Package arcfs provides a virtual filesystem backed by a RISC OS
// archive file. ArcFS archives are mounted read-write; other formats
// are read-only. It translates archive entries into an inode tree that
// can be served by FUSE or NFS transport adapters.
package arcfs

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/archive/arcfs"
	"github.com/cloudygreybeard/zarfs/internal/archive/cfs"
	"github.com/cloudygreybeard/zarfs/internal/archive/packdir"
	"github.com/cloudygreybeard/zarfs/internal/archive/spark"
	"github.com/cloudygreybeard/zarfs/internal/archive/squash"
	archivetar "github.com/cloudygreybeard/zarfs/internal/archive/tar"
	"github.com/cloudygreybeard/zarfs/internal/archive/targz"
)

// RootInodeID is the inode number of the filesystem root.
const RootInodeID uint64 = 1

const (
	statBlockSize   = 4096
	statTotalBlocks = 1024
)

// FS is a virtual filesystem backed by a RISC OS archive.
type FS struct {
	arc      archive.Archive
	writable archive.WritableArchive
	format   archive.Format
	inodes   *InodeTable
	arcPath  string
	readOnly bool

	mu      sync.Mutex
	nextHID HandleID
	handles map[HandleID]*openHandle
}

// HandleID identifies an open file handle.
type HandleID uint64

type openHandle struct {
	mu     sync.Mutex
	entry  *archive.Entry
	inode  *Inode
	data   []byte
	dirty  bool
}

// Attr holds filesystem attributes for a node.
type Attr struct {
	Size  uint64
	Mode  os.FileMode
	Nlink uint32
	Uid   uint32
	Gid   uint32
	Atime time.Time
	Mtime time.Time
	Ctime time.Time
}

// StatFS holds filesystem-level statistics.
type StatFS struct {
	BlockSize      uint32
	Blocks         uint64
	BlocksFree     uint64
	BlocksAvailable uint64
	Inodes         uint64
	InodesFree     uint64
}

// Open opens the archive at path and builds the inode tree. The
// password is used for garbled Spark/ArcFS archives; pass nil for
// unprotected archives. When readOnly is false and the format is
// ArcFS, the archive is opened for writing.
func Open(path string, password []byte, readOnly bool) (*FS, error) {
	return OpenFormat(path, password, readOnly, archive.FormatUnknown)
}

// OpenFormat opens the archive at path with an explicit format. If
// format is FormatUnknown, the format is auto-detected from the file
// header.
func OpenFormat(path string, password []byte, readOnly bool, format archive.Format) (*FS, error) {
	if format == archive.FormatUnknown {
		var err error
		format, err = archive.Detect(path)
		if err != nil {
			return nil, fmt.Errorf("detecting format: %w", err)
		}
		if format == archive.FormatUnknown {
			return nil, fmt.Errorf("unrecognised archive format: %s", path)
		}
	}

	arc, err := openArchive(path, format, password, readOnly)
	if err != nil {
		return nil, fmt.Errorf("opening %s archive: %w", format, err)
	}

	f := &FS{
		arc:      arc,
		format:   format,
		arcPath:  path,
		readOnly: readOnly || !formatSupportsWrite(format),
		nextHID:  1,
		handles:  make(map[HandleID]*openHandle),
	}

	if wa, ok := arc.(archive.WritableArchive); ok && !f.readOnly {
		f.writable = wa
	}

	f.inodes = NewInodeTable()
	f.buildTree(arc.Entries())

	return f, nil
}

// Format returns the detected archive format.
func (f *FS) Format() archive.Format {
	return f.format
}

// ReadOnly reports whether the filesystem is mounted read-only.
func (f *FS) ReadOnly() bool {
	return f.readOnly
}

// Close syncs any pending changes and releases the underlying archive.
func (f *FS) Close() error {
	if f.writable != nil {
		if err := f.Sync(); err != nil {
			_ = f.arc.Close()
			return err
		}
	}
	return f.arc.Close()
}

// Lookup finds a child by name within the given parent directory.
func (f *FS) Lookup(parentID uint64, name string) (*Inode, error) {
	parent := f.inodes.Get(parentID)
	if parent == nil {
		return nil, syscall.ENOENT
	}
	for _, childID := range parent.ChildIDs {
		child := f.inodes.Get(childID)
		if child != nil && child.Name == name {
			return child, nil
		}
	}
	return nil, syscall.ENOENT
}

// Children returns all child inodes of a directory.
func (f *FS) Children(parentID uint64) ([]*Inode, error) {
	parent := f.inodes.Get(parentID)
	if parent == nil {
		return nil, syscall.ENOENT
	}
	children := make([]*Inode, 0, len(parent.ChildIDs))
	for _, id := range parent.ChildIDs {
		if child := f.inodes.Get(id); child != nil {
			children = append(children, child)
		}
	}
	return children, nil
}

// GetInode returns the inode with the given ID.
func (f *FS) GetInode(id uint64) *Inode {
	return f.inodes.Get(id)
}

// GetAttr returns filesystem attributes for the given inode.
func (f *FS) GetAttr(id uint64) (Attr, error) {
	node := f.inodes.Get(id)
	if node == nil {
		return Attr{}, syscall.ENOENT
	}

	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	dirPerm := os.FileMode(0o555)
	filePerm := os.FileMode(0o444)
	if !f.readOnly {
		dirPerm = 0o755
		filePerm = 0o644
	}

	if node.Dir {
		return Attr{
			Mode:  os.ModeDir | dirPerm,
			Nlink: uint32(2 + len(node.ChildIDs)),
			Uid:   uid,
			Gid:   gid,
			Atime: node.ModTime,
			Mtime: node.ModTime,
			Ctime: node.ModTime,
		}, nil
	}

	size := uint64(0)
	if node.Entry != nil {
		size = uint64(node.Entry.OrigLen)
	}

	return Attr{
		Size:  size,
		Mode:  filePerm,
		Nlink: 1,
		Uid:   uid,
		Gid:   gid,
		Atime: node.ModTime,
		Mtime: node.ModTime,
		Ctime: node.ModTime,
	}, nil
}

// OpenFile opens a file for reading and returns a handle ID.
func (f *FS) OpenFile(id uint64) (HandleID, error) {
	node := f.inodes.Get(id)
	if node == nil {
		return 0, syscall.ENOENT
	}
	if node.Dir {
		return 0, syscall.EISDIR
	}

	rc, err := f.arc.OpenFile(node.Entry)
	if err != nil {
		return 0, fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return 0, fmt.Errorf("reading file: %w", err)
	}

	f.mu.Lock()
	hid := f.nextHID
	f.nextHID++
	f.handles[hid] = &openHandle{entry: node.Entry, data: data}
	f.mu.Unlock()

	return hid, nil
}

// ReadHandle reads from an open file handle at the given offset.
func (f *FS) ReadHandle(hid HandleID, off int64, buf []byte) (int, error) {
	f.mu.Lock()
	h, ok := f.handles[hid]
	f.mu.Unlock()

	if !ok {
		return 0, syscall.EBADF
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if off >= int64(len(h.data)) {
		return 0, io.EOF
	}

	n := copy(buf, h.data[off:])
	return n, nil
}

// ReleaseHandle closes an open file handle.
func (f *FS) ReleaseHandle(hid HandleID) {
	f.mu.Lock()
	delete(f.handles, hid)
	f.mu.Unlock()
}

// CreateFile creates a new empty file under parentID and returns an
// open handle for writing.
func (f *FS) CreateFile(parentID uint64, name string) (HandleID, *Inode, error) {
	if f.writable == nil {
		return 0, nil, syscall.EROFS
	}

	parent := f.inodes.Get(parentID)
	if parent == nil {
		return 0, nil, syscall.ENOENT
	}

	var parentEntry *archive.Entry
	if parentID != RootInodeID {
		parentEntry = parent.Entry
	}

	entry, err := f.writable.AddFile(parentEntry, name, nil, 0, 0, 0)
	if err != nil {
		return 0, nil, err
	}

	now := time.Now()
	id := f.inodes.Add(parentID, entry.Name, false, now, entry)
	node := f.inodes.Get(id)

	f.mu.Lock()
	hid := f.nextHID
	f.nextHID++
	f.handles[hid] = &openHandle{entry: entry, inode: node, data: nil, dirty: true}
	f.mu.Unlock()

	return hid, node, nil
}

// WriteHandle writes data to an open file handle's buffer.
func (f *FS) WriteHandle(hid HandleID, off int64, data []byte) (int, error) {
	f.mu.Lock()
	h, ok := f.handles[hid]
	f.mu.Unlock()

	if !ok {
		return 0, syscall.EBADF
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	end := off + int64(len(data))
	if end > int64(len(h.data)) {
		grown := make([]byte, end)
		copy(grown, h.data)
		h.data = grown
	}
	n := copy(h.data[off:], data)
	h.dirty = true

	if h.entry != nil {
		h.entry.OrigLen = len(h.data)
		h.entry.CompLen = len(h.data)
	}

	return n, nil
}

// TruncateHandle truncates the data in an open handle to size bytes.
func (f *FS) TruncateHandle(hid HandleID, size int64) error {
	f.mu.Lock()
	h, ok := f.handles[hid]
	f.mu.Unlock()

	if !ok {
		return syscall.EBADF
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if size < int64(len(h.data)) {
		h.data = h.data[:size]
	} else if size > int64(len(h.data)) {
		grown := make([]byte, size)
		copy(grown, h.data)
		h.data = grown
	}
	h.dirty = true

	if h.entry != nil {
		h.entry.OrigLen = int(size)
		h.entry.CompLen = int(size)
	}

	return nil
}

// FlushHandle writes a dirty handle's buffered data back to the
// archive.
func (f *FS) FlushHandle(hid HandleID) error {
	if f.writable == nil {
		return nil
	}

	f.mu.Lock()
	h, ok := f.handles[hid]
	f.mu.Unlock()

	if !ok {
		return syscall.EBADF
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.dirty {
		return nil
	}

	if h.entry != nil {
		f.writable.UpdateData(h.entry, h.data)
	}

	h.dirty = false
	return nil
}

// Remove removes a file or directory from the filesystem.
func (f *FS) Remove(parentID uint64, name string, isDir bool) error {
	if f.writable == nil {
		return syscall.EROFS
	}

	parent := f.inodes.Get(parentID)
	if parent == nil {
		return syscall.ENOENT
	}

	child, err := f.Lookup(parentID, name)
	if err != nil {
		return err
	}

	if isDir && child.Dir && len(child.ChildIDs) > 0 {
		return syscall.ENOTEMPTY
	}

	var parentEntry *archive.Entry
	if parentID != RootInodeID {
		parentEntry = parent.Entry
	}

	if err := f.writable.DeleteEntry(parentEntry, child.Entry); err != nil {
		return err
	}

	f.inodes.RemoveChild(parentID, child.ID)
	return nil
}

// Mkdir creates a new directory under parentID.
func (f *FS) Mkdir(parentID uint64, name string) (*Inode, error) {
	if f.writable == nil {
		return nil, syscall.EROFS
	}

	parent := f.inodes.Get(parentID)
	if parent == nil {
		return nil, syscall.ENOENT
	}

	var parentEntry *archive.Entry
	if parentID != RootInodeID {
		parentEntry = parent.Entry
	}

	entry, err := f.writable.AddDir(parentEntry, name, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	id := f.inodes.Add(parentID, entry.Name, true, now, entry)
	return f.inodes.Get(id), nil
}

// Sync flushes all pending changes to the underlying archive.
func (f *FS) Sync() error {
	if f.writable == nil {
		return nil
	}
	f.flushAllHandles()
	return f.writable.Flush()
}

func (f *FS) flushAllHandles() {
	f.mu.Lock()
	handles := make([]HandleID, 0, len(f.handles))
	for hid := range f.handles {
		handles = append(handles, hid)
	}
	f.mu.Unlock()

	for _, hid := range handles {
		_ = f.FlushHandle(hid)
	}
}

// StatFSInfo returns filesystem-level statistics.
func (f *FS) StatFSInfo() StatFS {
	count := f.inodes.Count()
	return StatFS{
		BlockSize: statBlockSize,
		Blocks:    statTotalBlocks,
		Inodes:    uint64(count),
	}
}

func (f *FS) buildTree(entries []*archive.Entry) {
	f.addEntries(RootInodeID, entries)
}

func (f *FS) addEntries(parentID uint64, entries []*archive.Entry) {
	for _, e := range entries {
		f.addEntry(parentID, e)
	}
}

// addEntry adds a single entry to the inode tree. If the entry name
// contains path separators, intermediate directories are synthesized.
func (f *FS) addEntry(parentID uint64, e *archive.Entry) {
	parts := strings.Split(e.Name, "/")
	cur := parentID

	// Create intermediate directories for path components before the
	// leaf. For example, "!dj3/!Run,feb" creates a "!dj3" directory
	// under the parent, then adds "!Run,feb" inside it.
	for _, dir := range parts[:len(parts)-1] {
		existing := f.inodes.FindChild(cur, dir)
		if existing != 0 {
			cur = existing
		} else {
			cur = f.inodes.Add(cur, dir, true, e.FileTime, nil)
		}
	}

	leaf := parts[len(parts)-1]
	id := f.inodes.Add(cur, leaf, e.IsDir, e.FileTime, e)
	if e.IsDir && len(e.Children) > 0 {
		f.addEntries(id, e.Children)
	}
}

func formatSupportsWrite(f archive.Format) bool {
	switch f {
	case archive.FormatArcFS, archive.FormatTar, archive.FormatTarGz:
		return true
	default:
		return false
	}
}

func openArchive(path string, format archive.Format, password []byte, readOnly bool) (archive.Archive, error) {
	switch format {
	case archive.FormatSpark:
		return spark.Open(path, password)
	case archive.FormatArcFS:
		if !readOnly {
			return arcfs.OpenRW(path, password)
		}
		return arcfs.Open(path, password)
	case archive.FormatPackDir:
		return packdir.Open(path)
	case archive.FormatSquash:
		return squash.Open(path)
	case archive.FormatCFS:
		return cfs.Open(path)
	case archive.FormatTar:
		if !readOnly {
			return archivetar.OpenRW(path)
		}
		return archivetar.Open(path)
	case archive.FormatTarGz:
		if !readOnly {
			return targz.OpenRW(path)
		}
		return targz.Open(path)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}
