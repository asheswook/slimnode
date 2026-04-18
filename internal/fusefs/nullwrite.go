package fusefs

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

var _ fs.FileWriter = (*NullWriteHandle)(nil)
var _ fs.FileReader = (*NullWriteHandle)(nil)
var _ fs.FileFsyncer = (*NullWriteHandle)(nil)
var _ fs.FileAllocater = (*NullWriteHandle)(nil)
var _ fs.FileReleaser = (*NullWriteHandle)(nil)

// NullWriteHandle is a FUSE file handle for non-ACTIVE files opened with
// write flags. Write operations are accepted and silently discarded.
// Read operations fetch directly from the remote server via HTTP Range requests.
// Fsync and Allocate always succeed to prevent bitcoind from aborting.
type NullWriteHandle struct {
	fs       *FS
	filename string
	logOnce  sync.Once
}

// Write accepts data and discards it silently. Always returns success so
// bitcoind does not abort. The first discarded write per handle is logged
// at Warn level to aid in detecting unexpected write-to-sealed-file patterns.
func (h *NullWriteHandle) Write(_ context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	h.logOnce.Do(func() {
		slog.Warn("discarding write to non-ACTIVE file",
			"op", "NullWriteHandle.Write",
			"file", h.filename,
			"offset", off,
			"size", len(data),
		)
	})
	return uint32(len(data)), fs.OK
}

func (h *NullWriteHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if len(dest) == 0 {
		return fuse.ReadResultData([]byte{}), fs.OK
	}

	if h.fs != nil && h.fs.st != nil {
		if entry, err := h.fs.st.GetFile(h.filename); err == nil {
			switch entry.State {
			case store.FileStateActive, store.FileStateLocalFinalized:
				if result, errno := preadLocal(filepath.Join(h.fs.localDir, h.filename), dest, off); errno == fs.OK {
					return result, fs.OK
				}
				if h.fs.ca != nil {
					if result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off); errno == fs.OK {
						return result, fs.OK
					}
				}
				return h.readRemote(ctx, dest, off)

			case store.FileStateCached, store.FileStateRemote:
				if h.fs.ca != nil {
					if result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off); errno == fs.OK {
						return result, fs.OK
					}
				}
				return h.readRemote(ctx, dest, off)
			}
		}
	}

	if result, errno := preadLocal(filepath.Join(h.fs.localDir, h.filename), dest, off); errno == fs.OK {
		return result, fs.OK
	}
	if h.fs.ca != nil {
		if result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off); errno == fs.OK {
			return result, fs.OK
		}
	}

	return h.readRemote(ctx, dest, off)
}

func (h *NullWriteHandle) readRemote(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
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
