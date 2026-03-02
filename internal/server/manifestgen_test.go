package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateManifest(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"blk00000.dat", "blk00001.dat", "blk00002.dat"} {
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, f.Truncate(128*1024*1024))
		require.NoError(t, f.Close())
	}

	partial, err := os.Create(filepath.Join(dir, "blk00003.dat"))
	require.NoError(t, err)
	require.NoError(t, partial.Truncate(store.FinalizedFileThreshold-1))
	require.NoError(t, partial.Close())

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m.Files, 3)
	require.Equal(t, 1, m.Version)
	require.Equal(t, "mainnet", m.Chain)
	require.Equal(t, "local", m.ServerID)
	for _, file := range m.Files {
		require.True(t, file.Finalized)
		require.Equal(t, int64(128*1024*1024), file.Size)
	}
}

func TestGenerateManifestEmpty(t *testing.T) {
	dir := t.TempDir()

	m, err := GenerateManifest(dir, "testnet")
	require.NoError(t, err)
	require.Len(t, m.Files, 0)
	require.Equal(t, "testnet", m.Chain)
}

func TestGenerateManifestSHA256(t *testing.T) {
	dir := t.TempDir()

	name := "blk00000.dat"
	f, err := os.Create(filepath.Join(dir, name))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	rf, err := os.Open(filepath.Join(dir, name))
	require.NoError(t, err)
	defer rf.Close()

	h := sha256.New()
	_, err = io.Copy(h, rf)
	require.NoError(t, err)
	expected := hex.EncodeToString(h.Sum(nil))

	require.Equal(t, expected, m.Files[0].SHA256)
}

func TestGenerateManifestWithBlockmapSHA256(t *testing.T) {
	blocksDir := t.TempDir()
	blockmapDir := t.TempDir()

	// Create finalized blk file
	blkName := "blk00000.dat"
	blkPath := filepath.Join(blocksDir, blkName)
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Create blockmap file
	bmPath := filepath.Join(blockmapDir, blkName+".blockmap")
	bmf, err := os.Create(bmPath)
	require.NoError(t, err)
	require.NoError(t, bmf.Truncate(1024))
	require.NoError(t, bmf.Close())

	// Generate manifest with blockmap directory
	m, err := GenerateManifest(blocksDir, "mainnet", WithBlockmapDir(blockmapDir))
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	// Verify blockmap SHA-256 is set
	require.NotEmpty(t, m.Files[0].BlockmapSHA256)

	// Verify blockmap SHA-256 matches actual file hash
	bmrf, err := os.Open(bmPath)
	require.NoError(t, err)
	defer bmrf.Close()

	h := sha256.New()
	_, err = io.Copy(h, bmrf)
	require.NoError(t, err)
	expected := hex.EncodeToString(h.Sum(nil))

	require.Equal(t, expected, m.Files[0].BlockmapSHA256)
}

func TestGenerateManifestWithoutBlockmapDir(t *testing.T) {
	blocksDir := t.TempDir()

	// Create finalized blk file
	blkName := "blk00000.dat"
	blkPath := filepath.Join(blocksDir, blkName)
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Generate manifest without blockmap directory (backward compat)
	m, err := GenerateManifest(blocksDir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	// Verify blockmap SHA-256 is empty
	require.Empty(t, m.Files[0].BlockmapSHA256)
}

func TestGenerateManifestBlockmapMissing(t *testing.T) {
	blocksDir := t.TempDir()
	blockmapDir := t.TempDir()

	// Create finalized blk file
	blkName := "blk00000.dat"
	blkPath := filepath.Join(blocksDir, blkName)
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Do NOT create blockmap file

	// Generate manifest with blockmap directory
	m, err := GenerateManifest(blocksDir, "mainnet", WithBlockmapDir(blockmapDir))
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	// Verify blockmap SHA-256 is empty (file doesn't exist)
	require.Empty(t, m.Files[0].BlockmapSHA256)
}

func TestGenerateManifestWithSnapshotDir(t *testing.T) {
	blocksDir := t.TempDir()
	snapshotDir := t.TempDir()

	f, err := os.Create(filepath.Join(blocksDir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "blocks-index-880000.tar.zst"), []byte("fake snapshot"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "utxo-880000.dat"), []byte("fake utxo"), 0644))

	m, err := GenerateManifest(blocksDir, "mainnet", WithSnapshotDir(snapshotDir))
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	assert.Equal(t, int64(880000), m.Snapshots.LatestHeight)
	assert.Equal(t, int64(880000), m.Snapshots.BlocksIndex.Height)
	assert.Contains(t, m.Snapshots.BlocksIndex.URL, "blocks-index-880000.tar.zst")
	assert.NotEmpty(t, m.Snapshots.BlocksIndex.SHA256)
	assert.Greater(t, m.Snapshots.BlocksIndex.Size, int64(0))

	assert.Equal(t, int64(880000), m.Snapshots.UTXO.Height)
	assert.Contains(t, m.Snapshots.UTXO.URL, "utxo-880000.dat")
	assert.NotEmpty(t, m.Snapshots.UTXO.SHA256)
}

