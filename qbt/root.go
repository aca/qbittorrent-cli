package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgPath  string
	instance string // --instance scope, shared by subcommands
	verbose  bool   // -v: emit debug logs to stderr
)

// warnSink, when non-nil, receives warning messages instead of stderr. The TUI
// sets it so per-instance warnings show in the status bar rather than corrupting
// the alternate screen. It may be called from fan-out goroutines, so it must be
// safe for concurrent use.
var warnSink func(string)

// warnf reports a non-fatal warning, to the sink if set, else stderr.
func warnf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if warnSink != nil {
		warnSink(msg)
		return
	}
	fmt.Fprintln(os.Stderr, "warning: "+msg)
}

// debugf prints a debug line to stderr when --verbose is set.
func debugf(format string, a ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "debug: "+format+"\n", a...)
	}
}

// clientLogger returns a *log.Logger wired to stderr when --verbose is set, so
// the qbittorrent library logs its HTTP activity. Returns nil otherwise (the
// library then discards its logs).
func clientLogger() *log.Logger {
	if !verbose {
		return nil
	}
	return log.New(os.Stderr, "qbt-http: ", log.LstdFlags)
}

// buildCluster loads config and constructs the cluster, honoring --instance.
func buildCluster() (*Cluster, error) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	return newCluster(cfg, instance)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "qbt",
		Short:         "Drive multiple qBittorrent instances as one",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// No subcommand → launch the interactive TUI.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config.json (default: $XDG_CONFIG_HOME/qbt/config.json)")
	root.PersistentFlags().StringVar(&instance, "instance", "", "scope the command to a single named instance")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "log debug info (incl. qbittorrent HTTP) to stderr")

	root.AddCommand(newListCmd(), newAddCmd(), newPauseCmd(), newResumeCmd(), newDeleteCmd(), newDedupCmd(), newTuiCmd())
	return root
}
