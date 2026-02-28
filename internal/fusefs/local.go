package fusefs

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// LocalFilePath returns the absolute path for a local file.
func LocalFilePath(localDir, filename string) string {
	return filepath.Join(localDir, filename)
}

// ScanLocalFiles scans localDir and returns FileEntry records for existing files.
// Files < 128 MiB are ACTIVE; files == 128 MiB are LOCAL_FINALIZED.
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

		state := store.FileStateActive
		if info.Size() >= store.FinalizedFileThreshold {
			state = store.FileStateLocalFinalized
		}

		result = append(result, store.FileEntry{
			Filename:   name,
			State:      state,
			Source:     store.FileSourceLocal,
			Size:       info.Size(),
			CreatedAt:  info.ModTime(),
			LastAccess: time.Now(),
		})
	}
	return result, nil
}
