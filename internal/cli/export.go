package cli

import "github.com/spf13/cobra"

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export projects/stages as a portable bundle",
		RunE:  notImplemented("export"),
	}
}
