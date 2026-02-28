package daemon_test

import (
	"github.com/asheswook/bitcoin-lfn/internal/daemon"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
)

var _ daemon.ManifestFetcher = (*remote.HTTPClient)(nil)
