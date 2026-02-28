package cache

import "errors"

var (
	ErrHashMismatch = errors.New("SHA-256 hash mismatch")
	ErrCacheFull    = errors.New("cache capacity exceeded")
)
