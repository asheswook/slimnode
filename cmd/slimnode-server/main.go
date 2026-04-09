package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	flags "github.com/jessevdk/go-flags"

	"github.com/asheswook/bitcoin-slimnode/internal/daemon"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	s3pkg "github.com/asheswook/bitcoin-slimnode/internal/s3"
	"github.com/asheswook/bitcoin-slimnode/internal/server"
)

type ManifestGenCmd struct {
	BlocksDir   string `long:"blocks-dir" description:"Bitcoin blocks directory" required:"true"`
	Output      string `long:"output" description:"Output manifest.json path" default:"manifest.json"`
	Chain       string `long:"chain" description:"Bitcoin chain" default:"mainnet"`
	BlockmapDir string `long:"blockmap-dir" description:"Directory containing blockmap files" default:""`
	SnapshotDir string `long:"snapshot-dir" description:"Directory containing snapshot files" default:""`
	Workers     int    `long:"workers" description:"Number of parallel hash workers (default: NumCPU)" default:"0"`
}

func (cmd *ManifestGenCmd) Execute(args []string) error {
	var opts []server.ManifestOption
	if cmd.BlockmapDir != "" {
		opts = append(opts, server.WithBlockmapDir(cmd.BlockmapDir))
	}
	if cmd.SnapshotDir != "" {
		opts = append(opts, server.WithSnapshotDir(cmd.SnapshotDir))
	}
	if cmd.Workers > 0 {
		opts = append(opts, server.WithWorkers(cmd.Workers))
	}
	m, err := server.GenerateManifest(cmd.BlocksDir, cmd.Chain, opts...)
	if err != nil {
		return fmt.Errorf("generate manifest: %w", err)
	}
	if err := manifest.WriteFile(cmd.Output, m); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Manifest written to %s (%d files)\n", cmd.Output, len(m.Files))
	return nil
}

func chainToNetworkMagic(chain string) (uint32, error) {
	switch chain {
	case "mainnet":
		return 0xD9B4BEF9, nil
	case "testnet", "testnet3":
		return 0x0709110B, nil
	case "signet":
		return 0x40CF030A, nil
	case "regtest":
		return 0xDAB5BFFA, nil
	case "testnet4":
		return 0x283F161C, nil
	default:
		return 0, fmt.Errorf("unknown chain: %s", chain)
	}
}

type BlockmapGenCmd struct {
	BlocksDir string `long:"blocks-dir" description:"Bitcoin blocks directory" required:"true"`
	Output    string `long:"output" description:"Output directory for blockmap files" default:"blockmaps/"`
	Chain     string `long:"chain" description:"Bitcoin chain" default:"mainnet"`
}

