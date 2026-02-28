package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_HappyPath(t *testing.T) {
	indexDir := t.TempDir()
	outDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(indexDir, "index.bin"), []byte("block index data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, "rev.bin"), []byte("rev index data"), 0644))

	outPath := filepath.Join(outDir, "snapshot.tar.zst")

	err := CreateBlocksIndexSnapshot(indexDir, outPath)
	require.NoError(t, err)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

func TestSnapshot_EmptyDir(t *testing.T) {
	indexDir := t.TempDir()
	outDir := t.TempDir()

	outPath := filepath.Join(outDir, "empty.tar.zst")

	err := CreateBlocksIndexSnapshot(indexDir, outPath)
	require.NoError(t, err)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

func TestSnapshot_NonexistentInputDir(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "out.tar.zst")

	err := CreateBlocksIndexSnapshot("/nonexistent/path/that/does/not/exist", outPath)
	require.Error(t, err)
}

func TestSnapshot_UnwritableOutputPath(t *testing.T) {
	indexDir := t.TempDir()

	err := CreateBlocksIndexSnapshot(indexDir, "/nonexistent/dir/out.tar.zst")
	require.Error(t, err)
}

func createTarZst(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	srcDir := t.TempDir()
	for name, data := range files {
		dir := filepath.Dir(filepath.Join(srcDir, name))
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, name), data, 0644))
	}
	outPath := filepath.Join(t.TempDir(), "test.tar.zst")
	require.NoError(t, CreateBlocksIndexSnapshot(srcDir, outPath))
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	return data
}

func TestExtractTarZst(t *testing.T) {
	archive := createTarZst(t, map[string][]byte{
		"CURRENT":       []byte("manifest data"),
		"000123.ldb":    []byte("leveldb block"),
		"000124.log":    []byte("log data"),
	})

	destDir := t.TempDir()
	err := ExtractTarZst(bytes.NewReader(archive), destDir)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(destDir, "CURRENT"))
	require.NoError(t, err)
	assert.Equal(t, []byte("manifest data"), got)

	got, err = os.ReadFile(filepath.Join(destDir, "000123.ldb"))
	require.NoError(t, err)
	assert.Equal(t, []byte("leveldb block"), got)

	got, err = os.ReadFile(filepath.Join(destDir, "000124.log"))
	require.NoError(t, err)
	assert.Equal(t, []byte("log data"), got)
}

func TestExtractTarZstPathTraversal(t *testing.T) {
	destDir := t.TempDir()

	err := ExtractTarZst(bytes.NewReader([]byte("not a zstd stream")), destDir)
	require.Error(t, err)
}

func TestExtractTarZstEmpty(t *testing.T) {
	archive := createTarZst(t, map[string][]byte{})

	destDir := t.TempDir()
	err := ExtractTarZst(bytes.NewReader(archive), destDir)
	require.NoError(t, err)

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
