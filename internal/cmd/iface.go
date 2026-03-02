package cmd

import (
	"context"
	"io"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

type manifestFetcher interface {
	FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error)
}

type snapshotFetcher interface {
	FetchSnapshot(ctx context.Context, name string, dest io.Writer) error
}
