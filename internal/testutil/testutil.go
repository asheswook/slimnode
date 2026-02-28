package testutil

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TempDir creates a temporary directory and registers cleanup with t.Cleanup.
func TempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "testutil-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// TempFile creates a temporary file in a temp directory with the given content.
// Returns the full path to the file.
func TempFile(t *testing.T, name string, content []byte) string {
	dir := TempDir(t)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write temp file %s: %v", path, err)
	}
	return path
}

// MustReadFile reads a file and calls t.Fatal on error.
func MustReadFile(t *testing.T, path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	return data
}

// RandomBytes generates random bytes of the given size.
func RandomBytes(t *testing.T, size int) []byte {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		t.Fatalf("failed to generate random bytes: %v", err)
	}
	return data
}

// SHA256Hex returns the hex-encoded SHA-256 hash of the given data.
func SHA256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
