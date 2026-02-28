package blockmap

import "sort"

// Binary format constants
const (
	Magic      = uint32(0x424D4150) // "BMAP" in little-endian
	Version    = uint16(1)
	HeaderSize = 16 // magic(4) + version(2) + entry_count(4) + reserved(6)
	EntrySize  = 44 // block_hash(32) + file_offset(8) + block_data_size(4)
)

// BlockmapEntry represents a single block entry in a blockmap file.
// FileOffset points to the 8-byte preamble start in the blk file (NOT after preamble).
// BlockDataSize is the size of block data EXCLUDING the 8-byte preamble.
type BlockmapEntry struct {
	BlockHash     [32]byte // SHA256d(block header 80 bytes), raw bytes (NOT reversed for display)
	FileOffset    int64    // byte offset of preamble start in the blk file
	BlockDataSize uint32   // block data size, excluding the 8-byte preamble
}

// Blockmap is the in-memory representation of a blockmap file.
// Entries are always sorted by FileOffset ascending.
type Blockmap struct {
	Filename string         // the blk file this blockmap was generated from
	Entries  []BlockmapEntry
}

// FindBlock performs a binary search to find the entry whose preamble+8+data range contains offset.
// An entry at FileOffset covers bytes [FileOffset, FileOffset+8+BlockDataSize).
// Returns nil if offset is outside all entries.
func (bm *Blockmap) FindBlock(offset int64) *BlockmapEntry {
	if len(bm.Entries) == 0 {
		return nil
	}

	// Binary search for the entry containing offset
	idx := sort.Search(len(bm.Entries), func(i int) bool {
		entry := &bm.Entries[i]
		entryEnd := entry.FileOffset + int64(entry.BlockDataSize) + 8
		return entryEnd > offset
	})

	if idx >= len(bm.Entries) {
		return nil
	}

	entry := &bm.Entries[idx]
	// Check if offset is actually within this entry's range
	if offset >= entry.FileOffset && offset < entry.FileOffset+int64(entry.BlockDataSize)+8 {
		return entry
	}

	return nil
}

// FindBlocks returns all entries whose byte ranges OVERLAP with [offset, offset+length).
// An entry overlaps if: entry.FileOffset < offset+length AND entry.FileOffset+8+entry.BlockDataSize > offset
func (bm *Blockmap) FindBlocks(offset, length int64) []BlockmapEntry {
	if len(bm.Entries) == 0 {
		return nil
	}

	rangeEnd := offset + length
	var result []BlockmapEntry

	for i := range bm.Entries {
		entry := &bm.Entries[i]
		entryEnd := entry.FileOffset + int64(entry.BlockDataSize) + 8

		// Check overlap: entry.FileOffset < rangeEnd AND entryEnd > offset
		if entry.FileOffset < rangeEnd && entryEnd > offset {
			result = append(result, *entry)
		}
	}

	return result
}
