package fusefs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// LocalFilePath returns the absolute path for a local file.
func LocalFilePath(localDir, filename string) string {
	return filepath.Join(localDir, filename)
}

// ScanLocalFiles scans localDir and returns FileEntry records for blk/rev files.
// A blk file is finalized when the next sequential blk file exists (indicating
// Bitcoin Core has moved on) or when its size reaches MaxBlockFileSize (128 MiB).
// A rev file is finalized when its corresponding blk file is finalized.
// Non-blk/rev files (.lock, xor.dat, etc.) are skipped; they are handled
// separately by the FUSE layer.
func ScanLocalFiles(localDir string) ([]store.FileEntry, error) {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type scannedFile struct {
		name string
		size int64
		mod  time.Time
	}

	blkFiles := make(map[int]scannedFile)
	revFiles := make(map[int]scannedFile)

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		info, err := de.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			slog.Warn("removing stale zero-byte file", "file", name)
			_ = os.Remove(filepath.Join(localDir, name))
			continue
		}

		sf := scannedFile{name: name, size: info.Size(), mod: info.ModTime()}
		if num, ok := parseBlockFileNumber(name, "blk"); ok {
			blkFiles[num] = sf
		} else if num, ok := parseBlockFileNumber(name, "rev"); ok {
			revFiles[num] = sf
		}
	}

	var result []store.FileEntry

	for num, sf := range blkFiles {
		state := store.FileStateActive
		_, hasSuccessor := blkFiles[num+1]
		if hasSuccessor || sf.size >= store.MaxBlockFileSize {
			state = store.FileStateLocalFinalized
		}
		result = append(result, store.FileEntry{
			Filename:   sf.name,
			State:      state,
			Source:     store.FileSourceLocal,
			Size:       sf.size,
			CreatedAt:  sf.mod,
			LastAccess: time.Now(),
		})
	}

	for num, sf := range revFiles {
		state := store.FileStateActive
		if blk, blkExists := blkFiles[num]; blkExists {
			_, hasSuccessor := blkFiles[num+1]
			if hasSuccessor || blk.size >= store.MaxBlockFileSize {
				state = store.FileStateLocalFinalized
			}
		}
		result = append(result, store.FileEntry{
			Filename:   sf.name,
			State:      state,
			Source:     store.FileSourceLocal,
			Size:       sf.size,
			CreatedAt:  sf.mod,
			LastAccess: time.Now(),
		})
	}

	return result, nil
}

// parseBlockFileNumber extracts the numeric portion from a block file name
// with the given prefix (e.g. "blk" or "rev"). Returns the number and true
// if the name matches the pattern prefix#####.dat.
func parseBlockFileNumber(name, prefix string) (int, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".dat") {
		return 0, false
	}
	mid := name[len(prefix) : len(name)-4]
	if len(mid) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(mid)
	if err != nil {
		return 0, false
	}
	return n, true
}
