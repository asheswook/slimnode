package daemon

import (
	"context"
	"io"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
)

// ManifestFetcher is the interface for fetching the server manifest.
type ManifestFetcher interface {
	FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error)
}

// S3Uploader is the interface for S3 upload operations required by Syncer.
type S3Uploader interface {
	Upload(ctx context.Context, key string, body io.Reader, size int64) error
	UploadManifest(ctx context.Context, key string, body io.Reader, size int64) error
	List(ctx context.Context, prefix string) ([]string, error)
}
