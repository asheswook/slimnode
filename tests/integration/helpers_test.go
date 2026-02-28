//go:build integration

package integration

import (
	"github.com/asheswook/bitcoin-lfn/internal/remote"
)

func newIntegrationRemoteClient(baseURL string) *remote.HTTPClient {
	return remote.New(baseURL, 0, 3)
}
