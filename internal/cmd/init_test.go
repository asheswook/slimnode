package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/config"
)

func newTestConfig(localDir, bitcoinDataDir string) *config.Config {
	return &config.Config{
		General: config.GeneralConfig{
			LocalDir:       localDir,
			BitcoinDataDir: bitcoinDataDir,
		},
	}
}

func TestEnsureBlocksIndexSymlink_CreatesSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "bitcoin")

	indexDir := filepath.Join(localDir, "index")
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	cfg := newTestConfig(localDir, bitcoinDir)
	err := ensureBlocksIndexSymlink(cfg)
	require.NoError(t, err)

	linkPath := filepath.Join(bitcoinDir, "blocks", "index")
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, indexDir, target)
}

func TestEnsureBlocksIndexSymlink_IdempotentWhenCorrect(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "bitcoin")

	indexDir := filepath.Join(localDir, "index")
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	cfg := newTestConfig(localDir, bitcoinDir)

	require.NoError(t, ensureBlocksIndexSymlink(cfg))
	require.NoError(t, ensureBlocksIndexSymlink(cfg))

	linkPath := filepath.Join(bitcoinDir, "blocks", "index")
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, indexDir, target)
}

func TestEnsureBlocksIndexSymlink_ErrorOnWrongTarget(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "bitcoin")

	indexDir := filepath.Join(localDir, "index")
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	linkPath := filepath.Join(bitcoinDir, "blocks", "index")
	require.NoError(t, os.MkdirAll(filepath.Dir(linkPath), 0755))
	require.NoError(t, os.Symlink("/wrong/target", linkPath))

	cfg := newTestConfig(localDir, bitcoinDir)
	err := ensureBlocksIndexSymlink(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already points to")
}

func TestEnsureBlocksIndexSymlink_ErrorOnRegularDir(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "bitcoin")

	indexDir := filepath.Join(localDir, "index")
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	existingDir := filepath.Join(bitcoinDir, "blocks", "index")
	require.NoError(t, os.MkdirAll(existingDir, 0755))

	cfg := newTestConfig(localDir, bitcoinDir)
	err := ensureBlocksIndexSymlink(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a symlink")
}

func TestEnsureBlocksIndexSymlink_ErrorOnMissingSource(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "bitcoin")

	cfg := newTestConfig(localDir, bitcoinDir)
	err := ensureBlocksIndexSymlink(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestEnsureBlocksIndexSymlink_CreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local")
	bitcoinDir := filepath.Join(tmpDir, "deep", "nested", "bitcoin")

	indexDir := filepath.Join(localDir, "index")
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	cfg := newTestConfig(localDir, bitcoinDir)
	err := ensureBlocksIndexSymlink(cfg)
	require.NoError(t, err)

	linkPath := filepath.Join(bitcoinDir, "blocks", "index")
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, indexDir, target)
}
