//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/cache"
	"github.com/asheswook/bitcoin-lfn/internal/fusefs"
	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
	"github.com/asheswook/bitcoin-lfn/internal/store"
	"github.com/asheswook/bitcoin-lfn/internal/testutil"
)

// TestFUSE_RemoteRevNoopWrite verifies that REMOTE rev files accept writes
// without creating local files (NullWriteHandle no-op behavior).
func TestFUSE_RemoteRevNoopWrite(t *testing.T) {
	dir := testutil.TempDir(t)
	mountPoint := filepath.Join(dir, "mount")
	localDir := filepath.Join(dir, "local")
	cacheDir := filepath.Join(dir, "cache")
	dbPath := filepath.Join(dir, "test.db")
	indexDir := filepath.Join(dir, "index")

	require.NoError(t, os.MkdirAll(mountPoint, 0755))
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	// Create manifest with rev00000.dat
	mf := &manifest.Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc123def456789abcdef0123456789abcdef0123456",
		ServerID:  "archive-test-01",
		Files: []manifest.ManifestFile{
			{
				Name:        "rev00000.dat",
				Size:        1024,
				SHA256:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				HeightFirst: 0,
				HeightLast:  1023,
				Finalized:   true,
			},
		},
	}

	// Create file data for the test server
	revData := testutil.RandomBytes(t, 1024)
	mf.Files[0].SHA256 = testutil.SHA256Hex(revData)
	fileData := map[string][]byte{
		"rev00000.dat": revData,
	}

	// Start test HTTP server
	srv := testutil.NewTestServer(t, mf, fileData)

	// Create store
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	// Create cache
	maxBytes := int64(10 * 1024 * 1024 * 1024)
	ca, err := cache.New(cacheDir, maxBytes, 0, st)
	require.NoError(t, err)

	// Create remote client
	rc := remote.New(srv.URL, 30*time.Second, 3)

	// Register rev00000.dat as REMOTE in store
	entry := &store.FileEntry{
		Filename:   "rev00000.dat",
		State:      store.FileStateRemote,
		Source:     store.FileSourceServer,
		Size:       1024,
		SHA256:     mf.Files[0].SHA256,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
	require.NoError(t, st.UpsertFile(entry))

	// Create and mount FUSE filesystem
	fs := fusefs.New(mountPoint, localDir, indexDir, st, ca, rc, mf, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- fs.Start(ctx) }()

	time.Sleep(500 * time.Millisecond)

	// Test: Open rev file for writing
	revPath := filepath.Join(mountPoint, "rev00000.dat")
	f, err := os.OpenFile(revPath, os.O_RDWR, 0)
	if err != nil {
		// FUSE might not be available on this system
		t.Skip("FUSE not available or mount failed")
	}
	defer f.Close()

	// Write 512 bytes at offset 0 - should succeed (no-op)
	n, err := f.WriteAt([]byte(testutil.RandomBytes(t, 512)), 0)
	require.NoError(t, err)
	assert.Equal(t, 512, n)

	// Write 512 bytes at offset 512 - should succeed (no-op)
	n, err = f.WriteAt([]byte(testutil.RandomBytes(t, 512)), 512)
	require.NoError(t, err)
	assert.Equal(t, 512, n)

	// Fsync - should succeed
	err = f.Sync()
	require.NoError(t, err)

	f.Close()

	// Verify: no local file created in localDir
	localRevPath := filepath.Join(localDir, "rev00000.dat")
	_, err = os.Stat(localRevPath)
	assert.True(t, os.IsNotExist(err), "local file should not exist after NullWriteHandle writes")

	// Verify: no cached file created in cacheDir
	cachedRevPath := filepath.Join(cacheDir, "rev00000.dat")
	_, err = os.Stat(cachedRevPath)
	assert.True(t, os.IsNotExist(err), "cached file should not exist after NullWriteHandle writes")

	// Verify: store still shows REMOTE state
	storedEntry, err := st.GetFile("rev00000.dat")
	require.NoError(t, err)
	assert.Equal(t, store.FileStateRemote, storedEntry.State)

	// Verify: reading the file returns data from mock server (not written data)
	f, err = os.Open(revPath)
	require.NoError(t, err)
	defer f.Close()

	readData := make([]byte, 1024)
	n, err = f.Read(readData)
	require.NoError(t, err)
	assert.Equal(t, 1024, n)
	assert.Equal(t, revData, readData, "read data should match server data, not written data")

	// Cleanup
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Log("FUSE server did not stop in time")
	}
}

// TestFUSE_ActiveRevStillWritable verifies that ACTIVE rev files remain
// writable and persist data to localDir (not affected by NullWriteHandle).
func TestFUSE_ActiveRevStillWritable(t *testing.T) {
	dir := testutil.TempDir(t)
	mountPoint := filepath.Join(dir, "mount")
	localDir := filepath.Join(dir, "local")
	cacheDir := filepath.Join(dir, "cache")
	dbPath := filepath.Join(dir, "test.db")
	indexDir := filepath.Join(dir, "index")

	require.NoError(t, os.MkdirAll(mountPoint, 0755))
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.MkdirAll(indexDir, 0755))

	// Create manifest (empty, we'll add ACTIVE file locally)
	mf := &manifest.Manifest{
		Version:   1,
		Chain:     "mainnet",
		TipHeight: 884521,
		TipHash:   "0000000000000000000abc123def456789abcdef0123456789abcdef0123456",
		ServerID:  "archive-test-01",
		Files:     []manifest.ManifestFile{},
	}

	// Start test HTTP server
	srv := testutil.NewTestServer(t, mf, map[string][]byte{})

	// Create store
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	// Create cache
	maxBytes := int64(10 * 1024 * 1024 * 1024)
	ca, err := cache.New(cacheDir, maxBytes, 0, st)
	require.NoError(t, err)

	// Create remote client
	rc := remote.New(srv.URL, 30*time.Second, 3)

	// Create local rev file
	localRevPath := filepath.Join(localDir, "rev00500.dat")
	initialData := []byte("initial content")
	require.NoError(t, os.WriteFile(localRevPath, initialData, 0644))

	// Register rev00500.dat as ACTIVE in store
	entry := &store.FileEntry{
		Filename:   "rev00500.dat",
		State:      store.FileStateActive,
		Source:     store.FileSourceLocal,
		Size:       int64(len(initialData)),
		SHA256:     "",
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}
	require.NoError(t, st.UpsertFile(entry))

	// Create and mount FUSE filesystem
	fs := fusefs.New(mountPoint, localDir, indexDir, st, ca, rc, mf, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- fs.Start(ctx) }()

	time.Sleep(500 * time.Millisecond)

	// Test: Open ACTIVE rev file for writing
	revPath := filepath.Join(mountPoint, "rev00500.dat")
	f, err := os.OpenFile(revPath, os.O_RDWR, 0)
	if err != nil {
		// FUSE might not be available on this system
		t.Skip("FUSE not available or mount failed")
	}
	defer f.Close()

	// Write new data - should persist to localDir
	newData := []byte("new written data")
	n, err := f.WriteAt(newData, 0)
	require.NoError(t, err)
	assert.Equal(t, len(newData), n)

	// Fsync
	err = f.Sync()
	require.NoError(t, err)

	f.Close()

	// Verify: local file contains written data
	localData, err := os.ReadFile(localRevPath)
	require.NoError(t, err)
	assert.Equal(t, newData, localData, "local file should contain written data")

	// Cleanup
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Log("FUSE server did not stop in time")
	}
}
