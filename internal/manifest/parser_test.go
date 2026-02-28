package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifest(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 884521,
		"tip_hash": "0000000000000000000abc",
		"server_id": "archive-eu-01",
		"files": [
			{
				"name": "blk00000.dat",
				"size": 134217728,
				"sha256": "a1b2c3d4e5f6",
				"height_first": 0,
				"height_last": 1023,
				"finalized": true
			},
			{
				"name": "blk00001.dat",
				"size": 134217728,
				"sha256": "b2c3d4e5f6a1",
				"height_first": 1024,
				"height_last": 2047,
				"finalized": true
			}
		],
		"snapshots": {
			"latest_height": 880000,
			"blocks_index": {
				"height": 880000,
				"url": "/v1/snapshot/blocks-index-880000.tar.zst",
				"sha256": "snap1",
				"size": 2147483648
			},
			"utxo": {
				"height": 880000,
				"url": "/v1/snapshot/utxo-880000.dat",
				"sha256": "snap2",
				"size": 12884901888
			}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Equal(t, 1, m.Version)
	assert.Equal(t, "mainnet", m.Chain)
	assert.Equal(t, int64(884521), m.TipHeight)
	assert.Equal(t, "archive-eu-01", m.ServerID)
	assert.Len(t, m.Files, 2)
	assert.Equal(t, "blk00000.dat", m.Files[0].Name)
	assert.Equal(t, int64(134217728), m.Files[0].Size)
	assert.Equal(t, "a1b2c3d4e5f6", m.Files[0].SHA256)
	assert.True(t, m.Files[0].Finalized)
}

func TestParseInvalidJSON(t *testing.T) {
	jsonData := `{invalid json`
	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	assert.Error(t, err)
	assert.Nil(t, m)
}

func TestParseEmptyFiles(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 0,
		"tip_hash": "genesis",
		"server_id": "test",
		"files": [],
		"snapshots": {
			"latest_height": 0,
			"blocks_index": {"height": 0, "url": "", "sha256": "", "size": 0},
			"utxo": {"height": 0, "url": "", "sha256": "", "size": 0}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Len(t, m.Files, 0)
}

func TestDiffAdded(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
			{Name: "blk00003.dat", Size: 100, SHA256: "hash4"},
			{Name: "blk00004.dat", Size: 100, SHA256: "hash5"},
		},
	}

	diff := Diff(old, new)

	assert.Len(t, diff.Added, 2)
	assert.Len(t, diff.Removed, 0)
	assert.Len(t, diff.Changed, 0)
	assert.Equal(t, "blk00003.dat", diff.Added[0].Name)
	assert.Equal(t, "blk00004.dat", diff.Added[1].Name)
}

func TestDiffRemoved(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
			{Name: "blk00003.dat", Size: 100, SHA256: "hash4"},
			{Name: "blk00004.dat", Size: 100, SHA256: "hash5"},
		},
	}

	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	diff := Diff(old, new)

	assert.Len(t, diff.Added, 0)
	assert.Len(t, diff.Removed, 2)
	assert.Len(t, diff.Changed, 0)
	assert.Equal(t, "blk00003.dat", diff.Removed[0].Name)
	assert.Equal(t, "blk00004.dat", diff.Removed[1].Name)
}

func TestDiffChanged(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2_changed"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	diff := Diff(old, new)

	assert.Len(t, diff.Added, 0)
	assert.Len(t, diff.Removed, 0)
	assert.Len(t, diff.Changed, 1)
	assert.Equal(t, "blk00001.dat", diff.Changed[0].Name)
}

func TestDiffChangedSize(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
		},
	}

	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 200, SHA256: "hash1"},
		},
	}

	diff := Diff(old, new)

	assert.Len(t, diff.Changed, 1)
	assert.Equal(t, "blk00000.dat", diff.Changed[0].Name)
}

func TestDiffNoChange(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
			{Name: "blk00002.dat", Size: 100, SHA256: "hash3"},
		},
	}

	diff := Diff(old, new)

	assert.Len(t, diff.Added, 0)
	assert.Len(t, diff.Removed, 0)
	assert.Len(t, diff.Changed, 0)
}

func TestDiffNilOld(t *testing.T) {
	new := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
		},
	}

	diff := Diff(nil, new)

	assert.Len(t, diff.Added, 1)
	assert.Len(t, diff.Removed, 0)
	assert.Len(t, diff.Changed, 0)
}

func TestDiffNilNew(t *testing.T) {
	old := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
		},
	}

	diff := Diff(old, nil)

	assert.Len(t, diff.Added, 0)
	assert.Len(t, diff.Removed, 1)
	assert.Len(t, diff.Changed, 0)
}

func TestLastFileNumber(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat"},
			{Name: "blk00050.dat"},
			{Name: "blk00100.dat"},
			{Name: "blk00075.dat"},
		},
	}

	num := LastFileNumber(m)
	assert.Equal(t, 100, num)
}

