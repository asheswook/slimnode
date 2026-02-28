package blockmap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Read parses a binary blockmap from an io.Reader and returns a Blockmap struct.
// The binary format consists of:
//   - Header (16 bytes): magic(4B) + version(2B) + entry_count(4B) + reserved(6B)
//   - Entries (44 bytes each, must be sorted by FileOffset ascending):
//     block_hash(32B) + file_offset(8B LE) + block_data_size(4B LE)
func Read(r io.Reader) (*Blockmap, error) {
	// Read header (16 bytes)
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Parse header
	magic := binary.LittleEndian.Uint32(header[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("invalid blockmap magic: got 0x%08X, want 0x%08X", magic, Magic)
	}

	version := binary.LittleEndian.Uint16(header[4:6])
	if version != Version {
		return nil, fmt.Errorf("unsupported blockmap version: %d", version)
	}

	entryCount := binary.LittleEndian.Uint32(header[6:10])

	// Sanity check: entry_count < 1,000,000
	if entryCount >= 1000000 {
		return nil, fmt.Errorf("entry count too large: %d", entryCount)
	}

	// Read entries (44 bytes each)
	entries := make([]BlockmapEntry, entryCount)
	for i := range entryCount {
		entryBuf := make([]byte, EntrySize)
		if _, err := io.ReadFull(r, entryBuf); err != nil {
			return nil, fmt.Errorf("read entry %d: %w", i, err)
		}

		// Parse entry
		var entry BlockmapEntry
		copy(entry.BlockHash[:], entryBuf[0:32])
		entry.FileOffset = int64(binary.LittleEndian.Uint64(entryBuf[32:40]))
		entry.BlockDataSize = binary.LittleEndian.Uint32(entryBuf[40:44])

		entries[i] = entry
	}

	// Validate entries are sorted by FileOffset ascending
	for i := 1; i < len(entries); i++ {
		if entries[i].FileOffset < entries[i-1].FileOffset {
			return nil, fmt.Errorf("blockmap entries not sorted by file offset")
		}
	}

	return &Blockmap{
		Entries: entries,
	}, nil
}

// ReadFile reads a blockmap from a file path and returns a Blockmap struct.
// The Filename field is set to the base name of the file with ".blockmap" suffix stripped if present.
func ReadFile(path string) (*Blockmap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	bm, err := Read(f)
	if err != nil {
		return nil, err
	}

	// Set Filename to base name, stripping ".blockmap" suffix if present
	filename := filepath.Base(path)
	if len(filename) > len(".blockmap") && filename[len(filename)-len(".blockmap"):] == ".blockmap" {
		filename = filename[:len(filename)-len(".blockmap")]
	}
	bm.Filename = filename

	return bm, nil
}
