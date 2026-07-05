// Package fusemount implements a FUSE-based mount transport for zarfs
// using hanwen/go-fuse. This is the primary transport on Linux and macOS
// with macFUSE installed.
package fusemount

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"syscall"
	"time"

	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fs"

	"github.com/cloudygreybeard/zarfs/internal/arcfs"
)

// Mount mounts the archive filesystem at mountpoint using kernel FUSE.
// When readOnly is false the mount permits write operations.
func Mount(afs *arcfs.FS, mountpoint string, readOnly, debug bool, logger *log.Logger) (*gofuse.Server, error) {
	root := &dirNode{afs: afs, nodeID: arcfs.RootInodeID}

	var mountOpts []string
	if readOnly {
		mountOpts = []string{"ro"}
	}

	opts := &fs.Options{
		MountOptions: gofuse.MountOptions{
			FsName:        "zarfs",
			Name:          "zarfs",
			DisableXAttrs: true,
			MaxReadAhead:  128 * 1024,
			Options:       mountOpts,
		},
		AttrTimeout:  &oneSecond,
		EntryTimeout: &oneSecond,
	}

	if debug {
		opts.Debug = true
	}

	server, err := fs.Mount(mountpoint, root, opts)
	if err != nil {
		return nil, fmt.Errorf("fuse mount: %w", err)
	}

	logger.Printf("mounted %s on %s (pid %d, fuse)", afs.Format(), mountpoint, os.Getpid())
	return server, nil
}

var oneSecond = 1 * time.Second

type dirNode struct {
	fs.Inode
	afs    *arcfs.FS
	nodeID uint64
}

var (
	_ fs.NodeLookuper  = (*dirNode)(nil)
	_ fs.NodeReaddirer = (*dirNode)(nil)
	_ fs.NodeGetattrer = (*dirNode)(nil)
	_ fs.NodeStatfser  = (*dirNode)(nil)
	_ fs.NodeCreater   = (*dirNode)(nil)
	_ fs.NodeUnlinker  = (*dirNode)(nil)
	_ fs.NodeMkdirer   = (*dirNode)(nil)
	_ fs.NodeRmdirer   = (*dirNode)(nil)
)

func (d *dirNode) Lookup(ctx context.Context, name string, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	node, err := d.afs.Lookup(d.nodeID, name)
	if err != nil {
		return nil, toErrno(err)
	}

	attr, _ := d.afs.GetAttr(node.ID)
	fillEntryOut(node, attr, out)

	var child fs.InodeEmbedder
	if node.IsDir() {
		child = &dirNode{afs: d.afs, nodeID: node.ID}
	} else {
		child = &fileNode{afs: d.afs, nodeID: node.ID}
	}

	stable := fs.StableAttr{Mode: uint32(attr.Mode), Ino: node.ID}
	return d.NewInode(ctx, child, stable), 0
}

