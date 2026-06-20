package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/spf13/cobra"
)

// victim is a duplicate copy slated for deletion.
type victim struct {
	instance string
	hash     string
	name     string
	progress float64
}

func newDedupCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "dedup",
		Short: "Find torrents duplicated across instances and delete the lower-progress copies (with files)",
		Long: "A torrent (identified by infohash) present on more than one instance is a duplicate.\n" +
			"dedup keeps the copy with the highest progress and deletes the rest together with\n" +
			"their files. By default it only prints the plan; pass --yes to actually delete.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			ctx := context.Background()

			torrents, err := cl.ListAll(ctx, qbt.TorrentFilterOptions{})
			if err != nil {
				return err
			}

			plan := planDedup(torrents)
			if len(plan) == 0 {
				fmt.Println("no duplicates found")
				return nil
			}

			for _, v := range plan {
				fmt.Printf("delete  %-12s  %5.0f%%  %s  %q\n",
					v.instance, v.progress*100, shortHash(v.hash), truncate(v.name, 50))
			}

			if !yes {
				fmt.Printf("\n(dry-run) %d copy(ies) would be deleted with files. Re-run with --yes to proceed.\n", len(plan))
				return nil
			}

			var failed int
			for _, v := range plan {
				if err := cl.deleteOn(ctx, v.instance, []string{v.hash}, true); err != nil {
					warnf("delete %s on %q: %v", shortHash(v.hash), v.instance, err)
					failed++
				}
			}
			fmt.Printf("deleted %d/%d duplicate copy(ies) with files\n", len(plan)-failed, len(plan))
			if failed > 0 {
				return fmt.Errorf("%d deletion(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "actually delete (default is a dry-run preview)")
	return cmd
}

// planDedup groups torrents by infohash and, for every hash present on more than
// one instance, returns all copies except the highest-progress one as victims.
func planDedup(torrents []AnnotatedTorrent) []victim {
	groups := map[string][]AnnotatedTorrent{}
	for _, t := range torrents {
		h := strings.ToLower(t.Hash)
		groups[h] = append(groups[h], t)
	}

	// Deterministic order over duplicate hashes.
	var hashes []string
	for h, g := range groups {
		if len(g) > 1 {
			hashes = append(hashes, h)
		}
	}
	sort.Strings(hashes)

	var plan []victim
	for _, h := range hashes {
		g := groups[h]
		// Keep the highest progress; tiebreak by more bytes downloaded, then
		// instance name for stability.
		sort.Slice(g, func(i, j int) bool {
			if g[i].Progress != g[j].Progress {
				return g[i].Progress > g[j].Progress
			}
			if g[i].Downloaded != g[j].Downloaded {
				return g[i].Downloaded > g[j].Downloaded
			}
			return g[i].Instance < g[j].Instance
		})
		for _, v := range g[1:] {
			plan = append(plan, victim{
				instance: v.Instance,
				hash:     v.Hash,
				name:     v.Name,
				progress: v.Progress,
			})
		}
	}
	return plan
}

func shortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}
