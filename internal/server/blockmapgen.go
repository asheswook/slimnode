package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/asheswook/bitcoin-slimnode/internal/blockmap"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// GenerateBlockmaps scans blocksDir for finalized blk files and generates binary
// blockmap files in outputDir. Returns a map of filename->sha256hex pairs.
// Only finalized files (size >= store.MaxBlockFileSize) are processed.
// Rev files are skipped entirely.
func GenerateBlockmaps(blocksDir, outputDir string, networkMagic uint32) (map[string]string, error) {
	allFiles, err := filepath.Glob(filepath.Join(blocksDir, "blk*.dat"))
	if err != nil {
		return nil, fmt.Errorf("glob blk files: %w", err)
	}
	sort.Strings(allFiles)

	var finalized []string
	for _, f := range allFiles {
		info, err := os.Stat(f)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", f, err)
		}
		if info.Size() >= store.FinalizedFileThreshold {
			finalized = append(finalized, f)
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	total := len(finalized)
	result := make(map[string]string, total)

	for i, path := range finalized {
		name := filepath.Base(path)

		bm, err := blockmap.ScanBlkFile(path, networkMagic)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}

		outputPath := filepath.Join(outputDir, name+".blockmap")
		if err := blockmap.WriteFile(outputPath, bm); err != nil {
			return nil, fmt.Errorf("write blockmap %s: %w", name, err)
		}

		hashStr, err := sha256File(outputPath)
		if err != nil {
			return nil, fmt.Errorf("sha256 %s: %w", outputPath, err)
		}

		result[name] = hashStr
		fmt.Fprintf(os.Stderr, "Blockmap %s (%d/%d, %d blocks)...\n", name, i+1, total, len(bm.Entries))
	}

	return result, nil
}
