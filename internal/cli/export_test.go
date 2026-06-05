package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmilioni/envault/internal/vault"
)

// swapExportDeps overrides the TTY check and the multi-select picker for one test.
func swapExportDeps(tty func() bool, sel func(*vault.Vault, string) ([]string, error)) func() {
	oldT, oldS := stdinIsTTY, exportSelect
	stdinIsTTY, exportSelect = tty, sel
	return func() { stdinIsTTY, exportSelect = oldT, oldS }
}

func TestExportImportPlaintextRoundTrip(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}

	bundlePath := filepath.Join(dir, "b.bundle")
	out, err := runCLI(t, "", "export", "--all", "--no-encrypt", "-o", bundlePath)
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	if !strings.Contains(out, "plaintext") {
		t.Errorf("export output missing plaintext note:\n%s", out)
	}

	// Drop the snapshot, then import re-creates it without prompting.
	if err := vaultAt().Delete("app", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	out, err = runCLI(t, "", "import", bundlePath)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if !strings.Contains(out, "+ app/prod (new)") || !strings.Contains(out, "1 new") {
		t.Errorf("unexpected import output:\n%s", out)
	}
	snap, err := vaultAt().Load("app", "prod")
	if err != nil {
		t.Fatalf("load after import: %v", err)
	}
	if string(snap.Files[0].Content) != "A=1\n" {
		t.Errorf("imported content = %q", snap.Files[0].Content)
	}
}

