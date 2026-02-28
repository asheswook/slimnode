package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ManifestDiff represents the difference between two manifests.
type ManifestDiff struct {
	Added   []ManifestFile // files in new but not in old
	Removed []ManifestFile // files in old but not in new
	Changed []ManifestFile // files in both but with different SHA256 or Size
}

// Parse decodes a manifest from r.
func Parse(r io.Reader) (*Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}
	return &m, nil
}

// ParseFile reads and parses a manifest from the given file path.
func ParseFile(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest file: %w", err)
	}
	defer f.Close()

	return Parse(f)
}

// Diff computes the difference between two manifests.
// old may be nil (treated as empty manifest).
func Diff(old, new *Manifest) ManifestDiff {
	diff := ManifestDiff{
		Added:   []ManifestFile{},
		Removed: []ManifestFile{},
		Changed: []ManifestFile{},
	}

	if new == nil {
		new = &Manifest{}
	}
	if old == nil {
		old = &Manifest{}
	}

	// Build maps for efficient lookup
	oldMap := make(map[string]*ManifestFile)
	for i := range old.Files {
		oldMap[old.Files[i].Name] = &old.Files[i]
	}

	newMap := make(map[string]*ManifestFile)
	for i := range new.Files {
		newMap[new.Files[i].Name] = &new.Files[i]
	}

	// Find added and changed files
	for _, newFile := range new.Files {
		if oldFile, exists := oldMap[newFile.Name]; !exists {
			// File is in new but not in old -> Added
			diff.Added = append(diff.Added, newFile)
		} else {
			// File exists in both, check if changed
			if oldFile.SHA256 != newFile.SHA256 || oldFile.Size != newFile.Size {
				diff.Changed = append(diff.Changed, newFile)
			}
		}
	}

	// Find removed files
	for _, oldFile := range old.Files {
		if _, exists := newMap[oldFile.Name]; !exists {
			// File is in old but not in new -> Removed
			diff.Removed = append(diff.Removed, oldFile)
		}
	}

	return diff
}

// FindFile returns the ManifestFile with the given name, or nil if not found.
func FindFile(m *Manifest, name string) *ManifestFile {
	if m == nil {
		return nil
	}
	for i := range m.Files {
		if m.Files[i].Name == name {
			return &m.Files[i]
		}
	}
	return nil
}

// LastFileNumber returns the highest blk file number found in the manifest.
// For example, "blk00100.dat" returns 100. Returns -1 if no blk files found.
func LastFileNumber(m *Manifest) int {
	if m == nil {
		return -1
	}

	maxNum := -1
	for _, file := range m.Files {
		// Check if filename matches pattern blkXXXXX.dat
		if strings.HasPrefix(file.Name, "blk") && strings.HasSuffix(file.Name, ".dat") {
			// Extract the number part (e.g., "blk00100.dat" -> "00100")
			numStr := file.Name[3 : len(file.Name)-4]
			if num, err := strconv.Atoi(numStr); err == nil {
				if num > maxNum {
					maxNum = num
				}
			}
		}
	}

	return maxNum
}
