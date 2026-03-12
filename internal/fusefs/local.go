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
// All local files are classified as ACTIVE. Runtime finalization (triggered when
// a file reaches MaxBlockFileSize via writes) handles the ACTIVE → LOCAL_FINALIZED
// transition. Startup classification is intentionally conservative because
// assumeUTXO interleaves file numbers across two chains, making heuristics
// like successor-based or size-threshold finalization unsafe.
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

	var result []store.FileEntry
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

		if _, ok := parseBlockFileNumber(name, "blk"); !ok {
			if _, ok := parseBlockFileNumber(name, "rev"); !ok {
				continue
			}
		}

		result = append(result, store.FileEntry{
			Filename:   name,
			State:      store.FileStateActive,
			Source:     store.FileSourceLocal,
			Size:       info.Size(),
			CreatedAt:  info.ModTime(),
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
