package daemon_test

import (
	"github.com/asheswook/bitcoin-slimnode/internal/daemon"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
)

var _ daemon.ManifestFetcher = (*remote.HTTPClient)(nil)
