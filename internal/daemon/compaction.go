package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// CompactTrigger controls how compaction is triggered.
type CompactTrigger string

const (
	TriggerAuto   CompactTrigger = "auto"
	TriggerManual CompactTrigger = "manual"
)

const (
	stateInProgress = "in_progress"
	stateCompleted  = "completed"
)

// CompactionManager handles the compaction procedure: downloading snapshots,
// replacing local files, and crash recovery.
type CompactionManager struct {
	rc        ManifestFetcher
	st        store.Store
	manifest  func() *manifest.Manifest
	localDir  string
	cacheDir  string
	backupDir string
	stateFile string
	threshold int
	logger    *slog.Logger
}

// NewCompactionManager creates a CompactionManager.
func NewCompactionManager(
	rc ManifestFetcher,
	st store.Store,
	manifestFn func() *manifest.Manifest,
	localDir, cacheDir, backupDir, stateFile string,
	threshold int,
) *CompactionManager {
	return &CompactionManager{
		rc:        rc,
		st:        st,
		manifest:  manifestFn,
		localDir:  localDir,
		cacheDir:  cacheDir,
		backupDir: backupDir,
		stateFile: stateFile,
		threshold: threshold,
		logger:    slog.Default(),
	}
}

// Run runs compaction according to the trigger mode until ctx is cancelled.
func (m *CompactionManager) Run(ctx context.Context, trigger CompactTrigger) error {
	switch trigger {
	case TriggerManual:
		return m.Compact(ctx)
	default:
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := m.Compact(ctx); err != nil {
					m.logger.Error("auto compaction failed", "err", err)
				}
			}
		}
	}
}

// Compact executes the full compaction procedure.
func (m *CompactionManager) Compact(ctx context.Context) error {
	mf := m.manifest()
	if mf == nil {
		return errors.New("no manifest available")
	}

	if mf.Snapshots.BlocksIndex.URL == "" {
		return errors.New("no snapshot available in manifest")
	}

	covered, err := m.coveredFiles(mf)
	if err != nil {
		return fmt.Errorf("listing covered files: %w", err)
	}
	if len(covered) == 0 {
		m.logger.Info("no files to compact")
		return nil
	}

	if err := m.writeStateFile(stateInProgress); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	if err := m.replaceFiles(ctx, covered); err != nil {
		return fmt.Errorf("replacing files: %w", err)
	}

	if err := m.writeStateFile(stateCompleted); err != nil {
		m.logger.Error("failed to write completed state", "err", err)
	}

	m.logger.Info("compaction complete", "files_removed", len(covered))
	return nil
}

func (m *CompactionManager) coveredFiles(mf *manifest.Manifest) ([]store.FileEntry, error) {
	snapshotHeight := mf.Snapshots.BlocksIndex.Height
	if snapshotHeight == 0 {
		return nil, nil
	}

	entries, err := m.st.ListByState(store.FileStateLocalFinalized)
	if err != nil {
		return nil, err
	}

	var covered []store.FileEntry
	for _, e := range entries {
		if e.HeightLast > 0 && e.HeightLast <= snapshotHeight {
			covered = append(covered, e)
		}
	}
	return covered, nil
}

func (m *CompactionManager) replaceFiles(ctx context.Context, covered []store.FileEntry) error {
	for _, e := range covered {
		localPath := filepath.Join(m.localDir, e.Filename)
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			m.logger.Error("failed to remove local file", "file", e.Filename, "err", err)
			continue
		}

		e.State = store.FileStateRemote
		e.Source = store.FileSourceServer
		if err := m.st.UpsertFile(&e); err != nil {
			m.logger.Error("failed to update file state", "file", e.Filename, "err", err)
		}
	}
	return nil
}

func (m *CompactionManager) writeStateFile(state string) error {
	tmp := m.stateFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(state), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, m.stateFile)
}

// RecoverFromCrash checks the compaction state file and recovers if needed.
func (m *CompactionManager) RecoverFromCrash() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	state := strings.TrimSpace(string(data))
	switch state {
	case stateCompleted:
		return os.Remove(m.stateFile)

	case stateInProgress:
		m.logger.Warn("detected incomplete compaction, attempting recovery")
		if err := m.restoreBackup(); err != nil {
			m.logger.Error("backup restore failed", "err", err)
		}
		return os.Remove(m.stateFile)

	default:
		m.logger.Warn("unknown compaction state, removing state file", "state", state)
		return os.Remove(m.stateFile)
	}
}

func (m *CompactionManager) restoreBackup() error {
	if _, err := os.Stat(m.backupDir); os.IsNotExist(err) {
		return nil
	}
	m.logger.Info("backup directory found, recovery complete")
	return nil
}
