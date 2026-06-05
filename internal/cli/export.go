package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/fmilioni/envault/internal/bundle"
	"github.com/fmilioni/envault/internal/envctx"
	"github.com/fmilioni/envault/internal/tui"
	"github.com/fmilioni/envault/internal/vault"
	"github.com/spf13/cobra"
)

var exportSelect = tui.SelectMulti

func newExportCmd() *cobra.Command {
	var (
		all       bool
		noEncrypt bool
		output    string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export projects/stages as a portable bundle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd, all, noEncrypt, output)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "export every project in the vault")
	cmd.Flags().BoolVar(&noEncrypt, "no-encrypt", false, "write a plaintext bundle instead of encrypting it")
	cmd.Flags().StringVarP(&output, "output", "o", "", "bundle file to write (default: envault-<timestamp>.bundle)")
	return cmd
}

func runExport(cmd *cobra.Command, all, noEncrypt bool, output string) error {
	if all && (flagStage != "" || flagProject != "") {
		return fmt.Errorf("--all exports the whole vault; drop --project/--stage")
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	payload, stages, ok, err := resolveExportPayload(cmd, v, all)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
		return nil
	}
	if len(payload.Projects) == 0 {
		return fmt.Errorf("nothing to export")
	}

	encrypt := !noEncrypt
	password := ""
	if encrypt {
		password, err = resolvePassword(true)
		if err != nil {
			return err
		}
	}

	data, err := bundle.Build(payload, password, encrypt)
	if err != nil {
		return err
	}

	if output == "" {
		output = fmt.Sprintf("envault-%s.bundle", time.Now().Format("20060102-150405"))
	}
	if err := os.WriteFile(output, data, 0o600); err != nil {
		return err
	}

	kind := "encrypted"
	if !encrypt {
		kind = "plaintext"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported %d project(s), %d stage(s) to %s (%s).\n",
		len(payload.Projects), stages, output, kind)
	return nil
}

// resolveExportPayload decides the scope and reads it from the vault. --all takes
// every project; --stage takes one stage of the inferred project; with neither, an
// interactive terminal opens the multi-select picker (pre-checking the current
// project) and a non-interactive one falls back to the current project's stages.
// ok=false means the user cancelled the picker.
func resolveExportPayload(cmd *cobra.Command, v *vault.Vault, all bool) (bundle.Payload, int, bool, error) {
	var empty bundle.Payload

	if all {
		projects, err := v.Projects()
		if err != nil {
			return empty, 0, false, err
		}
		payload, stages, err := collectProjects(v, projects)
		return payload, stages, true, err
	}

	dir, err := os.Getwd()
	if err != nil {
		return empty, 0, false, err
	}
	project, err := envctx.InferProject(dir, flagProject)
	if err != nil {
		return empty, 0, false, err
	}

	if flagStage != "" {
		stage := envctx.ResolveStage(flagStage)
		snap, err := v.Load(project, stage)
		if err != nil {
			if vault.IsNotFound(err) {
				return empty, 0, false, notFoundError(v, project, stage)
			}
			return empty, 0, false, err
		}
		payload := bundle.Payload{Projects: []bundle.Project{{Name: project, Stages: []bundle.Stage{snapToStage(stage, snap)}}}}
		return payload, 1, true, nil
	}

	projects, err := v.Projects()
	if err != nil {
		return empty, 0, false, err
	}
	if len(projects) > 0 && stdinIsTTY() {
		chosen, err := exportSelect(v, project)
		if err != nil {
			return empty, 0, false, err
		}
		if chosen == nil {
			return empty, 0, false, nil
		}
		payload, stages, err := collectProjects(v, chosen)
		return payload, stages, true, err
	}

	payload, stages, err := collectProjects(v, []string{project})
	return payload, stages, true, err
}

// collectProjects loads every stage of each project into a bundle payload. A
// project with no saved stages is skipped (so a half-written vault dir doesn't
// produce an empty project entry).
func collectProjects(v *vault.Vault, projects []string) (bundle.Payload, int, error) {
	var payload bundle.Payload
	stageCount := 0
	for _, p := range projects {
		stages, err := v.Stages(p)
		if err != nil {
			return payload, 0, err
		}
		if len(stages) == 0 {
			continue
		}
		bp := bundle.Project{Name: p}
		for _, s := range stages {
			snap, err := v.Load(p, s)
			if err != nil {
				return payload, 0, err
			}
			bp.Stages = append(bp.Stages, snapToStage(s, snap))
			stageCount++
		}
		payload.Projects = append(payload.Projects, bp)
	}
	return payload, stageCount, nil
}

func snapToStage(stage string, snap *vault.Snapshot) bundle.Stage {
	files := make([]bundle.File, len(snap.Files))
	for i, f := range snap.Files {
		files[i] = bundle.File{Path: f.Path, Content: f.Content}
	}
	return bundle.Stage{Name: stage, SavedAt: snap.SavedAt, Files: files}
}
