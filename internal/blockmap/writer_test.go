package blockmap

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteBinaryFormat verifies that Write() produces correct binary format
func TestWriteBinaryFormat(t *testing.T) {
	// Create blockmap with 100 entries
	bm := &Blockmap{
		Filename: "blk00000.dat",
		Entries:  make([]BlockmapEntry, 100),
	}

	// Generate predictable test data
	for i := 0; i < 100; i++ {
		entry := &bm.Entries[i]
		// Set block hash to predictable pattern
		for j := 0; j < 32; j++ {
			entry.BlockHash[j] = byte((i + j) % 256)
		}
		entry.FileOffset = int64(i) * 1000
		entry.BlockDataSize = uint32(500 + i)
	}

	// Write to buffer
	buf := &bytes.Buffer{}
	if err := Write(buf, bm); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data := buf.Bytes()

	// Verify header
	if len(data) < HeaderSize {
		t.Fatalf("data too short: got %d, want at least %d", len(data), HeaderSize)
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != Magic {
		t.Errorf("magic: got 0x%08x, want 0x%08x", magic, Magic)
	}

	version := binary.LittleEndian.Uint16(data[4:6])
	if version != Version {
		t.Errorf("version: got %d, want %d", version, Version)
	}

	entryCount := binary.LittleEndian.Uint32(data[6:10])
	if entryCount != 100 {
		t.Errorf("entry_count: got %d, want 100", entryCount)
	}

	// Verify reserved bytes are zeros
	for i := 10; i < 16; i++ {
		if data[i] != 0 {
			t.Errorf("reserved byte %d: got 0x%02x, want 0x00", i, data[i])
		}
	}

	// Verify total size
	expectedSize := HeaderSize + (100 * EntrySize)
	if len(data) != expectedSize {
		t.Errorf("total size: got %d, want %d", len(data), expectedSize)
	}

	// Spot-check first entry
	firstEntryOffset := HeaderSize
	firstHash := data[firstEntryOffset : firstEntryOffset+32]
	expectedFirstHash := bm.Entries[0].BlockHash[:]
	if !bytes.Equal(firstHash, expectedFirstHash) {
		t.Errorf("first entry hash mismatch")
	}

	firstOffset := int64(binary.LittleEndian.Uint64(data[firstEntryOffset+32 : firstEntryOffset+40]))
	if firstOffset != 0 {
		t.Errorf("first entry offset: got %d, want 0", firstOffset)
	}

	firstSize := binary.LittleEndian.Uint32(data[firstEntryOffset+40 : firstEntryOffset+44])
	if firstSize != 500 {
		t.Errorf("first entry size: got %d, want 500", firstSize)
	}
}

// TestWriteFileAtomic verifies that WriteFile creates file atomically
func TestWriteFileAtomic(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.blockmap")

	// Create blockmap with a few entries
	bm := &Blockmap{
		Filename: "blk00001.dat",
		Entries: []BlockmapEntry{
			{
				BlockHash:     [32]byte{1, 2, 3, 4},
				FileOffset:    100,
				BlockDataSize: 200,
			},
			{
				BlockHash:     [32]byte{5, 6, 7, 8},
				FileOffset:    500,
				BlockDataSize: 300,
			},
		},
	}

	// Write file
	if err := WriteFile(filePath, bm); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Read file back
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}

	// Verify content matches Write() output
	buf := &bytes.Buffer{}
	if err := Write(buf, bm); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !bytes.Equal(fileData, buf.Bytes()) {
		t.Errorf("file content mismatch")
	}

	// Verify no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read dir failed: %v", err)
	}

	for _, entry := range entries {
		if bytes.Contains([]byte(entry.Name()), []byte(".tmp")) {
			t.Errorf("temp file not cleaned up: %s", entry.Name())
		}
	}
}

// TestWriteEmptyBlockmap verifies empty blockmap is written correctly
func TestWriteEmptyBlockmap(t *testing.T) {
	bm := &Blockmap{
		Filename: "blk00002.dat",
		Entries:  []BlockmapEntry{},
	}

	buf := &bytes.Buffer{}
	if err := Write(buf, bm); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data := buf.Bytes()

	// Verify header only
	if len(data) != HeaderSize {
		t.Errorf("size: got %d, want %d", len(data), HeaderSize)
	}

	entryCount := binary.LittleEndian.Uint32(data[6:10])
	if entryCount != 0 {
		t.Errorf("entry_count: got %d, want 0", entryCount)
	}
}

