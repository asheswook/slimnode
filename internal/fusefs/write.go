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

// WriteHandle implements write operations for an ACTIVE file.
type WriteHandle struct {
	fs       *FS
	filename string
	file     *os.File
}

func (h *WriteHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	entry, err := h.fs.st.GetFile(h.filename)
	if err != nil {
		slog.Warn("FUSE write: file not found in store", "op", "Write", "file", h.filename, "err", err)
		return 0, syscall.ENOENT
	}

	if !state.IsWritable(entry.State) {
		slog.Warn("FUSE write: file not writable", "op", "Write", "file", h.filename, "state", entry.State)
		return 0, syscall.EACCES
	}

	n, err := h.file.WriteAt(data, off)
	if err != nil {
		slog.Error("FUSE write: WriteAt failed", "op", "Write", "file", h.filename, "offset", off, "len", len(data), "err", err)
		return 0, syscall.EIO
	}

	info, err := h.file.Stat()
	if err != nil {
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
		slog.Warn("FUSE fsync: file not found in store", "op", "Fsync", "file", h.filename, "err", err)
		return fs.OK
	}
	if entry.State == store.FileStateActive {
		if err := h.file.Sync(); err != nil {
			slog.Error("FUSE fsync: sync failed", "op", "Fsync", "file", h.filename, "err", err)
			return syscall.EIO
		}
	}
	return fs.OK
}

func (h *WriteHandle) finalize(entry *store.FileEntry) {
	path := filepath.Join(h.fs.localDir, h.filename)
	sha, err := fileSHA256(path)
	if err != nil {
		slog.Warn("finalize: SHA256 computation failed", "op", "finalize", "file", h.filename, "err", err)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		slog.Warn("finalize: stat failed", "op", "finalize", "file", h.filename, "err", err)
		return
	}

	entry.State = store.FileStateLocalFinalized
	entry.SHA256 = sha
	entry.Size = info.Size()
	entry.LastAccess = time.Now()
	if err := h.fs.st.UpsertFile(entry); err != nil {
		slog.Warn("finalize: store update failed", "op", "finalize", "file", h.filename, "err", err)
	}

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

// Read implements fs.FileReader so WriteHandle can also serve reads on ACTIVE files.
func (h *WriteHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	n, err := h.file.ReadAt(dest, off)
	if err != nil && n == 0 {
		return fuse.ReadResultData([]byte{}), fs.OK
	}
	return fuse.ReadResultData(dest[:n]), fs.OK
}
