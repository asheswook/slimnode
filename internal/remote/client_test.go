package remote

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
	c := New(baseURL, 10*time.Second, 3)
	c.SetRetryBaseDelay(10 * time.Millisecond)
	return c
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

	c := New(srv.URL, 10*time.Second, 3)
	c.SetRetryBaseDelay(10 * time.Millisecond)
	var buf bytes.Buffer
	err := c.FetchFile(context.Background(), "test.dat", &buf)

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

func TestFetchBlockRejectsZeroLength(t *testing.T) {
	c := New("http://localhost", 10*time.Second, 0)
	_, err := c.FetchBlock(context.Background(), "x", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "length")
}

func TestFetchBlockRejectsNegativeOffset(t *testing.T) {
	c := New("http://localhost", 10*time.Second, 0)
	_, err := c.FetchBlock(context.Background(), "x", -1, 128)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offset")
}

func TestFetchBlockRespectsLengthLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(bytes.Repeat([]byte{0xAB}, 200))
	}))
	t.Cleanup(srv.Close)

	got, err := newClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", 0, 64)
	require.NoError(t, err)
	assert.Equal(t, 64, len(got))
}

func TestFetchFileRequiresXSHA256Header(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingHash))
}

func TestFetchSnapshotRequiresXSHA256Header(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("snapshot data"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newClient(t, srv.URL).FetchSnapshot(context.Background(), "blocks-index-880000.tar.zst", &buf)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingHash))
}

func TestRetryBaseDelayZeroNoPanic(t *testing.T) {
	data := []byte("ok")
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

	c := New(srv.URL, 10*time.Second, 3)
	c.SetRetryBaseDelay(0)

	var buf bytes.Buffer
	err := c.FetchFile(context.Background(), "test.dat", &buf)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestDoWithRetryReturnsOnContextCancel(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	c := New(srv.URL, 30*time.Second, 3)
	c.SetRetryBaseDelay(10 * time.Millisecond)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var buf bytes.Buffer
	err := c.FetchFile(ctx, "test.dat", &buf)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}

func TestDoWithRetryLogsAttempts(t *testing.T) {
	data := testutil.RandomBytes(t, 64)
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

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	c := New(srv.URL, 10*time.Second, 3)
	c.SetRetryBaseDelay(10 * time.Millisecond)
	c.SetLogger(logger)

	var buf bytes.Buffer
	err := c.FetchFile(context.Background(), "test.dat", &buf)
	require.NoError(t, err)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "attempt")
}
