package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		category string
		paused   bool
	)
	cmd := &cobra.Command{
		Use:   "add <magnet|url|file.torrent> ...",
		Short: "Add torrents, routing to the instance with the most free disk space",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			ctx := context.Background()

			target, err := cl.leastLoaded(ctx, instance)
			if err != nil {
				return err
			}

			opts := map[string]string{}
			if category != "" {
				opts["category"] = category
			}
			if paused {
				opts["paused"] = "true"
				opts["stopped"] = "true" // qbit 5.x renamed paused->stopped
			}

			var firstErr error
			for _, arg := range args {
				if err := addOne(ctx, target, arg, opts); err != nil {
					warnf("add %q on %q: %v", arg, target.name, err)
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				fmt.Printf("added %q to %q\n", arg, target.name)
			}
			return firstErr
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "assign added torrents to this category")
	cmd.Flags().BoolVar(&paused, "paused", false, "add in paused/stopped state")
	return cmd
}

// addOne dispatches a single argument: a magnet/http(s) URL goes through the URL
// endpoint, an existing local path is uploaded as a .torrent file.
func addOne(ctx context.Context, target node, arg string, opts map[string]string) error {
	if isURL(arg) {
		_, err := target.client.AddTorrentFromUrlCtx(ctx, arg, copyOpts(opts))
		return err
	}
	if _, statErr := os.Stat(arg); statErr == nil {
		_, err := target.client.AddTorrentFromFileCtx(ctx, arg, copyOpts(opts))
		return err
	}
	return fmt.Errorf("not a magnet/URL and not an existing file")
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "magnet:") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://")
}

// copyOpts returns a fresh map so the library mutating it per-call cannot leak
// between arguments.
func copyOpts(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
