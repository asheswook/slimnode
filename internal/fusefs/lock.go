package fusefs

import (
	"context"
	"path/filepath"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var _ fs.NodeGetattrer = (*LockNode)(nil)
var _ fs.NodeOpener = (*LockNode)(nil)

// LockNode represents the .lock file in the FUSE filesystem.
// Unlike other special files (xor.dat), .lock is backed by a real file
// in localDir so that flock()/fcntl() work correctly.
// bitcoind requires flock() on blocks/.lock to ensure exclusive access.
type LockNode struct {
	fs.Inode
	fs *FS
}

func (n *LockNode) path() string {
	return filepath.Join(n.fs.localDir, ".lock")
}

func (n *LockNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	var st syscall.Stat_t
	if err := syscall.Stat(n.path(), &st); err != nil {
		out.Attr.Mode = fuse.S_IFREG | 0644
		out.Attr.Size = 0
		out.SetTimeout(attrTimeoutShort)
		return fs.OK
	}
	out.Attr.Mode = fuse.S_IFREG | 0644
	out.Attr.Size = uint64(st.Size)
	out.SetTimeout(attrTimeoutShort)
	return fs.OK
}

func (n *LockNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	fd, err := syscall.Open(n.path(), syscall.O_RDWR|syscall.O_CREAT, 0644)
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}
	return fs.NewLoopbackFile(fd), 0, fs.OK
}
