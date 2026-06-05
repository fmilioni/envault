package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fmilioni/envault/internal/envctx"
	"github.com/fmilioni/envault/internal/vault"
	"github.com/spf13/cobra"
)

func newLoadCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Restore a saved snapshot into the current folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLoad(cmd, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "overwrite differing files without confirmation")
	return cmd
}

func runLoad(cmd *cobra.Command, yes bool) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	project, err := envctx.InferProject(dir, flagProject)
	if err != nil {
		return err
	}
	stage := envctx.ResolveStage(flagStage)

	v, err := openVault()
	if err != nil {
		return err
	}

	snap, err := v.Load(project, stage)
	if err != nil {
		if vault.IsNotFound(err) {
			return notFoundError(v, project, stage)
		}
		return err
	}

	for _, f := range snap.Files {
		if err := vault.ValidateRelPath(f.Path); err != nil {
			return fmt.Errorf("refusing to restore %q: %w", f.Path, err)
		}
	}

	conflicts, err := differingTargets(dir, snap.Files)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 && !yes {
		fmt.Fprintln(cmd.OutOrStdout(), "These files already exist and differ from the snapshot:")
		for _, c := range conflicts {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", c)
		}
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "Overwrite them?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	for _, f := range snap.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, f.Content, 0o600); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Restored %d file(s) from %s/%s:\n", len(snap.Files), project, stage)
	for _, f := range snap.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.Path)
	}
	return nil
}

func differingTargets(dir string, files []vault.File) ([]string, error) {
	var out []string
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		current, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !bytes.Equal(current, f.Content) {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

func notFoundError(v *vault.Vault, project, stage string) error {
	stages, err := v.Stages(project)
	if err != nil {
		return err
	}
	if len(stages) == 0 {
		return fmt.Errorf("no snapshot for %s/%s (project %q has nothing saved)", project, stage, project)
	}
	return fmt.Errorf("no snapshot for %s/%s; available stages for %q: %s",
		project, stage, project, strings.Join(stages, ", "))
}