func (d *dirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	children, err := d.afs.Children(d.nodeID)
	if err != nil {
		return nil, toErrno(err)
	}

	entries := make([]gofuse.DirEntry, 0, len(children))
	for _, child := range children {
		attr, _ := d.afs.GetAttr(child.ID)
		entries = append(entries, gofuse.DirEntry{
			Name: child.Name,
			Ino:  child.ID,
			Mode: uint32(attr.Mode),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return fs.NewListDirStream(entries), 0
}

func (d *dirNode) Getattr(ctx context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	attr, err := d.afs.GetAttr(d.nodeID)
	if err != nil {
		return toErrno(err)
	}
	fillAttrOut(d.nodeID, attr, &out.Attr)
	return 0
}

func (d *dirNode) Statfs(ctx context.Context, out *gofuse.StatfsOut) syscall.Errno {
	s := d.afs.StatFSInfo()
	out.Bsize = s.BlockSize
	out.Blocks = s.Blocks
	out.Bfree = s.BlocksFree
	out.Bavail = s.BlocksAvailable
	out.Files = s.Inodes
	out.Ffree = s.InodesFree
	out.Frsize = s.BlockSize
	return 0
}

func (d *dirNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *gofuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	if d.afs.ReadOnly() {
		return nil, nil, 0, syscall.EROFS
	}

	hid, node, err := d.afs.CreateFile(d.nodeID, name)
	if err != nil {
		return nil, nil, 0, toErrno(err)
	}

	attr, _ := d.afs.GetAttr(node.ID)
	fillEntryOut(node, attr, out)

	child := &fileNode{afs: d.afs, nodeID: node.ID}
	stable := fs.StableAttr{Mode: uint32(attr.Mode), Ino: node.ID}

	return d.NewInode(ctx, child, stable), &fuseHandle{afs: d.afs, handle: hid}, 0, 0
}

func (d *dirNode) Unlink(_ context.Context, name string) syscall.Errno {
	if d.afs.ReadOnly() {
		return syscall.EROFS
	}
	return toErrno(d.afs.Remove(d.nodeID, name, false))
}

func (d *dirNode) Mkdir(ctx context.Context, name string, mode uint32, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if d.afs.ReadOnly() {
		return nil, syscall.EROFS
	}

	node, err := d.afs.Mkdir(d.nodeID, name)
	if err != nil {
		return nil, toErrno(err)
	}

	attr, _ := d.afs.GetAttr(node.ID)
	fillEntryOut(node, attr, out)

	child := &dirNode{afs: d.afs, nodeID: node.ID}
	stable := fs.StableAttr{Mode: uint32(attr.Mode), Ino: node.ID}
	return d.NewInode(ctx, child, stable), 0
}

func (d *dirNode) Rmdir(_ context.Context, name string) syscall.Errno {
	if d.afs.ReadOnly() {
		return syscall.EROFS
	}
	return toErrno(d.afs.Remove(d.nodeID, name, true))
}

type fileNode struct {
	fs.Inode
	afs    *arcfs.FS
	nodeID uint64
}

var (
	_ fs.NodeOpener    = (*fileNode)(nil)
	_ fs.NodeGetattrer = (*fileNode)(nil)
	_ fs.NodeSetattrer = (*fileNode)(nil)
)

func (f *fileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	handle, err := f.afs.OpenFile(f.nodeID)
	if err != nil {
		return nil, 0, toErrno(err)
	}
	return &fuseHandle{afs: f.afs, handle: handle}, 0, 0
}

func (f *fileNode) Getattr(ctx context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	attr, err := f.afs.GetAttr(f.nodeID)
	if err != nil {
		return toErrno(err)
	}
	fillAttrOut(f.nodeID, attr, &out.Attr)
	return 0
}

func (f *fileNode) Setattr(ctx context.Context, fh fs.FileHandle, in *gofuse.SetAttrIn, out *gofuse.AttrOut) syscall.Errno {
	if f.afs.ReadOnly() {
		return syscall.EROFS
	}
	if sz, ok := in.GetSize(); ok {
		if h, ok := fh.(*fuseHandle); ok {
			if err := f.afs.TruncateHandle(h.handle, int64(sz)); err != nil {
				return toErrno(err)
			}
		}
	}
	attr, err := f.afs.GetAttr(f.nodeID)
	if err != nil {
		return toErrno(err)
	}
	fillAttrOut(f.nodeID, attr, &out.Attr)
	return 0
}

type fuseHandle struct {
	afs    *arcfs.FS
	handle arcfs.HandleID
}

var (
	_ fs.FileReader   = (*fuseHandle)(nil)
	_ fs.FileWriter   = (*fuseHandle)(nil)
	_ fs.FileFlusher  = (*fuseHandle)(nil)
	_ fs.FileReleaser = (*fuseHandle)(nil)
)

func (h *fuseHandle) Read(ctx context.Context, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	buf := make([]byte, len(dest))
	n, err := h.afs.ReadHandle(h.handle, off, buf)
	if err != nil && n == 0 {
		return nil, toErrno(err)
	}
	return gofuse.ReadResultData(buf[:n]), 0
}

func (h *fuseHandle) Write(_ context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n, err := h.afs.WriteHandle(h.handle, off, data)
	if err != nil {
		return 0, toErrno(err)
	}
	return uint32(n), 0
}

func (h *fuseHandle) Flush(_ context.Context) syscall.Errno {
	return toErrno(h.afs.FlushHandle(h.handle))
}

func (h *fuseHandle) Release(_ context.Context) syscall.Errno {
	h.afs.ReleaseHandle(h.handle)
	return 0
}

func fillAttrOut(id uint64, attr arcfs.Attr, out *gofuse.Attr) {
	out.Ino = id
	out.Size = attr.Size
	out.Nlink = attr.Nlink
	out.Mode = uint32(attr.Mode)
	out.Uid = attr.Uid
	out.Gid = attr.Gid
	out.SetTimes(&attr.Atime, &attr.Mtime, &attr.Ctime)
}

func fillEntryOut(node *arcfs.Inode, attr arcfs.Attr, out *gofuse.EntryOut) {
	out.NodeId = node.ID
	out.SetAttrTimeout(5 * time.Second)
	out.SetEntryTimeout(5 * time.Second)
	fillAttrOut(node.ID, attr, &out.Attr)
}

func toErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno
	}
	return syscall.EIO
}
