package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/asheswook/bitcoin-slimnode/internal/config"
)

func configureLoggingFromArgs(args []string, fallback io.Writer) error {
	cfg, err := config.Load(args)
	if err != nil {
		return fmt.Errorf("loading config for logging: %w", err)
	}
	return configureLogging(cfg.General.LogLevel, fallback)
}

func configureLogging(level string, fallback io.Writer) error {
	parsedLevel, err := parseLogLevel(level)
	if err != nil {
		return err
	}

	out := fallback
	if out == nil {
		out = os.Stderr
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: parsedLevel})
	slog.SetDefault(slog.New(handler))
	return nil
}

func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid general.log-level %q: must be one of debug, info, warn, error", level)
	}
}
