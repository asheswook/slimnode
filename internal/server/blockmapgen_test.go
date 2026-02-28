package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/asheswook/bitcoin-lfn/internal/blockmap"
	"github.com/asheswook/bitcoin-lfn/internal/store"
	"github.com/stretchr/testify/require"
)

const testMagic uint32 = 0xD9B4BEF9

func buildSyntheticFinalizedBlk(t *testing.T, path string) {
	t.Helper()

	blockDataSize := uint32(store.MaxBlockFileSize - 8)

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	preamble := make([]byte, 8)
	binary.LittleEndian.PutUint32(preamble[0:4], testMagic)
	binary.LittleEndian.PutUint32(preamble[4:8], blockDataSize)
	_, err = f.Write(preamble)
	require.NoError(t, err)

	header := make([]byte, 80)
	_, err = f.Write(header)
	require.NoError(t, err)

	require.NoError(t, f.Truncate(store.MaxBlockFileSize))
}

func buildSmallBlkFile(t *testing.T, path string) {
	t.Helper()

	blockDataSize := uint32(80)

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	preamble := make([]byte, 8)
	binary.LittleEndian.PutUint32(preamble[0:4], testMagic)
	binary.LittleEndian.PutUint32(preamble[4:8], blockDataSize)
	_, err = f.Write(preamble)
	require.NoError(t, err)

	header := make([]byte, 80)
	_, err = f.Write(header)
	require.NoError(t, err)
}

func TestBlockmapGen(t *testing.T) {
	blocksDir := t.TempDir()
	outputDir := t.TempDir()

	for _, name := range []string{"blk00100.dat", "blk00101.dat", "blk00102.dat"} {
		buildSyntheticFinalizedBlk(t, filepath.Join(blocksDir, name))
	}

	buildSmallBlkFile(t, filepath.Join(blocksDir, "blk00103.dat"))

	require.NoError(t, os.WriteFile(filepath.Join(blocksDir, "rev00100.dat"), []byte("dummy"), 0644))

	result, err := GenerateBlockmaps(blocksDir, outputDir, testMagic)
	require.NoError(t, err)

	require.Len(t, result, 3)
	require.Contains(t, result, "blk00100.dat")
	require.Contains(t, result, "blk00101.dat")
	require.Contains(t, result, "blk00102.dat")

	_, activeExists := result["blk00103.dat"]
	require.False(t, activeExists, "active file should not be in result")

	_, revExists := result["rev00100.dat"]
	require.False(t, revExists, "rev file should not be in result")

	for _, blkName := range []string{"blk00100.dat", "blk00101.dat", "blk00102.dat"} {
		bmPath := filepath.Join(outputDir, blkName+".blockmap")

		_, statErr := os.Stat(bmPath)
		require.NoError(t, statErr, "blockmap file should exist: %s", bmPath)

		bm, readErr := blockmap.ReadFile(bmPath)
		require.NoError(t, readErr)
		require.Equal(t, blkName, bm.Filename)
		require.Len(t, bm.Entries, 1, "expected 1 block entry per finalized file")

		f, openErr := os.Open(bmPath)
		require.NoError(t, openErr)
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		f.Close()
		require.NoError(t, copyErr)
		expectedHash := hex.EncodeToString(h.Sum(nil))
		require.Equal(t, expectedHash, result[blkName], "SHA256 mismatch for %s", blkName)
	}
}

func TestBlockmapGenIdempotent(t *testing.T) {
	blocksDir := t.TempDir()
	outputDir1 := t.TempDir()
	outputDir2 := t.TempDir()

	buildSyntheticFinalizedBlk(t, filepath.Join(blocksDir, "blk00000.dat"))
	buildSyntheticFinalizedBlk(t, filepath.Join(blocksDir, "blk00001.dat"))

	result1, err := GenerateBlockmaps(blocksDir, outputDir1, testMagic)
	require.NoError(t, err)

	result2, err := GenerateBlockmaps(blocksDir, outputDir2, testMagic)
	require.NoError(t, err)

	require.Equal(t, result1, result2, "SHA256 maps should be identical across runs")

	for _, blkName := range []string{"blk00000.dat", "blk00001.dat"} {
		content1, err := os.ReadFile(filepath.Join(outputDir1, blkName+".blockmap"))
		require.NoError(t, err)

		content2, err := os.ReadFile(filepath.Join(outputDir2, blkName+".blockmap"))
		require.NoError(t, err)

		require.True(t, bytes.Equal(content1, content2), "blockmap contents should be byte-identical: %s", blkName)
	}
}
