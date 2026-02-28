//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/remote"
	"github.com/asheswook/bitcoin-lfn/internal/testutil"
)

// newCDNTestServer creates an httptest server that serves files at flat URLs
// (baseURL/filename) without X-SHA256 headers — simulating a real CDN.
func newCDNTestServer(t *testing.T, files map[string][]byte, manifestJSON []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Serve manifest.json
	if manifestJSON != nil {
		etag := testutil.SHA256Hex(manifestJSON)
		mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
			if match := r.Header.Get("If-None-Match"); match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(manifestJSON)
		})
	}

	// Serve files at flat URL (no /v1/file/ prefix, no X-SHA256 header)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		filename := strings.TrimPrefix(r.URL.Path, "/")
		data, ok := files[filename]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Handle Range requests
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
			spec := strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.Split(spec, "-")
			if len(parts) == 2 {
				start, _ := strconv.ParseInt(parts[0], 10, 64)
				end, _ := strconv.ParseInt(parts[1], 10, 64)
				if start >= 0 && end >= start && end < int64(len(data)) {
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data[start : end+1])
					return
				}
			}
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCDNClientFetchFileE2E tests that CDNClient can download a file from a CDN
// and that the content matches expected data. No X-SHA256 header involved.
func TestCDNClientFetchFileE2E(t *testing.T) {
	fileData := testutil.RandomBytes(t, 64*1024) // 64KB
	files := map[string][]byte{"blk00000.dat": fileData}

	srv := newCDNTestServer(t, files, nil)
	client := remote.NewCDN(srv.URL, 10*time.Second, 3)

	var buf bytes.Buffer
	err := client.FetchFile(context.Background(), "blk00000.dat", &buf)

	require.NoError(t, err)
	assert.Equal(t, fileData, buf.Bytes())
	assert.Equal(t, testutil.SHA256Hex(fileData), testutil.SHA256Hex(buf.Bytes()))
}

// TestCDNClientFetchManifestE2E tests that CDNClient fetches manifest.json
// from CDN and correctly parses it, including base_url field.
func TestCDNClientFetchManifestE2E(t *testing.T) {
	mf := testutil.SampleManifest()
	mf.BaseURL = "https://cdn.example.com"

	manifestJSON, err := json.Marshal(mf)
	require.NoError(t, err)

	srv := newCDNTestServer(t, nil, manifestJSON)
	client := remote.NewCDN(srv.URL, 10*time.Second, 3)

	// First fetch — 200 OK
	got, etag, err := client.FetchManifest(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "https://cdn.example.com", got.BaseURL)
	assert.Equal(t, mf.Chain, got.Chain)
	assert.Equal(t, mf.TipHeight, got.TipHeight)
	assert.NotEmpty(t, etag)

	// Second fetch with ETag — 304 Not Modified
	got2, etag2, err := client.FetchManifest(context.Background(), etag)
	require.NoError(t, err)
	assert.Nil(t, got2, "should return nil on 304")
	assert.Equal(t, etag, etag2)
}

// TestCDNClientFetchBlockE2E tests Range request support for partial reads.
func TestCDNClientFetchBlockE2E(t *testing.T) {
	fileData := testutil.RandomBytes(t, 4096)
	files := map[string][]byte{"blk00000.dat": fileData}

	srv := newCDNTestServer(t, files, nil)
	client := remote.NewCDN(srv.URL, 10*time.Second, 3)

	offset, length := int64(100), int64(512)
	got, err := client.FetchBlock(context.Background(), "blk00000.dat", offset, length)

	require.NoError(t, err)
	assert.Equal(t, fileData[offset:offset+length], got)
}

// TestCDNClientFetchBlockmapE2E tests blockmap fetching from CDN.
func TestCDNClientFetchBlockmapE2E(t *testing.T) {
	blockmapData := testutil.RandomBytes(t, 2048)
	files := map[string][]byte{"blk00000.dat.blockmap": blockmapData}

	srv := newCDNTestServer(t, files, nil)
	client := remote.NewCDN(srv.URL, 10*time.Second, 3)

	got, err := client.FetchBlockmap(context.Background(), "blk00000.dat")

	require.NoError(t, err)
	assert.Equal(t, blockmapData, got)
}

// TestCDNClientNotFoundE2E tests that CDN returns proper error for missing files.
func TestCDNClientNotFoundE2E(t *testing.T) {
	srv := newCDNTestServer(t, nil, nil)
	client := remote.NewCDN(srv.URL, 10*time.Second, 0)

	var buf bytes.Buffer
	err := client.FetchFile(context.Background(), "nonexistent.dat", &buf)

	require.Error(t, err)
	assert.ErrorIs(t, err, remote.ErrFileNotFound)
}

// TestCDNSHA256VerificationByCallerE2E demonstrates that SHA-256 verification
// is the caller's responsibility with CDN (unlike HTTPClient which uses X-SHA256).
func TestCDNSHA256VerificationByCallerE2E(t *testing.T) {
	fileData := testutil.RandomBytes(t, 32*1024)
	expectedSHA := testutil.SHA256Hex(fileData)
	files := map[string][]byte{"blk00000.dat": fileData}

	srv := newCDNTestServer(t, files, nil)
	client := remote.NewCDN(srv.URL, 10*time.Second, 3)

	var buf bytes.Buffer
	err := client.FetchFile(context.Background(), "blk00000.dat", &buf)
	require.NoError(t, err)

	// Caller verifies SHA-256 using manifest data
	actualSHA := testutil.SHA256Hex(buf.Bytes())
	assert.Equal(t, expectedSHA, actualSHA, "caller-side SHA-256 verification should pass")
}
