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

func TestScanLocalFiles_ActiveAndFinalized(t *testing.T) {
	dir := t.TempDir()

	activeFile := filepath.Join(dir, "blk00005.dat")
	require.NoError(t, os.WriteFile(activeFile, make([]byte, 1024), 0644))

	finalizedFile := filepath.Join(dir, "blk00000.dat")
	f, err := os.Create(finalizedFile)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(store.MaxBlockFileSize))
	f.Close()

	entries, err := ScanLocalFiles(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	byName := make(map[string]store.FileEntry)
	for _, e := range entries {
		byName[e.Filename] = e
	}

	assert.Equal(t, store.FileStateActive, byName["blk00005.dat"].State)
	assert.Equal(t, store.FileStateLocalFinalized, byName["blk00000.dat"].State)
	assert.Equal(t, int64(store.MaxBlockFileSize), byName["blk00000.dat"].Size)
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
