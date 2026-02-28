package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/store"
)

type mockS3 struct {
	mu      sync.Mutex
	calls   []string
	listed  []string
	failKey string
}

func (m *mockS3) Upload(_ context.Context, key string, _ io.Reader, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key == m.failKey {
		return errors.New("mock upload error")
	}
	m.calls = append(m.calls, key)
	return nil
}

func (m *mockS3) UploadManifest(_ context.Context, key string, _ io.Reader, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "manifest:"+key)
	return nil
}

func (m *mockS3) List(_ context.Context, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listed, nil
}

func (m *mockS3) snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.calls))
	copy(result, m.calls)
	return result
}

func newBlocksDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	blkPath := filepath.Join(dir, "blk00000.dat")
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(store.FinalizedFileThreshold))
	f.Close()

	revPath := filepath.Join(dir, "rev00000.dat")
	require.NoError(t, os.WriteFile(revPath, []byte("rev"), 0644))

	return dir
}

func TestSyncerUpload(t *testing.T) {
	blocksDir := newBlocksDir(t)
	mock := &mockS3{}

	s := NewSyncer(blocksDir, "mainnet", "http://example.com", mock)
	err := s.sync(context.Background())
	require.NoError(t, err)

	calls := mock.snapshot()
	require.NotEmpty(t, calls)
	assert.Contains(t, calls, "blk00000.dat")
	assert.Contains(t, calls, "rev00000.dat")
	assert.Contains(t, calls, "manifest:manifest.json")
	assert.Equal(t, "manifest:manifest.json", calls[len(calls)-1])
}

func TestSyncerSkipExisting(t *testing.T) {
	blocksDir := newBlocksDir(t)
	mock := &mockS3{
		listed: []string{"blk00000.dat", "rev00000.dat"},
	}

	s := NewSyncer(blocksDir, "mainnet", "http://example.com", mock)
	err := s.sync(context.Background())
	require.NoError(t, err)

	assert.Empty(t, mock.snapshot())
}

func TestSyncerUploadFailure(t *testing.T) {
	blocksDir := newBlocksDir(t)
	mock := &mockS3{failKey: "blk00000.dat"}

	s := NewSyncer(blocksDir, "mainnet", "http://example.com", mock)
	err := s.sync(context.Background())
	require.NoError(t, err)

	calls := mock.snapshot()
	assert.NotContains(t, calls, "manifest:manifest.json")
}

func TestSyncerGracefulShutdown(t *testing.T) {
	blocksDir := t.TempDir()
	mock := &mockS3{}

	s := NewSyncer(blocksDir, "mainnet", "http://example.com", mock,
		WithSyncerInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("syncer did not shut down in time")
	}
}
