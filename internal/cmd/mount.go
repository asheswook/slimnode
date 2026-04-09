package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/asheswook/bitcoin-slimnode/internal/cache"
	"github.com/asheswook/bitcoin-slimnode/internal/config"
	"github.com/asheswook/bitcoin-slimnode/internal/daemon"
	"github.com/asheswook/bitcoin-slimnode/internal/fusefs"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// MountCmd implements the `slimnode mount` subcommand.
type MountCmd struct {
	Background bool `long:"background" short:"b" description:"Run as background daemon"`
}

// Execute runs the mount command.
func (m *MountCmd) Execute(args []string) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	baseDir := filepath.Dir(cfg.ConfigFile)
	pidPath := filepath.Join(baseDir, "slimnode.pid")
	logPath := filepath.Join(baseDir, "slimnode.log")

	if m.Background && !isDaemonChild() {
		return daemonize(pidPath, logPath)
	}

	if err := os.MkdirAll(cfg.General.CacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	if err := os.MkdirAll(cfg.General.LocalDir, 0755); err != nil {
		return fmt.Errorf("creating local dir: %w", err)
	}

	dbPath := filepath.Join(cfg.General.CacheDir, "slimnode.db")
	st, err := store.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}

	maxBytes := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	ca, err := cache.New(cfg.General.CacheDir, maxBytes, cfg.Cache.MinKeepRecent, st)
	if err != nil {
		return fmt.Errorf("creating cache: %w", err)
	}

	rc := remote.New(cfg.Server.URL, cfg.Server.RequestTimeout, cfg.Server.RetryCount)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mf, err := loadInitialManifest(ctx, rc, cfg.General.CacheDir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	localFiles, err := fusefs.ScanLocalFiles(cfg.General.LocalDir)
	if err != nil {
		return fmt.Errorf("scanning local files: %w", err)
	}
	for i := range localFiles {
		existing, _ := st.GetFile(localFiles[i].Filename)
		if existing != nil && (existing.State == store.FileStateRemote || existing.State == store.FileStateCached) {
			// Don't downgrade a known remote/cached file to ACTIVE based on a local scan.
			// This prevents stale local artifacts from blocking remote fetching after a crash.
			slog.Debug("skipping local scan override for remote/cached file", "file", localFiles[i].Filename)
			continue
		}
		_ = st.UpsertFile(&localFiles[i])
	}

	// Clean up orphaned ACTIVE entries: if a file is ACTIVE in the store but no
	// longer exists locally, it was lost during a crash. Remove the entry so the
	// manifest loop below can re-register it as REMOTE.
	activeEntries, _ := st.ListByState(store.FileStateActive)
	for _, e := range activeEntries {
		if e.Filename == ".lock" {
			continue
		}
		localPath := filepath.Join(cfg.General.LocalDir, e.Filename)
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			slog.Warn("removing orphaned ACTIVE entry", "file", e.Filename)
			_ = st.DeleteFile(e.Filename)
		}
	}

	for _, f := range mf.Files {
		existing, _ := st.GetFile(f.Name)
		if existing == nil {
			entry := &store.FileEntry{
				Filename:   f.Name,
				State:      store.FileStateRemote,
				Source:     store.FileSourceServer,
				Size:       f.Size,
				SHA256:     f.SHA256,
				CreatedAt:  time.Now(),
				LastAccess: time.Now(),
			}
			_ = st.UpsertFile(entry)
		}
	}

	indexDir := filepath.Join(cfg.General.LocalDir, "index")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return fmt.Errorf("creating index dir: %w", err)
	}

	var fileClient fusefs.RemoteClient
	if mf.BaseURL != "" {
		fileClient = remote.NewCDN(mf.BaseURL, cfg.Server.RequestTimeout, cfg.Server.RetryCount)
		slog.Info("CDN mode enabled", "base_url", mf.BaseURL)
	} else {
		fileClient = rc
	}

	fetchPolicy := fusefs.NewFetchPolicy(fusefs.FetchPolicyConfig{
		Mode:                  cfg.General.RemoteFetchMode,
		AutoGapToleranceKB:    cfg.General.AutoGapToleranceKB,
		AutoMinRangeRequests:  cfg.General.AutoMinRangeRequests,
		AutoMinSequentialMB:   cfg.General.AutoMinSequentialMB,
		AutoMinSequentialRate: cfg.General.AutoMinSequentialRate,
		AutoMaxBackwardSeeks:  cfg.General.AutoMaxBackwardSeeks,
		AutoFileHintTTL:       cfg.General.AutoFileHintTTL,
		AutoPromotionCooldown: cfg.General.AutoPromotionCooldown,
	})

	fs := fusefs.New(cfg.General.MountPoint, cfg.General.LocalDir, indexDir, st, ca, fileClient, mf, nil, nil, fetchPolicy)

	poller := daemon.NewManifestPoller(rc, st, mf, 10*time.Minute)
	cacheMgr := daemon.NewCacheManager(ca, st, maxBytes, 30*time.Second)

	backupDir := filepath.Join(cfg.General.CacheDir, "backup")
	stateFile := filepath.Join(cfg.General.CacheDir, "compaction-state")
	compactMgr := daemon.NewCompactionManager(
		rc, st,
		poller.CurrentManifest,
		cfg.General.LocalDir,
		cfg.General.CacheDir,
		backupDir,
		stateFile,
		cfg.Compact.Threshold,
	)

	if err := compactMgr.RecoverFromCrash(); err != nil {
		slog.Warn("crash recovery failed", "err", err)
	}

	slog.Info("slimnode started",
		"mount", cfg.General.MountPoint,
		"server", cfg.Server.URL,
		"manifest_files", len(mf.Files),
		"local_files", len(localFiles),
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return fs.Start(gctx) })
	g.Go(func() error { return poller.Run(gctx) })
	g.Go(func() error { return cacheMgr.Run(gctx) })
	g.Go(func() error {
		trigger := daemon.TriggerAuto
		if cfg.Compact.Trigger == "manual" {
			return nil
		}
		return compactMgr.Run(gctx, trigger)
	})
	g.Go(func() error {
		<-gctx.Done()
		slog.Info("shutting down slimnode")
		_ = fs.Stop()
		_ = st.Close()
		removePID(pidPath)
		return nil
	})

	return g.Wait()
}

func loadInitialManifest(ctx context.Context, rc manifestFetcher, cacheDir string) (*manifest.Manifest, error) {
	localPath := filepath.Join(cacheDir, "manifest.json")
	if mf, err := manifest.ParseFile(localPath); err == nil {
		return mf, nil
	}

	mf, _, err := rc.FetchManifest(ctx, "")
	if err != nil {
		return nil, err
	}
	if mf == nil {
		return &manifest.Manifest{}, nil
	}

	_ = manifest.WriteFile(localPath, mf)
	return mf, nil
}
