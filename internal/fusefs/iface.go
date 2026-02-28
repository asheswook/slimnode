package fusefs

import (
	"context"
	"io"
)

// RemoteClient is the interface for remote file operations used by the FUSE filesystem.
type RemoteClient interface {
	FetchFile(ctx context.Context, filename string, dest io.Writer) error
	FetchBlockmap(ctx context.Context, filename string) ([]byte, error)
	FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error)
}
