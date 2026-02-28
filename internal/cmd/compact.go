package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/cache"
	"github.com/asheswook/bitcoin-lfn/internal/config"
	"github.com/asheswook/bitcoin-lfn/internal/daemon"
	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// CompactCmd implements the `slimnode compact` subcommand.
type CompactCmd struct{}

// Execute runs the compact command.
func (c *CompactCmd) Execute(args []string) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath := filepath.Join(cfg.General.CacheDir, "slimnode.db")
	st, err := store.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	maxBytes := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	ca, err := cache.New(cfg.General.CacheDir, maxBytes, cfg.Cache.MinKeepRecent, st)
	if err != nil {
		return fmt.Errorf("creating cache: %w", err)
	}

	rc := remote.New(cfg.Server.URL, cfg.Server.RequestTimeout, cfg.Server.RetryCount)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	mf, _, err := rc.FetchManifest(ctx, "")
	if err != nil {
		localPath := filepath.Join(cfg.General.CacheDir, "manifest.json")
		mf, err = manifest.ParseFile(localPath)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}
	}

	backupDir := filepath.Join(cfg.General.CacheDir, "backup")
	stateFile := filepath.Join(cfg.General.CacheDir, "compaction-state")
	mfCopy := mf
	compactMgr := daemon.NewCompactionManager(
		rc, st,
		func() *manifest.Manifest { return mfCopy },
		cfg.General.LocalDir,
		cfg.General.CacheDir,
		backupDir,
		stateFile,
		cfg.Compact.Threshold,
	)

	usedBefore, _ := ca.Usage()
	fmt.Printf("Cache before: %d MB\n", usedBefore/1024/1024)

	if err := compactMgr.Compact(ctx); err != nil {
		return fmt.Errorf("compaction failed: %w", err)
	}

	usedAfter, total := ca.Usage()
	fmt.Printf("Cache after:  %d MB / %d MB\n", usedAfter/1024/1024, total/1024/1024)
	fmt.Printf("Reclaimed:    %d MB\n", (usedBefore-usedAfter)/1024/1024)
	return nil
}
