package fusefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/state"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

var _ fs.FileWriter = (*WriteHandle)(nil)
var _ fs.FileFsyncer = (*WriteHandle)(nil)
var _ fs.FileAllocater = (*WriteHandle)(nil)

// WriteHandle implements write operations for an ACTIVE file.
type WriteHandle struct {
	fs       *FS
	filename string
	file     *os.File
}

func (h *WriteHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	entry, err := h.fs.st.GetFile(h.filename)
	if err != nil {
		slog.Error("WriteHandle.Write: GetFile failed",
			"op", "WriteHandle.Write",
			"file", h.filename,
			"offset", off,
			"len", len(data),
			"err", err,
		)
		return 0, syscall.ENOENT
	}

	if !state.IsWritable(entry.State) {
		slog.Error("WriteHandle.Write: file not writable",
			"op", "WriteHandle.Write",
			"file", h.filename,
			"state", entry.State,
			"offset", off,
			"len", len(data),
		)
		return 0, syscall.EACCES
	}

	n, err := h.file.WriteAt(data, off)
	if err != nil {
		slog.Error("WriteHandle.Write: WriteAt failed",
			"op", "WriteHandle.Write",
			"file", h.filename,
			"offset", off,
			"len", len(data),
			"written", n,
			"err", err,
		)
		return 0, syscall.EIO
	}

	if n < len(data) {
		// Preserve (n, OK) semantics; log only so callers decide escalation policy.
		slog.Warn("WriteHandle.Write: short write (no error)",
			"op", "WriteHandle.Write",
			"file", h.filename,
			"offset", off,
			"len", len(data),
			"written", n,
		)
	}

	info, err := h.file.Stat()
	if err != nil {
		slog.Warn("WriteHandle.Write: Stat after write failed",
			"op", "WriteHandle.Write",
			"file", h.filename,
			"offset", off,
			"written", n,
			"err", err,
		)
		return uint32(n), fs.OK
	}

	if info.Size() >= store.MaxBlockFileSize {
		h.finalize(entry)
	}

	return uint32(n), fs.OK
}

func (h *WriteHandle) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	entry, err := h.fs.st.GetFile(h.filename)
	if err != nil {
		slog.Warn("WriteHandle.Fsync: GetFile failed (treating as no-op)",
			"op", "WriteHandle.Fsync",
			"file", h.filename,
			"err", err,
		)
		return fs.OK
	}
	if entry.State == store.FileStateActive {
		if err := h.file.Sync(); err != nil {
			slog.Error("WriteHandle.Fsync: Sync failed",
				"op", "WriteHandle.Fsync",
				"file", h.filename,
				"err", err,
			)
			return syscall.EIO
		}
	}
	return fs.OK
}

func (h *WriteHandle) finalize(entry *store.FileEntry) {
	path := filepath.Join(h.fs.localDir, h.filename)
	sha, err := fileSHA256(path)
	if err != nil {
		slog.Error("WriteHandle.finalize: fileSHA256 failed (state left as ACTIVE)",
			"op", "WriteHandle.finalize",
			"file", h.filename,
			"err", err,
		)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		slog.Error("WriteHandle.finalize: Stat failed (state left as ACTIVE)",
			"op", "WriteHandle.finalize",
			"file", h.filename,
			"err", err,
		)
		return
	}

	entry.State = store.FileStateLocalFinalized
	entry.SHA256 = sha
	entry.Size = info.Size()
	entry.LastAccess = time.Now()
	if err := h.fs.st.UpsertFile(entry); err != nil {
		slog.Error("WriteHandle.finalize: UpsertFile failed",
			"op", "WriteHandle.finalize",
			"file", h.filename,
			"size", entry.Size,
			"err", err,
		)
		return
	}

	slog.Info("WriteHandle.finalize: file finalized",
		"op", "WriteHandle.finalize",
		"file", h.filename,
		"size", entry.Size,
		"sha256", sha,
	)

	select {
	case h.fs.finCh <- h.filename:
	default:
	}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (h *WriteHandle) Allocate(ctx context.Context, off uint64, size uint64, mode uint32) syscall.Errno {
	errno := platformAllocate(h.file, off, size)
	if errno != fs.OK {
		slog.Error("WriteHandle.Allocate: platformAllocate failed",
			"op", "WriteHandle.Allocate",
			"file", h.filename,
			"offset", off,
			"size", size,
			"mode", mode,
			"errno", errno,
		)
	}
	return errno
}

func (h *WriteHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	n, err := h.file.ReadAt(dest, off)
	if err != nil && n == 0 {
		if err != io.EOF {
			slog.Warn("WriteHandle.Read: ReadAt returned error with n=0",
				"op", "WriteHandle.Read",
				"file", h.filename,
				"offset", off,
				"len", len(dest),
				"err", err,
			)
		}
		return fuse.ReadResultData([]byte{}), fs.OK
	}
	if n < len(dest) {
		slog.Debug("WriteHandle.Read: short read (likely EOF)",
			"op", "WriteHandle.Read",
			"file", h.filename,
			"offset", off,
			"requested", len(dest),
			"returned", n,
			"err", err,
		)
	}
	return fuse.ReadResultData(dest[:n]), fs.OK
}