func (cmd *BlockmapGenCmd) Execute(args []string) error {
	networkMagic, err := chainToNetworkMagic(cmd.Chain)
	if err != nil {
		return err
	}

	hashes, err := server.GenerateBlockmaps(cmd.BlocksDir, cmd.Output, networkMagic)
	if err != nil {
		return fmt.Errorf("generate blockmaps: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Generated %d blockmaps in %s\n", len(hashes), cmd.Output)
	return nil
}

type SnapshotCmd struct {
	IndexDir string `long:"index-dir" description:"blocks/index directory" required:"true"`
	Output   string `long:"output" description:"Output .tar.zst path" required:"true"`
}

func (cmd *SnapshotCmd) Execute(args []string) error {
	return server.CreateBlocksIndexSnapshot(cmd.IndexDir, cmd.Output)
}

type ServeCmd struct {
	BlocksDir    string        `long:"blocks-dir" description:"Bitcoin blocks directory" required:"true"`
	ManifestPath string        `long:"manifest" description:"Path to manifest.json" default:"manifest.json"`
	Listen       string        `long:"listen" description:"Listen address" default:":8080"`
	BlockmapDir  string        `long:"blockmap-dir" description:"Directory containing blockmap files" default:""`
	SnapshotDir  string        `long:"snapshot-dir" description:"Directory containing snapshot files" default:""`
	Chain        string        `long:"chain" description:"Bitcoin chain (used when regenerating manifest)" default:"mainnet"`
	ScanInterval time.Duration `long:"scan-interval" description:"Interval for automatic manifest reload (0 disables)" default:"0"`
}

func (cmd *ServeCmd) Execute(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var opts []server.FileServerOption
	if cmd.Chain != "" {
		opts = append(opts, server.WithChain(cmd.Chain))
	}
	if cmd.ScanInterval > 0 {
		opts = append(opts, server.WithScanInterval(cmd.ScanInterval))
	}

	srv := server.NewFileServer(cmd.BlocksDir, cmd.ManifestPath, cmd.Listen, cmd.BlockmapDir, cmd.SnapshotDir, opts...)
	return srv.ListenAndServe(ctx)
}

// SyncCmd implements the `slimnode-server sync` subcommand. It periodically
// scans the blocks directory for finalized files, uploads them to S3, and
// updates the manifest.
type SyncCmd struct {
	BlocksDir    string        `long:"blocks-dir" required:"true" description:"Bitcoin blocks directory"`
	Chain        string        `long:"chain" default:"mainnet" description:"Bitcoin chain"`
	Bucket       string        `long:"bucket" required:"true" description:"S3 bucket name"`
	Endpoint     string        `long:"endpoint" description:"S3-compatible endpoint URL (for R2, DO Spaces, etc.)"`
	Region       string        `long:"region" default:"us-east-1" description:"S3 region"`
	BaseURL      string        `long:"base-url" required:"true" description:"CDN base URL for manifest"`
	ScanInterval time.Duration `long:"scan-interval" default:"10m" description:"Scan interval (e.g. 10m, 1h)"`
	BlockmapDir  string        `long:"blockmap-dir" description:"Local blockmap directory"`
	ManifestPath string        `long:"manifest" description:"Also write manifest to this local path (for serve)"`
	PathStyle    bool          `long:"path-style" description:"Use path-style S3 addressing"`
	StorageClass string        `long:"storage-class" default:"STANDARD_IA" description:"S3 storage class for block files (STANDARD, STANDARD_IA, etc.). Use STANDARD for Backblaze B2 or Wasabi."`
}

func (cmd *SyncCmd) Execute(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var s3Opts []s3pkg.Option
	if cmd.Endpoint != "" {
		s3Opts = append(s3Opts, s3pkg.WithEndpoint(cmd.Endpoint))
	}
	if cmd.Region != "" {
		s3Opts = append(s3Opts, s3pkg.WithRegion(cmd.Region))
	}
	if cmd.PathStyle {
		s3Opts = append(s3Opts, s3pkg.WithPathStyle(true))
	}
	if cmd.StorageClass != "" {
		s3Opts = append(s3Opts, s3pkg.WithStorageClass(cmd.StorageClass))
	}

	s3Client, err := s3pkg.New(ctx, cmd.Bucket, s3Opts...)
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}

	var syncerOpts []daemon.SyncerOption
	if cmd.BlockmapDir != "" {
		syncerOpts = append(syncerOpts, daemon.WithSyncerBlockmapDir(cmd.BlockmapDir))
	}
	if cmd.ManifestPath != "" {
		syncerOpts = append(syncerOpts, daemon.WithSyncerManifestPath(cmd.ManifestPath))
	}
	syncerOpts = append(syncerOpts, daemon.WithSyncerInterval(cmd.ScanInterval))

	syncer := daemon.NewSyncer(cmd.BlocksDir, cmd.Chain, cmd.BaseURL, s3Client, syncerOpts...)
	return syncer.Run(ctx)
}

type Options struct {
	ManifestGen ManifestGenCmd `command:"manifest-gen" description:"Generate manifest from blocks directory"`
	BlockmapGen BlockmapGenCmd `command:"blockmap-gen" description:"Generate blockmap files from blocks directory"`
	Snapshot    SnapshotCmd    `command:"snapshot" description:"Create blocks/index snapshot"`
	Serve       ServeCmd       `command:"serve" description:"Serve block files and manifest over HTTP"`
	Sync        SyncCmd        `command:"sync" description:"Sync finalized files to S3 and update manifest"`
}

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
