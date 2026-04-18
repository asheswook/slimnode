package fusefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/blockmap"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

var _ fs.FileReader = (*FileHandle)(nil)
var _ fs.FileFsyncer = (*FileHandle)(nil)

// FileHandle implements read operations for a single open file.
type FileHandle struct {
	fs       *FS
	filename string
	state    store.FileState
	special  bool
	auto     SequentialReadTracker
}

func (h *FileHandle) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	return fs.OK
}

func (h *FileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.special {
		xorKey := make([]byte, 8)
		if off >= int64(len(xorKey)) {
			return fuse.ReadResultData([]byte{}), fs.OK
		}
		return fuse.ReadResultData(xorKey[off:]), fs.OK
	}

	switch h.state {
	case store.FileStateActive, store.FileStateLocalFinalized:
		return preadLocal(filepath.Join(h.fs.localDir, h.filename), dest, off)

	case store.FileStateCached:
		result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off)
		if errno == fs.OK {
			_ = h.fs.st.UpdateLastAccess(h.filename, time.Now())
		}
		return result, errno

	case store.FileStateRemote:
		now := time.Now()
		autoEnabled := h.fs.fetchPolicy.Mode() == fetchModeAuto
		mode := h.fs.fetchPolicy.ResolveMode(h.filename, now)

		if mode == fetchModeFile {
			return h.readRemoteFileMode(ctx, dest, off, now)
		}

		if h.fs.bc != nil && strings.HasPrefix(h.filename, "blk") {
			result, errno := h.readViaBlockmap(ctx, dest, off)
			if errno != syscall.ENODATA {
				if errno == fs.OK {
					h.fs.fetchPolicy.ObserveRangeRead(&h.auto, off, len(dest))
					_ = h.fs.st.UpdateLastAccess(h.filename, time.Now())
					if autoEnabled && h.fs.fetchPolicy.ShouldPromoteToFile(h.filename, &h.auto, now) {
						slog.Info("promoting remote file read strategy to full-file",
							"file", h.filename,
							"range_requests", h.auto.rangeRequests,
							"sequential_bytes", h.auto.sequentialBytes,
							"total_bytes", h.auto.totalBytes,
						)
						h.fs.fetchPolicy.MarkPromotionSuccess(h.filename, now)
					}
				}
				return result, errno
			}
		}
		// Direct range-fetch: only download the bytes we need.
		// The server supports HTTP Range via http.ServeContent.
		// Falls back to full-file download only if the server rejects range requests.
		if len(dest) > 0 {
			data, err := h.fs.rc.FetchBlock(context.Background(), h.filename, off, int64(len(dest)))
			if err == nil {
				h.fs.fetchPolicy.ObserveRangeRead(&h.auto, off, len(data))
				_ = h.fs.st.UpdateLastAccess(h.filename, time.Now())
				if autoEnabled && h.fs.fetchPolicy.ShouldPromoteToFile(h.filename, &h.auto, now) {
					slog.Info("promoting remote file read strategy to full-file",
						"file", h.filename,
						"range_requests", h.auto.rangeRequests,
						"sequential_bytes", h.auto.sequentialBytes,
						"total_bytes", h.auto.totalBytes,
					)
					h.fs.fetchPolicy.MarkPromotionSuccess(h.filename, now)
				}
				return fuse.ReadResultData(data), fs.OK
			}
			slog.Warn("range fetch failed, falling back to full file download",
				"file", h.filename, "off", off, "size", len(dest), "err", err)
		}

		return h.readRemoteFileMode(ctx, dest, off, now)

	default:
		return nil, syscall.EIO
	}
}

