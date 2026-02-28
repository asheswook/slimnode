package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/asheswook/bitcoin-lfn/internal/cache"
	"github.com/asheswook/bitcoin-lfn/internal/config"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// StatusCmd implements the `slimnode status` subcommand.
type StatusCmd struct{}

// Execute runs the status command.
func (c *StatusCmd) Execute(args []string) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath := filepath.Join(cfg.General.CacheDir, "slimnode.db")
	st, err := store.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	maxBytes := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	ca, err := cache.New(cfg.General.CacheDir, maxBytes, cfg.Cache.MinKeepRecent, st)
	if err != nil {
		return fmt.Errorf("creating cache: %w", err)
	}

	counts := map[store.FileState]int{}
	for _, state := range []store.FileState{
		store.FileStateActive,
		store.FileStateLocalFinalized,
		store.FileStateCached,
		store.FileStateRemote,
	} {
		entries, _ := st.ListByState(state)
		counts[state] = len(entries)
	}

	used, total := ca.Usage()

	fmt.Printf("File States:\n")
	fmt.Printf("  ACTIVE:          %d\n", counts[store.FileStateActive])
	fmt.Printf("  LOCAL_FINALIZED: %d\n", counts[store.FileStateLocalFinalized])
	fmt.Printf("  CACHED:          %d\n", counts[store.FileStateCached])
	fmt.Printf("  REMOTE:          %d\n", counts[store.FileStateRemote])
	fmt.Printf("\nCache Usage: %d MB / %d MB\n", used/1024/1024, total/1024/1024)
	return nil
}
