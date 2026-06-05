package cli

import "github.com/spf13/cobra"

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Import a bundle into the vault",
		RunE:  notImplemented("import"),
	}
}
