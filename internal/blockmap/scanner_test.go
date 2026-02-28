package blockmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mainnetMagic = uint32(0xD9B4BEF9)

type testBlock struct {
	headerBytes [80]byte
	txData      []byte
}

func buildSyntheticBlk(blocks []testBlock) []byte {
	var buf bytes.Buffer
	for _, b := range blocks {
		blockData := append(b.headerBytes[:], b.txData...)

		var magic [4]byte
		binary.LittleEndian.PutUint32(magic[:], mainnetMagic)
		buf.Write(magic[:])

		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(blockData)))
		buf.Write(size[:])

		buf.Write(blockData)
	}
	return buf.Bytes()
}

func sha256d(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}


func TestScanSyntheticBlkFile(t *testing.T) {
	blocks := []testBlock{
		{txData: bytes.Repeat([]byte{0xAA}, 100)},
		{txData: bytes.Repeat([]byte{0xBB}, 200)},
		{txData: bytes.Repeat([]byte{0xCC}, 50)},
	}
	for i := range blocks {
		for j := range blocks[i].headerBytes {
			blocks[i].headerBytes[j] = byte(i*17 + j)
		}
	}

	data := buildSyntheticBlk(blocks)

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	bm, err := ScanBlkFile(path, mainnetMagic)
	if err != nil {
		t.Fatalf("ScanBlkFile: %v", err)
	}

	if len(bm.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(bm.Entries))
	}

	if bm.Filename != "blk00000.dat" {
		t.Fatalf("expected Filename=blk00000.dat, got %q", bm.Filename)
	}

	var sb strings.Builder
	sb.WriteString("=== TestScanSyntheticBlkFile ===\n")
	sb.WriteString(fmt.Sprintf("File: %s (%d bytes)\n", bm.Filename, len(data)))
	sb.WriteString(fmt.Sprintf("Entries: %d\n\n", len(bm.Entries)))

	var expectedOffset int64
	for i, block := range blocks {
		expectedDataSize := uint32(80 + len(block.txData))
		entry := bm.Entries[i]

		if entry.FileOffset != expectedOffset {
			t.Errorf("entry %d: FileOffset=%d, expected %d", i, entry.FileOffset, expectedOffset)
		}
		if entry.BlockDataSize != expectedDataSize {
			t.Errorf("entry %d: BlockDataSize=%d, expected %d", i, entry.BlockDataSize, expectedDataSize)
		}

		expectedHash := sha256d(block.headerBytes[:])
		if entry.BlockHash != expectedHash {
			t.Errorf("entry %d: BlockHash mismatch\n  got:  %x\n  want: %x", i, entry.BlockHash, expectedHash)
		}

		sb.WriteString(fmt.Sprintf("Entry %d:\n", i))
		sb.WriteString(fmt.Sprintf("  FileOffset:    %d\n", entry.FileOffset))
		sb.WriteString(fmt.Sprintf("  BlockDataSize: %d\n", entry.BlockDataSize))
		sb.WriteString(fmt.Sprintf("  BlockHash:     %x\n", entry.BlockHash))
		sb.WriteString(fmt.Sprintf("  Expected hash: %x\n", expectedHash))
		sb.WriteString(fmt.Sprintf("  Hash match:    %v\n\n", entry.BlockHash == expectedHash))

		expectedOffset += 8 + int64(expectedDataSize)
	}

	sb.WriteString("PASS\n")
	t.Log(sb.String())
}

func TestScanInvalidMagic(t *testing.T) {
	validBlock := testBlock{txData: bytes.Repeat([]byte{0x01}, 100)}
	for i := range validBlock.headerBytes {
		validBlock.headerBytes[i] = byte(i)
	}
	data := buildSyntheticBlk([]testBlock{validBlock})

	corruptPreamble := make([]byte, 8)
	binary.LittleEndian.PutUint32(corruptPreamble[0:4], 0xDEADBEEF)
	binary.LittleEndian.PutUint32(corruptPreamble[4:8], 100)
	corrupt := append(data, corruptPreamble...)
	corrupt = append(corrupt, bytes.Repeat([]byte{0xFF}, 100)...)

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00001.dat")
	if err := os.WriteFile(path, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ScanBlkFile(path, mainnetMagic)
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "invalid magic") {
		t.Errorf("expected error to contain 'invalid magic', got: %s", errStr)
	}

	var sb strings.Builder
	sb.WriteString("=== TestScanInvalidMagic ===\n")
	sb.WriteString(fmt.Sprintf("Error returned: %v\n", err))
	sb.WriteString(fmt.Sprintf("Contains 'invalid magic': %v\n", strings.Contains(errStr, "invalid magic")))
	sb.WriteString("PASS\n")
	t.Log(sb.String())
}

func TestScanEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	bm, err := ScanBlkFile(path, mainnetMagic)
	if err != nil {
		t.Fatalf("expected no error for empty file, got: %v", err)
	}
	if len(bm.Entries) != 0 {
		t.Fatalf("expected 0 entries for empty file, got %d", len(bm.Entries))
	}
}

func TestScanBlkFile_Truncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blk_truncated.dat")

	var buf bytes.Buffer
	var magic [4]byte
	binary.LittleEndian.PutUint32(magic[:], mainnetMagic)
	buf.Write(magic[:])

	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], 1000)
	buf.Write(size[:])

	buf.Write(bytes.Repeat([]byte{0x00}, 50))

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ScanBlkFile(path, mainnetMagic)
	if err == nil {
		t.Fatal("expected error for truncated blk file, got nil")
	}
}

func TestScanBlkFile_FileNotFound(t *testing.T) {
	_, err := ScanBlkFile(filepath.Join(t.TempDir(), "blk99999.dat"), mainnetMagic)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestScanBlkFile_TruncatedPreamble(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blk_short.dat")

	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ScanBlkFile(path, mainnetMagic)
	if err == nil {
		t.Fatal("expected error for file with less than 8 bytes, got nil")
	}
}

func TestScanBlkFile_BlockTooSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blk_small.dat")

	var buf bytes.Buffer
	var magic [4]byte
	binary.LittleEndian.PutUint32(magic[:], mainnetMagic)
	buf.Write(magic[:])

	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], 40)
	buf.Write(size[:])

	buf.Write(bytes.Repeat([]byte{0x00}, 40))

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ScanBlkFile(path, mainnetMagic)
	if err == nil {
		t.Fatal("expected error for block with data size < 80, got nil")
	}
}
