package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

func startTestFileServer(t *testing.T, blocksDir, manifestPath, blockmapDir, snapshotDir string) (string, context.CancelFunc) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewFileServer(blocksDir, manifestPath, addr, blockmapDir, snapshotDir)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		<-errCh
	})

	return "http://" + addr, cancel
}

func writeTestManifest(t *testing.T, dir string, m *manifest.Manifest) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
	return path
}

func TestFileServer_Health(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1, Chain: "testnet"})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestFileServer_Manifest(t *testing.T) {
	m := &manifest.Manifest{
		Version: 1,
		Chain:   "mainnet",
		Files: []manifest.ManifestFile{
			{Name: "blk00000.dat", Size: 1024, SHA256: "abc", Finalized: true},
		},
	}
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, m)
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/manifest")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("ETag"))
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var got manifest.Manifest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "mainnet", got.Chain)
	require.Len(t, got.Files, 1)
	assert.Equal(t, "blk00000.dat", got.Files[0].Name)
}

func TestFileServer_ManifestETag(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp1, err := http.Get(base + "/v1/manifest")
	require.NoError(t, err)
	etag := resp1.Header.Get("ETag")
	resp1.Body.Close()
	require.NotEmpty(t, etag)

	req, _ := http.NewRequest(http.MethodGet, base+"/v1/manifest", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, http.StatusNotModified, resp2.StatusCode)
}

func TestFileServer_File(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world block data")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blk00000.dat"), content, 0644))

	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/file/blk00000.dat")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, body)

	h := sha256.Sum256(content)
	assert.Equal(t, hex.EncodeToString(h[:]), resp.Header.Get("X-SHA256"))
}

func TestFileServer_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/file/nonexistent.dat")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileServer_FileRange(t *testing.T) {
	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blk00001.dat"), content, 0644))

	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	req, _ := http.NewRequest(http.MethodGet, base+"/v1/file/blk00001.dat", nil)
	req.Header.Set("Range", "bytes=4-7")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusPartialContent, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "4567", string(body))
	assert.Equal(t, strconv.Itoa(len(body)), resp.Header.Get("Content-Length"))
}

func TestFileServer_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/file/..%2F..%2Fetc%2Fpasswd")
	require.NoError(t, err)
	resp.Body.Close()

	assert.NotEqual(t, http.StatusOK, resp.StatusCode)
}

func TestFileServer_Shutdown(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	_, cancel := startTestFileServer(t, dir, mp, "", "")

	cancel()
}

func TestFileServer_BlockmapServe(t *testing.T) {
	dir := t.TempDir()
	blockmapDir := t.TempDir()
	content := []byte("blockmap binary data")
	require.NoError(t, os.WriteFile(filepath.Join(blockmapDir, "blk00100.dat.blockmap"), content, 0644))

	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, blockmapDir, "")

	resp, err := http.Get(base + "/v1/blockmap/blk00100.dat")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, body)
}

func TestFileServer_BlockmapNotFound(t *testing.T) {
	dir := t.TempDir()
	blockmapDir := t.TempDir()

	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, blockmapDir, "")

	resp, err := http.Get(base + "/v1/blockmap/blk99999.dat")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileServer_BlockmapPathTraversal(t *testing.T) {
	dir := t.TempDir()
	blockmapDir := t.TempDir()

	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, blockmapDir, "")

	resp, err := http.Get(base + "/v1/blockmap/..%2F..%2Fetc%2Fpasswd")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFileServer_BlockmapDisabled(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/blockmap/blk00100.dat")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileServer_Snapshot(t *testing.T) {
	dir := t.TempDir()
	snapshotDir := t.TempDir()
	content := []byte("snapshot archive data")
	require.NoError(t, os.WriteFile(filepath.Join(snapshotDir, "blocks-index-880000.tar.zst"), content, 0644))

	h := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(h[:])

	mp := writeTestManifest(t, dir, &manifest.Manifest{
		Version: 1,
		Snapshots: manifest.Snapshots{
			BlocksIndex: manifest.SnapshotEntry{
				URL:    "/v1/snapshot/blocks-index-880000.tar.zst",
				SHA256: expectedHash,
			},
		},
	})
	base, _ := startTestFileServer(t, dir, mp, "", snapshotDir)

	resp, err := http.Get(base + "/v1/snapshot/blocks-index-880000.tar.zst")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, body)

	assert.Equal(t, expectedHash, resp.Header.Get("X-SHA256"))
}

