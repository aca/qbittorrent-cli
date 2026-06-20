package main

import (
	"context"

	"github.com/spf13/cobra"
)

func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <hash> ...",
		Short: "Pause torrents by hash on whichever instance holds them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			return cl.Pause(context.Background(), args)
		},
	}
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <hash> ...",
		Short: "Resume torrents by hash on whichever instance holds them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			return cl.Resume(context.Background(), args)
		},
	}
}

func newDeleteCmd() *cobra.Command {
	var deleteFiles bool
	cmd := &cobra.Command{
		Use:   "delete <hash> ...",
		Short: "Delete torrents by hash on whichever instance holds them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := buildCluster()
			if err != nil {
				return err
			}
			return cl.Delete(context.Background(), args, deleteFiles)
		},
	}
	cmd.Flags().BoolVar(&deleteFiles, "delete-files", false, "also delete downloaded files from disk")
	return cmd
}
