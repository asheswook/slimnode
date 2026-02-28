package fusefs

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var _ fs.FileWriter = (*NullWriteHandle)(nil)
var _ fs.FileReader = (*NullWriteHandle)(nil)
var _ fs.FileFsyncer = (*NullWriteHandle)(nil)
var _ fs.FileAllocater = (*NullWriteHandle)(nil)
var _ fs.FileReleaser = (*NullWriteHandle)(nil)

// NullWriteHandle is a FUSE file handle for REMOTE rev files during -reindex.
// Write operations are accepted and silently discarded (zero disk usage).
// Read operations fetch directly from the remote server via HTTP Range requests.
// Fsync and Allocate always succeed to prevent bitcoind from aborting.
type NullWriteHandle struct {
	fs       *FS
	filename string
}

// Write accepts data and discards it silently. Always returns success so
// bitcoind does not abort during -reindex when writing rev files.
func (h *NullWriteHandle) Write(_ context.Context, data []byte, _ int64) (uint32, syscall.Errno) {
	return uint32(len(data)), fs.OK
}

// Read tries local file, then cache, then remote server. This ordering ensures
// LOCAL_FINALIZED files (not yet on the server) and CACHED files (already
// downloaded) are served from disk without unnecessary network round-trips.
func (h *NullWriteHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if result, errno := preadLocal(filepath.Join(h.fs.localDir, h.filename), dest, off); errno == fs.OK {
		return result, fs.OK
	}

	if h.fs.ca != nil {
		if result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off); errno == fs.OK {
			return result, fs.OK
		}
	}

	data, err := h.fs.rc.FetchBlock(ctx, h.filename, off, int64(len(dest)))
	if err != nil {
		return fuse.ReadResultData(nil), syscall.EIO
	}
	n := copy(dest, data)
	return fuse.ReadResultData(dest[:n]), fs.OK
}

// Fsync always returns OK. Rev files are not persisted locally, so there is
// nothing to flush.
func (h *NullWriteHandle) Fsync(_ context.Context, _ uint32) syscall.Errno {
	return fs.OK
}

// Allocate always returns OK. Returning ENOTSUP causes posix_fallocate to fall
// back to writing 64 KiB zero-filled chunks, which defeats the purpose of
// NullWriteHandle. Returning OK prevents that fallback.
func (h *NullWriteHandle) Allocate(_ context.Context, _ uint64, _ uint64, _ uint32) syscall.Errno {
	return fs.OK
}

// Release is called when the file handle is closed. It is a no-op for
// NullWriteHandle since no resources are held.
func (h *NullWriteHandle) Release(_ context.Context) syscall.Errno {
	return fs.OK
}

// isRevFile reports whether filename is a Bitcoin rev file (rev#####.dat).
// The middle portion must consist entirely of decimal digits with length > 0.
func isRevFile(filename string) bool {
	if !strings.HasPrefix(filename, "rev") || !strings.HasSuffix(filename, ".dat") {
		return false
	}
	mid := filename[3 : len(filename)-4] // strip "rev" prefix and ".dat" suffix
	if len(mid) == 0 {
		return false
	}
	for _, c := range mid {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
