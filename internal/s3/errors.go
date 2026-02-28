package s3

import "errors"

var (
	ErrBucketNotFound = errors.New("bucket not found")
	ErrAccessDenied   = errors.New("access denied")
	ErrKeyNotFound    = errors.New("key not found")
)
