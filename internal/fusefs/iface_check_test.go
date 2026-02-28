package fusefs_test

import (
	"github.com/asheswook/bitcoin-lfn/internal/fusefs"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
)

var _ fusefs.RemoteClient = (*remote.HTTPClient)(nil)