func TestLastFileNumberZero(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat"},
		},
	}

	num := LastFileNumber(m)
	assert.Equal(t, 0, num)
}

func TestLastFileNumberNoBlkFiles(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{
			{Name: "other.dat"},
			{Name: "file.txt"},
		},
	}

	num := LastFileNumber(m)
	assert.Equal(t, -1, num)
}

func TestLastFileNumberNil(t *testing.T) {
	num := LastFileNumber(nil)
	assert.Equal(t, -1, num)
}

func TestLastFileNumberEmpty(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{},
	}

	num := LastFileNumber(m)
	assert.Equal(t, -1, num)
}

func TestFindFile(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
			{Name: "blk00001.dat", Size: 100, SHA256: "hash2"},
		},
	}

	file := FindFile(m, "blk00000.dat")
	require.NotNil(t, file)
	assert.Equal(t, "blk00000.dat", file.Name)
	assert.Equal(t, int64(100), file.Size)
}

func TestFindFileNotFound(t *testing.T) {
	m := &Manifest{
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
		},
	}

	file := FindFile(m, "blk00999.dat")
	assert.Nil(t, file)
}

func TestFindFileNil(t *testing.T) {
	file := FindFile(nil, "blk00000.dat")
	assert.Nil(t, file)
}

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "manifest.json")

	m := &Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc",
		ServerID:  "archive-eu-01",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1", HeightFirst: 0, HeightLast: 1023, Finalized: true},
		},
		Snapshots: Snapshots{
			LatestHeight: 880000,
			BlocksIndex: SnapshotEntry{Height: 880000, URL: "/v1/snapshot/blocks-index-880000.tar.zst", SHA256: "snap1", Size: 2147483648},
			UTXO:        SnapshotEntry{Height: 880000, URL: "/v1/snapshot/utxo-880000.dat", SHA256: "snap2", Size: 12884901888},
		},
	}

	err := WriteFile(filePath, m)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Verify content
	parsed, err := ParseFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, m.Version, parsed.Version)
	assert.Equal(t, m.Chain, parsed.Chain)
	assert.Equal(t, m.TipHeight, parsed.TipHeight)
	assert.Len(t, parsed.Files, 1)
	assert.Equal(t, "blk00000.dat", parsed.Files[0].Name)
}

func TestWriteToBuffer(t *testing.T) {
	m := &Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 100,
		TipHash:   "hash",
		ServerID:  "test",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1"},
		},
		Snapshots: Snapshots{
			LatestHeight: 100,
			BlocksIndex:  SnapshotEntry{Height: 100, URL: "/url", SHA256: "hash", Size: 1000},
			UTXO:         SnapshotEntry{Height: 100, URL: "/url", SHA256: "hash", Size: 1000},
		},
	}

	var buf bytes.Buffer
	err := Write(&buf, m)
	require.NoError(t, err)

	// Verify JSON is valid
	parsed, err := Parse(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, m.Version, parsed.Version)
	assert.Equal(t, m.Chain, parsed.Chain)
}

