package remote

import "errors"

var (
	ErrServerUnavailable = errors.New("archive server unavailable")
	ErrFileNotFound      = errors.New("file not found")
	ErrHashMismatch      = errors.New("SHA-256 hash mismatch")
)
