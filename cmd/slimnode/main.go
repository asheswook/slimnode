// Package main is the entry point for the slimnode command.
package main

import (
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/asheswook/bitcoin-lfn/internal/cmd"
)

type options struct {
	ConfigFile string `short:"c" long:"config" description:"Config file path" default:"~/.slimnode/config.conf"`

	Mount   cmd.MountCmd   `command:"mount" description:"Mount the SlimNode FUSE filesystem"`
	Init    cmd.InitCmd    `command:"init" description:"Initialize SlimNode (download snapshots, create config)"`
	Status  cmd.StatusCmd  `command:"status" description:"Show SlimNode status"`
	Compact cmd.CompactCmd `command:"compact" description:"Run manual compaction"`
}

func main() {
	var opts options
	parser := flags.NewParser(&opts, flags.Default)
	parser.LongDescription = "SlimNode: Bitcoin full node with 72% storage reduction via FUSE filesystem"

	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