func TestFileServer_SnapshotNotFound(t *testing.T) {
	dir := t.TempDir()
	snapshotDir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", snapshotDir)

	resp, err := http.Get(base + "/v1/snapshot/nonexistent.tar.zst")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileServer_SnapshotPathTraversal(t *testing.T) {
	dir := t.TempDir()
	snapshotDir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", snapshotDir)

	resp, err := http.Get(base + "/v1/snapshot/..%2F..%2Fetc%2Fpasswd")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFileServer_SnapshotDisabled(t *testing.T) {
	dir := t.TempDir()
	mp := writeTestManifest(t, dir, &manifest.Manifest{Version: 1})
	base, _ := startTestFileServer(t, dir, mp, "", "")

	resp, err := http.Get(base + "/v1/snapshot/blocks-index-880000.tar.zst")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAutoReload(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	writeTestManifest(t, dir, &manifest.Manifest{Version: 1, Chain: "mainnet"})

	srv := NewFileServer(dir, manifestPath, "127.0.0.1:0", "", "",
		WithChain("mainnet"),
		WithScanInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.ListenAndServe(ctx) }()

	time.Sleep(20 * time.Millisecond)

	blkPath := filepath.Join(dir, "blk00000.dat")
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Truncate(blkPath, store.FinalizedFileThreshold))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.RLock()
		data := srv.manifestJSON
		srv.mu.RUnlock()

		var got manifest.Manifest
		if json.Unmarshal(data, &got) == nil && len(got.Files) == 1 {
			assert.Equal(t, "blk00000.dat", got.Files[0].Name)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("manifest not reloaded within timeout")
}

func startTestFileServerWithOpts(t *testing.T, blocksDir, manifestPath, blockmapDir, snapshotDir string, opts ...FileServerOption) (string, context.CancelFunc) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewFileServer(blocksDir, manifestPath, addr, blockmapDir, snapshotDir, opts...)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		<-errCh
	})

	return "http://" + addr, cancel
}

func TestAutoReloadShutdown(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	writeTestManifest(t, dir, &manifest.Manifest{Version: 1, Chain: "mainnet"})

	srv := NewFileServer(dir, manifestPath, "127.0.0.1:0", "", "",
		WithScanInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestServeManifestEndpoint(t *testing.T) {
	dir := t.TempDir()

	blkPath := filepath.Join(dir, "blk00000.dat")
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Truncate(blkPath, store.FinalizedFileThreshold))

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	manifestPath := filepath.Join(dir, "manifest.json")
	require.NoError(t, manifest.WriteFile(manifestPath, m))

	base, _ := startTestFileServer(t, dir, manifestPath, "", "")

	resp, err := http.Get(base + "/v1/manifest")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	etag := resp.Header.Get("ETag")
	require.NotEmpty(t, etag)

	var got manifest.Manifest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "mainnet", got.Chain)
	require.Len(t, got.Files, 1)
	assert.Equal(t, "blk00000.dat", got.Files[0].Name)

	req, err := http.NewRequest(http.MethodGet, base+"/v1/manifest", nil)
	require.NoError(t, err)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, http.StatusNotModified, resp2.StatusCode)
}

func TestServeAutoReloadViaHTTP(t *testing.T) {
	dir := t.TempDir()

	blkPath := filepath.Join(dir, "blk00000.dat")
	f, err := os.Create(blkPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Truncate(blkPath, store.FinalizedFileThreshold))

	m, err := GenerateManifest(dir, "mainnet")
	require.NoError(t, err)
	manifestPath := filepath.Join(dir, "manifest.json")
	require.NoError(t, manifest.WriteFile(manifestPath, m))

	base, _ := startTestFileServerWithOpts(t, dir, manifestPath, "", "",
		WithChain("mainnet"),
		WithScanInterval(100*time.Millisecond),
	)

	resp, err := http.Get(base + "/v1/manifest")
	require.NoError(t, err)
	initialETag := resp.Header.Get("ETag")
	resp.Body.Close()
	require.NotEmpty(t, initialETag)

	blkPath2 := filepath.Join(dir, "blk00001.dat")
	f2, err := os.Create(blkPath2)
	require.NoError(t, err)
	require.NoError(t, f2.Close())
	require.NoError(t, os.Truncate(blkPath2, store.FinalizedFileThreshold))

	deadline := time.Now().Add(5 * time.Second)
	var newETag string
	for time.Now().Before(deadline) {
		r, err := http.Get(base + "/v1/manifest")
		if err == nil {
			newETag = r.Header.Get("ETag")
			r.Body.Close()
			if newETag != initialETag {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.NotEqual(t, initialETag, newETag, "manifest ETag should have changed after adding a new file")

	resp, err = http.Get(base + "/v1/manifest")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got manifest.Manifest
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Len(t, got.Files, 2)
}
