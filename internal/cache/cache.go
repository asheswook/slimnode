package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// DiskCache implements the Cache interface using local disk storage.
// LRU eviction is tracked via SQLite last_access timestamps in the store.
type DiskCache struct {
	dir      string
	maxBytes int64
	minKeep  int
	store    store.Store
	mu       sync.RWMutex
}

// New creates a new DiskCache backed by the given directory.
func New(dir string, maxBytes int64, minKeep int, s store.Store) (*DiskCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &DiskCache{
		dir:      dir,
		maxBytes: maxBytes,
		minKeep:  minKeep,
		store:    s,
	}, nil
}

// Has returns true if the file exists in the cache directory.
func (c *DiskCache) Has(filename string) bool {
	_, err := os.Stat(filepath.Join(c.dir, filename))
	return err == nil
}

// Path returns the absolute path to a cached file.
func (c *DiskCache) Path(filename string) string {
	return filepath.Join(c.dir, filename)
}

// Store writes data from r to the cache, verifying SHA-256.
// Uses atomic write (temp file → rename) to prevent partial writes.
func (c *DiskCache) Store(filename string, r io.Reader, expectedSHA256 string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Write to temp file in the same directory (same filesystem → atomic rename)
	tmp, err := os.CreateTemp(c.dir, filename+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Stream r → tmp while computing SHA-256
	h := sha256.New()
	tee := io.TeeReader(r, h)
	bytesWritten, err := io.Copy(tmp, tee)
	tmp.Close()
	if err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write cache file: %w", err)
	}

	// Verify hash
	computed := hex.EncodeToString(h.Sum(nil))
	if computed != expectedSHA256 {
		os.Remove(tmpName)
		return ErrHashMismatch
	}

	// Atomic rename
	dest := filepath.Join(c.dir, filename)
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename to final path: %w", err)
	}

	// Update metadata store
	now := time.Now()
	return c.store.UpsertFile(&store.FileEntry{
		Filename:   filename,
		State:      store.FileStateCached,
		Source:     store.FileSourceServer,
		Size:       bytesWritten,
		SHA256:     expectedSHA256,
		CreatedAt:  now,
		LastAccess: now,
	})
}

// Remove deletes a cached file and marks it as REMOTE in the store.
func (c *DiskCache) Remove(filename string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.Remove(filepath.Join(c.dir, filename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cache file: %w", err)
	}
	return c.store.UpdateState(filename, store.FileStateRemote)
}

// Usage returns current disk usage and maximum capacity.
func (c *DiskCache) Usage() (used int64, total int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return 0, c.maxBytes
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			used += info.Size()
		}
	}
	return used, c.maxBytes
}

// Evict removes the oldest (by last_access) cached files, keeping at least minKeep files.
// Returns the list of evicted filenames.
func (c *DiskCache) Evict(count int) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fetch enough entries so we can protect minKeep
	entries, err := c.store.ListCachedByLRU(count + c.minKeep)
	if err != nil {
		return nil, fmt.Errorf("list cached files: %w", err)
	}

	// Protect the minKeep most-recent entries (they are at the end, since list is ASC)
	if len(entries) <= c.minKeep {
		return nil, nil // nothing to evict
	}

	// Evict from the oldest entries (beginning of list)
	toEvict := entries[:len(entries)-c.minKeep]
	if len(toEvict) > count {
		toEvict = toEvict[:count]
	}

	var evicted []string
	for _, e := range toEvict {
		path := filepath.Join(c.dir, e.Filename)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			continue // best-effort
		}
		if err := c.store.UpdateState(e.Filename, store.FileStateRemote); err != nil {
			continue
		}
		evicted = append(evicted, e.Filename)
	}
	return evicted, nil
}
