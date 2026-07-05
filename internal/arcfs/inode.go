package arcfs

import (
	"sync"
	"time"

	"github.com/cloudygreybeard/zarfs/internal/archive"
)

// Inode represents a file or directory in the virtual filesystem.
type Inode struct {
	ID       uint64
	ParentID uint64
	Name     string
	Dir      bool
	ModTime  time.Time
	Entry    *archive.Entry
	ChildIDs []uint64
}

// IsDir returns true if the inode is a directory.
func (n *Inode) IsDir() bool {
	return n.Dir
}

// InodeTable manages inode allocation and lookup.
type InodeTable struct {
	mu     sync.RWMutex
	nodes  map[uint64]*Inode
	nextID uint64
}

// NewInodeTable creates a table with a pre-allocated root directory.
func NewInodeTable() *InodeTable {
	t := &InodeTable{
		nodes:  make(map[uint64]*Inode),
		nextID: RootInodeID + 1,
	}
	t.nodes[RootInodeID] = &Inode{
		ID:      RootInodeID,
		Name:    "",
		Dir:     true,
		ModTime: time.Now(),
	}
	return t
}

// Get returns the inode with the given ID, or nil.
func (t *InodeTable) Get(id uint64) *Inode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodes[id]
}

// Add creates a new inode under parentID and returns its ID.
func (t *InodeTable) Add(parentID uint64, name string, isDir bool, modTime time.Time, entry *archive.Entry) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.nextID
	t.nextID++

	t.nodes[id] = &Inode{
		ID:       id,
		ParentID: parentID,
		Name:     name,
		Dir:      isDir,
		ModTime:  modTime,
		Entry:    entry,
	}

	if parent, ok := t.nodes[parentID]; ok {
		parent.ChildIDs = append(parent.ChildIDs, id)
	}

	return id
}

// FindChild returns the ID of the child with the given name under
// parentID, or 0 if not found.
func (t *InodeTable) FindChild(parentID uint64, name string) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	parent, ok := t.nodes[parentID]
	if !ok {
		return 0
	}
	for _, childID := range parent.ChildIDs {
		if child, ok := t.nodes[childID]; ok && child.Name == name {
			return childID
		}
	}
	return 0
}

// RemoveChild removes childID from parentID's child list and deletes
// the child inode from the table.
func (t *InodeTable) RemoveChild(parentID, childID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if parent, ok := t.nodes[parentID]; ok {
		for i, id := range parent.ChildIDs {
			if id == childID {
				parent.ChildIDs = append(parent.ChildIDs[:i], parent.ChildIDs[i+1:]...)
				break
			}
		}
	}
	delete(t.nodes, childID)
}

// Count returns the total number of inodes.
func (t *InodeTable) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.nodes)
}
