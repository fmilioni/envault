package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fmilioni/envault/internal/tui"
)

var (
	flagProject string
	flagStage   string
)

// Version is the build version shown by `envault --version`; main overrides it
// with the -ldflags-injected value.
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "envault",
		Short:        "Save and restore .env files — like git stash for your environment variables",
		Version:      Version,
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			v, err := openVault()
			if err != nil {
				return err
			}
			return tui.Run(v)
		},
	}

	root.PersistentFlags().StringVar(&flagProject, "project", "", "override the inferred project name")
	root.PersistentFlags().StringVar(&flagStage, "stage", "", `stage to operate on (default "default")`)

	root.AddCommand(newSaveCmd(), newLoadCmd(), newExportCmd(), newImportCmd())
	return root
}

func Execute() error {
	return newRootCmd().Execute()
}

func notImplemented(name string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "envault %s: not implemented yet\n", name)
		return nil
	}
}
