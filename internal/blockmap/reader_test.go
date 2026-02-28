package blockmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildTestBlockmapBytes constructs valid binary blockmap data for testing.
// It builds a blockmap with the given entries, properly formatted with header and all entries.
func buildTestBlockmapBytes(entries []BlockmapEntry) []byte {
	buf := new(bytes.Buffer)

	// Write header (16 bytes)
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint16(header[4:6], Version)
	binary.LittleEndian.PutUint32(header[6:10], uint32(len(entries)))
	// header[10:16] is reserved (zeros, already initialized)
	buf.Write(header)

	// Write entries (44 bytes each)
	for _, entry := range entries {
		entryBuf := make([]byte, EntrySize)
		copy(entryBuf[0:32], entry.BlockHash[:])
		binary.LittleEndian.PutUint64(entryBuf[32:40], uint64(entry.FileOffset))
		binary.LittleEndian.PutUint32(entryBuf[40:44], entry.BlockDataSize)
		buf.Write(entryBuf)
	}

	return buf.Bytes()
}

// TestReadRoundtrip builds binary data, reads it back, and verifies all entries match.
func TestReadRoundtrip(t *testing.T) {
	// Build test entries
	entries := make([]BlockmapEntry, 500)
	for i := 0; i < 500; i++ {
		// Create deterministic block hash
		hash := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		entries[i] = BlockmapEntry{
			BlockHash:     hash,
			FileOffset:    int64(i) * 1000,
			BlockDataSize: uint32(100 + i),
		}
	}

	// Build binary data
	data := buildTestBlockmapBytes(entries)

	// Read it back
	bm, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify
	if len(bm.Entries) != len(entries) {
		t.Errorf("entry count mismatch: got %d, want %d", len(bm.Entries), len(entries))
	}

	for i, entry := range bm.Entries {
		if entry.BlockHash != entries[i].BlockHash {
			t.Errorf("entry %d: block hash mismatch", i)
		}
		if entry.FileOffset != entries[i].FileOffset {
			t.Errorf("entry %d: file offset mismatch: got %d, want %d", i, entry.FileOffset, entries[i].FileOffset)
		}
		if entry.BlockDataSize != entries[i].BlockDataSize {
			t.Errorf("entry %d: block data size mismatch: got %d, want %d", i, entry.BlockDataSize, entries[i].BlockDataSize)
		}
	}
}

// TestReadInvalidMagic verifies Read rejects invalid magic bytes.
func TestReadInvalidMagic(t *testing.T) {
	// Build header with wrong magic
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], 0xDEADBEEF)
	binary.LittleEndian.PutUint16(header[4:6], Version)
	binary.LittleEndian.PutUint32(header[6:10], 0)

	_, err := Read(bytes.NewReader(header))
	if err == nil {
		t.Fatal("Read should reject invalid magic")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("invalid")) && !bytes.Contains([]byte(err.Error()), []byte("magic")) {
		t.Errorf("error should mention 'invalid' or 'magic': %v", err)
	}
}

// TestReadInvalidVersion verifies Read rejects unsupported version.
func TestReadInvalidVersion(t *testing.T) {
	// Build header with wrong version
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint16(header[4:6], 99)
	binary.LittleEndian.PutUint32(header[6:10], 0)

	_, err := Read(bytes.NewReader(header))
	if err == nil {
		t.Fatal("Read should reject unsupported version")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("version")) {
		t.Errorf("error should mention 'version': %v", err)
	}
}

// TestReadUnsortedEntries verifies Read rejects entries not sorted by file offset.
func TestReadUnsortedEntries(t *testing.T) {
	// Build entries in wrong order
	entries := []BlockmapEntry{
		{
			BlockHash:     [32]byte{1},
			FileOffset:    100,
			BlockDataSize: 50,
		},
		{
			BlockHash:     [32]byte{2},
			FileOffset:    0, // Out of order!
			BlockDataSize: 50,
		},
	}

	data := buildTestBlockmapBytes(entries)

	_, err := Read(bytes.NewReader(data))
	if err == nil {
		t.Fatal("Read should reject unsorted entries")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("not sorted")) {
		t.Errorf("error should mention 'not sorted': %v", err)
	}
}

