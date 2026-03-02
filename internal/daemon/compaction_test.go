package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
)

func setupCompaction(t *testing.T) (*CompactionManager, store.Store, string) {
	t.Helper()
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"
	localDir := dir + "/local"
	cacheDir := dir + "/cache"
	backupDir := dir + "/backup"
	stateFile := dir + "/compaction-state"

	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	st, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	mf := testutil.SampleManifest()
	mf.Snapshots = manifest.Snapshots{
		LatestHeight: 800000,
		BlocksIndex: manifest.SnapshotEntry{
			Height: 800000,
			URL:    "http://example.com/snapshot.tar.zst",
			SHA256: "abc123",
			Size:   1024,
		},
	}

	mgr := NewCompactionManager(
		&noopRemoteClient{},
		st,
		func() *manifest.Manifest { return mf },
		localDir, cacheDir, backupDir, stateFile,
		85,
	)
	return mgr, st, localDir
}

func TestCompactNoSnapshot(t *testing.T) {
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	mgr := NewCompactionManager(
		&noopRemoteClient{},
		st,
		func() *manifest.Manifest {
			return &manifest.Manifest{}
		},
		dir, dir, dir, dir+"/state",
		85,
	)

	err = mgr.Compact(context.Background())
	assert.Error(t, err)
}

func TestCompactNoFiles(t *testing.T) {
	mgr, _, _ := setupCompaction(t)
	err := mgr.Compact(context.Background())
	assert.NoError(t, err)
}

func TestCompactRemovesFiles(t *testing.T) {
	mgr, st, localDir := setupCompaction(t)

	for i := 0; i < 3; i++ {
		name := filepath.Join(localDir, "blk0000"+string(rune('0'+i))+".dat")
		require.NoError(t, os.WriteFile(name, []byte("data"), 0644))

		entry := &store.FileEntry{
			Filename:    "blk0000" + string(rune('0'+i)) + ".dat",
			State:       store.FileStateLocalFinalized,
			Source:      store.FileSourceLocal,
			Size:        store.MaxBlockFileSize,
			CreatedAt:   time.Now(),
			LastAccess:  time.Now(),
			HeightFirst: int64(i * 100000),
			HeightLast:  int64((i+1)*100000 - 1),
		}
		require.NoError(t, st.UpsertFile(entry))
	}

	err := mgr.Compact(context.Background())
	require.NoError(t, err)

	entries, err := st.ListByState(store.FileStateRemote)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

func TestCompactCrashRecovery_InProgress(t *testing.T) {
	mgr, _, _ := setupCompaction(t)

	require.NoError(t, os.WriteFile(mgr.stateFile, []byte(stateInProgress), 0644))

	err := mgr.RecoverFromCrash()
	assert.NoError(t, err)

	_, err = os.Stat(mgr.stateFile)
	assert.True(t, os.IsNotExist(err))
}

func TestCompactCrashRecovery_Completed(t *testing.T) {
	mgr, _, _ := setupCompaction(t)

	require.NoError(t, os.WriteFile(mgr.stateFile, []byte(stateCompleted), 0644))

	err := mgr.RecoverFromCrash()
	assert.NoError(t, err)

	_, err = os.Stat(mgr.stateFile)
	assert.True(t, os.IsNotExist(err))
}

func TestCompactCrashRecovery_NoStateFile(t *testing.T) {
	mgr, _, _ := setupCompaction(t)
	err := mgr.RecoverFromCrash()
	assert.NoError(t, err)
}

type noopRemoteClient struct{}

func (c *noopRemoteClient) FetchFile(ctx context.Context, filename string, dest io.Writer) error {
	return nil
}

func (c *noopRemoteClient) FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error) {
	return nil, etag, nil
}

func (c *noopRemoteClient) HealthCheck(ctx context.Context) error {
	return nil
}

func (c *noopRemoteClient) FetchBlockmap(ctx context.Context, filename string) ([]byte, error) {
	return nil, nil
}

func (c *noopRemoteClient) FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error) {
	return nil, nil
}

func (c *noopRemoteClient) FetchSnapshot(ctx context.Context, name string, dest io.Writer) error {
	return nil
}
