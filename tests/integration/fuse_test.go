//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/cache"
	"github.com/asheswook/bitcoin-slimnode/internal/fusefs"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
)

func setupFUSE(t *testing.T) (mountPoint string, cleanup func()) {
	t.Helper()

	dir := testutil.TempDir(t)
	mountPoint = filepath.Join(dir, "mount")
	localDir := filepath.Join(dir, "local")
	cacheDir := filepath.Join(dir, "cache")
	dbPath := filepath.Join(dir, "test.db")

	indexDir := filepath.Join(dir, "index")

	require.NoError(t, os.MkdirAll(mountPoint, 0755))
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	mf := testutil.SampleManifest()
	fileData := make(map[string][]byte)
	for i := range mf.Files {
		data := testutil.RandomBytes(t, int(mf.Files[i].Size))
		fileData[mf.Files[i].Name] = data
		mf.Files[i].SHA256 = testutil.SHA256Hex(data)
	}

	srv := testutil.NewTestServer(t, mf, fileData)

	st, err := store.New(dbPath)
	require.NoError(t, err)

	maxBytes := int64(10 * 1024 * 1024 * 1024)
	ca, err := cache.New(cacheDir, maxBytes, 0, st)
	require.NoError(t, err)

	rc := remote.New(srv.URL, 30*time.Second, 3)

	for _, f := range mf.Files {
		entry := &store.FileEntry{
			Filename:   f.Name,
			State:      store.FileStateRemote,
			Source:     store.FileSourceServer,
			Size:       f.Size,
			SHA256:     f.SHA256,
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
		}
		require.NoError(t, st.UpsertFile(entry))
	}

	fs := fusefs.New(mountPoint, localDir, indexDir, st, ca, rc, mf, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- fs.Start(ctx) }()

	time.Sleep(500 * time.Millisecond)

	cleanup = func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Log("FUSE server did not stop in time")
		}
		st.Close()
	}

	return mountPoint, cleanup
}

func TestReaddir(t *testing.T) {
	mountPoint, cleanup := setupFUSE(t)
	defer cleanup()

	entries, err := os.ReadDir(mountPoint)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestReadRemoteFile(t *testing.T) {
	mountPoint, cleanup := setupFUSE(t)
	defer cleanup()

	mf := testutil.SampleManifest()
	if len(mf.Files) == 0 {
		t.Skip("no files in sample manifest")
	}

	path := filepath.Join(mountPoint, mf.Files[0].Name)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestWriteToReadOnlyFile(t *testing.T) {
	mountPoint, cleanup := setupFUSE(t)
	defer cleanup()

	mf := testutil.SampleManifest()
	if len(mf.Files) == 0 {
		t.Skip("no files in sample manifest")
	}

	path := filepath.Join(mountPoint, mf.Files[0].Name)
	err := os.WriteFile(path, []byte("test"), 0644)
	assert.Error(t, err)
}

func TestCreateNewFile(t *testing.T) {
	mountPoint, cleanup := setupFUSE(t)
	defer cleanup()

	path := filepath.Join(mountPoint, "blk09999.dat")
	err := os.WriteFile(path, []byte("new block data"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("new block data"), data)
}

func TestConcurrentReads(t *testing.T) {
	mountPoint, cleanup := setupFUSE(t)
	defer cleanup()

	mf := testutil.SampleManifest()
	if len(mf.Files) == 0 {
		t.Skip("no files in sample manifest")
	}

	path := filepath.Join(mountPoint, mf.Files[0].Name)

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = os.ReadFile(path)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err)
	}
}
