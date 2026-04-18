package fusefs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

var _ fs.NodeCreater = (*RootNode)(nil)

func (r *RootNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	if existing, _ := r.fs.st.GetFile(name); existing != nil {
		switch existing.State {
		case store.FileStateRemote, store.FileStateCached, store.FileStateLocalFinalized:
			slog.Warn("Create called on non-ACTIVE file, returning NullWriteHandle",
				"file", name, "state", existing.State)
			out.Attr.Mode = fuse.S_IFREG | 0644
			out.Attr.Size = uint64(existing.Size)
			out.SetAttrTimeout(attrTimeoutShort)
			out.SetEntryTimeout(entryTimeoutShort)

			node := &FileNode{fs: r.fs, filename: name}
			stable := fs.StableAttr{Ino: InodeForFile(name), Mode: fuse.S_IFREG}
			inode := r.NewInode(ctx, node, stable)

			nh := &NullWriteHandle{fs: r.fs, filename: name}
			return inode, nh, fuse.FOPEN_KEEP_CACHE, fs.OK
		}
	}

	path := filepath.Join(r.fs.localDir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|int(flags)&^os.O_EXCL, os.FileMode(mode))
	if err != nil {
		return nil, nil, 0, syscall.EIO
	}

	entry := &store.FileEntry{
		Filename:   name,
		State:      store.FileStateActive,
		Source:     store.FileSourceLocal,
		Size:       0,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
	if err := r.fs.st.UpsertFile(entry); err != nil {
		f.Close()
		return nil, nil, 0, syscall.EIO
	}

	out.Attr.Mode = fuse.S_IFREG | 0644
	out.Attr.Size = 0
	out.SetAttrTimeout(attrTimeoutShort)
	out.SetEntryTimeout(entryTimeoutShort)

	node := &FileNode{fs: r.fs, filename: name}
	stable := fs.StableAttr{Ino: InodeForFile(name), Mode: fuse.S_IFREG}
	inode := r.NewInode(ctx, node, stable)

	wh := &WriteHandle{fs: r.fs, filename: name, file: f}
	return inode, wh, 0, fs.OK
}
