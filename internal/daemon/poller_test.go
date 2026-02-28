package daemon

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/remote"
	"github.com/asheswook/bitcoin-lfn/internal/store"
	"github.com/asheswook/bitcoin-lfn/internal/testutil"
)

func TestPollerNewFile(t *testing.T) {
	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)

	dbPath := testutil.TempDir(t) + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	rc := newTestRemoteClient(t, srv.URL)
	poller := NewManifestPoller(rc, st, initial, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- poller.Run(ctx) }()

	<-ctx.Done()
	err = <-errCh
	assert.NoError(t, err)
}

func TestPollerShutdown(t *testing.T) {
	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)

	dbPath := testutil.TempDir(t) + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	rc := newTestRemoteClient(t, srv.URL)
	poller := NewManifestPoller(rc, st, initial, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- poller.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not shut down in time")
	}
}

func TestPollerCurrentManifest(t *testing.T) {
	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)

	dbPath := testutil.TempDir(t) + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	rc := newTestRemoteClient(t, srv.URL)
	poller := NewManifestPoller(rc, st, initial, time.Hour)

	m := poller.CurrentManifest()
	require.NotNil(t, m)
	assert.Equal(t, initial.Chain, m.Chain)
}

func newTestRemoteClient(t *testing.T, baseURL string) *testRemoteClient {
	t.Helper()
	return &testRemoteClient{baseURL: baseURL}
}

type testRemoteClient struct {
	baseURL string
}

func (c *testRemoteClient) FetchFile(ctx context.Context, filename string, dest io.Writer) error {
	return nil
}

func (c *testRemoteClient) FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error) {
	return nil, etag, nil
}

func (c *testRemoteClient) HealthCheck(ctx context.Context) error {
	return nil
}

func (c *testRemoteClient) FetchBlockmap(ctx context.Context, filename string) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}

func (c *testRemoteClient) FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}

func (c *testRemoteClient) FetchSnapshot(ctx context.Context, name string, dest io.Writer) error {
	return remote.ErrFileNotFound
}

func TestPollerServerError(t *testing.T) {
	initial := testutil.SampleManifest()

	dbPath := testutil.TempDir(t) + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()

	rc := &errorRemoteClient{}
	poller := NewManifestPoller(rc, st, initial, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- poller.Run(ctx) }()

	<-ctx.Done()
	err = <-errCh
	assert.NoError(t, err)
}

type errorRemoteClient struct{}

func (c *errorRemoteClient) FetchManifest(_ context.Context, etag string) (*manifest.Manifest, string, error) {
	return nil, "", fmt.Errorf("server unavailable")
}
func (c *errorRemoteClient) FetchFile(_ context.Context, _ string, _ io.Writer) error {
	return fmt.Errorf("server unavailable")
}
func (c *errorRemoteClient) HealthCheck(_ context.Context) error {
	return fmt.Errorf("server unavailable")
}
func (c *errorRemoteClient) FetchBlockmap(_ context.Context, _ string) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}
func (c *errorRemoteClient) FetchBlock(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, remote.ErrFileNotFound
}

func (c *errorRemoteClient) FetchSnapshot(_ context.Context, _ string, _ io.Writer) error {
	return fmt.Errorf("server unavailable")
}

func TestPollerCurrentManifest_IsInitial(t *testing.T) {
	initial := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, initial, nil)
	dbPath := testutil.TempDir(t) + "/test.db"
	st, err := store.New(dbPath)
	require.NoError(t, err)
	defer st.Close()
	rc := newTestRemoteClient(t, srv.URL)
	poller := NewManifestPoller(rc, st, initial, time.Hour)
	m := poller.CurrentManifest()
	require.NotNil(t, m)
	assert.Equal(t, initial.Chain, m.Chain)
}
