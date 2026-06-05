package cli

import (
	"fmt"
	"os"

	"github.com/fmilioni/envault/internal/envctx"
	"github.com/spf13/cobra"
)

func newSaveCmd() *cobra.Command {
	var fileFlags []string
	var yes bool

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save the current folder's .env files into the vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSave(cmd, fileFlags, yes)
		},
	}
	cmd.Flags().StringArrayVar(&fileFlags, "file", nil, "env file to save (repeatable); skips the interactive picker")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "overwrite an existing snapshot without confirmation")
	return cmd
}

func runSave(cmd *cobra.Command, fileFlags []string, yes bool) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	project, err := envctx.InferProject(dir, flagProject)
	if err != nil {
		return err
	}
	stage := envctx.ResolveStage(flagStage)

	paths, err := resolveSavePaths(cmd, dir, fileFlags)
	if err != nil {
		return err
	}

	files, err := readFilesFromDir(dir, paths)
	if err != nil {
		return err
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	if exists, err := v.Exists(project, stage); err != nil {
		return err
	} else if exists {
		current, err := v.Load(project, stage)
		if err != nil {
			return err
		}
		if snapshotsEqual(current.Files, files) {
			fmt.Fprintf(cmd.OutOrStdout(), "Already up to date: %s/%s\n", project, stage)
			return nil
		}
		if !yes {
			ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
				fmt.Sprintf("Snapshot %s/%s already exists and differs. Overwrite?", project, stage))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
	}

	if err := v.Save(project, stage, files); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Saved %d file(s) to %s/%s:\n", len(files), project, stage)
	for _, f := range files {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.Path)
	}
	return nil
}

func resolveSavePaths(cmd *cobra.Command, dir string, fileFlags []string) ([]string, error) {
	if len(fileFlags) > 0 {
		return fileFlags, nil
	}
	candidates, err := envctx.DetectEnvFiles(dir)
	if err != nil {
		return nil, err
	}
	switch len(candidates) {
	case 0:
		return nil, fmt.Errorf("no .env files found in %s; pass --file to choose explicitly", dir)
	case 1:
		return candidates, nil
	default:
		return selectFiles(cmd.InOrStdin(), cmd.OutOrStdout(), candidates)
	}
}
