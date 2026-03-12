package fusefs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

var _ fs.NodeGetattrer = (*FileNode)(nil)
var _ fs.NodeOpener = (*FileNode)(nil)
var _ fs.NodeSetattrer = (*FileNode)(nil)

const (
	attrTimeoutShort  = time.Second
	attrTimeoutLong   = time.Hour
	entryTimeoutShort = time.Second
	entryTimeoutLong  = time.Hour
)

// FileNode represents a single file inode in the SlimNode FUSE filesystem.
type FileNode struct {
	fs.Inode
	fs       *FS
	filename string
	special  bool
}

func (n *FileNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if n.special {
		out.Attr.Mode = fuse.S_IFREG | 0644
		out.Attr.Size = 8
		return fs.OK
	}

	entry, err := n.fs.st.GetFile(n.filename)
	if err != nil {
		return syscall.ENOENT
	}

	mode := uint32(0444)
	if entry.State == store.FileStateActive {
		mode = 0644
	}
	out.Attr.Mode = fuse.S_IFREG | mode
	// ACTIVE files: use real file size from disk because the store value
	// is only updated at finalization and can be stale after writes.
	if entry.State == store.FileStateActive {
		if fi, err := os.Stat(filepath.Join(n.fs.localDir, n.filename)); err == nil {
			out.Attr.Size = uint64(fi.Size())
		} else {
			out.Attr.Size = uint64(entry.Size)
		}
	} else {
		out.Attr.Size = uint64(entry.Size)
	}

	switch entry.State {
	case store.FileStateCached, store.FileStateLocalFinalized:
		out.SetTimeout(attrTimeoutLong)
	default:
		out.SetTimeout(attrTimeoutShort)
	}
	return fs.OK
}

func (n *FileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if n.special {
		fh := &FileHandle{fs: n.fs, filename: n.filename, special: true}
		return fh, 0, fs.OK
	}

	entry, err := n.fs.st.GetFile(n.filename)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}

	switch entry.State {
	case store.FileStateCached, store.FileStateLocalFinalized:
		if (flags & (syscall.O_WRONLY | syscall.O_RDWR)) != 0 {
			return &NullWriteHandle{fs: n.fs, filename: n.filename}, fuse.FOPEN_KEEP_CACHE, fs.OK
		}
		fh := &FileHandle{fs: n.fs, filename: n.filename, state: entry.State}
		return fh, fuse.FOPEN_KEEP_CACHE, fs.OK

	case store.FileStateActive:
		path := filepath.Join(n.fs.localDir, n.filename)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return nil, 0, syscall.EIO
		}
		wh := &WriteHandle{fs: n.fs, filename: n.filename, file: f}
		return wh, 0, fs.OK

	default:
		if (flags & (syscall.O_WRONLY | syscall.O_RDWR)) != 0 {
			return &NullWriteHandle{fs: n.fs, filename: n.filename}, fuse.FOPEN_KEEP_CACHE, fs.OK
		}
		fh := &FileHandle{fs: n.fs, filename: n.filename, state: entry.State}
		return fh, 0, fs.OK
	}
}

// Setattr implements fs.NodeSetattrer.
// For non-ACTIVE files, truncate (FATTR_SIZE) and other attribute changes are
// silently accepted as a no-op — rev and blk file data for REMOTE, CACHED, and
// LOCAL_FINALIZED states is immutable from SlimNode's perspective, so ftruncate
// during FlushUndoFile has no effect on the stored copy.
func (n *FileNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	return fs.OK
}
