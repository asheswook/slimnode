// Package server provides tools for the slimnode-server: manifest generation
// and snapshot creation.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

// ManifestOption configures manifest generation behavior.
type ManifestOption func(*manifestConfig)

type manifestConfig struct {
	blockmapDir string
	snapshotDir string
	baseURL     string
	workers     int
	prev        *manifest.Manifest
}

// WithBlockmapDir configures manifest generation to include blockmap SHA-256 hashes
// for finalized blk files that have corresponding blockmap files in the given directory.
func WithBlockmapDir(dir string) ManifestOption {
	return func(c *manifestConfig) {
		c.blockmapDir = dir
	}
}

// WithSnapshotDir configures manifest generation to discover and include
// snapshot entries (blocks-index and UTXO) from the given directory.
func WithSnapshotDir(dir string) ManifestOption {
	return func(c *manifestConfig) {
		c.snapshotDir = dir
	}
}

// WithBaseURL configures the base URL to be included in the manifest.
func WithBaseURL(url string) ManifestOption {
	return func(c *manifestConfig) {
		c.baseURL = url
	}
}

func WithWorkers(n int) ManifestOption {
	return func(c *manifestConfig) {
		c.workers = n
	}
}

// WithPreviousManifest supplies a previously generated manifest whose file hashes
// can be reused when name and size match, avoiding full SHA-256 rehash.
// A nil prev is a no-op: all files are hashed as usual.
func WithPreviousManifest(prev *manifest.Manifest) ManifestOption {
	return func(c *manifestConfig) {
		c.prev = prev
	}
}

