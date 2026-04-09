package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

// HTTPClient fetches files and manifests from the archive server over HTTP.
type HTTPClient struct {
	baseURL        string
	httpClient     *http.Client
	retryCount     int
	retryBaseDelay time.Duration
	logger         *slog.Logger
}

// New creates a new Client.
func New(baseURL string, timeout time.Duration, retryCount int) *HTTPClient {
	return &HTTPClient{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     &http.Client{Timeout: timeout},
		retryCount:     retryCount,
		retryBaseDelay: time.Second,
		logger:         slog.Default(),
	}
}

func (c *HTTPClient) withRetryBaseDelay(d time.Duration) *HTTPClient {
	c.retryBaseDelay = d
	return c
}

// FetchFile downloads a file from the server and streams it to dest.
// It computes SHA-256 while streaming and verifies against the X-SHA256 response header.
// Never buffers the entire file in memory.
func (c *HTTPClient) FetchFile(ctx context.Context, filename string, dest io.Writer) error {
	url := c.baseURL + "/v1/file/" + filename

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

	expectedHash := resp.Header.Get("X-SHA256")

	// Stream and compute SHA-256 simultaneously via TeeReader - no full-file buffering.
	h := sha256.New()
	tee := io.TeeReader(resp.Body, h)
	if _, err := io.Copy(dest, tee); err != nil {
		return fmt.Errorf("failed to stream file: %w", err)
	}

	if expectedHash != "" {
		computedHash := hex.EncodeToString(h.Sum(nil))
		if computedHash != expectedHash {
			return ErrHashMismatch
		}
	}

	return nil
}

// FetchManifest fetches the server manifest.
// If etag is non-empty and the server returns 304 Not Modified,
// returns (nil, etag, nil) indicating the manifest has not changed.
func (c *HTTPClient) FetchManifest(ctx context.Context, etag string) (*manifest.Manifest, string, error) {
	url := c.baseURL + "/v1/manifest"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create manifest request: %w", err)
	}

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
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

// HealthCheck pings the server health endpoint.
// Returns ErrServerUnavailable if the server is not ok.
func (c *HTTPClient) HealthCheck(ctx context.Context) error {
	url := c.baseURL + "/v1/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}

	if health.Status != "ok" {
		return fmt.Errorf("%w: status=%s", ErrServerUnavailable, health.Status)
	}

	return nil
}

// FetchBlockmap fetches the raw blockmap bytes for a given filename from /v1/blockmap/{filename}.
// Returns ErrFileNotFound if the server responds with 404.
// The caller is responsible for parsing and verifying the blockmap.
func (c *HTTPClient) FetchBlockmap(ctx context.Context, filename string) ([]byte, error) {
	url := c.baseURL + "/v1/blockmap/" + filename

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

// FetchBlock fetches a specific byte range of a file from /v1/file/{filename} using an HTTP Range header.
// Returns ErrFileNotFound if the server responds with 404.
// Accepts both 206 Partial Content and 200 OK responses.
// No hash verification is done here - the caller handles it.
func (c *HTTPClient) FetchBlock(ctx context.Context, filename string, offset, length int64) ([]byte, error) {
	url := c.baseURL + "/v1/file/" + filename

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

// FetchSnapshot downloads a snapshot file from /v1/snapshot/{name} and streams it to dest.
// It computes SHA-256 while streaming and verifies against the X-SHA256 response header.
func (c *HTTPClient) FetchSnapshot(ctx context.Context, name string, dest io.Writer) error {
	url := c.baseURL + "/v1/snapshot/" + name

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

	expectedHash := resp.Header.Get("X-SHA256")

	h := sha256.New()
	tee := io.TeeReader(resp.Body, h)
	if _, err := io.Copy(dest, tee); err != nil {
		return fmt.Errorf("failed to stream snapshot: %w", err)
	}

	if expectedHash != "" {
		computedHash := hex.EncodeToString(h.Sum(nil))
		if computedHash != expectedHash {
			return ErrHashMismatch
		}
	}

	return nil
}

// doWithRetry executes an HTTP request with exponential backoff on 5xx errors.
// It respects ctx cancellation both during waits and during requests.
func (c *HTTPClient) doWithRetry(ctx context.Context, makeReq func() (*http.Request, error)) (*http.Response, error) {
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
