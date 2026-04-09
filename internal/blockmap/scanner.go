package blockmap

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

// ScanBlkFile scans a Bitcoin blk file and extracts all block entries.
// It reads sequentially using offset-based ReadAt and does NOT load the
// entire file into memory.
//
// networkMagic must match the 4-byte LE magic at the start of each preamble.
// Mainnet: 0xD9B4BEF9, testnet3: 0x0709110B, signet: 0x40CF030A,
// regtest: 0xDAB5BFFA, testnet4: 0x283F161C.
//
// Returns an empty Blockmap (no error) for an empty file.
// Returns an error for invalid magic bytes or truncated data.
func ScanBlkFile(path string, networkMagic uint32) (*Blockmap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	fileSize := info.Size()
	bm := &Blockmap{
		Filename: filepath.Base(path),
	}

	if fileSize == 0 {
		return bm, nil
	}

	var offset int64
	var preamble [8]byte

	for offset < fileSize {
		if offset+8 > fileSize {
			return nil, fmt.Errorf("truncated preamble at offset %d: need 8 bytes, only %d remaining", offset, fileSize-offset)
		}

		if _, err := f.ReadAt(preamble[:], offset); err != nil {
			return nil, fmt.Errorf("read preamble at offset %d: %w", offset, err)
		}

		magic := binary.LittleEndian.Uint32(preamble[0:4])
		if magic != networkMagic {
			return nil, fmt.Errorf("invalid magic at offset %d: got 0x%08X, expected 0x%08X", offset, magic, networkMagic)
		}

		blockSize := binary.LittleEndian.Uint32(preamble[4:8])

		if offset+8+int64(blockSize) > fileSize {
			return nil, fmt.Errorf("truncated block at offset %d: preamble claims %d bytes but only %d remain after preamble", offset, blockSize, fileSize-(offset+8))
		}

		if blockSize < 80 {
			return nil, fmt.Errorf("block at offset %d has data size %d, need at least 80 bytes for header", offset, blockSize)
		}

		var header [80]byte
		if _, err := f.ReadAt(header[:], offset+8); err != nil {
			return nil, fmt.Errorf("read header at offset %d: %w", offset+8, err)
		}

		first := sha256.Sum256(header[:])
		blockHash := sha256.Sum256(first[:])

		bm.Entries = append(bm.Entries, BlockmapEntry{
			BlockHash:     blockHash,
			FileOffset:    offset,
			BlockDataSize: blockSize,
		})

		offset += 8 + int64(blockSize)
	}

	return bm, nil
}
