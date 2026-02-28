package blockcache

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreGetHas(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 100*1024*1024)
	require.NoError(t, err)

	data := []byte("hello block")
	require.NoError(t, c.StoreBlock("blk02100.dat", 44800, data))

	assert.True(t, c.HasBlock("blk02100.dat", 44800))

	got, err := c.GetBlock("blk02100.dat", 44800)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestGetBlockNonexistent(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 100*1024*1024)
	require.NoError(t, err)

	assert.False(t, c.HasBlock("blk00000.dat", 0))

	_, err = c.GetBlock("blk00000.dat", 0)
	assert.Error(t, err)
}

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 100*1024*1024)
	require.NoError(t, err)

	require.NoError(t, c.StoreBlock("blk00001.dat", 0, []byte("a")))
	require.NoError(t, c.StoreBlock("blk00001.dat", 128, []byte("b")))
	require.NoError(t, c.StoreBlock("blk00002.dat", 0, []byte("c")))

	require.NoError(t, c.RemoveFile("blk00001.dat"))

	assert.False(t, c.HasBlock("blk00001.dat", 0))
	assert.False(t, c.HasBlock("blk00001.dat", 128))
	assert.True(t, c.HasBlock("blk00002.dat", 0))
}

func TestConcurrentStores(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, 100*1024*1024)
	require.NoError(t, err)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			data := []byte(fmt.Sprintf("data-%d", i))
			err := c.StoreBlock("blk00000.dat", int64(i*512), data)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		assert.True(t, c.HasBlock("blk00000.dat", int64(i*512)))
	}
}

func TestUsage(t *testing.T) {
	dir := t.TempDir()
	const maxBytes = 50 * 1024 * 1024
	c, err := New(dir, maxBytes)
	require.NoError(t, err)

	data1 := []byte("aaaaaaaaaa")
	data2 := []byte("bbbbbbbbbbbbbb")
	require.NoError(t, c.StoreBlock("blk00000.dat", 0, data1))
	require.NoError(t, c.StoreBlock("blk00000.dat", 512, data2))

	used, total := c.Usage()
	assert.Equal(t, int64(maxBytes), total)
	assert.Equal(t, int64(len(data1)+len(data2)), used)
}
