package remote

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClient(t *testing.T, baseURL string) *HTTPClient {
	t.Helper()
	return New(baseURL, 10*time.Second, 3).withRetryBaseDelay(10 * time.Millisecond)
}

func TestFetchFileSuccess(t *testing.T) {
	data := testutil.RandomBytes(t, 1*1024*1024)
	srv := testutil.NewTestServer(t, testutil.SampleManifest(), map[string][]byte{
		"blk00000.dat": data,
	})

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
}

func TestFetchFileSHA256Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SHA256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHashMismatch)
}

func TestFetchFileNotFound(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.SampleManifest(), map[string][]byte{})

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
}

func TestRetryOnServerError(t *testing.T) {
	data := testutil.RandomBytes(t, 1024)
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-SHA256", testutil.SHA256Hex(data))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := New(srv.URL, 10*time.Second, 3).withRetryBaseDelay(10 * time.Millisecond).
		FetchFile(context.Background(), "test.dat", &buf)

	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestFetchManifest304(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.SampleManifest(), nil)
	client := newClient(t, srv.URL)

	_, etag, err := client.FetchManifest(context.Background(), "")
	require.NoError(t, err)
	require.NotEmpty(t, etag)

	got, etag2, err := client.FetchManifest(context.Background(), etag)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, etag, etag2)
}

func TestFetchManifest200(t *testing.T) {
	m := testutil.SampleManifest()
	srv := testutil.NewTestServer(t, m, nil)

	got, etag, err := newClient(t, srv.URL).FetchManifest(context.Background(), "")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotEmpty(t, etag)
	assert.Equal(t, m.Version, got.Version)
	assert.Equal(t, m.Chain, got.Chain)
	assert.Equal(t, m.TipHeight, got.TipHeight)
}

func TestHealthCheck(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.SampleManifest(), nil)
	err := newClient(t, srv.URL).HealthCheck(context.Background())
	require.NoError(t, err)
}

func TestFetchBlockmapSuccess(t *testing.T) {
	blockmapData := []byte("blockmap-payload-data")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/blockmap/blk00000.dat" {
			w.WriteHeader(http.StatusOK)
			w.Write(blockmapData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newClient(t, srv.URL).FetchBlockmap(context.Background(), "blk00000.dat")

	require.NoError(t, err)
	assert.Equal(t, blockmapData, got)
}

func TestFetchBlockmap404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newClient(t, srv.URL).FetchBlockmap(context.Background(), "blk00000.dat")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.Nil(t, got)
}

func TestFetchBlockRange(t *testing.T) {
	data := testutil.RandomBytes(t, 2048)
	const offset, length = int64(100), int64(508)

	var gotRangeHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRangeHeader = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(data[offset : offset+length])
	}))
	t.Cleanup(srv.Close)

	got, err := newClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", offset, length)

	require.NoError(t, err)
	assert.Equal(t, "bytes=100-607", gotRangeHeader)
	assert.Equal(t, data[offset:offset+length], got)
}

func TestFetchBlock404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", 0, 128)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.Nil(t, got)
}


func TestFetchSnapshotSuccess(t *testing.T) {
	data := testutil.RandomBytes(t, 64*1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/snapshot/blocks-index-880000.tar.zst" {
			w.Header().Set("X-SHA256", testutil.SHA256Hex(data))
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchSnapshot(context.Background(), "blocks-index-880000.tar.zst", &buf)

	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
}

func TestFetchSnapshotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchSnapshot(context.Background(), "nonexistent.tar.zst", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
}

func TestFetchSnapshotSHA256Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SHA256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("snapshot data"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchSnapshot(context.Background(), "test.tar.zst", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHashMismatch)
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	client := New(srv.URL, 30*time.Second, 0)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- client.FetchFile(ctx, "test.dat", &buf)
	}()

	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("FetchFile did not return after context cancellation")
	}
}
