package cli

import (
	"strings"
	"testing"

	"github.com/fmilioni/envault/internal/tui"
	"github.com/fmilioni/envault/internal/vault"
)

func swapDeleteDeps(tty func() bool, sel func(*vault.Vault, string, string) (*tui.Selection, error)) func() {
	oldT, oldS := stdinIsTTY, deleteSelect
	stdinIsTTY, deleteSelect = tty, sel
	return func() { stdinIsTTY, deleteSelect = oldT, oldS }
}

func saveStage(t *testing.T, dir, project, stage, content string) {
	t.Helper()
	writeFile(t, dir, ".env", content)
	if _, err := runCLI(t, "", "save", "--project", project, "--stage", stage); err != nil {
		t.Fatalf("save %s/%s: %v", project, stage, err)
	}
}

func TestDeleteStageKeepsOthers(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "app", "dev", "A=1")
	saveStage(t, dir, "app", "prod", "B=2")

	out, err := runCLI(t, "", "delete", "--project", "app", "--stage", "dev", "--yes")
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Deleted app/dev") {
		t.Errorf("unexpected output:\n%s", out)
	}

	if ok, _ := vaultAt().Exists("app", "dev"); ok {
		t.Error("app/dev still exists after delete")
	}
	if ok, _ := vaultAt().Exists("app", "prod"); !ok {
		t.Error("app/prod was deleted but should remain")
	}
}

func TestDeleteLastStagePrunesProject(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "solo", "default", "X=1")

	if _, err := runCLI(t, "", "delete", "--project", "solo", "--stage", "default", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	projects, err := vaultAt().Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	for _, p := range projects {
		if p == "solo" {
			t.Error("project dir not pruned after deleting its last stage")
		}
	}
}

func TestDeleteWholeProject(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "multi", "dev", "A=1")
	saveStage(t, dir, "multi", "prod", "B=2")
	saveStage(t, dir, "other", "default", "C=3")

	out, err := runCLI(t, "", "delete", "--project", "multi", "--yes")
	if err != nil {
		t.Fatalf("delete project: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Deleted project "multi" (2 stage(s))`) {
		t.Errorf("unexpected output:\n%s", out)
	}

	stages, _ := vaultAt().Stages("multi")
	if len(stages) != 0 {
		t.Errorf("multi still has stages: %v", stages)
	}
	if ok, _ := vaultAt().Exists("other", "default"); !ok {
		t.Error("unrelated project 'other' was affected")
	}
}

func TestDeleteWholeProjectReinforcedConfirm(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "multi", "dev", "A=1")
	saveStage(t, dir, "multi", "prod", "B=2")

	out, err := runCLI(t, "n\n", "delete", "--project", "multi")
	if err != nil {
		t.Fatalf("delete declined: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ALL 2 stage(s)") || !strings.Contains(out, "dev, prod") {
		t.Errorf("confirmation should spell out all stages:\n%s", out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort on decline:\n%s", out)
	}
	if stages, _ := vaultAt().Stages("multi"); len(stages) != 2 {
		t.Errorf("decline still deleted stages: %v", stages)
	}
}

func TestDeleteConfirmDeclineKeepsSnapshot(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "app", "dev", "A=1")

	out, err := runCLI(t, "n\n", "delete", "--project", "app", "--stage", "dev")
	if err != nil {
		t.Fatalf("declined delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort, got:\n%s", out)
	}
	if ok, _ := vaultAt().Exists("app", "dev"); !ok {
		t.Error("snapshot deleted despite declining confirmation")
	}
}

func TestDeleteConfirmAcceptsYes(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "app", "dev", "A=1")

	if _, err := runCLI(t, "y\n", "delete", "--project", "app", "--stage", "dev"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok, _ := vaultAt().Exists("app", "dev"); ok {
		t.Error("snapshot not deleted after confirming")
	}
}

func TestDeleteNotFoundListsStages(t *testing.T) {
	dir, _ := workspace(t)
	saveStage(t, dir, "app", "dev", "A=1")

	_, err := runCLI(t, "", "delete", "--project", "app", "--stage", "prod", "--yes")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "available stages") || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error should list available stages: %v", err)
	}
}

func TestDeleteWholeProjectNotFound(t *testing.T) {
	dir, _ := workspace(t)
	saveStage(t, dir, "app", "dev", "A=1")

	_, err := runCLI(t, "", "delete", "--project", "ghost", "--yes")
	if err == nil {
		t.Fatal("expected not-found error for missing project")
	}
	if !strings.Contains(err.Error(), "available projects") || !strings.Contains(err.Error(), "app") {
		t.Errorf("error should list available projects: %v", err)
	}
}

func TestDeleteNoTargetInteractiveSelects(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "svc", "staging", "A=1")

	restore := swapDeleteDeps(
		func() bool { return true },
		func(_ *vault.Vault, _, _ string) (*tui.Selection, error) {
			return &tui.Selection{Project: "svc", Stage: "staging"}, nil
		})
	defer restore()

	out, err := runCLI(t, "y\n", "delete")
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Deleted svc/staging") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if ok, _ := vaultAt().Exists("svc", "staging"); ok {
		t.Error("selected snapshot was not deleted")
	}
}

func TestDeleteNoTargetInteractiveCancel(t *testing.T) {
	dir, vaultAt := workspace(t)
	saveStage(t, dir, "svc", "staging", "A=1")

	restore := swapDeleteDeps(
		func() bool { return true },
		func(_ *vault.Vault, _, _ string) (*tui.Selection, error) { return nil, nil })
	defer restore()

	out, err := runCLI(t, "", "delete")
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort on cancel:\n%s", out)
	}
	if ok, _ := vaultAt().Exists("svc", "staging"); !ok {
		t.Error("snapshot deleted despite cancelling the selector")
	}
}

func TestDeleteNoTargetNonInteractiveErrors(t *testing.T) {
	dir, _ := workspace(t)
	saveStage(t, dir, "svc", "staging", "A=1")

	restore := swapDeleteDeps(func() bool { return false }, deleteSelect)
	defer restore()

	_, err := runCLI(t, "", "delete")
	if err == nil {
		t.Fatal("expected error without a resolvable target")
	}
	if !strings.Contains(err.Error(), "no target to delete") || !strings.Contains(err.Error(), "svc") {
		t.Errorf("error should list available projects: %v", err)
	}
}
