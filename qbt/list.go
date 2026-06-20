package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		filter   string
		category string
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List torrents across all instances",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			opts := qbt.TorrentFilterOptions{
				Filter:   qbt.TorrentFilter(filter),
				Category: category,
			}
			torrents, err := cl.ListAll(context.Background(), opts)
			if err != nil {
				return err
			}
			sort.Slice(torrents, func(i, j int) bool {
				if torrents[i].Instance != torrents[j].Instance {
					return torrents[i].Instance < torrents[j].Instance
				}
				return torrents[i].Name < torrents[j].Name
			})
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(torrents)
			}
			printTable(torrents)
			return nil
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "state filter (e.g. downloading, seeding, paused, all)")
	cmd.Flags().StringVar(&category, "category", "", "only torrents in this category")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON instead of a table")
	return cmd
}

func printTable(torrents []AnnotatedTorrent) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INSTANCE\tNAME\tSTATE\tPROGRESS\tSIZE\tDL\tUP\tRATIO\tHASH")
	for _, t := range torrents {
		fmt.Fprintf(w, "%s\t%s\t%s\t%.0f%%\t%s\t%s/s\t%s/s\t%.2f\t%s\n",
			t.Instance,
			truncate(t.Name, 50),
			t.State,
			t.Progress*100,
			humanize.IBytes(uint64(t.Size)),
			humanize.IBytes(uint64(t.DlSpeed)),
			humanize.IBytes(uint64(t.UpSpeed)),
			t.Ratio,
			t.Hash,
		)
	}
	w.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
