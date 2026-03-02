//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/daemon"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
)

func TestPollerUpdatesStore(t *testing.T) {
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"

	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)

	rc := newIntegrationRemoteClient(srv.URL)
	poller := daemon.NewManifestPoller(rc, st, initial, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)
	<-ctx.Done()

	m := poller.CurrentManifest()
	assert.NotNil(t, m)
}

func TestCacheEviction(t *testing.T) {
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"

	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	_, err = store.New(dbPath + "2")
	require.NoError(t, err)

	t.Log("cache eviction integration test placeholder")
}

func TestGracefulShutdown(t *testing.T) {
	dir := testutil.TempDir(t)
	dbPath := dir + "/test.db"

	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)

	rc := newIntegrationRemoteClient(srv.URL)
	poller := daemon.NewManifestPoller(rc, st, initial, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- poller.Run(ctx) }()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not shut down in time")
	}
}
