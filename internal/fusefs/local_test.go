package fusefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

func TestLocalFilePath(t *testing.T) {
	result := LocalFilePath("/tmp/local", "blk00000.dat")
	assert.Equal(t, "/tmp/local/blk00000.dat", result)
}

func TestScanLocalFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	entries, err := ScanLocalFiles(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestScanLocalFiles_NotExist(t *testing.T) {
	entries, err := ScanLocalFiles("/nonexistent/path/xyz")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestScanLocalFiles_AllLocalFilesAreActive(t *testing.T) {
	dir := t.TempDir()

	// Small file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blk00005.dat"), make([]byte, 1024), 0644))

	// Large file (128 MiB) — still ACTIVE at startup, runtime handles finalization
	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(store.MaxBlockFileSize))
	f.Close()

	// Rev file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rev00000.dat"), make([]byte, 512), 0644))

	entries, err := ScanLocalFiles(dir)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	for _, e := range entries {
		assert.Equal(t, store.FileStateActive, e.State,
			"all local files should be ACTIVE at startup: %s", e.Filename)
	}
}

func TestScanLocalFiles_SkipsNonBlockFiles(t *testing.T) {
	dir := t.TempDir()

	// Non-block files should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(dir, "xor.dat"), make([]byte, 8), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".lock"), make([]byte, 1), 0644))
	// Only blk/rev should be returned
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blk00000.dat"), make([]byte, 1024), 0644))

	entries, err := ScanLocalFiles(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "blk00000.dat", entries[0].Filename)
}

func TestScanLocalFiles_SkipsZeroByteFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a 0-byte file (crash artifact)
	zeroFile := filepath.Join(dir, "blk99999.dat")
	require.NoError(t, os.WriteFile(zeroFile, []byte{}, 0644))

	// Create a valid file
	validFile := filepath.Join(dir, "blk00001.dat")
	require.NoError(t, os.WriteFile(validFile, make([]byte, 1024), 0644))

	entries, err := ScanLocalFiles(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "blk00001.dat", entries[0].Filename)

	// 0-byte file should be removed from disk
	_, statErr := os.Stat(zeroFile)
	assert.True(t, os.IsNotExist(statErr), "zero-byte file should be removed")
}

func TestParseBlockFileNumber(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		wantN  int
		wantOK bool
	}{
		{"blk00000.dat", "blk", 0, true},
		{"blk00100.dat", "blk", 100, true},
		{"rev00005.dat", "rev", 5, true},
		{"blk.dat", "blk", 0, false},      // no digits
		{"xor.dat", "blk", 0, false},       // wrong prefix
		{"blk00000.txt", "blk", 0, false},  // wrong suffix
		{"blk00abc.dat", "blk", 0, false},  // non-numeric
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := parseBlockFileNumber(tt.name, tt.prefix)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantN, n)
			}
		})
	}
}

func TestInodeForFile(t *testing.T) {
	assert.Equal(t, uint64(3), InodeForFile("xor.dat"))
	assert.Equal(t, uint64(4), InodeForFile(".lock"))
	assert.Equal(t, uint64(1_000_100), InodeForFile("blk00100.dat"))
	assert.Equal(t, uint64(2_000_100), InodeForFile("rev00100.dat"))
	assert.Equal(t, uint64(1_000_000), InodeForFile("blk00000.dat"))

	unknown1 := InodeForFile("unknown.dat")
	unknown2 := InodeForFile("unknown.dat")
	assert.Equal(t, unknown1, unknown2)
	assert.GreaterOrEqual(t, unknown1, uint64(3_000_000))
}
