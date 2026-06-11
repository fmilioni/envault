package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fmilioni/envault/internal/envctx"
	"github.com/fmilioni/envault/internal/tui"
	"github.com/fmilioni/envault/internal/vault"
	"github.com/spf13/cobra"
)

var deleteSelect = tui.SelectForDelete

func newDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a saved snapshot — or a whole project — from the vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without confirmation")
	return cmd
}

func runDelete(cmd *cobra.Command, yes bool) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	project, err := envctx.InferProject(dir, flagProject)
	if err != nil {
		return err
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	if flagStage != "" {
		return deleteStage(cmd, v, project, envctx.ResolveStage(flagStage), yes)
	}
	if flagProject != "" {
		return deleteProject(cmd, v, project, yes)
	}

	projects, err := v.Projects()
	if err != nil {
		return err
	}
	if len(projects) > 0 && stdinIsTTY() {
		sel, err := deleteSelect(v, project, envctx.ResolveStage(flagStage))
		if err != nil {
			return err
		}
		if sel == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		return deleteStage(cmd, v, sel.Project, sel.Stage, yes)
	}
	return noTargetError(projects)
}

func deleteStage(cmd *cobra.Command, v *vault.Vault, project, stage string, yes bool) error {
	exists, err := v.Exists(project, stage)
	if err != nil {
		return err
	}
	if !exists {
		return notFoundError(v, project, stage)
	}

	if !yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Delete snapshot %s/%s?", project, stage))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	if err := v.Delete(project, stage); err != nil {
		if vault.IsNotFound(err) {
			return notFoundError(v, project, stage)
		}
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s/%s.\n", project, stage)
	return nil
}

func deleteProject(cmd *cobra.Command, v *vault.Vault, project string, yes bool) error {
	stages, err := v.Stages(project)
	if err != nil {
		return err
	}
	if len(stages) == 0 {
		return projectNotFoundError(v, project)
	}

	if !yes {
		fmt.Fprintf(cmd.OutOrStdout(), "About to delete ALL %d stage(s) of project %q: %s\n",
			len(stages), project, strings.Join(stages, ", "))
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "Delete the entire project?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	for _, stage := range stages {
		if err := v.Delete(project, stage); err != nil && !vault.IsNotFound(err) {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted project %q (%d stage(s)).\n", project, len(stages))
	return nil
}

func noTargetError(projects []string) error {
	if len(projects) == 0 {
		return fmt.Errorf("nothing to delete: the vault is empty")
	}
	return fmt.Errorf("no target to delete: pass --stage and/or --project (available projects: %s)",
		strings.Join(projects, ", "))
}

func projectNotFoundError(v *vault.Vault, project string) error {
	projects, err := v.Projects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return fmt.Errorf("no project %q (the vault is empty)", project)
	}
	return fmt.Errorf("no project %q; available projects: %s", project, strings.Join(projects, ", "))
}
