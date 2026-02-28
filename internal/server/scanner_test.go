package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/store"
)

func TestScanFinalizedFiles(t *testing.T) {
	createFile := func(t *testing.T, dir, name string, size int64) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, f.Close())
		require.NoError(t, os.Truncate(filepath.Join(dir, name), size))
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		wantNames []string
		wantErr   bool
	}{
		{
			name:      "empty directory returns empty slice",
			setup:     func(t *testing.T, dir string) {},
			wantNames: nil,
		},
		{
			name: "blk file at threshold is included",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold)
			},
			wantNames: []string{"blk00000.dat"},
		},
		{
			name: "blk file below threshold is excluded",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold-1)
			},
			wantNames: nil,
		},
		{
			name: "rev file included when corresponding blk is finalized",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold)
				createFile(t, dir, "rev00000.dat", 1024)
			},
			wantNames: []string{"blk00000.dat", "rev00000.dat"},
		},
		{
			name: "rev file excluded when corresponding blk is not finalized",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold-1)
				createFile(t, dir, "rev00000.dat", 1024)
			},
			wantNames: nil,
		},
		{
			name: "rev file excluded when no corresponding blk exists",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "rev00000.dat", 1024)
			},
			wantNames: nil,
		},
		{
			name: "results sorted by path",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00001.dat", store.FinalizedFileThreshold)
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold)
				createFile(t, dir, "rev00001.dat", 1024)
				createFile(t, dir, "rev00000.dat", 1024)
			},
			wantNames: []string{"blk00000.dat", "blk00001.dat", "rev00000.dat", "rev00001.dat"},
		},
		{
			name: "size is populated in result",
			setup: func(t *testing.T, dir string) {
				createFile(t, dir, "blk00000.dat", store.FinalizedFileThreshold)
			},
			wantNames: []string{"blk00000.dat"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			got, err := ScanFinalizedFiles(dir)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantNames == nil {
				assert.Empty(t, got)
				return
			}

			require.Len(t, got, len(tt.wantNames))
			for i, sf := range got {
				assert.Equal(t, tt.wantNames[i], sf.Name)
				assert.NotEmpty(t, sf.Path)
				assert.Positive(t, sf.Size)
			}
		})
	}

	t.Run("non-existent directory returns error", func(t *testing.T) {
		_, err := ScanFinalizedFiles("/nonexistent/path/that/does/not/exist")
		require.Error(t, err)
	})
}
