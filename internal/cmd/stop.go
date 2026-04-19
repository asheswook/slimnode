package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/asheswook/bitcoin-slimnode/internal/config"
)

const stopTimeout = 3 * time.Second
const stopPollInterval = 100 * time.Millisecond

// StopCmd implements the `slimnode stop` subcommand.
type StopCmd struct{}

// Execute sends SIGTERM to the running slimnode daemon and waits for it to exit.
func (c *StopCmd) Execute(args []string) error {
	if err := configureLoggingFromArgs(os.Args[1:], os.Stderr); err != nil {
		return err
	}

	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	pidPath := filepath.Join(filepath.Dir(cfg.ConfigFile), "slimnode.pid")
	pid, err := readPID(pidPath)
	if err != nil {
		return fmt.Errorf("slimnode is not running")
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		removePID(pidPath)
		return fmt.Errorf("sending signal: %w", err)
	}

	deadline := time.After(stopTimeout)
	ticker := time.NewTicker(stopPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("slimnode (pid %d) did not stop within %s", pid, stopTimeout)
		case <-ticker.C:
			if err := syscall.Kill(pid, 0); err != nil {
				removePID(pidPath)
				fmt.Println("slimnode stopped")
				return nil
			}
		}
	}
}
