package cache

import "io"

// Cache manages local disk cache for server files.
// This interface is defined in the cache package following the btcd database pattern.
type Cache interface {
	Has(filename string) bool
	Path(filename string) string
	Store(filename string, r io.Reader, expectedSHA256 string) error
	Remove(filename string) error
	Usage() (used int64, total int64)
}
