package config

import "time"

// Config holds all slimnode configuration.
type Config struct {
	ConfigFile string `short:"c" long:"config" description:"Config file path" default:"~/.slimnode/config.conf"`

	General GeneralConfig `group:"general" namespace:"general"`
	Cache   CacheConfig   `group:"cache" namespace:"cache"`
	Server  ServerConfig  `group:"server" namespace:"server"`
	Compact CompactConfig `group:"compaction" namespace:"compaction"`
}

// GeneralConfig holds general slimnode settings.
type GeneralConfig struct {
	Chain                 string        `long:"chain" description:"Bitcoin chain (mainnet, testnet, testnet4, signet, regtest)" default:"mainnet"`
	CacheDir              string        `long:"cache-dir" description:"Cache directory for remote files" default:"~/.slimnode/cache"`
	LocalDir              string        `long:"local-dir" description:"Directory for local active files" default:"~/.slimnode/local"`
	MountPoint            string        `short:"m" long:"mount-point" description:"FUSE mount point"`
	BitcoinDataDir        string        `long:"bitcoin-datadir" description:"Bitcoin Core data directory (for blocks/index symlink)" default:"~/.bitcoin"`
	LogLevel              string        `long:"log-level" description:"Log level (debug, info, warn, error)" default:"info"`
	RemoteFetchMode       string        `long:"remote-fetch-mode" description:"REMOTE read mode (auto, file, range)" default:"auto"`
	AutoGapToleranceKB    int           `long:"auto-gap-tolerance-kb" description:"AUTO mode: max forward gap treated as sequential in KB" default:"64"`
	AutoMinRangeRequests  int           `long:"auto-min-range-requests" description:"AUTO mode: minimum range requests before file promotion" default:"256"`
	AutoMinSequentialMB   int           `long:"auto-min-sequential-mb" description:"AUTO mode: minimum sequential bytes before file promotion in MB" default:"4"`
	AutoMinSequentialRate float64       `long:"auto-min-sequential-rate" description:"AUTO mode: minimum sequential byte ratio (0,1] for promotion" default:"0.90"`
	AutoMaxBackwardSeeks  int           `long:"auto-max-backward-seeks" description:"AUTO mode: maximum backward seeks allowed for promotion" default:"2"`
	AutoFileHintTTL       time.Duration `long:"auto-file-hint-ttl" description:"AUTO mode: duration to prefer full-file after promotion" default:"10m"`
	AutoPromotionCooldown time.Duration `long:"auto-promotion-cooldown" description:"AUTO mode: cooldown after promotion failure" default:"30s"`
}

// CacheConfig holds cache-related settings.
type CacheConfig struct {
	MaxSizeGB     int `long:"max-size-gb" description:"Max cache size in GB" default:"50"`
	MinKeepRecent int `long:"min-keep-recent" description:"Min recent cached files to keep" default:"10"`
}

// ServerConfig holds archive server settings.
type ServerConfig struct {
	URL            string        `long:"url" description:"Archive server base URL"`
	RequestTimeout time.Duration `long:"request-timeout" description:"HTTP request timeout" default:"30s"`
	RetryCount     int           `long:"retry-count" description:"HTTP retry count on failure" default:"3"`
}

// CompactConfig holds compaction settings.
type CompactConfig struct {
	Trigger     string `long:"trigger" description:"Compaction trigger (auto, scheduled, manual)" default:"auto"`
	Threshold   int    `long:"threshold" description:"Auto trigger: storage usage % threshold" default:"85"`
	PreDownload bool   `long:"pre-download" description:"Pre-download snapshots before compaction"`
}
