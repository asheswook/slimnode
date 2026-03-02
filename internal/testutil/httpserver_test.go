package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	mfst "github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

func TestHealthEndpoint(t *testing.T) {
	manifest := SampleManifest()
	server := NewTestServer(t, manifest, map[string][]byte{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/health")
	if err != nil {
		t.Fatalf("failed to GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if !contains(string(body), "ok") {
		t.Errorf("expected response to contain 'ok', got: %s", string(body))
	}
}

func TestManifestEndpoint(t *testing.T) {
	manifest := SampleManifest()
	server := NewTestServer(t, manifest, map[string][]byte{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/manifest")
	if err != nil {
		t.Fatalf("failed to GET /v1/manifest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("ETag") == "" {
		t.Error("expected ETag header to be present")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var respManifest mfst.Manifest
	if err := json.Unmarshal(body, &respManifest); err != nil {
		t.Errorf("failed to unmarshal manifest JSON: %v", err)
	}

	if respManifest.Version != manifest.Version {
		t.Errorf("expected version %d, got %d", manifest.Version, respManifest.Version)
	}
}

func TestManifestETag(t *testing.T) {
	manifest := SampleManifest()
	server := NewTestServer(t, manifest, map[string][]byte{})
	defer server.Close()

	// First request to get the ETag
	resp1, err := http.Get(server.URL + "/v1/manifest")
	if err != nil {
		t.Fatalf("failed to GET /v1/manifest: %v", err)
	}
	etag := resp1.Header.Get("ETag")
	resp1.Body.Close()

	if etag == "" {
		t.Fatal("expected ETag header to be present")
	}

	// Second request with If-None-Match
	req, err := http.NewRequest("GET", server.URL+"/v1/manifest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /v1/manifest with If-None-Match: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("expected status 304, got %d", resp2.StatusCode)
	}
}

func TestFileEndpoint(t *testing.T) {
	manifest := SampleManifest()
	fileContent := []byte("test file content")
	files := map[string][]byte{
		"blk00000.dat": fileContent,
	}
	server := NewTestServer(t, manifest, files)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/file/blk00000.dat")
	if err != nil {
		t.Fatalf("failed to GET /v1/file/blk00000.dat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("X-SHA256") == "" {
		t.Error("expected X-SHA256 header to be present")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if string(body) != string(fileContent) {
		t.Errorf("expected body %q, got %q", string(fileContent), string(body))
	}
}

func TestFileNotFound(t *testing.T) {
	manifest := SampleManifest()
	server := NewTestServer(t, manifest, map[string][]byte{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/file/nonexistent.dat")
	if err != nil {
		t.Fatalf("failed to GET /v1/file/nonexistent.dat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestFileRange(t *testing.T) {
	manifest := SampleManifest()
	fileContent := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	files := map[string][]byte{
		"blk00000.dat": fileContent,
	}
	server := NewTestServer(t, manifest, files)
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/v1/file/blk00000.dat", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Range", "bytes=0-9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /v1/file/blk00000.dat with Range: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("expected status 206, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	expected := []byte("0123456789")
	if string(body) != string(expected) {
		t.Errorf("expected body %q, got %q", string(expected), string(body))
	}

	if resp.Header.Get("Content-Range") == "" {
		t.Error("expected Content-Range header to be present")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && s[0:len(substr)] == substr) || (len(s) > len(substr) && s[len(s)-len(substr):] == substr) || (len(s) > len(substr) && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
