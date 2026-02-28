package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write serializes m to w as indented JSON.
func Write(w io.Writer, m *Manifest) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(m); err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}
	return nil
}

// WriteFile atomically writes m to path.
// It writes to a temp file in the same directory, then renames.
func WriteFile(path string, m *Manifest) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}

	// Create temp file in the same directory
	tmpFile, err := os.CreateTemp(dir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write manifest to temp file
	if err := Write(tmpFile, m); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write manifest to temp file: %w", err)
	}

	// Close temp file before rename
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomically rename temp file to target path
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to target path: %w", err)
	}

	return nil
}
