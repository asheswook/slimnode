package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
)

type mockRC struct {
	manifest    *manifest.Manifest
	manifestErr error
}

func (m *mockRC) FetchManifest(_ context.Context, etag string) (*manifest.Manifest, string, error) {
	return m.manifest, etag, m.manifestErr
}

func (m *mockRC) FetchFile(_ context.Context, _ string, _ io.Writer) error { return nil }
func (m *mockRC) HealthCheck(_ context.Context) error                       { return nil }
func (m *mockRC) FetchBlockmap(_ context.Context, _ string) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}
func (m *mockRC) FetchBlock(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}
func (m *mockRC) FetchSnapshot(_ context.Context, _ string, _ io.Writer) error { return nil }

func writeManifestFile(t *testing.T, path string, mf *manifest.Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(mf, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}

func TestLoadInitialManifest_LocalCacheHit(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "manifest.json")

	expected := &manifest.Manifest{Chain: "mainnet"}
	writeManifestFile(t, localPath, expected)

	rc := &mockRC{manifestErr: assert.AnError}
	got, err := loadInitialManifest(t.Context(), rc, dir)
	require.NoError(t, err)
	assert.Equal(t, expected.Chain, got.Chain)
}

func TestLoadInitialManifest_FetchFromServer(t *testing.T) {
	dir := t.TempDir()
	serverMf := &manifest.Manifest{Chain: "testnet"}
	rc := &mockRC{manifest: serverMf}
	got, err := loadInitialManifest(t.Context(), rc, dir)
	require.NoError(t, err)
	assert.Equal(t, "testnet", got.Chain)

	localPath := filepath.Join(dir, "manifest.json")
	_, statErr := os.Stat(localPath)
	assert.NoError(t, statErr, "manifest should be saved locally after fetch")
}

func TestLoadInitialManifest_ServerReturnsNil(t *testing.T) {
	dir := t.TempDir()
	rc := &mockRC{manifest: nil}
	got, err := loadInitialManifest(t.Context(), rc, dir)
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestLoadInitialManifest_ServerError(t *testing.T) {
	dir := t.TempDir()
	rc := &mockRC{manifestErr: assert.AnError}
	_, err := loadInitialManifest(t.Context(), rc, dir)
	assert.Error(t, err)
}