// TestReadEmptyBlockmap verifies Read handles empty blockmap (0 entries).
func TestReadEmptyBlockmap(t *testing.T) {
	data := buildTestBlockmapBytes([]BlockmapEntry{})

	bm, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(bm.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(bm.Entries))
	}
}

// TestReadFile_Success writes a blockmap with WriteFile then reads it back with
// ReadFile, verifying the full roundtrip and that ".blockmap" is stripped from Filename.
func TestReadFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "blk00000.dat.blockmap")

	bm := &Blockmap{
		Filename: "blk00000.dat",
		Entries: []BlockmapEntry{
			{BlockHash: [32]byte{1, 2, 3}, FileOffset: 0, BlockDataSize: 100},
			{BlockHash: [32]byte{4, 5, 6}, FileOffset: 200, BlockDataSize: 50},
		},
	}

	if err := WriteFile(path, bm); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if got.Filename != "blk00000.dat" {
		t.Errorf("Filename: got %q, want %q", got.Filename, "blk00000.dat")
	}

	if len(got.Entries) != len(bm.Entries) {
		t.Fatalf("entry count: got %d, want %d", len(got.Entries), len(bm.Entries))
	}

	for i, e := range got.Entries {
		if e.BlockHash != bm.Entries[i].BlockHash {
			t.Errorf("entry %d: block hash mismatch", i)
		}
		if e.FileOffset != bm.Entries[i].FileOffset {
			t.Errorf("entry %d: FileOffset: got %d, want %d", i, e.FileOffset, bm.Entries[i].FileOffset)
		}
		if e.BlockDataSize != bm.Entries[i].BlockDataSize {
			t.Errorf("entry %d: BlockDataSize: got %d, want %d", i, e.BlockDataSize, bm.Entries[i].BlockDataSize)
		}
	}
}

// TestReadFile_FilenameNoStrip verifies ReadFile keeps the filename as-is when
// it does not end with ".blockmap".
func TestReadFile_FilenameNoStrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "blk00001.dat")

	bm := &Blockmap{
		Filename: "blk00001.dat",
		Entries:  []BlockmapEntry{},
	}

	if err := WriteFile(path, bm); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if got.Filename != "blk00001.dat" {
		t.Errorf("Filename: got %q, want %q", got.Filename, "blk00001.dat")
	}
}

// TestReadFile_NotFound verifies ReadFile returns an error for a nonexistent path.
func TestReadFile_NotFound(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "does_not_exist.blockmap"))
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

// TestReadFile_Corrupted verifies ReadFile returns an error when the file
// contains garbage (not a valid blockmap binary).
func TestReadFile_Corrupted(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupted.blockmap")

	if err := os.WriteFile(path, []byte("this is not a valid blockmap"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err := ReadFile(path)
	if err == nil {
		t.Fatal("expected error for corrupted file, got nil")
	}
}

// TestReadEntryCountTooLarge verifies Read rejects entry counts >= 1,000,000.
func TestReadEntryCountTooLarge(t *testing.T) {
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint16(header[4:6], Version)
	binary.LittleEndian.PutUint32(header[6:10], 1_000_000)

	_, err := Read(bytes.NewReader(header))
	if err == nil {
		t.Fatal("Read should reject entry count >= 1,000,000")
	}
}

// TestReadTruncatedEntry verifies Read returns an error when entries are truncated mid-stream.
func TestReadTruncatedEntry(t *testing.T) {
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint16(header[4:6], Version)
	binary.LittleEndian.PutUint32(header[6:10], 2)

	entry := make([]byte, EntrySize)

	data := append(header, entry...)

	_, err := Read(bytes.NewReader(data))
	if err == nil {
		t.Fatal("Read should fail on truncated entry data")
	}
}
