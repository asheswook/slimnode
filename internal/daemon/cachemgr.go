package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/cache"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// CacheStats holds a snapshot of cache manager state.
type CacheStats struct {
	UsedBytes  int64
	TotalBytes int64
	FileCount  int
}

// CacheManager periodically checks cache usage and evicts LRU files when over limit.
type CacheManager struct {
	ca       cache.Cache
	st       store.Store
	maxBytes int64
	interval time.Duration
	logger   *slog.Logger
}

// NewCacheManager creates a CacheManager.
func NewCacheManager(ca cache.Cache, st store.Store, maxBytes int64, interval time.Duration) *CacheManager {
	return &CacheManager{
		ca:       ca,
		st:       st,
		maxBytes: maxBytes,
		interval: interval,
		logger:   slog.Default(),
	}
}

// Run checks cache usage on each tick and evicts files when over limit.
func (m *CacheManager) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.evictIfNeeded(); err != nil {
				m.logger.Error("cache eviction failed", "err", err)
			}
		}
	}
}

func (m *CacheManager) evictIfNeeded() error {
	used, _ := m.ca.Usage()
	if used <= m.maxBytes {
		return nil
	}

	excess := used - m.maxBytes
	count := int(excess/store.MaxBlockFileSize) + 1

	evicted, err := m.ForceEvict(count)
	if err != nil {
		return err
	}

	if len(evicted) > 0 {
		m.logger.Info("evicted cached files", "count", len(evicted), "files", evicted)
	}
	return nil
}

// ForceEvict evicts up to count LRU cached files. Returns evicted filenames.
func (m *CacheManager) ForceEvict(count int) ([]string, error) {
	entries, err := m.st.ListCachedByLRU(count)
	if err != nil {
		return nil, err
	}

	var evicted []string
	for _, e := range entries {
		if err := m.ca.Remove(e.Filename); err != nil {
			m.logger.Error("failed to evict file", "file", e.Filename, "err", err)
			continue
		}
		evicted = append(evicted, e.Filename)
	}
	return evicted, nil
}

// Stats returns current cache statistics.
func (m *CacheManager) Stats() CacheStats {
	used, total := m.ca.Usage()
	entries, _ := m.st.ListByState(store.FileStateCached)
	return CacheStats{
		UsedBytes:  used,
		TotalBytes: total,
		FileCount:  len(entries),
	}
}