// GenerateManifest scans blocksDir for finalized blk/rev files and builds a manifest.
// A blk file is finalized when its size >= FinalizedFileThreshold.
// A rev file is finalized when its corresponding blk file (same number) is finalized.
func GenerateManifest(blocksDir, chain string, opts ...ManifestOption) (*manifest.Manifest, error) {
	cfg := &manifestConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.workers <= 0 {
		cfg.workers = runtime.NumCPU()
	}

	dirInfo, err := os.Stat(blocksDir)
	if err != nil {
		return nil, fmt.Errorf("blocks directory %q: %w", blocksDir, err)
	}
	if !dirInfo.IsDir() {
		return nil, fmt.Errorf("blocks directory %q is not a directory", blocksDir)
	}

	fmt.Fprintf(os.Stderr, "Scanning %s\n", blocksDir)

	scanned, err := ScanFinalizedFiles(blocksDir)
	if err != nil {
		return nil, fmt.Errorf("scan finalized files: %w", err)
	}

	if len(scanned) == 0 {
		entries, err := os.ReadDir(blocksDir)
		if err == nil {
			fmt.Fprintf(os.Stderr, "WARNING: No finalized blk*.dat/rev*.dat found. Directory has %d entries", len(entries))
			if len(entries) > 0 {
				fmt.Fprintf(os.Stderr, ":")
				limit := 10
				if len(entries) < limit {
					limit = len(entries)
				}
				for _, e := range entries[:limit] {
					fmt.Fprintf(os.Stderr, " %s", e.Name())
				}
				if len(entries) > 10 {
					fmt.Fprintf(os.Stderr, " ... (%d more)", len(entries)-10)
				}
			}
			fmt.Fprintln(os.Stderr)
		}
	}

	total := len(scanned)
	manifestFiles := make([]manifest.ManifestFile, total)

	// Build hash cache from previous manifest.
	// Finalized block files are immutable: name+size match guarantees the hash is still valid.
	prevCache := make(map[string]manifest.ManifestFile)
	if cfg.prev != nil {
		for _, f := range cfg.prev.Files {
			prevCache[f.Name] = f
		}
	}

	fmt.Fprintf(os.Stderr, "Processing %d files with %d workers...\n", total, cfg.workers)

	var done, cached atomic.Int64
	g := new(errgroup.Group)
	g.SetLimit(cfg.workers)

	for i, sf := range scanned {
		i, sf := i, sf
		g.Go(func() error {
			// Check cache: if previous manifest has this file at the same size,
			// reuse the SHA-256 (finalized block files are immutable).
			if prev, ok := prevCache[sf.Name]; ok && prev.Size == sf.Size && prev.SHA256 != "" {
				mf := manifest.ManifestFile{
					Name:        sf.Name,
					Size:        sf.Size,
					SHA256:      prev.SHA256,
					HeightFirst: 0,
					HeightLast:  0,
					Finalized:   true,
				}
				// Reuse blockmap SHA-256 only when blockmapDir is configured and cache has it.
				if cfg.blockmapDir != "" && prev.BlockmapSHA256 != "" {
					mf.BlockmapSHA256 = prev.BlockmapSHA256
				}
				manifestFiles[i] = mf
				cached.Add(1)
				n := done.Add(1)
				if n%100 == 0 || n == int64(total) {
					fmt.Fprintf(os.Stderr, "Processed %d/%d files...\n", n, total)
				}
				return nil
			}

			// Cache miss: compute SHA-256.
			hashStr, err := sha256File(sf.Path)
			if err != nil {
				return fmt.Errorf("sha256 %s: %w", sf.Path, err)
			}

			mf := manifest.ManifestFile{
				Name:        sf.Name,
				Size:        sf.Size,
				SHA256:      hashStr,
				HeightFirst: 0,
				HeightLast:  0,
				Finalized:   true,
			}

			if cfg.blockmapDir != "" {
				bmPath := filepath.Join(cfg.blockmapDir, sf.Name+".blockmap")
				if _, err := os.Stat(bmPath); err == nil {
					bmHashStr, err := sha256File(bmPath)
					if err != nil {
						return fmt.Errorf("sha256 blockmap %s: %w", bmPath, err)
					}
					mf.BlockmapSHA256 = bmHashStr
				}
			}

			manifestFiles[i] = mf

			n := done.Add(1)
			if n%100 == 0 || n == int64(total) {
				fmt.Fprintf(os.Stderr, "Processed %d/%d files...\n", n, total)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	if cachedCount := cached.Load(); cachedCount > 0 {
		fmt.Fprintf(os.Stderr, "Manifest: %d/%d files from cache, %d hashed\n", cachedCount, total, int64(total)-cachedCount)
	}

	m := &manifest.Manifest{
		Version:   1,
		Chain:     chain,
		TipHeight: 0,
		TipHash:   "",
		ServerID:  "local",
		BaseURL:   cfg.baseURL,
		Files:     manifestFiles,
	}

	// Discover snapshots if snapshot directory is configured
	if cfg.snapshotDir != "" {
		snapshots, err := discoverSnapshots(cfg.snapshotDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: snapshot discovery failed: %v\n", err)
		} else {
			m.Snapshots = snapshots
		}
	}

	return m, nil
}

func extractFileNumber(name string) string {
	for _, prefix := range []string{"blk", "rev"} {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".dat") {
			return name[len(prefix) : len(name)-len(".dat")]
		}
	}
	return ""
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// discoverSnapshots scans the snapshot directory for the latest blocks-index and UTXO snapshots.
func discoverSnapshots(snapshotDir string) (manifest.Snapshots, error) {
	var snapshots manifest.Snapshots

	blocksIndexFile, blocksIndexHeight, err := findLatestSnapshot(snapshotDir, "blocks-index-")
	if err != nil {
		return snapshots, fmt.Errorf("find blocks-index snapshot: %w", err)
	}

	utxoFile, utxoHeight, err := findLatestSnapshot(snapshotDir, "utxo-")
	if err != nil {
		return snapshots, fmt.Errorf("find utxo snapshot: %w", err)
	}

	if blocksIndexFile != "" {
		hashStr, err := sha256File(filepath.Join(snapshotDir, blocksIndexFile))
		if err != nil {
			return snapshots, fmt.Errorf("sha256 blocks-index: %w", err)
		}
		info, err := os.Stat(filepath.Join(snapshotDir, blocksIndexFile))
		if err != nil {
			return snapshots, fmt.Errorf("stat blocks-index: %w", err)
		}
		snapshots.BlocksIndex = manifest.SnapshotEntry{
			Height: blocksIndexHeight,
			URL:    "/v1/snapshot/" + blocksIndexFile,
			SHA256: hashStr,
			Size:   info.Size(),
		}
	}

	if utxoFile != "" {
		hashStr, err := sha256File(filepath.Join(snapshotDir, utxoFile))
		if err != nil {
			return snapshots, fmt.Errorf("sha256 utxo: %w", err)
		}
		info, err := os.Stat(filepath.Join(snapshotDir, utxoFile))
		if err != nil {
			return snapshots, fmt.Errorf("stat utxo: %w", err)
		}
		snapshots.UTXO = manifest.SnapshotEntry{
			Height: utxoHeight,
			URL:    "/v1/snapshot/" + utxoFile,
			SHA256: hashStr,
			Size:   info.Size(),
		}
	}

	if blocksIndexHeight > utxoHeight {
		snapshots.LatestHeight = blocksIndexHeight
	} else {
		snapshots.LatestHeight = utxoHeight
	}

	return snapshots, nil
}

// findLatestSnapshot finds the snapshot file with the highest height matching the given prefix.
func findLatestSnapshot(dir, prefix string) (string, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, err
	}

	var bestFile string
	var bestHeight int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		height := parseHeightFromFilename(name)
		if height < 0 {
			continue
		}
		if height > bestHeight || bestFile == "" {
			bestHeight = height
			bestFile = name
		}
	}

	return bestFile, bestHeight, nil
}

// parseHeightFromFilename extracts the block height from snapshot filenames.
// Expected patterns: blocks-index-880000.tar.zst → 880000, utxo-880000.dat → 880000
func parseHeightFromFilename(name string) int64 {
	base := name
	for _, ext := range []string{".tar.zst", ".dat"} {
		if strings.HasSuffix(base, ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}

	idx := strings.LastIndex(base, "-")
	if idx < 0 || idx+1 >= len(base) {
		return -1
	}

	height, err := strconv.ParseInt(base[idx+1:], 10, 64)
	if err != nil {
		return -1
	}

	return height
}
