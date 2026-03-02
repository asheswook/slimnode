package cache

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCache(t *testing.T, maxBytes int64, minKeep int) (*DiskCache, store.Store) {
	t.Helper()
	tmpDir := t.TempDir()
	s, err := store.New(filepath.Join(tmpDir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	c, err := New(filepath.Join(tmpDir, "cache"), maxBytes, minKeep, s)
	require.NoError(t, err)
	return c, s
}

func randomData(t *testing.T, size int) ([]byte, string) {
	t.Helper()
	data := make([]byte, size)
	_, err := io.ReadFull(rand.Reader, data)
	require.NoError(t, err)
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

func TestStoreSuccess(t *testing.T) {
	c, _ := setupCache(t, 10*1024*1024*1024, 2)

	data, hash := randomData(t, 1024*1024)
	err := c.Store("blk00001.dat", bytes.NewReader(data), hash)
	require.NoError(t, err)

	assert.True(t, c.Has("blk00001.dat"))
	assert.FileExists(t, c.Path("blk00001.dat"))
}

func TestStoreHashMismatch(t *testing.T) {
	c, _ := setupCache(t, 10*1024*1024*1024, 2)

	data, _ := randomData(t, 1024)
	err := c.Store("blk00002.dat", bytes.NewReader(data), "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	require.ErrorIs(t, err, ErrHashMismatch)

	assert.False(t, c.Has("blk00002.dat"))
}

func TestHasPath(t *testing.T) {
	c, _ := setupCache(t, 10*1024*1024*1024, 2)

	assert.False(t, c.Has("blk00003.dat"))
	assert.Equal(t, filepath.Join(c.dir, "blk00003.dat"), c.Path("blk00003.dat"))

	data, hash := randomData(t, 512)
	require.NoError(t, c.Store("blk00003.dat", bytes.NewReader(data), hash))
	assert.True(t, c.Has("blk00003.dat"))
}

func TestRemove(t *testing.T) {
	c, s := setupCache(t, 10*1024*1024*1024, 2)

	data, hash := randomData(t, 512)
	require.NoError(t, c.Store("blk00004.dat", bytes.NewReader(data), hash))
	assert.True(t, c.Has("blk00004.dat"))

	require.NoError(t, c.Remove("blk00004.dat"))
	assert.False(t, c.Has("blk00004.dat"))

	entry, err := s.GetFile("blk00004.dat")
	require.NoError(t, err)
	assert.Equal(t, store.FileStateRemote, entry.State)
}

func TestEvictLRU(t *testing.T) {
	c, s := setupCache(t, 10*1024*1024*1024, 1)

	// Store 5 files
	files := []string{"blk00010.dat", "blk00011.dat", "blk00012.dat", "blk00013.dat", "blk00014.dat"}
	for _, f := range files {
		data, hash := randomData(t, 256)
		require.NoError(t, c.Store(f, bytes.NewReader(data), hash))
	}

	// Set different last_access times (oldest first)
	for i, f := range files {
		accessTime := time.Now().Add(time.Duration(-5+i) * time.Hour)
		require.NoError(t, s.UpdateLastAccess(f, accessTime))
	}

	// Evict 2 oldest
	evicted, err := c.Evict(2)
	require.NoError(t, err)
	assert.Len(t, evicted, 2)

	// Oldest 2 should be gone
	assert.False(t, c.Has("blk00010.dat"))
	assert.False(t, c.Has("blk00011.dat"))
	// Newer ones should remain
	assert.True(t, c.Has("blk00012.dat"))
	assert.True(t, c.Has("blk00013.dat"))
	assert.True(t, c.Has("blk00014.dat"))
}

func TestEvictMinKeep(t *testing.T) {
	c, s := setupCache(t, 10*1024*1024*1024, 2)

	// Store 3 files
	files := []string{"blk00020.dat", "blk00021.dat", "blk00022.dat"}
	for i, f := range files {
		data, hash := randomData(t, 256)
		require.NoError(t, c.Store(f, bytes.NewReader(data), hash))
		require.NoError(t, s.UpdateLastAccess(f, time.Now().Add(time.Duration(-3+i)*time.Hour)))
	}

	// Try to evict 3, but minKeep=2 means only 1 can be evicted
	evicted, err := c.Evict(3)
	require.NoError(t, err)
	assert.Len(t, evicted, 1, "only 1 should be evicted when minKeep=2 and 3 files exist")

	// 2 files should remain
	remaining := 0
	for _, f := range files {
		if c.Has(f) {
			remaining++
		}
	}
	assert.Equal(t, 2, remaining)
}

func TestUsage(t *testing.T) {
	c, _ := setupCache(t, 10*1024*1024*1024, 2)

	data1, hash1 := randomData(t, 1024)
	data2, hash2 := randomData(t, 2048)
	require.NoError(t, c.Store("blk00030.dat", bytes.NewReader(data1), hash1))
	require.NoError(t, c.Store("blk00031.dat", bytes.NewReader(data2), hash2))

	used, total := c.Usage()
	assert.GreaterOrEqual(t, used, int64(1024+2048))
	assert.Equal(t, int64(10*1024*1024*1024), total)
}

func TestConcurrentStore(t *testing.T) {
	c, _ := setupCache(t, 10*1024*1024*1024, 2)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			filename := fmt.Sprintf("blk%05d.dat", 40+i)
			data, hash := randomData(t, 512)
			err := c.Store(filename, bytes.NewReader(data), hash)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all 5 files were stored
	for i := 0; i < 5; i++ {
		assert.True(t, c.Has(fmt.Sprintf("blk%05d.dat", 40+i)))
	}
}