func TestBackwardCompatibilityNoBlockmap(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 884521,
		"tip_hash": "0000000000000000000abc",
		"server_id": "archive-eu-01",
		"files": [
			{
				"name": "blk00000.dat",
				"size": 134217728,
				"sha256": "a1b2c3d4e5f6",
				"height_first": 0,
				"height_last": 1023,
				"finalized": true
			},
			{
				"name": "blk00001.dat",
				"size": 134217728,
				"sha256": "b2c3d4e5f6a1",
				"height_first": 1024,
				"height_last": 2047,
				"finalized": true
			}
		],
		"snapshots": {
			"latest_height": 880000,
			"blocks_index": {
				"height": 880000,
				"url": "/v1/snapshot/blocks-index-880000.tar.zst",
				"sha256": "snap1",
				"size": 2147483648
			},
			"utxo": {
				"height": 880000,
				"url": "/v1/snapshot/utxo-880000.dat",
				"sha256": "snap2",
				"size": 12884901888
			}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Len(t, m.Files, 2)

	for _, file := range m.Files {
		assert.False(t, file.HasBlockmap(), "file %s should not have blockmap", file.Name)
		assert.Equal(t, "", file.BlockmapSHA256)
	}
}

func TestManifestWithBlockmapSHA256(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 884521,
		"tip_hash": "0000000000000000000abc",
		"server_id": "archive-eu-01",
		"files": [
			{
				"name": "blk00000.dat",
				"size": 134217728,
				"sha256": "a1b2c3d4e5f6",
				"height_first": 0,
				"height_last": 1023,
				"finalized": true,
				"blockmap_sha256": "blockmap_hash_0"
			},
			{
				"name": "blk00001.dat",
				"size": 134217728,
				"sha256": "b2c3d4e5f6a1",
				"height_first": 1024,
				"height_last": 2047,
				"finalized": true
			},
			{
				"name": "blk00002.dat",
				"size": 134217728,
				"sha256": "c3d4e5f6a1b2",
				"height_first": 2048,
				"height_last": 3071,
				"finalized": true,
				"blockmap_sha256": "blockmap_hash_2"
			}
		],
		"snapshots": {
			"latest_height": 880000,
			"blocks_index": {
				"height": 880000,
				"url": "/v1/snapshot/blocks-index-880000.tar.zst",
				"sha256": "snap1",
				"size": 2147483648
			},
			"utxo": {
				"height": 880000,
				"url": "/v1/snapshot/utxo-880000.dat",
				"sha256": "snap2",
				"size": 12884901888
			}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Len(t, m.Files, 3)

	assert.True(t, m.Files[0].HasBlockmap())
	assert.Equal(t, "blockmap_hash_0", m.Files[0].BlockmapSHA256)

	assert.False(t, m.Files[1].HasBlockmap())
	assert.Equal(t, "", m.Files[1].BlockmapSHA256)

	assert.True(t, m.Files[2].HasBlockmap())
	assert.Equal(t, "blockmap_hash_2", m.Files[2].BlockmapSHA256)
}

func TestManifestWithBaseURL(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 884521,
		"tip_hash": "0000000000000000000abc",
		"server_id": "archive-eu-01",
		"base_url": "https://cdn.example.com",
		"files": [
			{
				"name": "blk00000.dat",
				"size": 134217728,
				"sha256": "a1b2c3d4e5f6",
				"height_first": 0,
				"height_last": 1023,
				"finalized": true
			}
		],
		"snapshots": {
			"latest_height": 880000,
			"blocks_index": {
				"height": 880000,
				"url": "/v1/snapshot/blocks-index-880000.tar.zst",
				"sha256": "snap1",
				"size": 2147483648
			},
			"utxo": {
				"height": 880000,
				"url": "/v1/snapshot/utxo-880000.dat",
				"sha256": "snap2",
				"size": 12884901888
			}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Equal(t, "https://cdn.example.com", m.BaseURL)
}

func TestManifestBaseURLOmitted(t *testing.T) {
	jsonData := `{
		"version": 1,
		"chain": "mainnet",
		"tip_height": 884521,
		"tip_hash": "0000000000000000000abc",
		"server_id": "archive-eu-01",
		"files": [
			{
				"name": "blk00000.dat",
				"size": 134217728,
				"sha256": "a1b2c3d4e5f6",
				"height_first": 0,
				"height_last": 1023,
				"finalized": true
			}
		],
		"snapshots": {
			"latest_height": 880000,
			"blocks_index": {
				"height": 880000,
				"url": "/v1/snapshot/blocks-index-880000.tar.zst",
				"sha256": "snap1",
				"size": 2147483648
			},
			"utxo": {
				"height": 880000,
				"url": "/v1/snapshot/utxo-880000.dat",
				"sha256": "snap2",
				"size": 12884901888
			}
		}
	}`

	r := bytes.NewReader([]byte(jsonData))
	m, err := Parse(r)

	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Equal(t, "", m.BaseURL)
}

func TestManifestBaseURLOmittedFromJSON(t *testing.T) {
	m := &Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc",
		ServerID:  "archive-eu-01",
		BaseURL:   "",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1", HeightFirst: 0, HeightLast: 1023, Finalized: true},
		},
		Snapshots: Snapshots{
			LatestHeight: 880000,
			BlocksIndex:  SnapshotEntry{Height: 880000, URL: "/v1/snapshot/blocks-index-880000.tar.zst", SHA256: "snap1", Size: 2147483648},
			UTXO:         SnapshotEntry{Height: 880000, URL: "/v1/snapshot/utxo-880000.dat", SHA256: "snap2", Size: 12884901888},
		},
	}

	var buf bytes.Buffer
	err := Write(&buf, m)
	require.NoError(t, err)

	// Verify base_url is NOT in JSON when empty
	jsonStr := buf.String()
	assert.NotContains(t, jsonStr, "base_url")
}

func TestManifestBaseURLIncludedInJSON(t *testing.T) {
	m := &Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc",
		ServerID:  "archive-eu-01",
		BaseURL:   "https://cdn.example.com",
		Files: []ManifestFile{
			{Name: "blk00000.dat", Size: 100, SHA256: "hash1", HeightFirst: 0, HeightLast: 1023, Finalized: true},
		},
		Snapshots: Snapshots{
			LatestHeight: 880000,
			BlocksIndex:  SnapshotEntry{Height: 880000, URL: "/v1/snapshot/blocks-index-880000.tar.zst", SHA256: "snap1", Size: 2147483648},
			UTXO:         SnapshotEntry{Height: 880000, URL: "/v1/snapshot/utxo-880000.dat", SHA256: "snap2", Size: 12884901888},
		},
	}

	var buf bytes.Buffer
	err := Write(&buf, m)
	require.NoError(t, err)

	// Verify base_url IS in JSON when set
	jsonStr := buf.String()
	assert.Contains(t, jsonStr, "base_url")
	assert.Contains(t, jsonStr, "https://cdn.example.com")
}
