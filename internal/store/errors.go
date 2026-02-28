package store

import "errors"

var (
	ErrFileNotFound = errors.New("file not found")
	ErrReadOnlyFile = errors.New("file is read-only")
)
