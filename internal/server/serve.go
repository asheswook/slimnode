package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
)

// FileServer serves block files and manifests over HTTP.
// It implements the API contract expected by the slimnode remote client:
//
//	GET /v1/health       → {"status":"ok"}
//	GET /v1/manifest     → manifest JSON with ETag support
//	GET /v1/file/{name}  → file content with X-SHA256 header and Range support
//	GET /v1/blockmap/{name} → blockmap file content (binary)
//	GET /v1/snapshot/{name} → snapshot file content with X-SHA256 header
type FileServer struct {
	blocksDir    string
	manifestPath string
	listenAddr   string
	blockmapDir  string
	snapshotDir  string
	chain        string
	scanInterval time.Duration // 0 means no auto-reload
	logger       *slog.Logger

	mu             sync.RWMutex
	manifestJSON   []byte
	manifestETag   string
	snapshotHashes map[string]string // filename → SHA-256, loaded from manifest
}

// FileServerOption configures FileServer behaviour.
type FileServerOption func(*FileServer)

// WithChain sets the chain name used when regenerating the manifest during
// auto-reload. Defaults to "mainnet" if not set.
func WithChain(chain string) FileServerOption {
	return func(s *FileServer) {
		s.chain = chain
	}
}

// WithScanInterval enables periodic manifest auto-reload. On each tick
// ScanFinalizedFiles is called; if the set of files has changed the
// manifest is regenerated and reloaded. A zero or negative value disables
// auto-reload.
func WithScanInterval(d time.Duration) FileServerOption {
	return func(s *FileServer) {
		s.scanInterval = d
	}
}

// NewFileServer creates a FileServer.
func NewFileServer(blocksDir, manifestPath, listenAddr, blockmapDir, snapshotDir string, opts ...FileServerOption) *FileServer {
	s := &FileServer{
		blocksDir:    blocksDir,
		manifestPath: manifestPath,
		listenAddr:   listenAddr,
		blockmapDir:  blockmapDir,
		snapshotDir:  snapshotDir,
		chain:        "mainnet",
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *FileServer) loadManifest() error {
	data, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("invalid manifest JSON: %w", err)
	}

	h := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(h[:]) + `"`

	hashes := make(map[string]string)
	if m.Snapshots.BlocksIndex.URL != "" {
		name := filepath.Base(m.Snapshots.BlocksIndex.URL)
		hashes[name] = m.Snapshots.BlocksIndex.SHA256
	}
	if m.Snapshots.UTXO.URL != "" {
		name := filepath.Base(m.Snapshots.UTXO.URL)
		hashes[name] = m.Snapshots.UTXO.SHA256
	}

	s.mu.Lock()
	s.manifestJSON = data
	s.manifestETag = etag
	s.snapshotHashes = hashes
	s.mu.Unlock()

	return nil
}

// ListenAndServe starts the HTTP server. It blocks until ctx is cancelled.
func (s *FileServer) ListenAndServe(ctx context.Context) error {
	if err := s.loadManifest(); err != nil {
		return err
	}

	if s.scanInterval > 0 {
		go s.runAutoReload(ctx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/manifest", s.handleManifest)
	mux.HandleFunc("GET /v1/file/{filename}", s.handleFile)
	mux.HandleFunc("GET /v1/blockmap/{filename}", s.handleBlockmap)
	mux.HandleFunc("GET /v1/snapshot/{filename}", s.handleSnapshot)

	srv := &http.Server{
		Addr:    s.listenAddr,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("serving", "addr", s.listenAddr, "blocks", s.blocksDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

func (s *FileServer) runAutoReload(ctx context.Context) {
	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	var lastFiles []string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.maybeReloadManifest(&lastFiles)
		}
	}
}

func (s *FileServer) maybeReloadManifest(lastFiles *[]string) {
	scanned, err := ScanFinalizedFiles(s.blocksDir)
	if err != nil {
		s.logger.Error("auto-reload: scan finalized files", "err", err)
		return
	}

	current := make([]string, len(scanned))
	for i, f := range scanned {
		current[i] = f.Name
	}

	if slicesEqual(current, *lastFiles) {
		return
	}

	// Parse current manifest JSON to use as cache for incremental hashing.
	// Finalized block files are immutable, so SHA-256 hashes from the previous
	// manifest remain valid as long as file name and size match.
	// On parse failure, proceed without cache (full rehash).
	var prev *manifest.Manifest
	s.mu.RLock()
	jsonCopy := s.manifestJSON
	s.mu.RUnlock()
	if len(jsonCopy) > 0 {
		if p, err := manifest.Parse(bytes.NewReader(jsonCopy)); err == nil {
			prev = p
		}
	}

	var opts []ManifestOption
	if prev != nil {
		opts = append(opts, WithPreviousManifest(prev))
	}
	if s.blockmapDir != "" {
		opts = append(opts, WithBlockmapDir(s.blockmapDir))
	}
	if s.snapshotDir != "" {
		opts = append(opts, WithSnapshotDir(s.snapshotDir))
	}

	m, err := GenerateManifest(s.blocksDir, s.chain, opts...)
	if err != nil {
		s.logger.Error("auto-reload: generate manifest", "err", err)
		return
	}

	if err := manifest.WriteFile(s.manifestPath, m); err != nil {
		s.logger.Error("auto-reload: write manifest", "err", err)
		return
	}

	if err := s.loadManifest(); err != nil {
		s.logger.Error("auto-reload: load manifest", "err", err)
		return
	}

	s.logger.Info("manifest reloaded", "files", len(scanned))
	*lastFiles = current
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *FileServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *FileServer) handleManifest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	data := s.manifestJSON
	etag := s.manifestETag
	s.mu.RUnlock()

	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

func (s *FileServer) handleFile(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")

	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.blocksDir, filename)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("open file", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		s.logger.Error("stat file", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		s.logger.Error("hash file", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-SHA256", hex.EncodeToString(h.Sum(nil)))

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		s.logger.Error("seek file", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func (s *FileServer) handleBlockmap(w http.ResponseWriter, r *http.Request) {
	if s.blockmapDir == "" {
		http.Error(w, "blockmaps not available", http.StatusNotFound)
		return
	}

	filename := r.PathValue("filename")

	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	bmPath := filepath.Join(s.blockmapDir, filename+".blockmap")
	http.ServeFile(w, r, bmPath)
}

func (s *FileServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshotDir == "" {
		http.Error(w, "snapshots not available", http.StatusNotFound)
		return
	}

	filename := r.PathValue("filename")

	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.snapshotDir, filename)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("open snapshot", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		s.logger.Error("stat snapshot", "file", filename, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.mu.RLock()
	cachedHash := s.snapshotHashes[filename]
	s.mu.RUnlock()

	if cachedHash != "" {
		w.Header().Set("X-SHA256", cachedHash)
	}

	http.ServeContent(w, r, filename, info.ModTime(), f)
}
