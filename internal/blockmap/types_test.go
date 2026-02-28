package blockmap

import (
	"testing"
)

func TestFindBlock(t *testing.T) {
	// Create a blockmap with 3 entries:
	// Entry 0: offset=0, size=992 (covers bytes [0, 1000))
	// Entry 1: offset=1000, size=4000 (covers bytes [1000, 5008))
	// Entry 2: offset=5008, size=3000 (covers bytes [5008, 8016))
	bm := &Blockmap{
		Filename: "blk00000.dat",
		Entries: []BlockmapEntry{
			{
				BlockHash:     [32]byte{0x00},
				FileOffset:    0,
				BlockDataSize: 992,
			},
			{
				BlockHash:     [32]byte{0x01},
				FileOffset:    1000,
				BlockDataSize: 4000,
			},
			{
				BlockHash:     [32]byte{0x02},
				FileOffset:    5008,
				BlockDataSize: 3000,
			},
		},
	}

	tests := []struct {
		name     string
		offset   int64
		expected *BlockmapEntry
	}{
		{
			name:     "offset in entry 0",
			offset:   500,
			expected: &bm.Entries[0],
		},
		{
			name:     "offset at entry 1 boundary",
			offset:   1000,
			expected: &bm.Entries[1],
		},
		{
			name:     "offset in entry 1",
			offset:   4999,
			expected: &bm.Entries[1],
		},
		{
			name:     "offset in entry 2",
			offset:   5010,
			expected: &bm.Entries[2],
		},
		{
			name:     "offset before all entries",
			offset:   -1,
			expected: nil,
		},
		{
			name:     "offset after all entries",
			offset:   9999,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bm.FindBlock(tt.offset)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("expected entry, got nil")
				} else if result.FileOffset != tt.expected.FileOffset {
					t.Errorf("expected offset %d, got %d", tt.expected.FileOffset, result.FileOffset)
				}
			}
		})
	}
}

func TestFindBlocks(t *testing.T) {
	// Create a blockmap with 3 entries:
	// Entry 0: offset=0, size=992 (covers bytes [0, 1000))
	// Entry 1: offset=1000, size=4000 (covers bytes [1000, 5008))
	// Entry 2: offset=5008, size=3000 (covers bytes [5008, 8016))
	bm := &Blockmap{
		Filename: "blk00000.dat",
		Entries: []BlockmapEntry{
			{
				BlockHash:     [32]byte{0x00},
				FileOffset:    0,
				BlockDataSize: 992,
			},
			{
				BlockHash:     [32]byte{0x01},
				FileOffset:    1000,
				BlockDataSize: 4000,
			},
			{
				BlockHash:     [32]byte{0x02},
				FileOffset:    5008,
				BlockDataSize: 3000,
			},
		},
	}

	tests := []struct {
		name           string
		offset         int64
		length         int64
		expectedCount  int
		expectedOffsets []int64
	}{
		{
			name:            "range spans entries 0 and 1",
			offset:          990,
			length:          100,
			expectedCount:   2,
			expectedOffsets: []int64{0, 1000},
		},
		{
			name:            "range within entry 0",
			offset:          500,
			length:          10,
			expectedCount:   1,
			expectedOffsets: []int64{0},
		},
		{
			name:            "range spans all entries",
			offset:          0,
			length:          8016,
			expectedCount:   3,
			expectedOffsets: []int64{0, 1000, 5008},
		},
		{
			name:            "range before all entries",
			offset:          -100,
			length:          50,
			expectedCount:   0,
			expectedOffsets: []int64{},
		},
		{
			name:            "range after all entries",
			offset:          9000,
			length:          100,
			expectedCount:   0,
			expectedOffsets: []int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bm.FindBlocks(tt.offset, tt.length)
			if len(result) != tt.expectedCount {
				t.Errorf("expected %d entries, got %d", tt.expectedCount, len(result))
			}
			for i, offset := range tt.expectedOffsets {
				if i >= len(result) {
					t.Errorf("expected entry at offset %d, but got fewer entries", offset)
					break
				}
				if result[i].FileOffset != offset {
					t.Errorf("entry %d: expected offset %d, got %d", i, offset, result[i].FileOffset)
				}
			}
		})
	}
}
