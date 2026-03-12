package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const daemonEnvKey = "_SLIMNODE_DAEMON"

// isDaemonChild returns true if this process was re-execed as a daemon child.
func isDaemonChild() bool {
	return os.Getenv(daemonEnvKey) == "1"
}

// daemonize re-executes the current process in the background.
// It writes the child PID to pidPath and redirects output to logPath.
func daemonize(pidPath, logPath string) error {
	if pid, err := readPID(pidPath); err == nil {
		return fmt.Errorf("slimnode already running (pid %d)", pid)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("resolving executable: %w", err)
	}

	args := filterBackgroundFlag(os.Args[1:])

	cmd := exec.Command(executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), daemonEnvKey+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting daemon: %w", err)
	}
	logFile.Close()

	if err := writePID(pidPath, cmd.Process.Pid); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}

	fmt.Printf("slimnode started (pid %d)\n", cmd.Process.Pid)
	fmt.Printf("  log: %s\n", logPath)
	fmt.Printf("  pid: %s\n", pidPath)

	cmd.Process.Release()
	return nil
}

// filterBackgroundFlag removes --background and -b from args so the
// re-execed child runs in foreground mode.
func filterBackgroundFlag(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--background" || arg == "-b" {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// writePID writes the process ID to the given file path.
func writePID(pidPath string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// readPID reads the PID from the given file and verifies the process is alive.
func readPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return 0, fmt.Errorf("process %d not running: %w", pid, err)
	}
	return pid, nil
}

// removePID removes the PID file.
func removePID(pidPath string) {
	os.Remove(pidPath)
}
