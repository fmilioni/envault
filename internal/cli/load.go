package cli

import "github.com/spf13/cobra"

func newLoadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load",
		Short: "Restore a saved snapshot into the current folder",
		RunE:  notImplemented("load"),
	}
}
