package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// ManifestPoller periodically fetches the server manifest and registers new files.
type ManifestPoller struct {
	rc       ManifestFetcher
	st       store.Store
	mu       sync.RWMutex
	current  *manifest.Manifest
	etag     string
	interval time.Duration
	logger   *slog.Logger
}

// NewManifestPoller creates a ManifestPoller.
func NewManifestPoller(rc ManifestFetcher, st store.Store, initial *manifest.Manifest, interval time.Duration) *ManifestPoller {
	return &ManifestPoller{
		rc:       rc,
		st:       st,
		current:  initial,
		interval: interval,
		logger:   slog.Default(),
	}
}

// Run polls the server manifest on each tick until ctx is cancelled.
func (p *ManifestPoller) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.logger.Error("manifest poll failed", "err", err)
			}
		}
	}
}

func (p *ManifestPoller) poll(ctx context.Context) error {
	p.mu.RLock()
	etag := p.etag
	old := p.current
	p.mu.RUnlock()

	newManifest, newEtag, err := p.rc.FetchManifest(ctx, etag)
	if err != nil {
		return err
	}
	if newManifest == nil {
		return nil
	}

	diff := manifest.Diff(old, newManifest)

	for _, f := range diff.Added {
		entry := &store.FileEntry{
			Filename:   f.Name,
			State:      store.FileStateRemote,
			Source:     store.FileSourceServer,
			Size:       f.Size,
			SHA256:     f.SHA256,
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
		}
		if err := p.st.UpsertFile(entry); err != nil {
			p.logger.Error("failed to register new remote file", "file", f.Name, "err", err)
		}
	}

	for _, f := range diff.Removed {
		p.logger.Warn("server removed file from manifest", "file", f.Name)
		_ = p.st.DeleteFile(f.Name)
	}

	p.mu.Lock()
	p.current = newManifest
	p.etag = newEtag
	p.mu.Unlock()

	return nil
}

// CurrentManifest returns the most recently fetched manifest.
func (p *ManifestPoller) CurrentManifest() *manifest.Manifest {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// SetInterval changes the polling interval at runtime.
func (p *ManifestPoller) SetInterval(d time.Duration) {
	p.mu.Lock()
	p.interval = d
	p.mu.Unlock()
}
