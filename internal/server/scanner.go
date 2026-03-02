package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// ScannedFile represents a finalized block file found during scanning.
type ScannedFile struct {
	Name string
	Path string
	Size int64
}

// ScanFinalizedFiles scans blocksDir for finalized blk/rev files.
// A blk file is finalized when its size >= store.FinalizedFileThreshold.
// A rev file is finalized when its corresponding blk file (same number) is finalized.
func ScanFinalizedFiles(blocksDir string) ([]ScannedFile, error) {
	if _, err := os.Stat(blocksDir); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	blkFiles, err := filepath.Glob(filepath.Join(blocksDir, "blk*.dat"))
	if err != nil {
		return nil, fmt.Errorf("glob blk files: %w", err)
	}
	revFiles, err := filepath.Glob(filepath.Join(blocksDir, "rev*.dat"))
	if err != nil {
		return nil, fmt.Errorf("glob rev files: %w", err)
	}

	// First pass: identify finalized blk files by size threshold.
	finalizedBlkNums := make(map[string]bool) // e.g. "00000" -> true
	var scanned []ScannedFile
	for _, f := range blkFiles {
		info, err := os.Stat(f)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", f, err)
		}
		if info.Size() >= store.FinalizedFileThreshold {
			scanned = append(scanned, ScannedFile{
				Name: filepath.Base(f),
				Path: f,
				Size: info.Size(),
			})
			num := extractFileNumber(filepath.Base(f))
			if num != "" {
				finalizedBlkNums[num] = true
			}
		}
	}

	// Second pass: rev files are finalized iff their corresponding blk file is.
	for _, f := range revFiles {
		num := extractFileNumber(filepath.Base(f))
		if num != "" && finalizedBlkNums[num] {
			info, err := os.Stat(f)
			if err != nil {
				return nil, fmt.Errorf("stat %s: %w", f, err)
			}
			scanned = append(scanned, ScannedFile{
				Name: filepath.Base(f),
				Path: f,
				Size: info.Size(),
			})
		}
	}

	sort.Slice(scanned, func(i, j int) bool {
		return scanned[i].Path < scanned[j].Path
	})

	return scanned, nil
}
