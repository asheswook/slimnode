package blockcache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

type DiskBlockCache struct {
	dir      string
	maxBytes int64
	mu       sync.RWMutex
}

func New(dir string, maxBytes int64) (*DiskBlockCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("blockcache: mkdir %q: %w", dir, err)
	}
	return &DiskBlockCache{dir: dir, maxBytes: maxBytes}, nil
}

func (c *DiskBlockCache) blockPath(blkFile string, fileOffset int64) string {
	return filepath.Join(c.dir, blkFile, fmt.Sprintf("%d", fileOffset))
}

func (c *DiskBlockCache) StoreBlock(blkFile string, fileOffset int64, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	subDir := filepath.Join(c.dir, blkFile)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return fmt.Errorf("blockcache: mkdir sub-dir %q: %w", subDir, err)
	}

	tmp, err := os.CreateTemp(subDir, ".tmp-")
	if err != nil {
		return fmt.Errorf("blockcache: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("blockcache: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blockcache: close temp: %w", err)
	}

	dest := c.blockPath(blkFile, fileOffset)
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blockcache: rename to %q: %w", dest, err)
	}
	return nil
}

func (c *DiskBlockCache) GetBlock(blkFile string, fileOffset int64) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.blockPath(blkFile, fileOffset)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("blockcache: read %q: %w", path, err)
	}
	return data, nil
}

func (c *DiskBlockCache) HasBlock(blkFile string, fileOffset int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, err := os.Stat(c.blockPath(blkFile, fileOffset))
	return err == nil
}

func (c *DiskBlockCache) RemoveFile(blkFile string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dir := filepath.Join(c.dir, blkFile)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("blockcache: remove %q: %w", dir, err)
	}
	return nil
}

func (c *DiskBlockCache) Usage() (used int64, total int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sum int64
	_ = filepath.WalkDir(c.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sum += info.Size()
		return nil
	})
	return sum, c.maxBytes
}
