package cmd

import (
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
)

var _ manifestFetcher = (*remote.HTTPClient)(nil)
var _ snapshotFetcher = (*remote.HTTPClient)(nil)
