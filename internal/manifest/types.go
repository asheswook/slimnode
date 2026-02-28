package manifest

// ManifestFile represents a single file entry in the server manifest.
type ManifestFile struct {
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	SHA256         string `json:"sha256"`
	HeightFirst    int64  `json:"height_first"`
	HeightLast     int64  `json:"height_last"`
	Finalized      bool   `json:"finalized"`
	BlockmapSHA256 string `json:"blockmap_sha256,omitempty"`
}

// HasBlockmap returns true if the file has a blockmap SHA256 hash.
func (f *ManifestFile) HasBlockmap() bool {
	return f.BlockmapSHA256 != ""
}

// Manifest represents the server's manifest.json.
type Manifest struct {
	Version   int            `json:"version"`
	Chain     string         `json:"chain"`
	TipHeight int64          `json:"tip_height"`
	TipHash   string         `json:"tip_hash"`
	ServerID  string         `json:"server_id"`
	BaseURL   string         `json:"base_url,omitempty"`
	Files     []ManifestFile `json:"files"`
	Snapshots Snapshots      `json:"snapshots"`
}

// Snapshots section of the manifest.
type Snapshots struct {
	LatestHeight int64         `json:"latest_height"`
	BlocksIndex  SnapshotEntry `json:"blocks_index"`
	UTXO         SnapshotEntry `json:"utxo"`
}

// SnapshotEntry describes a single snapshot artifact.
type SnapshotEntry struct {
	Height int64  `json:"height"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
