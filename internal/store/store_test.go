package store_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func entry(name string, state store.FileState) *store.FileEntry {
	return &store.FileEntry{
		Filename:    name,
		State:       state,
		Source:      store.FileSourceServer,
		Size:        1024,
		SHA256:      "abc123",
		CreatedAt:   time.Now().Truncate(time.Second),
		HeightFirst: 100,
		HeightLast:  200,
	}
}

func TestCRUD(t *testing.T) {
	s := newStore(t)

	e := entry("blk00001.dat", store.FileStateActive)
	require.NoError(t, s.UpsertFile(e))

	got, err := s.GetFile("blk00001.dat")
	require.NoError(t, err)
	assert.Equal(t, e.Filename, got.Filename)
	assert.Equal(t, e.State, got.State)
	assert.Equal(t, e.Source, got.Source)
	assert.Equal(t, e.Size, got.Size)
	assert.Equal(t, e.SHA256, got.SHA256)
	assert.Equal(t, e.HeightFirst, got.HeightFirst)
	assert.Equal(t, e.HeightLast, got.HeightLast)

	require.NoError(t, s.UpdateState("blk00001.dat", store.FileStateCached))
	got2, err := s.GetFile("blk00001.dat")
	require.NoError(t, err)
	assert.Equal(t, store.FileStateCached, got2.State)

	require.NoError(t, s.DeleteFile("blk00001.dat"))
	_, err = s.GetFile("blk00001.dat")
	assert.Error(t, err)
}

func TestListByState(t *testing.T) {
	s := newStore(t)

	states := []store.FileState{
		store.FileStateActive,
		store.FileStateActive,
		store.FileStateCached,
		store.FileStateRemote,
	}
	for i, st := range states {
		require.NoError(t, s.UpsertFile(entry(fmt.Sprintf("blk%05d.dat", i), st)))
	}

	active, err := s.ListByState(store.FileStateActive)
	require.NoError(t, err)
	assert.Len(t, active, 2)

	cached, err := s.ListByState(store.FileStateCached)
	require.NoError(t, err)
	assert.Len(t, cached, 1)

	remote, err := s.ListByState(store.FileStateRemote)
	require.NoError(t, err)
	assert.Len(t, remote, 1)

	all, err := s.ListFiles()
	require.NoError(t, err)
	assert.Len(t, all, 4)
}

func TestListCachedByLRU(t *testing.T) {
	s := newStore(t)

	base := time.Now().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		e := entry(fmt.Sprintf("blk%05d.dat", i), store.FileStateCached)
		e.LastAccess = base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, s.UpsertFile(e))
	}

	results, err := s.ListCachedByLRU(5)
	require.NoError(t, err)
	require.Len(t, results, 5)

	for i := 1; i < len(results); i++ {
		assert.True(t, !results[i].LastAccess.Before(results[i-1].LastAccess),
			"expected ascending last_access at index %d", i)
	}

	limited, err := s.ListCachedByLRU(3)
	require.NoError(t, err)
	assert.Len(t, limited, 3)
}

func TestUpdateLastAccess(t *testing.T) {
	s := newStore(t)

	e := entry("blk00001.dat", store.FileStateCached)
	require.NoError(t, s.UpsertFile(e))

	newTime := time.Now().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, s.UpdateLastAccess("blk00001.dat", newTime))

	got, err := s.GetFile("blk00001.dat")
	require.NoError(t, err)
	assert.Equal(t, newTime.Unix(), got.LastAccess.Unix())
}

func TestConcurrent(t *testing.T) {
	s := newStore(t)

	for i := 0; i < 20; i++ {
		e := entry(fmt.Sprintf("blk%05d.dat", i), store.FileStateCached)
		require.NoError(t, s.UpsertFile(e))
	}

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				name := fmt.Sprintf("blk%05d.dat", (g*20+i)%20)
				if i%3 == 0 {
					s.UpsertFile(entry(name, store.FileStateCached))
				} else if i%3 == 1 {
					s.GetFile(name)
				} else {
					s.UpdateLastAccess(name, time.Now())
				}
			}
		}()
	}
	wg.Wait()
}

func TestCloseAndReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	s1, err := store.New(dbPath)
	require.NoError(t, err)

	e := entry("blk00001.dat", store.FileStateRemote)
	require.NoError(t, s1.UpsertFile(e))
	require.NoError(t, s1.Close())

	s2, err := store.New(dbPath)
	require.NoError(t, err)
	defer s2.Close()

	got, err := s2.GetFile("blk00001.dat")
	require.NoError(t, err)
	assert.Equal(t, "blk00001.dat", got.Filename)
	assert.Equal(t, store.FileStateRemote, got.State)
}