func TestExportImportEncryptedRoundTrip(t *testing.T) {
	dir, vaultAt := workspace(t)
	t.Setenv("ENVAULT_PASSWORD", "hunter2")
	writeFile(t, dir, ".env", "SECRET=TOPSECRETVALUE\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}

	bundlePath := filepath.Join(dir, "b.bundle")
	if out, err := runCLI(t, "", "export", "--all", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(bundlePath)
	if bytes.Contains(data, []byte("TOPSECRETVALUE")) {
		t.Error("encrypted bundle leaks secret content")
	}

	if err := vaultAt().Delete("app", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if out, err := runCLI(t, "", "import", bundlePath); err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	snap, err := vaultAt().Load("app", "prod")
	if err != nil {
		t.Fatalf("load after import: %v", err)
	}
	if string(snap.Files[0].Content) != "SECRET=TOPSECRETVALUE\n" {
		t.Errorf("decrypted content = %q", snap.Files[0].Content)
	}
}

func TestImportWrongPasswordLeavesVaultUntouched(t *testing.T) {
	dir, vaultAt := workspace(t)
	t.Setenv("ENVAULT_PASSWORD", "right")
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	bundlePath := filepath.Join(dir, "b.bundle")
	if _, err := runCLI(t, "", "export", "--all", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := vaultAt().Delete("app", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	t.Setenv("ENVAULT_PASSWORD", "wrong")
	_, err := runCLI(t, "", "import", bundlePath)
	if err == nil || !strings.Contains(err.Error(), "wrong password or corrupt bundle") {
		t.Fatalf("expected wrong-password error, got %v", err)
	}
	if exists, _ := vaultAt().Exists("app", "prod"); exists {
		t.Error("vault was modified despite a failed decrypt")
	}
}

func TestImportConflictShowsDiffAndOverwrites(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	bundlePath := filepath.Join(dir, "b.bundle")
	if _, err := runCLI(t, "", "export", "--all", "--no-encrypt", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Vault now diverges from the bundle.
	writeFile(t, dir, ".env", "A=2\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod", "--yes"); err != nil {
		t.Fatalf("re-save: %v", err)
	}

	out, err := runCLI(t, "y\n", "import", bundlePath)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	for _, want := range []string{"Conflict on app/prod", "vault saved", "bundle saved", "- A=2", "+ A=1", "Overwrite app/prod?", "1 overwritten"} {
		if !strings.Contains(out, want) {
			t.Errorf("import output missing %q:\n%s", want, out)
		}
	}
	if snap, _ := vaultAt().Load("app", "prod"); string(snap.Files[0].Content) != "A=1\n" {
		t.Errorf("conflict not overwritten: %q", snap.Files[0].Content)
	}
}

func TestImportConflictDeclineKeepsVault(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	bundlePath := filepath.Join(dir, "b.bundle")
	if _, err := runCLI(t, "", "export", "--all", "--no-encrypt", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v", err)
	}
	writeFile(t, dir, ".env", "A=2\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod", "--yes"); err != nil {
		t.Fatalf("re-save: %v", err)
	}

	out, err := runCLI(t, "n\n", "import", bundlePath)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("expected a skipped item:\n%s", out)
	}
	if snap, _ := vaultAt().Load("app", "prod"); string(snap.Files[0].Content) != "A=2\n" {
		t.Errorf("declined import overwrote vault: %q", snap.Files[0].Content)
	}
}

func TestImportUnchangedIsSilent(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	bundlePath := filepath.Join(dir, "b.bundle")
	if _, err := runCLI(t, "", "export", "--all", "--no-encrypt", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v", err)
	}

	out, err := runCLI(t, "", "import", bundlePath)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if strings.Contains(out, "Conflict") || strings.Contains(out, "Overwrite") {
		t.Errorf("identical snapshot should not prompt:\n%s", out)
	}
	if !strings.Contains(out, "1 unchanged") {
		t.Errorf("expected unchanged count:\n%s", out)
	}
}

func TestExportMultiSelectViaTUI(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeFile(t, dir, ".env", "B=1\n")
	if _, err := runCLI(t, "", "save", "--project", "other", "--stage", "dev"); err != nil {
		t.Fatalf("save: %v", err)
	}

	var gotPre string
	restore := swapExportDeps(
		func() bool { return true },
		func(_ *vault.Vault, pre string) ([]string, error) {
			gotPre = pre
			return []string{"app"}, nil // user picks only "app"
		})
	defer restore()

	bundlePath := filepath.Join(dir, "b.bundle")
	// No --all/--stage: the picker drives the scope. --project sets the pre-check.
	out, err := runCLI(t, "", "export", "--no-encrypt", "--project", "other", "-o", bundlePath)
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	if gotPre != "other" {
		t.Errorf("picker pre-selection = %q, want other", gotPre)
	}
	if !strings.Contains(out, "1 project(s)") {
		t.Errorf("expected one project exported:\n%s", out)
	}

	// Import into a wiped vault: only "app" should come back.
	_ = vaultAt().Delete("app", "prod")
	_ = vaultAt().Delete("other", "dev")
	if _, err := runCLI(t, "", "import", bundlePath); err != nil {
		t.Fatalf("import: %v", err)
	}
	if exists, _ := vaultAt().Exists("app", "prod"); !exists {
		t.Error("app/prod not imported")
	}
	if exists, _ := vaultAt().Exists("other", "dev"); exists {
		t.Error("other/dev should not have been exported")
	}
}

func TestExportCancelViaTUI(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}

	restore := swapExportDeps(
		func() bool { return true },
		func(_ *vault.Vault, _ string) ([]string, error) { return nil, nil }) // cancelled
	defer restore()

	bundlePath := filepath.Join(dir, "b.bundle")
	out, err := runCLI(t, "", "export", "--no-encrypt", "--project", "app", "-o", bundlePath)
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort on cancel:\n%s", out)
	}
	if _, err := os.Stat(bundlePath); err == nil {
		t.Error("cancelled export should not write a bundle")
	}
}

func TestExportAllRejectsScopeFlags(t *testing.T) {
	workspace(t)
	for _, args := range [][]string{
		{"export", "--all", "--stage", "dev", "--no-encrypt"},
		{"export", "--all", "--project", "foo", "--no-encrypt"},
	} {
		_, err := runCLI(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "--all exports the whole vault") {
			t.Errorf("args %v: expected --all conflict error, got %v", args, err)
		}
	}
}

func TestImportPreservesSavedAt(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")
	if _, err := runCLI(t, "", "save", "--project", "app", "--stage", "prod"); err != nil {
		t.Fatalf("save: %v", err)
	}
	orig, err := vaultAt().Load("app", "prod")
	if err != nil {
		t.Fatalf("load orig: %v", err)
	}

	bundlePath := filepath.Join(dir, "b.bundle")
	if _, err := runCLI(t, "", "export", "--all", "--no-encrypt", "-o", bundlePath); err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := vaultAt().Delete("app", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := runCLI(t, "", "import", bundlePath); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := vaultAt().Load("app", "prod")
	if err != nil {
		t.Fatalf("load after import: %v", err)
	}
	if !got.SavedAt.Equal(orig.SavedAt) {
		t.Errorf("import re-stamped savedAt: got %v, want %v", got.SavedAt, orig.SavedAt)
	}
}
