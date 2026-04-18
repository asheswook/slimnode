package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

// CDNClient fetches files from a CDN base URL using standard HTTP GET.
// It is a drop-in replacement for HTTPClient for FUSE file fetching.
// The CDN serves files at: baseURL/filename (flat structure).
type CDNClient struct {
	baseURL        string
	httpClient     *http.Client
	retryCount     int // retryCount is the maximum number of retries (total attempts = retryCount + 1).
	retryBaseDelay time.Duration
	logger         *slog.Logger
}

// NewCDN creates a CDNClient.
func NewCDN(baseURL string, timeout time.Duration, retryCount int) *CDNClient {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &CDNClient{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     &http.Client{Transport: transport},
		retryCount:     retryCount,
		retryBaseDelay: time.Second,
		logger:         slog.Default(),
	}
}

// SetRetryBaseDelay sets the base delay for exponential backoff between retry attempts.
func (c *CDNClient) SetRetryBaseDelay(d time.Duration) {
	c.retryBaseDelay = d
}

// SetLogger replaces the default logger.
func (c *CDNClient) SetLogger(l *slog.Logger) {
	c.logger = l
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
		return errors.Join(ErrServerUnavailable, err)
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
		return nil, "", errors.Join(ErrServerUnavailable, err)
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
// Response is capped at maxBlockmapSize (64 MiB).
func (c *CDNClient) FetchBlockmap(ctx context.Context, filename string) ([]byte, error) {
	url := c.baseURL + "/" + filename + ".blockmap"

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	})
	if err != nil {
		return nil, errors.Join(ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFileNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxBlockmapSize))
}

// FetchBlock fetches a byte range of a file from the CDN using HTTP Range header.
// Only accepts 206 Partial Content; 200 OK is treated as an error.
// Response is capped at the requested length via io.LimitReader.
func (c *CDNClient) FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("FetchBlock: length must be positive, got %d", length)
	}
	if offset < 0 {
		return nil, fmt.Errorf("FetchBlock: offset must be non-negative, got %d", offset)
	}

	url := c.baseURL + "/" + filename

	resp, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
		return req, nil
	})
	if err != nil {
		return nil, errors.Join(ErrServerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFileNotFound
	}
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("%w: expected 206, got HTTP %d", ErrServerUnavailable, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, length))
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
		return errors.Join(ErrServerUnavailable, err)
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
			base := time.Duration(1<<uint(attempt-1)) * c.retryBaseDelay
			var jitter time.Duration
			if j := int64(base / 5); j > 0 {
				jitter = time.Duration(rand.Int63n(j))
			}
			wait := base + jitter
			c.logger.Debug("retrying request",
				"attempt", attempt, "wait", wait, "err", lastErr)
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
		req.Header.Set("User-Agent", userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			c.logger.Debug("server 5xx, will retry",
				"attempt", attempt, "status", resp.StatusCode)
			continue
		}

		return resp, nil
	}
	return nil, lastErr
}
