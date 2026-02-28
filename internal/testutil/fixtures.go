package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// SampleManifest returns a test manifest with realistic data.
func SampleManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc123def456789abcdef0123456789abcdef0123456",
		ServerID:  "archive-test-01",
		Files: []manifest.ManifestFile{
			{
				Name:        "blk00000.dat",
				Size:        134217728,
				SHA256:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				HeightFirst: 0,
				HeightLast:  1023,
				Finalized:   true,
			},
			{
				Name:        "blk00001.dat",
				Size:        134217728,
				SHA256:      "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
				HeightFirst: 1024,
				HeightLast:  2047,
				Finalized:   true,
			},
			{
				Name:        "rev00000.dat",
				Size:        134217728,
				SHA256:      "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
				HeightFirst: 0,
				HeightLast:  1023,
				Finalized:   true,
			},
		},
		Snapshots: manifest.Snapshots{
			LatestHeight: 880000,
			BlocksIndex: manifest.SnapshotEntry{
				Height: 880000,
				URL:    "/v1/snapshot/blocks-index-880000.tar.zst",
				SHA256: "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5",
				Size:   2147483648,
			},
			UTXO: manifest.SnapshotEntry{
				Height: 880000,
				URL:    "/v1/snapshot/utxo-880000.dat",
				SHA256: "e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6",
				Size:   12884901888,
			},
		},
	}
}

// SampleManifestJSON returns the JSON encoding of SampleManifest().
func SampleManifestJSON() []byte {
	manifest := SampleManifest()
	data, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	return data
}

// SampleFileEntry creates a FileEntry with the given name and state.
func SampleFileEntry(name string, state store.FileState) *store.FileEntry {
	source := store.FileSourceServer
	sha256 := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	if state == store.FileStateActive {
		source = store.FileSourceLocal
		sha256 = ""
	}

	return &store.FileEntry{
		Filename:    name,
		State:       state,
		Source:      source,
		Size:        134217728,
		SHA256:      sha256,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
		HeightFirst: 0,
		HeightLast:  1023,
	}
}

// SampleBlockFile creates a random file in a temp directory and returns its path and SHA256 hash.
func SampleBlockFile(t *testing.T, size int) (path string, sha256hex string) {
	data := RandomBytes(t, size)
	path = TempFile(t, "blk00000.dat", data)
	sha256hex = SHA256Hex(data)
	return path, sha256hex
}