// TestWriteFileCreatesDirectory verifies WriteFile creates parent directory
func TestWriteFileCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir", "nested", "test.blockmap")

	bm := &Blockmap{
		Filename: "blk00003.dat",
		Entries:  []BlockmapEntry{},
	}

	if err := WriteFile(filePath, bm); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// TestWriteEntriesSorted verifies entries are sorted by FileOffset in output
func TestWriteEntriesSorted(t *testing.T) {
	bm := &Blockmap{
		Filename: "blk00004.dat",
		Entries: []BlockmapEntry{
			{BlockHash: [32]byte{3}, FileOffset: 300, BlockDataSize: 30},
			{BlockHash: [32]byte{1}, FileOffset: 100, BlockDataSize: 10},
			{BlockHash: [32]byte{2}, FileOffset: 200, BlockDataSize: 20},
		},
	}

	buf := &bytes.Buffer{}
	if err := Write(buf, bm); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data := buf.Bytes()

	for i := 0; i < 3; i++ {
		entryOffset := HeaderSize + (i * EntrySize)
		offset := int64(binary.LittleEndian.Uint64(data[entryOffset+32 : entryOffset+40]))
		expectedOffset := int64((i + 1) * 100)
		if offset != expectedOffset {
			t.Errorf("entry %d offset: got %d, want %d", i, offset, expectedOffset)
		}
	}
}

// TestWriteNilBlockmap verifies Write rejects a nil blockmap.
func TestWriteNilBlockmap(t *testing.T) {
	buf := &bytes.Buffer{}
	err := Write(buf, nil)
	if err == nil {
		t.Fatal("expected error for nil blockmap, got nil")
	}
}

// TestWriteFile_Success verifies WriteFile produces a file with the correct size.
func TestWriteFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "blk00010.dat.blockmap")

	bm := &Blockmap{
		Filename: "blk00010.dat",
		Entries: []BlockmapEntry{
			{BlockHash: [32]byte{0xAA}, FileOffset: 0, BlockDataSize: 80},
		},
	}

	if err := WriteFile(path, bm); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not found after WriteFile: %v", err)
	}

	expectedSize := int64(HeaderSize + EntrySize)
	if info.Size() != expectedSize {
		t.Errorf("file size: got %d, want %d", info.Size(), expectedSize)
	}
}

// TestWriteFile_UnwritablePath verifies WriteFile fails when a path component
// is a regular file instead of a directory (making MkdirAll fail).
func TestWriteFile_UnwritablePath(t *testing.T) {
	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "notadir")
	if err := os.WriteFile(blockingFile, []byte("block"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	path := filepath.Join(tmpDir, "notadir", "out.blockmap")
	bm := &Blockmap{Entries: []BlockmapEntry{}}

	err := WriteFile(path, bm)
	if err == nil {
		t.Fatal("expected error when parent path component is a file, got nil")
	}
}

func TestWriteFile_NilBlockmap(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nil.blockmap")

	err := WriteFile(path, nil)
	if err == nil {
		t.Fatal("expected error for nil blockmap, got nil")
	}
}

func TestWriteFile_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping read-only dir test when running as root")
	}
	tmpDir := t.TempDir()
	roDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(roDir, 0555); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer os.Chmod(roDir, 0755)

	path := filepath.Join(roDir, "out.blockmap")
	bm := &Blockmap{Entries: []BlockmapEntry{}}

	err := WriteFile(path, bm)
	if err == nil {
		t.Fatal("expected error writing to read-only directory, got nil")
	}
}

func TestWriteFile_RenameTargetIsDir(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "targetdir")
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	bm := &Blockmap{Entries: []BlockmapEntry{}}

	err := WriteFile(targetDir, bm)
	if err == nil {
		t.Fatal("expected error when rename target is an existing directory, got nil")
	}
}
