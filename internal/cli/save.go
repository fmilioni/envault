package cli

import "github.com/spf13/cobra"

func newSaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "save",
		Short: "Save the current folder's .env files into the vault",
		RunE:  notImplemented("save"),
	}
}
