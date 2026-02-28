package manifest

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated write error")
}

func TestWrite_IoError(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Chain:   "mainnet",
	}
	err := Write(&errorWriter{}, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode manifest")
}

func TestWrite_RoundTrip(t *testing.T) {
	m := &Manifest{
		Version:   2,
		Chain:     "testnet",
		TipHeight: 500,
		TipHash:   "deadbeef",
		ServerID:  "test-node",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "h1", Finalized: true, BlockmapSHA256: "bm1"},
			{Name: "blk00001.dat", Size: 200, SHA256: "h2", Finalized: false},
		},
	}

	var buf bytes.Buffer
	err := Write(&buf, m)
	require.NoError(t, err)

	parsed, err := Parse(&buf)
	require.NoError(t, err)
	assert.Equal(t, m.Version, parsed.Version)
	assert.Equal(t, m.Chain, parsed.Chain)
	assert.Len(t, parsed.Files, 2)
	assert.Equal(t, "bm1", parsed.Files[0].BlockmapSHA256)
	assert.Equal(t, "", parsed.Files[1].BlockmapSHA256)
}

func TestWriteFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "manifest.json")

	m := &Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 100,
		TipHash:   "abc123",
		ServerID:  "test-server",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 134217728, SHA256: "hash1", HeightFirst: 0, HeightLast: 1023, Finalized: true},
		},
		Snapshots: Snapshots{
			LatestHeight: 100,
			BlocksIndex:  SnapshotEntry{Height: 100, URL: "/url/blocks", SHA256: "shash1", Size: 1000},
			UTXO:         SnapshotEntry{Height: 100, URL: "/url/utxo", SHA256: "shash2", Size: 2000},
		},
	}

	err := WriteFile(filePath, m)
	require.NoError(t, err)

	_, statErr := os.Stat(filePath)
	require.NoError(t, statErr)

	parsed, err := ParseFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, m.Version, parsed.Version)
	assert.Equal(t, m.Chain, parsed.Chain)
	assert.Equal(t, m.TipHeight, parsed.TipHeight)
	assert.Equal(t, m.TipHash, parsed.TipHash)
	assert.Equal(t, m.ServerID, parsed.ServerID)
	assert.Len(t, parsed.Files, 1)
	assert.Equal(t, "blk00000.dat", parsed.Files[0].Name)
	assert.True(t, parsed.Files[0].Finalized)
}

func TestWriteFile_EmptyManifest(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty-manifest.json")

	err := WriteFile(filePath, &Manifest{})
	require.NoError(t, err)

	_, statErr := os.Stat(filePath)
	require.NoError(t, statErr)

	parsed, parseErr := ParseFile(filePath)
	require.NoError(t, parseErr)
	assert.Equal(t, 0, parsed.Version)
	assert.Equal(t, "", parsed.Chain)
	assert.Len(t, parsed.Files, 0)
}

func TestWriteFile_UnwritableDir(t *testing.T) {
	err := WriteFile("/nonexistent/path/manifest.json", &Manifest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp file")
}

func TestWriteFile_RenameError(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "manifest.json")
	require.NoError(t, os.Mkdir(targetPath, 0755))

	err := WriteFile(targetPath, &Manifest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename temp file")
}

func TestWriteFile_WriteError(t *testing.T) {
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Skip("cannot get RLIMIT_FSIZE: " + err.Error())
	}

	signal.Ignore(syscall.SIGXFSZ)
	t.Cleanup(func() { signal.Reset(syscall.SIGXFSZ) })

	zero := syscall.Rlimit{Cur: 0, Max: orig.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &zero); err != nil {
		t.Skip("cannot set RLIMIT_FSIZE=0: " + err.Error())
	}
	t.Cleanup(func() { syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig) })

	tmpDir := t.TempDir()
	err := WriteFile(filepath.Join(tmpDir, "manifest.json"), &Manifest{Version: 1, Chain: "testnet"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write manifest to temp file")
}
