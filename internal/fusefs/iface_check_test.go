package fusefs_test

import (
	"github.com/asheswook/bitcoin-slimnode/internal/fusefs"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
)

var _ fusefs.RemoteClient = (*remote.HTTPClient)(nil)
