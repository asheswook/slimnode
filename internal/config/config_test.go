package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile(t *testing.T) {
	// Create a temporary directory and config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.conf")

	configContent := `[general]
general.chain = mainnet
general.cache-dir = ~/.slimnode/cache
general.local-dir = ~/.slimnode/local
general.mount-point = /mnt/bitcoin-blocks
general.log-level = info

[cache]
cache.max-size-gb = 50
cache.min-keep-recent = 10

[server]
server.url = https://bitcoin-archive.example.com
server.request-timeout = 30s
server.retry-count = 3

[compaction]
compaction.trigger = auto
compaction.threshold = 85
compaction.pre-download = true
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load config from file
	args := []string{"--config", configPath}
	cfg, err := Load(args)
	require.NoError(t, err)

	// Verify all fields are parsed correctly
	assert.Equal(t, "mainnet", cfg.General.Chain)
	assert.Equal(t, "/mnt/bitcoin-blocks", cfg.General.MountPoint)
	assert.Equal(t, "info", cfg.General.LogLevel)
	assert.Equal(t, 50, cfg.Cache.MaxSizeGB)
	assert.Equal(t, 10, cfg.Cache.MinKeepRecent)
	assert.Equal(t, "https://bitcoin-archive.example.com", cfg.Server.URL)
	assert.Equal(t, 30*time.Second, cfg.Server.RequestTimeout)
	assert.Equal(t, 3, cfg.Server.RetryCount)
	assert.Equal(t, "auto", cfg.Compact.Trigger)
	assert.Equal(t, 85, cfg.Compact.Threshold)
	assert.Equal(t, true, cfg.Compact.PreDownload)
}

func TestCLIOverride(t *testing.T) {
	// Create a temporary directory and config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.conf")

	configContent := `[general]
general.mount-point = /mnt/bitcoin-blocks

[cache]
cache.max-size-gb = 50

[server]
server.url = https://bitcoin-archive.example.com
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load config with CLI override
	args := []string{
		"--config", configPath,
		"--cache.max-size-gb", "100",
	}
	cfg, err := Load(args)
	require.NoError(t, err)

	// Verify CLI flag overrides INI value
	assert.Equal(t, 100, cfg.Cache.MaxSizeGB)
	// Verify default is used for fields not in INI or CLI
	assert.Equal(t, 10, cfg.Cache.MinKeepRecent)
}

func TestDefaults(t *testing.T) {
	// Load config with minimal args (no config file)
	args := []string{
		"--general.mount-point", "/mnt/bitcoin-blocks",
		"--server.url", "https://bitcoin-archive.example.com",
	}
	cfg, err := Load(args)
	require.NoError(t, err)

	// Verify default values are set
	assert.Equal(t, "mainnet", cfg.General.Chain)
	assert.Equal(t, "info", cfg.General.LogLevel)
	assert.Equal(t, 50, cfg.Cache.MaxSizeGB)
	assert.Equal(t, 10, cfg.Cache.MinKeepRecent)
	assert.Equal(t, 30*time.Second, cfg.Server.RequestTimeout)
	assert.Equal(t, 3, cfg.Server.RetryCount)
	assert.Equal(t, "auto", cfg.Compact.Trigger)
	assert.Equal(t, 85, cfg.Compact.Threshold)
	assert.Equal(t, true, cfg.Compact.PreDownload)
}

func TestMissingRequired(t *testing.T) {
	// Load config without required mount-point
	args := []string{
		"--server.url", "https://bitcoin-archive.example.com",
	}
	_, err := Load(args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount-point")

	// Load config without required server.url
	args = []string{
		"--general.mount-point", "/mnt/bitcoin-blocks",
	}
	_, err = Load(args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url")
}

func TestPathExpansion(t *testing.T) {
	args := []string{
		"--general.mount-point", "/mnt/bitcoin-blocks",
		"--server.url", "https://bitcoin-archive.example.com",
		"--general.cache-dir", "~/.slimnode/cache",
		"--general.local-dir", "~/.slimnode/local",
		"--general.bitcoin-datadir", "~/.bitcoin",
	}
	cfg, err := Load(args)
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	expectedCacheDir := filepath.Join(home, ".slimnode/cache")
	expectedLocalDir := filepath.Join(home, ".slimnode/local")
	expectedBitcoinDir := filepath.Join(home, ".bitcoin")

	assert.Equal(t, expectedCacheDir, cfg.General.CacheDir)
	assert.Equal(t, expectedLocalDir, cfg.General.LocalDir)
	assert.Equal(t, expectedBitcoinDir, cfg.General.BitcoinDataDir)
}

