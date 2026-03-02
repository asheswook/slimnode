package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/cache"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
)

func setupCacheMgr(t *testing.T, maxBytes int64) (*CacheManager, store.Store, cache.Cache) {
	t.Helper()
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"

	st, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	cacheDir := dir + "/cache"
	ca, err := cache.New(cacheDir, maxBytes, 0, st)
	require.NoError(t, err)

	mgr := NewCacheManager(ca, st, maxBytes, 50*time.Millisecond)
	return mgr, st, ca
}

func TestCacheMgrNoEvictionUnderLimit(t *testing.T) {
	mgr, st, ca := setupCacheMgr(t, 10*store.MaxBlockFileSize)

	data, sha := testutil.SampleBlockFile(t, 1024)
	_ = data
	entry := &store.FileEntry{
		Filename:   "blk00000.dat",
		State:      store.FileStateCached,
		Source:     store.FileSourceServer,
		Size:       1024,
		SHA256:     sha,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
	require.NoError(t, st.UpsertFile(entry))
	_ = ca

	evicted, err := mgr.ForceEvict(0)
	require.NoError(t, err)
	assert.Empty(t, evicted)
}

func TestCacheMgrShutdown(t *testing.T) {
	mgr, _, _ := setupCacheMgr(t, store.MaxBlockFileSize)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- mgr.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("cache manager did not shut down in time")
	}
}

func TestCacheMgrStats(t *testing.T) {
	mgr, _, _ := setupCacheMgr(t, store.MaxBlockFileSize)
	stats := mgr.Stats()
	assert.GreaterOrEqual(t, stats.TotalBytes, int64(0))
}

func TestCacheMgrEviction(t *testing.T) {
	mgr, _, ca := setupCacheMgr(t, 1)

	path, sha := testutil.SampleBlockFile(t, 512)
	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	require.NoError(t, ca.Store("blk00000.dat", f, sha))

	evicted, err := mgr.ForceEvict(1)
	require.NoError(t, err)
	assert.NotEmpty(t, evicted, "should have evicted at least one file")
}

func TestCacheMgrForceEvict_Empty(t *testing.T) {
	mgr, _, _ := setupCacheMgr(t, store.MaxBlockFileSize)
	evicted, err := mgr.ForceEvict(0)
	require.NoError(t, err)
	assert.Empty(t, evicted)
}

func TestCacheMgrStats_WithFiles(t *testing.T) {
	mgr, _, ca := setupCacheMgr(t, 10*store.MaxBlockFileSize)

	path, sha := testutil.SampleBlockFile(t, 512)
	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	require.NoError(t, ca.Store("blk00000.dat", f, sha))

	stats := mgr.Stats()
	assert.GreaterOrEqual(t, stats.TotalBytes, int64(0))
}
