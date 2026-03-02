package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/server"
)

// Syncer periodically scans for new finalized block files and uploads them to S3,
// then regenerates and uploads the manifest.
type Syncer struct {
	blocksDir    string
	chain        string
	baseURL      string
	blockmapDir  string
	manifestPath string
	s3           S3Uploader
	scanInterval time.Duration
	logger       *slog.Logger

	// lastManifest is the most recently generated manifest, used as a hash
	// cache on the next sync cycle. Finalized block files are immutable, so
	// SHA-256 hashes remain valid as long as name and size match.
	// Accessed only from the sync() goroutine — no mutex needed.
	lastManifest *manifest.Manifest
}

// SyncerOption configures a Syncer.
type SyncerOption func(*Syncer)

// WithSyncerBlockmapDir sets the directory containing pre-generated blockmap files.
// When set, the syncer uploads the corresponding .blockmap file alongside each new blk file.
func WithSyncerBlockmapDir(dir string) SyncerOption {
	return func(s *Syncer) { s.blockmapDir = dir }
}

// WithSyncerManifestPath sets a local file path where the manifest is written
// before uploading to S3. Useful for serving the manifest locally as well.
func WithSyncerManifestPath(path string) SyncerOption {
	return func(s *Syncer) { s.manifestPath = path }
}

// WithSyncerInterval sets the interval between sync runs.
func WithSyncerInterval(d time.Duration) SyncerOption {
	return func(s *Syncer) { s.scanInterval = d }
}

// NewSyncer creates a Syncer that uploads finalized block files from blocksDir to S3.
func NewSyncer(blocksDir, chain, baseURL string, s3 S3Uploader, opts ...SyncerOption) *Syncer {
	s := &Syncer{
		blocksDir:    blocksDir,
		chain:        chain,
		baseURL:      baseURL,
		s3:           s3,
		scanInterval: 10 * time.Minute,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run starts the sync loop. It performs an initial sync immediately, then repeats
// on each tick until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) error {
	if err := s.sync(ctx); err != nil {
		s.logger.Error("syncer: initial sync failed", "err", err)
	}

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.sync(ctx); err != nil {
				s.logger.Error("syncer: sync failed", "err", err)
			}
		}
	}
}

func (s *Syncer) sync(ctx context.Context) error {
	localFiles, err := server.ScanFinalizedFiles(s.blocksDir)
	if err != nil {
		return fmt.Errorf("syncer: scan: %w", err)
	}

	s3Keys, err := s.s3.List(ctx, "")
	if err != nil {
		return fmt.Errorf("syncer: list s3: %w", err)
	}
	s3Set := make(map[string]bool, len(s3Keys))
	for _, k := range s3Keys {
		s3Set[k] = true
	}

	var newFiles []server.ScannedFile
	for _, f := range localFiles {
		if !s3Set[f.Name] {
			newFiles = append(newFiles, f)
		}
	}
	if len(newFiles) == 0 {
		return nil
	}

	uploadFailed := false
	for _, f := range newFiles {
		if err := s.uploadFile(ctx, f); err != nil {
			s.logger.Error("syncer: upload file", "file", f.Name, "err", err)
			uploadFailed = true
			continue
		}
		if s.blockmapDir != "" && isBlkFile(f.Name) {
			if err := s.uploadBlockmap(ctx, f.Name); err != nil {
				s.logger.Error("syncer: upload blockmap", "file", f.Name, "err", err)
				uploadFailed = true
			}
		}
	}

	if uploadFailed {
		return nil
	}

	return s.uploadManifest(ctx)
}

func (s *Syncer) uploadFile(ctx context.Context, f server.ScannedFile) error {
	file, err := os.Open(f.Path)
	if err != nil {
		return fmt.Errorf("syncer: open: %w", err)
	}
	defer file.Close()
	return s.s3.Upload(ctx, f.Name, file, f.Size)
}

func (s *Syncer) uploadBlockmap(ctx context.Context, blkName string) error {
	bmPath := filepath.Join(s.blockmapDir, blkName+".blockmap")
	bmFile, err := os.Open(bmPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Warn("syncer: blockmap not found, skipping", "file", blkName)
			return nil
		}
		return fmt.Errorf("syncer: open blockmap: %w", err)
	}
	defer bmFile.Close()

	info, err := bmFile.Stat()
	if err != nil {
		return fmt.Errorf("syncer: stat blockmap: %w", err)
	}

	return s.s3.Upload(ctx, blkName+".blockmap", bmFile, info.Size())
}

func (s *Syncer) uploadManifest(ctx context.Context) error {
	manifestOpts := []server.ManifestOption{server.WithBaseURL(s.baseURL)}
	if s.lastManifest != nil {
		manifestOpts = append(manifestOpts, server.WithPreviousManifest(s.lastManifest))
	}
	if s.blockmapDir != "" {
		manifestOpts = append(manifestOpts, server.WithBlockmapDir(s.blockmapDir))
	}

	m, err := server.GenerateManifest(s.blocksDir, s.chain, manifestOpts...)
	if err != nil {
		return fmt.Errorf("syncer: generate manifest: %w", err)
	}

	s.lastManifest = m

	if s.manifestPath != "" {
		if err := manifest.WriteFile(s.manifestPath, m); err != nil {
			s.logger.Warn("syncer: write local manifest", "path", s.manifestPath, "err", err)
		}
	}

	var buf bytes.Buffer
	if err := manifest.Write(&buf, m); err != nil {
		return fmt.Errorf("syncer: encode manifest: %w", err)
	}

	if err := s.s3.UploadManifest(ctx, "manifest.json", &buf, int64(buf.Len())); err != nil {
		return fmt.Errorf("syncer: upload manifest: %w", err)
	}

	s.logger.Info("syncer: sync complete")
	return nil
}

func isBlkFile(name string) bool {
	return strings.HasPrefix(name, "blk") && strings.HasSuffix(name, ".dat")
}
