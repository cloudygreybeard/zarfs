package nfsmount

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	billy "github.com/go-git/go-billy/v5"
	nfsfile "github.com/willscott/go-nfs/file"

	"github.com/cloudygreybeard/zarfs/internal/arcfs"
)

// BillyFS adapts an arcfs.FS to the billy.Filesystem interface used by
// willscott/go-nfs.
type BillyFS struct {
	afs *arcfs.FS
	ctx context.Context

	mu      sync.Mutex
	handles map[*billyFile]bool
}

// NewBillyFS creates a billy.Filesystem backed by an arcfs.FS.
func NewBillyFS(ctx context.Context, afs *arcfs.FS) *BillyFS {
	return &BillyFS{
		afs:     afs,
		ctx:     ctx,
		handles: make(map[*billyFile]bool),
	}
}

func (b *BillyFS) resolve(path string) (*arcfs.Inode, error) {
	path = filepath.Clean(path)
	if path == "." || path == "/" || path == "" {
		return b.afs.GetInode(arcfs.RootInodeID), nil
	}

	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")

	current := arcfs.RootInodeID
	for _, name := range parts {
		node, err := b.afs.Lookup(current, name)
		if err != nil {
			return nil, err
		}
		current = node.ID
	}
	return b.afs.GetInode(current), nil
}

// Create creates a new file if the filesystem is writable.
func (b *BillyFS) Create(filename string) (billy.File, error) {
	if b.afs.ReadOnly() {
		return nil, syscall.EROFS
	}

	dir, _ := splitPath(filename)
	parent, err := b.resolve(dir)
	if err != nil {
		return nil, err
	}

	hid, node, err := b.afs.CreateFile(parent.ID, filepath.Base(filename))
	if err != nil {
		return nil, err
	}

	f := &billyFile{
		bfs:    b,
		node:   node,
		handle: hid,
		name:   filename,
	}
	b.mu.Lock()
	b.handles[f] = true
	b.mu.Unlock()
	return f, nil
}

// Open opens a file for reading.
func (b *BillyFS) Open(filename string) (billy.File, error) {
	return b.OpenFile(filename, os.O_RDONLY, 0)
}

// OpenFile opens a file with the given flags.
func (b *BillyFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	wrFlags := flag & (os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_TRUNC)

	if wrFlags != 0 && b.afs.ReadOnly() {
		return nil, syscall.EROFS
	}

	if flag&os.O_CREATE != 0 {
		if _, err := b.resolve(filename); err != nil {
			return b.Create(filename)
		}
	}

	node, err := b.resolve(filename)
	if err != nil {
		return nil, err
	}

	if node.IsDir() {
		return &billyFile{bfs: b, node: node, name: filename, isDir: true}, nil
	}

	handle, err := b.afs.OpenFile(node.ID)
	if err != nil {
		return nil, err
	}

	f := &billyFile{
		bfs:    b,
		node:   node,
		handle: handle,
		name:   filename,
	}
	b.mu.Lock()
	b.handles[f] = true
	b.mu.Unlock()
	return f, nil
}

// Stat returns file info for the given path.
func (b *BillyFS) Stat(filename string) (os.FileInfo, error) {
	node, err := b.resolve(filename)
	if err != nil {
		return nil, err
	}
	attr, err := b.afs.GetAttr(node.ID)
	if err != nil {
		return nil, err
	}
	return &billyFileInfo{node: node, attr: attr}, nil
}

// ReadDir returns directory entries.
func (b *BillyFS) ReadDir(path string) ([]os.FileInfo, error) {
	node, err := b.resolve(path)
	if err != nil {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: err}
	}

	children, err := b.afs.Children(node.ID)
	if err != nil {
		return nil, err
	}

	infos := make([]os.FileInfo, 0, len(children))
	for _, child := range children {
		attr, err := b.afs.GetAttr(child.ID)
		if err != nil {
			continue
		}
		infos = append(infos, &billyFileInfo{node: child, attr: attr})
	}
	return infos, nil
}

// Remove deletes a file when the filesystem is writable.
func (b *BillyFS) Remove(filename string) error {
	if b.afs.ReadOnly() {
		return syscall.EROFS
	}
	dir, base := splitPath(filename)
	parent, err := b.resolve(dir)
	if err != nil {
		return err
	}
	return b.afs.Remove(parent.ID, base, false)
}

// Rename is not supported (read-only filesystem).
func (b *BillyFS) Rename(_, _ string) error { return syscall.EROFS }