func (h *FileHandle) readRemoteFileMode(ctx context.Context, dest []byte, off int64, now time.Time) (fuse.ReadResult, syscall.Errno) {
	if err := h.fetchAndCache(ctx); err != nil {
		h.fs.fetchPolicy.MarkPromotionFailure(h.filename, now)
		if len(dest) > 0 {
			data, rangeErr := h.fs.rc.FetchBlock(context.Background(), h.filename, off, int64(len(dest)))
			if rangeErr == nil {
				h.fs.fetchPolicy.ObserveRangeRead(&h.auto, off, len(data))
				_ = h.fs.st.UpdateLastAccess(h.filename, time.Now())
				return fuse.ReadResultData(data), fs.OK
			}
			slog.Warn("full-file fetch failed and range fallback also failed",
				"file", h.filename, "off", off, "size", len(dest),
				"full_file_err", err, "range_err", rangeErr)
		}
		slog.Error("failed to fetch remote file", "file", h.filename, "err", err)
		return nil, syscall.EIO
	}
	h.state = store.FileStateCached
	h.fs.fetchPolicy.MarkPromotionSuccess(h.filename, now)
	result, errno := preadLocal(h.fs.ca.Path(h.filename), dest, off)
	if errno == fs.OK {
		_ = h.fs.st.UpdateLastAccess(h.filename, time.Now())
	}
	return result, errno
}

func (h *FileHandle) fetchAndCache(ctx context.Context) error {
	if existing, err := h.fs.st.GetFile(h.filename); err == nil {
		if existing.State == store.FileStateCached || existing.State == store.FileStateLocalFinalized {
			return nil
		}
	}
	_, err, _ := h.fs.downloads.Do(h.filename, func() (interface{}, error) {
		entry, err := h.fs.st.GetFile(h.filename)
		if err != nil {
			return nil, err
		}

		// Limit concurrent file downloads to prevent OOM.
		// Each download buffers ~128 MB in memory; cap to 4 concurrent downloads.
		h.fs.downloadSem <- struct{}{}
		defer func() { <-h.fs.downloadSem }()

		var buf bytes.Buffer
		if err := h.fs.rc.FetchFile(context.Background(), h.filename, &buf); err != nil {
			return nil, err
		}

		expectedSHA256 := entry.SHA256
		if expectedSHA256 == "" {
			h.fs.mu.RLock()
			m := h.fs.manifest
			h.fs.mu.RUnlock()
			if m != nil {
				for i := range m.Files {
					if m.Files[i].Name == h.filename {
						expectedSHA256 = m.Files[i].SHA256
						break
					}
				}
			}
		}

		if err := h.fs.ca.Store(h.filename, &buf, expectedSHA256); err != nil {
			return nil, err
		}

		return nil, h.fs.st.UpdateState(h.filename, store.FileStateCached)
	})
	return err
}

func preadLocal(path string, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, syscall.ENOENT
		}
		return nil, syscall.EIO
	}
	defer f.Close()

	n, err := f.ReadAt(dest, off)
	if err != nil && n == 0 {
		if err.Error() == "EOF" {
			return fuse.ReadResultData([]byte{}), fs.OK
		}
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(dest[:n]), fs.OK
}

func (h *FileHandle) readViaBlockmap(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	bm, err := h.getOrLoadBlockmap(ctx)
	if err != nil || bm == nil {
		return nil, syscall.ENODATA
	}

	blocks := bm.FindBlocks(off, int64(len(dest)))
	if len(blocks) == 0 {
		return nil, syscall.ENODATA
	}

	for i := range blocks {
		if !h.fs.bc.HasBlock(h.filename, blocks[i].FileOffset) {
			if err := h.fetchBlock(ctx, blocks[i]); err != nil {
				slog.Error("block fetch failed", "file", h.filename, "offset", blocks[i].FileOffset, "err", err)
				return nil, syscall.EIO
			}
		}
	}

	n, err := h.assembleRead(dest, off, blocks)
	if err != nil {
		slog.Error("assemble read failed", "file", h.filename, "err", err)
		return nil, syscall.EIO
	}

	return fuse.ReadResultData(dest[:n]), fs.OK
}