func TestGenerateManifestSnapshotDirEmpty(t *testing.T) {
	blocksDir := t.TempDir()
	snapshotDir := t.TempDir()

	m, err := GenerateManifest(blocksDir, "mainnet", WithSnapshotDir(snapshotDir))
	require.NoError(t, err)

	assert.Equal(t, int64(0), m.Snapshots.LatestHeight)
	assert.Empty(t, m.Snapshots.BlocksIndex.URL)
	assert.Empty(t, m.Snapshots.UTXO.URL)
}

func TestGenerateManifestSnapshotPicksLatest(t *testing.T) {
	blocksDir := t.TempDir()
	snapshotDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "blocks-index-870000.tar.zst"), []byte("old"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "blocks-index-880000.tar.zst"), []byte("new"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "utxo-880000.dat"), []byte("utxo"), 0644))

	m, err := GenerateManifest(blocksDir, "mainnet", WithSnapshotDir(snapshotDir))
	require.NoError(t, err)

	assert.Equal(t, int64(880000), m.Snapshots.BlocksIndex.Height)
	assert.Contains(t, m.Snapshots.BlocksIndex.URL, "880000")
}

func TestGenerateManifestIncludesRevFiles(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"blk00000.dat", "blk00001.dat"} {
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, f.Truncate(128*1024*1024))
		require.NoError(t, f.Close())
	}
	for _, name := range []string{"rev00000.dat", "rev00001.dat"} {
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, f.Truncate(50*1024*1024))
		require.NoError(t, f.Close())
	}

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range m.Files {
		names[f.Name] = true
	}
	assert.True(t, names["blk00000.dat"])
	assert.True(t, names["blk00001.dat"])
	assert.True(t, names["rev00000.dat"])
	assert.True(t, names["rev00001.dat"])
	assert.Len(t, m.Files, 4)
}

func TestGenerateManifestRevExcludedWhenBlkActive(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	active, err := os.Create(filepath.Join(dir, "blk00001.dat"))
	require.NoError(t, err)
	require.NoError(t, active.Truncate(store.FinalizedFileThreshold - 1))
	require.NoError(t, active.Close())

	for _, name := range []string{"rev00000.dat", "rev00001.dat"} {
		rf, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, rf.Truncate(30*1024*1024))
		require.NoError(t, rf.Close())
	}

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range m.Files {
		names[f.Name] = true
	}
	assert.True(t, names["blk00000.dat"])
	assert.True(t, names["rev00000.dat"])
	assert.False(t, names["blk00001.dat"], "active blk should be excluded")
	assert.False(t, names["rev00001.dat"], "rev for active blk should be excluded")
	assert.Len(t, m.Files, 2)
}

func TestExtractFileNumber(t *testing.T) {
	tests := []struct {
		name   string
		expect string
	}{
		{"blk00000.dat", "00000"},
		{"rev00123.dat", "00123"},
		{"blk99999.dat", "99999"},
		{"xor.dat", ""},
		{"random.txt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, extractFileNumber(tt.name))
		})
	}
}

func TestParseHeightFromFilename(t *testing.T) {
	tests := []struct {
		name   string
		expect int64
	}{
		{"blocks-index-880000.tar.zst", 880000},
		{"utxo-880000.dat", 880000},
		{"blocks-index-0.tar.zst", 0},
		{"utxo-123456.dat", 123456},
		{"random-file.txt", -1},
		{"no-height-.dat", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, parseHeightFromFilename(tt.name))
		})
	}
}

func TestGenerateManifestWithBaseURL(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	m, err := GenerateManifest(dir, "mainnet", WithBaseURL("https://cdn.example.com"))
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	assert.Equal(t, "https://cdn.example.com", m.BaseURL)
}

func TestGenerateManifestWithoutBaseURL(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	assert.Equal(t, "", m.BaseURL)
}

