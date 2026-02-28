package remote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
)

// CDNClient fetches files from a CDN base URL using standard HTTP GET.
// It is a drop-in replacement for HTTPClient for FUSE file fetching.
// The CDN serves files at: baseURL/filename (flat structure).
type CDNClient struct {
	baseURL        string
	httpClient     *http.Client
	retryCount     int
	retryBaseDelay time.Duration
	logger         *slog.Logger
}

// NewCDN creates a CDNClient.
func NewCDN(baseURL string, timeout time.Duration, retryCount int) *CDNClient {
	return &CDNClient{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     &http.Client{Timeout: timeout},
		retryCount:     retryCount,
		retryBaseDelay: time.Second,
		logger:         slog.Default(),
	}
}

func (c *CDNClient) withRetryBaseDelay(d time.Duration) *CDNClient {
	c.retryBaseDelay = d
	return c
}

// FetchFile downloads a file from the CDN and streams it to dest.
// CDN responses do not carry X-SHA256; SHA-256 verification is done by the
// caller via manifest lookup (see fusefs/handle.go fetchAndCache).
func (c *CDNClient) FetchFile(ctx context.Context, filename string, dest io.Writer) error {
	url := c.baseURL + "/" + filename

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrFileNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	if _, err := io.Copy(dest, resp.Body); err != nil {
		return fmt.Errorf("failed to stream file: %w", err)
	}

	return nil
}

// FetchManifest fetches the manifest JSON from the CDN.
// The manifest is served at: baseURL/manifest.json
// Supports ETag-based conditional requests.
// Returns (nil, etag, nil) if the server responds with 304 Not Modified.
func (c *CDNClient) FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error) {
	url := c.baseURL + "/manifest.json"

	makeReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		return req, nil
	}

	resp, err := c.doWithRetry(ctx, makeReq)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	newEtag := resp.Header.Get("ETag")

	m, err := manifest.Parse(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse manifest: %w", err)
	}

	return m, newEtag, nil
}

// FetchBlockmap fetches the blockmap for a file from the CDN.
// Blockmap URL: baseURL/filename.blockmap
func (c *CDNClient) FetchBlockmap(ctx context.Context, filename string) ([]byte, error) {
	url := c.baseURL + "/" + filename + ".blockmap"

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFileNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// FetchBlock fetches a byte range of a file from the CDN using HTTP Range header.
// CDN natively supports Range requests.
// Accepts both 206 Partial Content and 200 OK responses.
func (c *CDNClient) FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error) {
	url := c.baseURL + "/" + filename

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		// Range header: bytes=offset-(offset+length-1), inclusive both ends.
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFileNotFound
	}
	// Accept both 206 Partial Content and 200 OK (some servers may return full file).
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// HealthCheck pings the CDN base URL with a HEAD request.
// Returns ErrServerUnavailable if the CDN is not reachable or returns a non-200 status.
func (c *CDNClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	return nil
}

// doWithRetry executes an HTTP request with exponential backoff on 5xx errors.
// It respects ctx cancellation both during waits and during requests.
func (c *CDNClient) doWithRetry(ctx context.Context, makeReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			// Exponential backoff: base * 2^(attempt-1), with ±20% jitter.
			base := time.Duration(1<<uint(attempt-1)) * c.retryBaseDelay
			jitter := time.Duration(rand.Int63n(int64(base / 5)))
			wait := base + jitter
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := makeReq()
		if err != nil {
			return nil, err
		}
		req = req.WithContext(ctx)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}
	return nil, lastErr
}
