package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmilioni/envault/internal/tui"
	"github.com/fmilioni/envault/internal/vault"
)

// swapLoadDeps overrides the interactivity check and the selector for one test,
// returning a restore func — keeps the TUI out of the non-TTY test harness.
func swapLoadDeps(interactive func() bool, sel func(*vault.Vault, string, string) (*tui.Selection, error)) func() {
	oldI, oldS := loadInteractive, loadSelect
	loadInteractive, loadSelect = interactive, sel
	return func() { loadInteractive, loadSelect = oldI, oldS }
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "SECRET=xyz\n")

	if out, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	if err := os.Remove(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	out, err := runCLI(t, "", "load", "--project", "app", "--stage", "prod")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Restored 1 file(s) from app/prod") {
		t.Errorf("unexpected output:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(got) != "SECRET=xyz\n" {
		t.Errorf("restored content = %q", got)
	}
}

func TestSaveLoadNestedPath(t *testing.T) {
	dir, _ := workspace(t)
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, dir, filepath.Join("config", ".env"), "NESTED=1")

	if _, err := runCLI(t, "", "save", "--project", "nest", "--file", "config/.env"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "config", ".env")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if out, err := runCLI(t, "", "load", "--project", "nest"); err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config", ".env"))
	if err != nil {
		t.Fatalf("read restored nested: %v", err)
	}
	if string(got) != "NESTED=1" {
		t.Errorf("nested restore = %q", got)
	}
}

func TestLoadOverwriteConfirmation(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "SAVED=1")
	if _, err := runCLI(t, "", "save", "--project", "app"); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeFile(t, dir, ".env", "LOCAL_CHANGES=1")

	out, err := runCLI(t, "n\n", "load", "--project", "app")
	if err != nil {
		t.Fatalf("declined load: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort, got:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".env")); string(got) != "LOCAL_CHANGES=1" {
		t.Errorf("local file overwritten despite decline: %q", got)
	}

	if _, err := runCLI(t, "", "load", "--project", "app", "--yes"); err != nil {
		t.Fatalf("forced load: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".env")); string(got) != "SAVED=1" {
		t.Errorf("load did not restore: %q", got)
	}
}

func TestLoadNotFound(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "X=1")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "dev"); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err := runCLI(t, "", "load", "--project", "app", "--stage", "prod")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "available stages") || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error should list available stages: %v", err)
	}
}

func TestLoadInteractiveSelectRestores(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "PICKED=1")
	// Saved under a non-default stage, so no --stage triggers the selector.
	if _, err := runCLI(t, "", "save", "--project", "svc", "--stage", "staging"); err != nil {
		t.Fatalf("save: %v", err)
	}
	os.Remove(filepath.Join(dir, ".env"))

	restore := swapLoadDeps(
		func() bool { return true },
		func(_ *vault.Vault, _, _ string) (*tui.Selection, error) {
			return &tui.Selection{Project: "svc", Stage: "staging"}, nil
		})
	defer restore()

	out, err := runCLI(t, "", "load", "--project", "svc")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Restored 1 file(s) from svc/staging") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".env")); string(got) != "PICKED=1" {
		t.Errorf("restored content = %q", got)
	}
}

func TestLoadInteractiveCancel(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "ORIG=1")
	if _, err := runCLI(t, "", "save", "--project", "svc", "--stage", "staging"); err != nil {
		t.Fatalf("save: %v", err)
	}

	restore := swapLoadDeps(
		func() bool { return true },
		func(_ *vault.Vault, _, _ string) (*tui.Selection, error) { return nil, nil })
	defer restore()

	out, err := runCLI(t, "", "load", "--project", "svc")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort on cancel, got:\n%s", out)
	}
}

func TestLoadNoDefaultNonInteractiveListsStages(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "X=1")
	if _, err := runCLI(t, "", "save", "--project", "svc", "--stage", "staging"); err != nil {
		t.Fatalf("save: %v", err)
	}

	restore := swapLoadDeps(func() bool { return false }, loadSelect)
	defer restore()

	_, err := runCLI(t, "", "load", "--project", "svc")
	if err == nil {
		t.Fatal("expected not-found error without a default stage")
	}
	if !strings.Contains(err.Error(), "available stages") || !strings.Contains(err.Error(), "staging") {
		t.Errorf("error should list available stages: %v", err)
	}
}

func TestLoadRefusesTraversalPath(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "X=1")
	if _, err := runCLI(t, "", "save", "--project", "trav"); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Tamper the manifest so it points outside the folder; the blob 000 already
	// exists from the legit save. load must refuse before writing anywhere.
	manifest := filepath.Join(os.Getenv("ENVAULT_HOME"), "trav", "default", "manifest.json")
	bad := `{"version":1,"savedAt":"2026-01-01T00:00:00Z","files":[{"path":"../escape.env","blob":"000"}]}`
	if err := os.WriteFile(manifest, []byte(bad), 0o600); err != nil {
		t.Fatalf("tamper manifest: %v", err)
	}

	_, err := runCLI(t, "", "load", "--project", "trav", "--yes")
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Errorf("load accepted traversal path: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "escape.env")); statErr == nil {
		t.Error("traversal file was written outside the folder")
	}
}