func TestGenerateManifestWithBaseURLAndOtherOptions(t *testing.T) {
	blocksDir := t.TempDir()
	snapshotDir := t.TempDir()

	f, err := os.Create(filepath.Join(blocksDir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "blocks-index-880000.tar.zst"), []byte("fake snapshot"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "utxo-880000.dat"), []byte("fake utxo"), 0644))

	m, err := GenerateManifest(blocksDir, "mainnet", WithBaseURL("https://cdn.example.com"), WithSnapshotDir(snapshotDir))
	require.NoError(t, err)
	require.Len(t, m.Files, 1)

	assert.Equal(t, "https://cdn.example.com", m.BaseURL)
	assert.Equal(t, int64(880000), m.Snapshots.LatestHeight)
}

func TestGenerateManifestWithPreviousManifest(t *testing.T) {
	dir := t.TempDir()

	// Create blk00000.dat (128MB sparse)
	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Generate first manifest
	m1, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m1.Files, 1)

	// Generate second manifest with previous manifest as cache
	m2, err := GenerateManifest(dir, "mainnet", WithPreviousManifest(m1))
	require.NoError(t, err)
	require.Len(t, m2.Files, 1)

	// Verify SHA256 is reused from cache
	assert.Equal(t, m1.Files[0].SHA256, m2.Files[0].SHA256)
	assert.Equal(t, m1.Files[0].Size, m2.Files[0].Size)
	assert.True(t, m2.Files[0].Finalized)
}

func TestGenerateManifestPrevNewFile(t *testing.T) {
	dir := t.TempDir()

	// Create blk00000.dat
	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Generate first manifest
	m1, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m1.Files, 1)

	// Add blk00001.dat
	f2, err := os.Create(filepath.Join(dir, "blk00001.dat"))
	require.NoError(t, err)
	require.NoError(t, f2.Truncate(128*1024*1024))
	require.NoError(t, f2.Close())

	// Generate second manifest with previous manifest as cache
	m2, err := GenerateManifest(dir, "mainnet", WithPreviousManifest(m1))
	require.NoError(t, err)
	require.Len(t, m2.Files, 2)

	// Find blk00000 and blk00001 in m2
	var blk0, blk1 *manifest.ManifestFile
	for i := range m2.Files {
		if m2.Files[i].Name == "blk00000.dat" {
			blk0 = &m2.Files[i]
		} else if m2.Files[i].Name == "blk00001.dat" {
			blk1 = &m2.Files[i]
		}
	}

	require.NotNil(t, blk0)
	require.NotNil(t, blk1)

	// Verify blk00000 SHA256 is reused from cache
	assert.Equal(t, m1.Files[0].SHA256, blk0.SHA256)

	// Verify blk00001 SHA256 is not empty (newly hashed)
	assert.NotEmpty(t, blk1.SHA256)
}

func TestGenerateManifestPrevSizeMismatch(t *testing.T) {
	dir := t.TempDir()

	// Create blk00000.dat (128MB)
	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Generate real manifest to get actual SHA256
	realM, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, realM.Files, 1)
	realSHA256 := realM.Files[0].SHA256

	// Create fake previous manifest with wrong size
	fakePrev := &manifest.Manifest{
		Files: []manifest.ManifestFile{
			{
				Name:      "blk00000.dat",
				Size:      999, // Wrong size
				SHA256:    "fakehash",
				Finalized: true,
			},
		},
	}

	// Generate manifest with fake previous manifest
	m2, err := GenerateManifest(dir, "mainnet", WithPreviousManifest(fakePrev))
	require.NoError(t, err)
	require.Len(t, m2.Files, 1)

	// Verify SHA256 is NOT from fake cache (size mismatch forces rehash)
	assert.Equal(t, realSHA256, m2.Files[0].SHA256)
	assert.NotEqual(t, "fakehash", m2.Files[0].SHA256)
}

func TestGenerateManifestPrevNil(t *testing.T) {
	dir := t.TempDir()

	// Create blk00000.dat
	f, err := os.Create(filepath.Join(dir, "blk00000.dat"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(128*1024*1024))
	require.NoError(t, f.Close())

	// Generate first manifest
	m1, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	require.Len(t, m1.Files, 1)

	// Generate second manifest with nil previous manifest
	m2, err := GenerateManifest(dir, "mainnet", WithPreviousManifest(nil))
	require.NoError(t, err)
	require.Len(t, m2.Files, 1)

	// Verify SHA256 is identical (nil = full hash, same result)
	assert.Equal(t, m1.Files[0].SHA256, m2.Files[0].SHA256)
}
