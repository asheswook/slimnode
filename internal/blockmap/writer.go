package blockmap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Write serializes a Blockmap to binary format and writes it to w.
// The binary format consists of:
//   - Header (16 bytes): magic(4B) + version(2B) + entry_count(4B) + reserved(6B)
//   - Entries (44 bytes each, sorted by FileOffset ascending):
//     block_hash(32B) + file_offset(8B LE) + block_data_size(4B LE)
func Write(w io.Writer, bm *Blockmap) error {
	if bm == nil {
		return fmt.Errorf("blockmap is nil")
	}

	// Sort entries by FileOffset ascending (required for binary search)
	entries := make([]BlockmapEntry, len(bm.Entries))
	copy(entries, bm.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].FileOffset < entries[j].FileOffset
	})

	// Write header (16 bytes)
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint16(header[4:6], Version)
	binary.LittleEndian.PutUint32(header[6:10], uint32(len(entries)))
	// header[10:16] is reserved (zeros, already initialized)

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write entries (44 bytes each)
	for _, entry := range entries {
		entryBuf := make([]byte, EntrySize)
		copy(entryBuf[0:32], entry.BlockHash[:])
		binary.LittleEndian.PutUint64(entryBuf[32:40], uint64(entry.FileOffset))
		binary.LittleEndian.PutUint32(entryBuf[40:44], entry.BlockDataSize)

		if _, err := w.Write(entryBuf); err != nil {
			return fmt.Errorf("write entry: %w", err)
		}
	}

	return nil
}

// WriteFile writes a Blockmap to a file atomically using temp file + rename pattern.
// The parent directory is created if it doesn't exist.
func WriteFile(path string, bm *Blockmap) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Create temp file in the same directory (ensures atomic rename)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Write blockmap to temp file
	if err := Write(tmp, bm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write blockmap: %w", err)
	}

	// Close temp file before rename
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename to final path: %w", err)
	}

	return nil
}