// MkdirAll creates a directory when the filesystem is writable.
func (b *BillyFS) MkdirAll(path string, _ os.FileMode) error {
	if b.afs.ReadOnly() {
		return syscall.EROFS
	}
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "." {
		return nil
	}
	parts := strings.Split(path, "/")
	current := arcfs.RootInodeID
	for _, name := range parts {
		node, err := b.afs.Lookup(current, name)
		if err == nil {
			current = node.ID
			continue
		}
		created, err := b.afs.Mkdir(current, name)
		if err != nil {
			return err
		}
		current = created.ID
	}
	return nil
}

// Lstat is the same as Stat (no symlinks).
func (b *BillyFS) Lstat(filename string) (os.FileInfo, error) { return b.Stat(filename) }

// Symlink is not supported.
func (b *BillyFS) Symlink(_, _ string) error { return syscall.EROFS }

// Readlink is not supported.
func (b *BillyFS) Readlink(_ string) (string, error) { return "", syscall.EINVAL }

// Join joins path elements.
func (b *BillyFS) Join(elem ...string) string { return filepath.Join(elem...) }

// TempFile is not supported (read-only filesystem).
func (b *BillyFS) TempFile(_, _ string) (billy.File, error) { return nil, syscall.EROFS }

// Chroot is not supported.
func (b *BillyFS) Chroot(_ string) (billy.Filesystem, error) { return nil, syscall.EPERM }

// Root returns the root path.
func (b *BillyFS) Root() string { return "/" }

func splitPath(p string) (dir, base string) {
	p = filepath.Clean(p)
	p = strings.TrimPrefix(p, "/")
	dir = filepath.Dir(p)
	base = filepath.Base(p)
	if dir == "." {
		dir = ""
	}
	return dir, base
}

type billyFile struct {
	bfs    *BillyFS
	node   *arcfs.Inode
	handle arcfs.HandleID
	name   string
	offset int64
	isDir  bool
	closed bool
}

func (f *billyFile) Name() string { return f.name }

func (f *billyFile) Read(p []byte) (int, error) {
	if f.isDir {
		return 0, fmt.Errorf("is a directory")
	}
	n, err := f.bfs.afs.ReadHandle(f.handle, f.offset, p)
	f.offset += int64(n)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func (f *billyFile) ReadAt(p []byte, off int64) (int, error) {
	if f.isDir {
		return 0, fmt.Errorf("is a directory")
	}
	return f.bfs.afs.ReadHandle(f.handle, off, p)
}

func (f *billyFile) Write(p []byte) (int, error) {
	if f.bfs.afs.ReadOnly() {
		return 0, syscall.EROFS
	}
	n, err := f.bfs.afs.WriteHandle(f.handle, f.offset, p)
	f.offset += int64(n)
	return n, err
}

func (f *billyFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		attr, err := f.bfs.afs.GetAttr(f.node.ID)
		if err != nil {
			return 0, err
		}
		f.offset = int64(attr.Size) + offset
	}
	return f.offset, nil
}

func (f *billyFile) Close() error {
	if f.closed || f.isDir {
		return nil
	}
	f.closed = true

	if !f.bfs.afs.ReadOnly() {
		_ = f.bfs.afs.FlushHandle(f.handle)
	}

	f.bfs.afs.ReleaseHandle(f.handle)

	f.bfs.mu.Lock()
	delete(f.bfs.handles, f)
	f.bfs.mu.Unlock()
	return nil
}

func (f *billyFile) Lock() error      { return nil }
func (f *billyFile) Unlock() error    { return nil }
func (f *billyFile) Truncate(size int64) error {
	if f.bfs.afs.ReadOnly() {
		return syscall.EROFS
	}
	return f.bfs.afs.TruncateHandle(f.handle, size)
}

type billyFileInfo struct {
	node *arcfs.Inode
	attr arcfs.Attr
}

func (fi *billyFileInfo) Name() string      { return fi.node.Name }
func (fi *billyFileInfo) Size() int64       { return int64(fi.attr.Size) }
func (fi *billyFileInfo) Mode() os.FileMode { return fi.attr.Mode }
func (fi *billyFileInfo) ModTime() time.Time { return fi.attr.Mtime }
func (fi *billyFileInfo) IsDir() bool       { return fi.node.IsDir() }

// Sys returns a *nfsfile.FileInfo so go-nfs can read uid/gid and
// fileid consistently across platforms.
func (fi *billyFileInfo) Sys() interface{} {
	return &nfsfile.FileInfo{
		Fileid: fi.node.ID,
		Nlink:  fi.attr.Nlink,
		UID:    fi.attr.Uid,
		GID:    fi.attr.Gid,
	}
}
