package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilterBackgroundFlag tests that --background and -b flags are removed
func TestFilterBackgroundFlag(t *testing.T) {
	// Test long form
	args := []string{"mount", "--background", "--config", "foo"}
	result := filterBackgroundFlag(args)
	expected := []string{"mount", "--config", "foo"}
	assert.Equal(t, expected, result)

	// Test short form
	args = []string{"mount", "-b", "--config", "foo"}
	result = filterBackgroundFlag(args)
	expected = []string{"mount", "--config", "foo"}
	assert.Equal(t, expected, result)
}

// TestFilterBackgroundFlag_NoFlag tests that args without background flag are unchanged
func TestFilterBackgroundFlag_NoFlag(t *testing.T) {
	args := []string{"mount", "--config", "foo"}
	result := filterBackgroundFlag(args)
	expected := []string{"mount", "--config", "foo"}
	assert.Equal(t, expected, result)
}

// TestIsDaemonChild_Set tests isDaemonChild returns true when env var is set
func TestIsDaemonChild_Set(t *testing.T) {
	t.Setenv(daemonEnvKey, "1")
	assert.True(t, isDaemonChild())
}

// TestIsDaemonChild_Unset tests isDaemonChild returns false when env var is unset
func TestIsDaemonChild_Unset(t *testing.T) {
	t.Setenv(daemonEnvKey, "")
	assert.False(t, isDaemonChild())
}

// TestWriteAndReadPID tests writing and reading a PID file
func TestWriteAndReadPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	currentPID := os.Getpid()
	err := writePID(pidPath, currentPID)
	require.NoError(t, err)

	readPIDValue, err := readPID(pidPath)
	require.NoError(t, err)
	assert.Equal(t, currentPID, readPIDValue)
}

// TestReadPID_StalePID tests that readPID returns error for non-existent process
func TestReadPID_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "stale.pid")

	// Write a very high PID that's unlikely to exist
	stalePID := 999999999
	err := writePID(pidPath, stalePID)
	require.NoError(t, err)

	_, err = readPID(pidPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// TestReadPID_InvalidContent tests that readPID returns error for invalid content
func TestReadPID_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "invalid.pid")

	err := os.WriteFile(pidPath, []byte("notanumber"), 0644)
	require.NoError(t, err)

	_, err = readPID(pidPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PID file")
}

// TestReadPID_NoFile tests that readPID returns error for non-existent file
func TestReadPID_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	_, err := readPID(pidPath)
	require.Error(t, err)
}

// TestRemovePID tests that removePID deletes the PID file
func TestRemovePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "remove.pid")

	currentPID := os.Getpid()
	err := writePID(pidPath, currentPID)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(pidPath)
	require.NoError(t, err)

	// Remove the PID file
	removePID(pidPath)

	// Verify file no longer exists
	_, err = os.Stat(pidPath)
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

// TestDaemonize_AlreadyRunning tests that daemonize returns error when process already running
func TestDaemonize_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")
	logPath := filepath.Join(tmpDir, "daemon.log")

	// Write current process PID to simulate already running daemon
	currentPID := os.Getpid()
	err := writePID(pidPath, currentPID)
	require.NoError(t, err)

	// Attempt to daemonize should fail
	err = daemonize(pidPath, logPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
	assert.Contains(t, err.Error(), strconv.Itoa(currentPID))
}

