package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mfst "github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

// NewTestServer creates a test HTTP server with manifest and file endpoints.
func NewTestServer(t *testing.T, manifest *mfst.Manifest, files map[string][]byte) *httptest.Server {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Manifest endpoint with ETag support
	manifestJSON := mustMarshalJSON(manifest)
	manifestETag := SHA256Hex(manifestJSON)

	mux.HandleFunc("GET /v1/manifest", func(w http.ResponseWriter, r *http.Request) {
		if match := r.Header.Get("If-None-Match"); match == manifestETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", manifestETag)
		w.Write(manifestJSON)
	})

	// File endpoint with Range support
	mux.HandleFunc("GET /v1/file/{filename}", func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		data, ok := files[filename]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("X-SHA256", SHA256Hex(data))

		// Handle Range header
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			if start, end, ok := parseRange(rangeHeader, int64(len(data))); ok {
				partialData := data[start : end+1]
				w.Header().Set("Content-Length", strconv.FormatInt(int64(len(partialData)), 10))
				w.Header().Set("Content-Range", formatContentRange(start, end, int64(len(data))))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(partialData)
				return
			}
		}

		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// parseRange parses a Range header and returns start and end indices (inclusive).
// Returns false if the range is invalid.
func parseRange(rangeHeader string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || end < start || end >= size {
		return 0, 0, false
	}

	return start, end, true
}

func formatContentRange(start, end, size int64) string {
	return "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(size, 10)
}
