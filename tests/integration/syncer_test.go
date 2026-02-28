//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/daemon"
	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

type mockS3Integ struct {
	mu            sync.Mutex
	uploadedKeys  map[string]bool   // tracks uploaded file keys (body discarded to avoid OOM)
	manifestData  map[string][]byte // stores only small manifest payloads
	calls         []string
	s3Set         map[string]bool
	doneCh        chan struct{}
	doneOnce      sync.Once
}

func newMockS3Integ() *mockS3Integ {
	return &mockS3Integ{
		uploadedKeys: make(map[string]bool),
		manifestData: make(map[string][]byte),
		s3Set:        make(map[string]bool),
		doneCh:       make(chan struct{}),
	}
}

func (m *mockS3Integ) Upload(_ context.Context, key string, body io.Reader, _ int64) error {
	// Discard body to avoid OOM on large sparse files.
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploadedKeys[key] = true
	m.calls = append(m.calls, key)
	return nil
}

func (m *mockS3Integ) UploadManifest(_ context.Context, key string, body io.Reader, _ int64) error {
	content, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifestData[key] = content
	m.uploadedKeys[key] = true
	m.calls = append(m.calls, "manifest:"+key)
	m.doneOnce.Do(func() { close(m.doneCh) })
	return nil
}

func (m *mockS3Integ) List(_ context.Context, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.s3Set {
		keys = append(keys, k)
	}
	return keys, nil
}

func createSparseFile(t *testing.T, dir, name string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(store.FinalizedFileThreshold))
	require.NoError(t, f.Close())
}

func TestSyncerE2EUploadAndManifest(t *testing.T) {
	blocksDir := t.TempDir()

	createSparseFile(t, blocksDir, "blk00000.dat")
	createSparseFile(t, blocksDir, "rev00000.dat")

	s3mock := newMockS3Integ()
	baseURL := "https://cdn.example.com"
	syncer := daemon.NewSyncer(blocksDir, "mainnet", baseURL, s3mock,
		daemon.WithSyncerInterval(1*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go syncer.Run(ctx)

	select {
	case <-s3mock.doneCh:
	case <-time.After(120 * time.Second):
		t.Fatal("syncer did not complete initial sync within timeout")
	}
	cancel()

	s3mock.mu.Lock()
	defer s3mock.mu.Unlock()

	assert.True(t, s3mock.uploadedKeys["blk00000.dat"])
	assert.True(t, s3mock.uploadedKeys["rev00000.dat"])
	assert.True(t, s3mock.uploadedKeys["manifest.json"], "manifest should be uploaded")

	lastCall := s3mock.calls[len(s3mock.calls)-1]
	assert.Equal(t, "manifest:manifest.json", lastCall, "manifest should be last upload")

	var mf manifest.Manifest
	require.NoError(t, json.Unmarshal(s3mock.manifestData["manifest.json"], &mf))
	assert.Equal(t, baseURL, mf.BaseURL)
	assert.Equal(t, "mainnet", mf.Chain)

	fileNames := make(map[string]bool)
	for _, f := range mf.Files {
		fileNames[f.Name] = true
	}
	assert.True(t, fileNames["blk00000.dat"])
	assert.True(t, fileNames["rev00000.dat"])
}

func TestSyncerE2ESkipExisting(t *testing.T) {
	blocksDir := t.TempDir()
	createSparseFile(t, blocksDir, "blk00000.dat")
	createSparseFile(t, blocksDir, "rev00000.dat")

	s3mock := newMockS3Integ()
	s3mock.s3Set["blk00000.dat"] = true
	s3mock.s3Set["rev00000.dat"] = true

	syncer := daemon.NewSyncer(blocksDir, "mainnet", "https://cdn.example.com", s3mock,
		daemon.WithSyncerInterval(1*time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go syncer.Run(ctx)
	<-ctx.Done()

	s3mock.mu.Lock()
	defer s3mock.mu.Unlock()

	assert.False(t, s3mock.uploadedKeys["blk00000.dat"], "should skip existing file")
	assert.False(t, s3mock.uploadedKeys["rev00000.dat"], "should skip existing file")
	assert.Empty(t, s3mock.uploadedKeys, "nothing should be uploaded when all files exist in S3")
}


