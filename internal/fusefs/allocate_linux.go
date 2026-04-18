package fusefs

import (
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"golang.org/x/sys/unix"
)

func platformAllocate(f *os.File, off uint64, size uint64) syscall.Errno {
	if err := unix.Fallocate(int(f.Fd()), 0, int64(off), int64(size)); err != nil {
		if err == unix.ENOTSUP || err == unix.EOPNOTSUPP {
			return fs.OK
		}
		return syscall.EIO
	}
	return fs.OK
}