func (h *FileHandle) getOrLoadBlockmap(ctx context.Context) (*blockmap.Blockmap, error) {
	h.fs.blockmapsMu.RLock()
	bm, ok := h.fs.blockmaps[h.filename]
	h.fs.blockmapsMu.RUnlock()
	if ok {
		return bm, nil
	}

	h.fs.noBlockmapMu.RLock()
	noBM := h.fs.noBlockmap[h.filename]
	h.fs.noBlockmapMu.RUnlock()
	if noBM {
		return nil, nil
	}

	h.fs.mu.RLock()
	m := h.fs.manifest
	h.fs.mu.RUnlock()

	var mf *manifest.ManifestFile
	if m != nil {
		for i := range m.Files {
			if m.Files[i].Name == h.filename {
				mf = &m.Files[i]
				break
			}
		}
	}

	if mf == nil || !mf.HasBlockmap() {
		h.fs.noBlockmapMu.Lock()
		h.fs.noBlockmap[h.filename] = true
		h.fs.noBlockmapMu.Unlock()
		return nil, nil
	}

	raw, err := h.fs.rc.FetchBlockmap(ctx, h.filename)
	if err != nil {
		if errors.Is(err, remote.ErrFileNotFound) {
			h.fs.noBlockmapMu.Lock()
			h.fs.noBlockmap[h.filename] = true
			h.fs.noBlockmapMu.Unlock()
			return nil, nil
		}
		return nil, fmt.Errorf("fetch blockmap: %w", err)
	}

	hash := sha256.Sum256(raw)
	hashHex := fmt.Sprintf("%x", hash)
	if hashHex != mf.BlockmapSHA256 {
		slog.Warn("blockmap SHA-256 mismatch, falling back", "file", h.filename,
			"expected", mf.BlockmapSHA256, "got", hashHex)
		h.fs.noBlockmapMu.Lock()
		h.fs.noBlockmap[h.filename] = true
		h.fs.noBlockmapMu.Unlock()
		return nil, nil
	}

	bm, err = blockmap.Read(bytes.NewReader(raw))
	if err != nil {
		slog.Warn("blockmap parse failed, falling back", "file", h.filename, "err", err)
		h.fs.noBlockmapMu.Lock()
		h.fs.noBlockmap[h.filename] = true
		h.fs.noBlockmapMu.Unlock()
		return nil, nil
	}
	bm.Filename = h.filename

	h.fs.blockmapsMu.Lock()
	h.fs.blockmaps[h.filename] = bm
	h.fs.blockmapsMu.Unlock()

	return bm, nil
}

func (h *FileHandle) fetchBlock(ctx context.Context, entry blockmap.BlockmapEntry) error {
	key := fmt.Sprintf("%s:%d", h.filename, entry.FileOffset)
	_, err, _ := h.fs.blockDL.Do(key, func() (interface{}, error) {
		if h.fs.bc.HasBlock(h.filename, entry.FileOffset) {
			return nil, nil
		}

		fetchLen := int64(8) + int64(entry.BlockDataSize)
		data, err := h.fs.rc.FetchBlock(context.Background(), h.filename, entry.FileOffset, fetchLen)
		if err != nil {
			return nil, fmt.Errorf("fetch block at %d: %w", entry.FileOffset, err)
		}

		if int64(len(data)) < fetchLen {
			return nil, fmt.Errorf("short block: got %d bytes, want %d", len(data), fetchLen)
		}

		if len(data) < 88 {
			return nil, fmt.Errorf("block too small for header verification: %d bytes", len(data))
		}
		first := sha256.Sum256(data[8:88])
		blockHash := sha256.Sum256(first[:])
		if blockHash != entry.BlockHash {
			return nil, fmt.Errorf("block hash mismatch at offset %d", entry.FileOffset)
		}

		if err := h.fs.bc.StoreBlock(h.filename, entry.FileOffset, data); err != nil {
			return nil, fmt.Errorf("cache block: %w", err)
		}

		return nil, nil
	})
	return err
}

func (h *FileHandle) assembleRead(dest []byte, off int64, blocks []blockmap.BlockmapEntry) (int, error) {
	written := 0
	readEnd := off + int64(len(dest))

	for _, entry := range blocks {
		blockData, err := h.fs.bc.GetBlock(h.filename, entry.FileOffset)
		if err != nil {
			return 0, fmt.Errorf("get cached block at %d: %w", entry.FileOffset, err)
		}

		blockStart := entry.FileOffset
		blockEnd := blockStart + int64(len(blockData))

		copyStart := off
		if copyStart < blockStart {
			copyStart = blockStart
		}
		copyEnd := readEnd
		if copyEnd > blockEnd {
			copyEnd = blockEnd
		}

		if copyStart >= copyEnd {
			continue
		}

		srcOffset := copyStart - blockStart
		dstOffset := copyStart - off
		n := copy(dest[dstOffset:], blockData[srcOffset:srcOffset+(copyEnd-copyStart)])
		written += n
	}

	return written, nil
}
