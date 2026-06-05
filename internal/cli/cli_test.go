package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmilioni/envault/internal/vault"
)

// runCLI builds a fresh root command (resetting persistent flags), drives it with
// the given stdin, and returns combined stdout/stderr.
func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// workspace sets up an isolated project dir (cwd) and vault root (ENVAULT_HOME).
func workspace(t *testing.T) (dir string, openVaultAt func() *vault.Vault) {
	t.Helper()
	dir = t.TempDir()
	home := filepath.Join(t.TempDir(), ".envault")
	t.Chdir(dir)
	t.Setenv("ENVAULT_HOME", home)
	return dir, func() *vault.Vault {
		v, err := vault.OpenAt(home)
		if err != nil {
			t.Fatalf("OpenAt: %v", err)
		}
		return v
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestSaveSingleEnv(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")

	out, err := runCLI(t, "", "save", "--project", "myapp", "--stage", "default")
	if err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Saved 1 file(s) to myapp/default") {
		t.Errorf("unexpected output:\n%s", out)
	}

	snap, err := vaultAt().Load("myapp", "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != ".env" || string(snap.Files[0].Content) != "A=1\n" {
		t.Errorf("snapshot mismatch: %+v", snap.Files)
	}
}

func TestSaveMultipleViaFileFlag(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "ROOT=1")
	writeFile(t, dir, ".env.local", "LOCAL=1")

	out, err := runCLI(t, "", "save", "--project", "multi", "--file", ".env", "--file", ".env.local")
	if err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Saved 2 file(s) to multi/default") {
		t.Errorf("unexpected output:\n%s", out)
	}
	snap, err := vaultAt().Load("multi", "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Files) != 2 {
		t.Errorf("got %d files, want 2", len(snap.Files))
	}
}

func TestSaveMultiSelectPrompt(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "ROOT=1")
	writeFile(t, dir, ".env.local", "LOCAL=1")

	// Two candidates, no --file: the picker runs; "1" selects .env (sorted first).
	out, err := runCLI(t, "1\n", "save", "--project", "pick")
	if err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	snap, err := vaultAt().Load("pick", "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != ".env" {
		t.Errorf("picker selected wrong files: %+v", snap.Files)
	}
}

func TestSaveNoCandidates(t *testing.T) {
	workspace(t)
	out, err := runCLI(t, "", "save", "--project", "empty")
	if err == nil {
		t.Fatalf("expected error, got none:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no .env files found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveIdenticalIsNoop(t *testing.T) {
	dir, _ := workspace(t)
	writeFile(t, dir, ".env", "A=1\n")

	if _, err := runCLI(t, "", "save", "--project", "app"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	out, err := runCLI(t, "", "save", "--project", "app")
	if err != nil {
		t.Fatalf("second save: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Already up to date: app/default") {
		t.Errorf("expected no-op message, got:\n%s", out)
	}
}

func TestSaveOverwriteConfirmation(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "OLD=1")
	if _, err := runCLI(t, "", "save", "--project", "app"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	writeFile(t, dir, ".env", "NEW=1")

	// Decline: snapshot must keep the old content.
	out, err := runCLI(t, "n\n", "save", "--project", "app")
	if err != nil {
		t.Fatalf("declined save: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected abort, got:\n%s", out)
	}
	if snap, _ := vaultAt().Load("app", "default"); string(snap.Files[0].Content) != "OLD=1" {
		t.Errorf("snapshot changed despite decline: %q", snap.Files[0].Content)
	}

	// --yes: overwrite without prompting.
	if _, err := runCLI(t, "", "save", "--project", "app", "--yes"); err != nil {
		t.Fatalf("forced save: %v", err)
	}
	if snap, _ := vaultAt().Load("app", "default"); string(snap.Files[0].Content) != "NEW=1" {
		t.Errorf("snapshot not overwritten: %q", snap.Files[0].Content)
	}
}

func TestSaveOverwriteAccept(t *testing.T) {
	dir, vaultAt := workspace(t)
	writeFile(t, dir, ".env", "OLD=1")
	if _, err := runCLI(t, "", "save", "--project", "app"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	writeFile(t, dir, ".env", "NEW=1")

	if _, err := runCLI(t, "y\n", "save", "--project", "app"); err != nil {
		t.Fatalf("accepted save: %v", err)
	}
	if snap, _ := vaultAt().Load("app", "default"); string(snap.Files[0].Content) != "NEW=1" {
		t.Errorf("accept did not overwrite: %q", snap.Files[0].Content)
	}
}
