package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"
)

// Load parses configuration from a .conf file and command-line arguments.
// It uses two-pass parsing: first to extract --config path, then to load INI file,
// then full parse where CLI flags override INI values.
func Load(args []string) (*Config, error) {
	// Extract --config flag from args manually to avoid required field validation
	configPath := "~/.slimnode/config.conf"
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			break
		} else if strings.HasPrefix(arg, "--config=") {
			configPath = arg[9:]
			break
		}
	}

	// Expand ~ in ConfigFile path
	if strings.HasPrefix(configPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		configPath = filepath.Join(home, configPath[1:])
	}

	// Create the config with defaults
	cfg := &Config{
		ConfigFile: configPath,
		Compact: CompactConfig{
			PreDownload: true,
		},
	}

	// Create parser once and reuse it.
	// IgnoreUnknown: subcommand-specific flags (e.g. --foreground) are passed
	// via os.Args but don't belong to Config - ignore them here.
	parser := flags.NewParser(cfg, flags.Default|flags.IgnoreUnknown)

	// Load INI file (so CLI flags can override)
	if _, err := os.Stat(configPath); err == nil {
		iniParser := flags.NewIniParser(parser)
		iniParser.ParseAsDefaults = true
		err = iniParser.ParseFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse INI file %s: %w", configPath, err)
		}
	}

	// Parse CLI args (CLI flags override INI values)
	_, err := parser.ParseArgs(args)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	// Expand ~ in path fields
	if err := expandPaths(cfg); err != nil {
		return nil, err
	}

	// Validate required fields
	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// expandPaths expands ~ to absolute path in CacheDir and LocalDir.
func expandPaths(cfg *Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if strings.HasPrefix(cfg.General.CacheDir, "~") {
		cfg.General.CacheDir = filepath.Join(home, cfg.General.CacheDir[1:])
	}

	if strings.HasPrefix(cfg.General.LocalDir, "~") {
		cfg.General.LocalDir = filepath.Join(home, cfg.General.LocalDir[1:])
	}

	if strings.HasPrefix(cfg.General.BitcoinDataDir, "~") {
		cfg.General.BitcoinDataDir = filepath.Join(home, cfg.General.BitcoinDataDir[1:])
	}

	return nil
}

// validate checks that required fields are set.
func validate(cfg *Config) error {
	if cfg.General.MountPoint == "" {
		return fmt.Errorf("required field missing: mount-point")
	}
	if cfg.Server.URL == "" {
		return fmt.Errorf("required field missing: server.url")
	}
	if cfg.General.RemoteFetchMode != "auto" && cfg.General.RemoteFetchMode != "file" && cfg.General.RemoteFetchMode != "range" {
		return fmt.Errorf("invalid general.remote-fetch-mode %q: must be one of auto, file, range", cfg.General.RemoteFetchMode)
	}
	if cfg.General.AutoGapToleranceKB < 0 {
		return fmt.Errorf("invalid general.auto-gap-tolerance-kb %d: must be >= 0", cfg.General.AutoGapToleranceKB)
	}
	if cfg.General.AutoMinRangeRequests < 1 {
		return fmt.Errorf("invalid general.auto-min-range-requests %d: must be >= 1", cfg.General.AutoMinRangeRequests)
	}
	if cfg.General.AutoMinSequentialMB < 1 {
		return fmt.Errorf("invalid general.auto-min-sequential-mb %d: must be >= 1", cfg.General.AutoMinSequentialMB)
	}
	if cfg.General.AutoMinSequentialRate <= 0 || cfg.General.AutoMinSequentialRate > 1 {
		return fmt.Errorf("invalid general.auto-min-sequential-rate %f: must be in (0,1]", cfg.General.AutoMinSequentialRate)
	}
	if cfg.General.AutoMaxBackwardSeeks < 0 {
		return fmt.Errorf("invalid general.auto-max-backward-seeks %d: must be >= 0", cfg.General.AutoMaxBackwardSeeks)
	}
	if cfg.General.AutoFileHintTTL <= 0 {
		return fmt.Errorf("invalid general.auto-file-hint-ttl %s: must be > 0", cfg.General.AutoFileHintTTL)
	}
	if cfg.General.AutoPromotionCooldown <= 0 {
		return fmt.Errorf("invalid general.auto-promotion-cooldown %s: must be > 0", cfg.General.AutoPromotionCooldown)
	}
	return nil
}
