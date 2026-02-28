package fusefs

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-lfn/internal/store"
)

var _ fs.NodeLookuper = (*RootNode)(nil)
var _ fs.NodeReaddirer = (*RootNode)(nil)
var _ fs.NodeStatfser = (*RootNode)(nil)

// RootNode is the root directory inode of the SlimNode FUSE filesystem.
type RootNode struct {
	fs.Inode
	fs *FS
}

func (r *RootNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	var st syscall.Statfs_t
	if err := syscall.Statfs(r.fs.localDir, &st); err != nil {
		return fs.ToErrno(err)
	}
	out.FromStatfsT(&st)
	return fs.OK
}

func (r *RootNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	entries, err := r.fs.st.ListFiles()
	if err != nil {
		return nil, syscall.EIO
	}

	dirs := make([]fuse.DirEntry, 0, len(entries)+3)
	for _, e := range entries {
		mode := uint32(fuse.S_IFREG | 0444)
		if e.State == store.FileStateActive {
			mode = fuse.S_IFREG | 0644
		}
		dirs = append(dirs, fuse.DirEntry{
			Name: e.Filename,
			Ino:  InodeForFile(e.Filename),
			Mode: mode,
		})
	}

	dirs = append(dirs,
		fuse.DirEntry{Name: "xor.dat", Ino: InodeForFile("xor.dat"), Mode: fuse.S_IFREG | 0644},
		fuse.DirEntry{Name: ".lock", Ino: InodeForFile(".lock"), Mode: fuse.S_IFREG | 0644},
	)

	if r.fs.indexLoopback != nil {
		dirs = append(dirs, fuse.DirEntry{
			Name: "index",
			Ino:  InodeForFile("index"),
			Mode: fuse.S_IFDIR | 0755,
		})
	}

	return fs.NewListDirStream(dirs), fs.OK
}

func (r *RootNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if name == "index" && r.fs.indexLoopback != nil {
		out.Attr.Mode = fuse.S_IFDIR | 0755
		out.SetAttrTimeout(attrTimeoutShort)
		out.SetEntryTimeout(entryTimeoutShort)
		stable := fs.StableAttr{Ino: InodeForFile("index"), Mode: fuse.S_IFDIR}
		return r.NewInode(ctx, r.fs.indexLoopback, stable), fs.OK
	}

	if name == "xor.dat" {
		return r.newSpecialInode(ctx, name, out), fs.OK
	}

	if name == ".lock" {
		return r.newLockInode(ctx, out), fs.OK
	}

	entry, err := r.fs.st.GetFile(name)
	if err != nil {
		return nil, syscall.ENOENT
	}

	mode := uint32(0444)
	if entry.State == store.FileStateActive {
		mode = 0644
	}

	out.Attr.Size = uint64(entry.Size)
	out.Attr.Mode = fuse.S_IFREG | mode

	switch entry.State {
	case store.FileStateCached, store.FileStateLocalFinalized:
		out.SetAttrTimeout(attrTimeoutLong)
		out.SetEntryTimeout(entryTimeoutLong)
	default:
		out.SetAttrTimeout(attrTimeoutShort)
		out.SetEntryTimeout(entryTimeoutShort)
	}

	node := &FileNode{fs: r.fs, filename: name}
	stable := fs.StableAttr{Ino: InodeForFile(name), Mode: fuse.S_IFREG}
	return r.NewInode(ctx, node, stable), fs.OK
}

func (r *RootNode) newLockInode(ctx context.Context, out *fuse.EntryOut) *fs.Inode {
	out.Attr.Mode = fuse.S_IFREG | 0644
	out.SetAttrTimeout(attrTimeoutShort)
	out.SetEntryTimeout(entryTimeoutShort)
	node := &LockNode{fs: r.fs}
	stable := fs.StableAttr{Ino: InodeForFile(".lock"), Mode: fuse.S_IFREG}
	return r.NewInode(ctx, node, stable)
}

func (r *RootNode) newSpecialInode(ctx context.Context, name string, out *fuse.EntryOut) *fs.Inode {
	out.Attr.Mode = fuse.S_IFREG | 0644
	out.Attr.Size = 8
	out.SetAttrTimeout(attrTimeoutShort)
	out.SetEntryTimeout(entryTimeoutShort)
	node := &FileNode{fs: r.fs, filename: name, special: true}
	stable := fs.StableAttr{Ino: InodeForFile(name), Mode: fuse.S_IFREG}
	return r.NewInode(ctx, node, stable)
}
