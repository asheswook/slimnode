//go:build !linux

package fusefs

import (
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
)

func platformAllocate(_ *os.File, _ uint64, _ uint64) syscall.Errno {
	return fs.OK
}
