//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/server"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

func TestManifestGeneration(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, "blk0000"+string(rune('0'+i))+".dat")
		f, err := os.Create(name)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(store.MaxBlockFileSize))
		f.Close()
	}

	partial := filepath.Join(dir, "blk00003.dat")
	require.NoError(t, os.WriteFile(partial, make([]byte, 1024), 0644))

	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, "rev0000"+string(rune('0'+i))+".dat")
		f, err := os.Create(name)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(store.MaxBlockFileSize))
		f.Close()
	}

	mf, err := server.GenerateManifest(dir, "mainnet")
	require.NoError(t, err)

	assert.Equal(t, 6, len(mf.Files))

	for _, f := range mf.Files {
		path := filepath.Join(dir, f.Name)
		file, err := os.Open(path)
		require.NoError(t, err)
		h := sha256.New()
		_, err = io.Copy(h, file)
		file.Close()
		require.NoError(t, err)
		expected := hex.EncodeToString(h.Sum(nil))
		assert.Equal(t, expected, f.SHA256, "SHA-256 mismatch for %s", f.Name)
	}
}

func TestManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 2; i++ {
		name := filepath.Join(dir, "blk0000"+string(rune('0'+i))+".dat")
		f, err := os.Create(name)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(store.MaxBlockFileSize))
		f.Close()
	}

	mf, err := server.GenerateManifest(dir, "mainnet")
	require.NoError(t, err)

	outPath := filepath.Join(dir, "manifest.json")
	require.NoError(t, manifest.WriteFile(outPath, mf))

	parsed, err := manifest.ParseFile(outPath)
	require.NoError(t, err)

	assert.Equal(t, mf.Version, parsed.Version)
	assert.Equal(t, mf.Chain, parsed.Chain)
	assert.Equal(t, len(mf.Files), len(parsed.Files))

	for i, f := range mf.Files {
		assert.Equal(t, f.Name, parsed.Files[i].Name)
		assert.Equal(t, f.SHA256, parsed.Files[i].SHA256)
		assert.Equal(t, f.Size, parsed.Files[i].Size)
	}
}
