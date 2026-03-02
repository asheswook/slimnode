package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/testutil"
)

func newCDNClient(t *testing.T, baseURL string) *CDNClient {
	t.Helper()
	return NewCDN(baseURL, 10*time.Second, 3).withRetryBaseDelay(10 * time.Millisecond)
}

func TestCDNFetchFileSuccess(t *testing.T) {
	data := testutil.RandomBytes(t, 1*1024*1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blk00000.dat" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newCDNClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
}

func TestCDNFetchFileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := newCDNClient(t, srv.URL).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
}

func TestCDNFetchFileServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := NewCDN(srv.URL, 10*time.Second, 0).FetchFile(context.Background(), "blk00000.dat", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerUnavailable)
}

func TestCDNFetchFileRetryOnServerError(t *testing.T) {
	data := testutil.RandomBytes(t, 1024)
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	err := NewCDN(srv.URL, 10*time.Second, 3).withRetryBaseDelay(10 * time.Millisecond).
		FetchFile(context.Background(), "blk00000.dat", &buf)

	require.NoError(t, err)
	assert.Equal(t, data, buf.Bytes())
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestCDNFetchBlock(t *testing.T) {
	data := testutil.RandomBytes(t, 2048)
	const offset, length = int64(100), int64(508)

	var gotRangeHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRangeHeader = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[offset : offset+length])
	}))
	t.Cleanup(srv.Close)

	got, err := newCDNClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", offset, length)

	require.NoError(t, err)
	assert.Equal(t, "bytes=100-607", gotRangeHeader)
	assert.Equal(t, data[offset:offset+length], got)
}

func TestCDNFetchBlockAccepts200(t *testing.T) {
	data := testutil.RandomBytes(t, 512)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	got, err := newCDNClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", 0, int64(len(data)))

	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestCDNFetchBlock404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newCDNClient(t, srv.URL).FetchBlock(context.Background(), "blk00000.dat", 0, 128)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.Nil(t, got)
}

func TestCDNFetchManifest200(t *testing.T) {
	m := testutil.SampleManifest()
	manifestJSON, err := json.Marshal(m)
	require.NoError(t, err)
	etag := testutil.SHA256Hex(manifestJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifestJSON)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, gotEtag, err := newCDNClient(t, srv.URL).FetchManifest(context.Background(), "")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, etag, gotEtag)
	assert.Equal(t, m.Version, got.Version)
	assert.Equal(t, m.Chain, got.Chain)
	assert.Equal(t, m.TipHeight, got.TipHeight)
}

func TestCDNFetchManifest304(t *testing.T) {
	m := testutil.SampleManifest()
	manifestJSON, err := json.Marshal(m)
	require.NoError(t, err)
	etag := testutil.SHA256Hex(manifestJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(manifestJSON)
	}))
	t.Cleanup(srv.Close)

	client := newCDNClient(t, srv.URL)

	_, firstEtag, err := client.FetchManifest(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, etag, firstEtag)

	got, secondEtag, err := client.FetchManifest(context.Background(), firstEtag)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, etag, secondEtag)
}

func TestCDNFetchManifestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	got, etag, err := newCDNClient(t, srv.URL).FetchManifest(context.Background(), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerUnavailable)
	assert.Nil(t, got)
	assert.Empty(t, etag)
}

func TestCDNFetchBlockmap(t *testing.T) {
	blockmapData := []byte("blockmap-payload-data")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blk00000.dat.blockmap" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(blockmapData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newCDNClient(t, srv.URL).FetchBlockmap(context.Background(), "blk00000.dat")

	require.NoError(t, err)
	assert.Equal(t, blockmapData, got)
}

func TestCDNFetchBlockmap404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	got, err := newCDNClient(t, srv.URL).FetchBlockmap(context.Background(), "blk00000.dat")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.Nil(t, got)
}

func TestCDNHealthCheck(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	err := newCDNClient(t, srv.URL).HealthCheck(context.Background())

	require.NoError(t, err)
	assert.Equal(t, http.MethodHead, gotMethod)
}

func TestCDNHealthCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	err := newCDNClient(t, srv.URL).HealthCheck(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServerUnavailable)
}

func TestCDNContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	client := NewCDN(srv.URL, 30*time.Second, 0)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- client.FetchFile(ctx, "blk00000.dat", &buf)
	}()

	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("FetchFile did not return after context cancellation")
	}
}
