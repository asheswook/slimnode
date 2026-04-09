package fusefs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hanwen/go-fuse/v2/fs"
)

// ============================================================================
// TestNullWriteHandle_Write
// ============================================================================

func TestNullWriteHandle_Write(t *testing.T) {
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: "rev00000.dat"}

	tests := []struct {
		name string
		data []byte
		off  int64
	}{
		{"zero bytes at offset 0", []byte{}, 0},
		{"4096 bytes at offset 0", make([]byte, 4096), 0},
		{"65536 bytes at offset 4096", make([]byte, 65536), 4096},
		{"1 byte at large offset", []byte{0x42}, 1 << 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, errno := h.Write(context.Background(), tt.data, tt.off)
			assert.Equal(t, syscall.Errno(0), errno, "Write must return OK")
			assert.Equal(t, uint32(len(tt.data)), n, "Write must return full data length")
		})
	}
}

// ============================================================================
// TestNullWriteHandle_Fsync
// ============================================================================

func TestNullWriteHandle_Fsync(t *testing.T) {
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: "rev00000.dat"}

	assert.Equal(t, fs.OK, h.Fsync(context.Background(), 0), "Fsync(0) must return OK")
	assert.Equal(t, fs.OK, h.Fsync(context.Background(), 1), "Fsync(1) must return OK")
}

// ============================================================================
// TestNullWriteHandle_Fallocate
// (calls Allocate - the go-fuse FileAllocater method - named "Fallocate" for
//
//	test-filter compatibility with -run TestNullWrite)
//
// ============================================================================

func TestNullWriteHandle_Fallocate(t *testing.T) {
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: "rev00000.dat"}

	tests := []struct {
		off  uint64
		size uint64
		mode uint32
	}{
		{0, 128 * 1024 * 1024, 0},
		{4096, 65536, 1},
		{0, 0, 0},
	}

	for _, tt := range tests {
		errno := h.Allocate(context.Background(), tt.off, tt.size, tt.mode)
		assert.Equal(t, syscall.Errno(0), errno, "Allocate must always return OK")
	}
}

// ============================================================================
// TestNullWriteHandle_Read
// ============================================================================

func TestNullWriteHandle_Read(t *testing.T) {
	const filename = "rev00100.dat"
	expected := []byte("hello from remote server, this is rev file data for testing")

	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, int64(0))] = expected

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: filename}

	dest := make([]byte, len(expected))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno, "Read must return OK on success")

	data := readResultBytes(t, result)
	assert.Equal(t, expected, data, "Read must return the remote data")

	// Verify no local file was created.
	localPath := filepath.Join(fsys.localDir, filename)
	_, err := os.Stat(localPath)
	assert.True(t, os.IsNotExist(err), "NullWriteHandle.Read must not create a local file")
}

func TestNullWriteHandle_Read_PrefersLocalFile(t *testing.T) {
	const filename = "rev00300.dat"
	localData := []byte("local-finalized rev data on disk")

	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, int64(0))] = []byte("remote data that should NOT be used")

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, nil, nil)
	require.NoError(t, os.WriteFile(filepath.Join(fsys.localDir, filename), localData, 0644))

	h := &NullWriteHandle{fs: fsys, filename: filename}
	dest := make([]byte, len(localData))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, localData, readResultBytes(t, result))
	assert.Equal(t, 0, rc.fetchBlockCallCount(), "must not call remote when local file exists")
}

func TestNullWriteHandle_Read_PrefersCacheOverRemote(t *testing.T) {
	const filename = "rev00400.dat"
	cacheData := []byte("cached rev data downloaded from server")

	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, int64(0))] = []byte("remote data that should NOT be used")

	ca := newMockCache(t.TempDir())
	require.NoError(t, os.WriteFile(ca.Path(filename), cacheData, 0644))

	fsys := makeTestFS(t, newMockStore(), ca, rc, nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: filename}
	dest := make([]byte, len(cacheData))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, cacheData, readResultBytes(t, result))
	assert.Equal(t, 0, rc.fetchBlockCallCount(), "must not call remote when cache file exists")
}

func TestNullWriteHandle_Read_FallsBackToRemote(t *testing.T) {
	const filename = "rev00500.dat"
	remoteData := []byte("remote-only rev data")

	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, int64(0))] = remoteData

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: filename}
	dest := make([]byte, len(remoteData))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, remoteData, readResultBytes(t, result))
	assert.Equal(t, 1, rc.fetchBlockCallCount(), "must call remote when no local/cache file")
}

func TestNullWriteHandle_Read_FetchError(t *testing.T) {
	const filename = "rev00200.dat"
	rc := newMockRemoteClient()

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, nil, nil)
	h := &NullWriteHandle{fs: fsys, filename: filename}

	dest := make([]byte, 100)
	_, errno := h.Read(context.Background(), dest, 0)
	assert.Equal(t, syscall.EIO, errno, "Read must return EIO when all sources fail")
}

// ============================================================================
// TestIsRevFile
// ============================================================================

func TestIsRevFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"rev00000.dat", true},
		{"rev01125.dat", true},
		{"rev99999.dat", true},
		{"blk00000.dat", false},
		{"revindex", false},
		{"rev.dat", false},   // mid is empty
		{"revabc.dat", false}, // non-digits
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := isRevFile(tt.filename)
			assert.Equal(t, tt.want, got, "isRevFile(%q)", tt.filename)
		})
	}
}
