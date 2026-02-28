package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/config"
	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
	"github.com/asheswook/bitcoin-lfn/internal/server"
)

// InitCmd implements the `slimnode init` subcommand.
type InitCmd struct{}

// Execute runs the init command.
func (c *InitCmd) Execute(args []string) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dirs := []string{cfg.General.CacheDir, cfg.General.LocalDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	rc := remote.New(cfg.Server.URL, cfg.Server.RequestTimeout, cfg.Server.RetryCount)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	snapshotRC := remote.New(cfg.Server.URL, 0, cfg.Server.RetryCount)

	fmt.Println("Fetching manifest from server...")
	mf, _, err := rc.FetchManifest(ctx, "")
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}
	if mf == nil {
		return fmt.Errorf("server returned empty manifest")
	}

	manifestPath := filepath.Join(cfg.General.CacheDir, "manifest.json")
	if err := manifest.WriteFile(manifestPath, mf); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}
	fmt.Printf("Manifest saved to %s (%d files)\n", manifestPath, len(mf.Files))

	blockmapDir := filepath.Join(cfg.General.CacheDir, "blockmaps")
	if err := os.MkdirAll(blockmapDir, 0755); err != nil {
		return fmt.Errorf("creating blockmap cache dir: %w", err)
	}

	var bmCount int
	for i, f := range mf.Files {
		if !f.HasBlockmap() {
			continue
		}
		bmCount++
		fmt.Fprintf(os.Stderr, "Downloading blockmap %s (%d)...\r", f.Name, i+1)

		data, err := rc.FetchBlockmap(ctx, f.Name)
		if err != nil {
			slog.Warn("blockmap fetch failed, skipping", "file", f.Name, "err", err)
			continue
		}

		h := sha256.Sum256(data)
		hashStr := hex.EncodeToString(h[:])
		if hashStr != f.BlockmapSHA256 {
			slog.Warn("blockmap SHA-256 mismatch, skipping", "file", f.Name)
			continue
		}

		bmPath := filepath.Join(blockmapDir, f.Name+".blockmap")
		if err := os.WriteFile(bmPath, data, 0644); err != nil {
			slog.Warn("blockmap save failed", "file", f.Name, "err", err)
			continue
		}
	}
	if bmCount > 0 {
		fmt.Fprintf(os.Stderr, "\nDownloaded %d blockmaps\n", bmCount)
	}

	snapshotCtx := context.Background()

	if mf.Snapshots.BlocksIndex.URL != "" {
		if err := downloadBlocksIndex(snapshotCtx, snapshotRC, cfg, mf.Snapshots.BlocksIndex); err != nil {
			slog.Warn("blocks/index snapshot download failed", "err", err)
		}
	}

	if mf.Snapshots.UTXO.URL != "" {
		if err := downloadUTXOSnapshot(snapshotCtx, snapshotRC, cfg, mf.Snapshots.UTXO); err != nil {
			slog.Warn("UTXO snapshot download failed", "err", err)
		}
	}

	// Create blocks/index symlink: <bitcoin-datadir>/blocks/index → <local-dir>/index
	// bitcoind always reads blocks/index from <datadir>/blocks/index/ regardless of -blocksdir.
	if err := ensureBlocksIndexSymlink(cfg); err != nil {
		slog.Warn("blocks/index symlink creation failed", "err", err)
		fmt.Printf("Warning: could not create blocks/index symlink: %v\n", err)
		fmt.Printf("You may need to create it manually:\n  ln -s %s %s\n",
			filepath.Join(cfg.General.LocalDir, "index"),
			filepath.Join(cfg.General.BitcoinDataDir, "blocks", "index"))
	}

	fmt.Println("\nInitialization complete. Run 'slimnode mount' to start.")
	return nil
}

// ensureBlocksIndexSymlink creates a symlink from <bitcoin-datadir>/blocks/index
// to <local-dir>/index so bitcoind can find the blocks/index LevelDB.
func ensureBlocksIndexSymlink(cfg *config.Config) error {
	source := filepath.Join(cfg.General.LocalDir, "index")
	linkPath := filepath.Join(cfg.General.BitcoinDataDir, "blocks", "index")

	if _, err := os.Stat(source); os.IsNotExist(err) {
		return fmt.Errorf("source index directory does not exist: %s", source)
	}

	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	existing, err := os.Lstat(linkPath)
	if err == nil {
		if existing.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(linkPath)
			if err != nil {
				return fmt.Errorf("reading existing symlink: %w", err)
			}
			if target == source {
				fmt.Printf("blocks/index symlink already exists: %s → %s\n", linkPath, source)
				return nil
			}
			return fmt.Errorf("symlink %s already points to %s (expected %s) — remove it manually to fix", linkPath, target, source)
		}
		return fmt.Errorf("%s already exists and is not a symlink — back it up and remove it to proceed", linkPath)
	}

	if err := os.Symlink(source, linkPath); err != nil {
		return fmt.Errorf("creating symlink: %w", err)
	}
	fmt.Printf("Created blocks/index symlink: %s → %s\n", linkPath, source)
	return nil
}

// downloadBlocksIndex downloads and extracts the blocks/index snapshot
// into LocalDir/index/ where the FUSE loopback exposes it.
func downloadBlocksIndex(ctx context.Context, rc snapshotFetcher, cfg *config.Config, entry manifest.SnapshotEntry) error {
	name := filepath.Base(entry.URL)

	tmpFile, err := os.CreateTemp("", "blocks-index-*.tar.zst")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	fmt.Printf("Downloading blocks/index snapshot at height %d (%s)...\n", entry.Height, name)
	if err := rc.FetchSnapshot(ctx, name, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download blocks/index: %w", err)
	}
	tmpFile.Close()

	// Extract to LocalDir/index/ — FUSE exposes this via loopback
	destDir := filepath.Join(cfg.General.LocalDir, "index")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create index dir: %w", err)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer f.Close()

	fmt.Printf("Extracting to %s...\n", destDir)
	if err := server.ExtractTarZst(f, destDir); err != nil {
		return fmt.Errorf("extract blocks/index: %w", err)
	}

	fmt.Printf("Blocks/index snapshot extracted to %s\n", destDir)
	return nil
}

// downloadUTXOSnapshot downloads the UTXO snapshot to CacheDir for loadtxoutset.
func downloadUTXOSnapshot(ctx context.Context, rc snapshotFetcher, cfg *config.Config, entry manifest.SnapshotEntry) error {
	name := filepath.Base(entry.URL)
	destPath := filepath.Join(cfg.General.CacheDir, name)

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create UTXO file: %w", err)
	}

	fmt.Printf("Downloading UTXO snapshot at height %d (%s)...\n", entry.Height, name)
	if err := rc.FetchSnapshot(ctx, name, f); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("download UTXO: %w", err)
	}
	f.Close()

	fmt.Printf("UTXO snapshot saved to %s\n", destPath)
	fmt.Printf("To load into Bitcoin Core, run:\n  bitcoin-cli loadtxoutset %s\n", destPath)
	return nil
}
