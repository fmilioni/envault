package envctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDetectEnvFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".env", ".env.local", ".env.production", ".env.example", ".env.sample", "config.yaml", "README.md"} {
		writeFile(t, filepath.Join(dir, name), "x")
	}
	if err := os.Mkdir(filepath.Join(dir, ".env.d"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := DetectEnvFiles(dir)
	if err != nil {
		t.Fatalf("DetectEnvFiles: %v", err)
	}
	want := []string{".env", ".env.local", ".env.production"}
	if !equal(got, want) {
		t.Errorf("DetectEnvFiles = %v, want %v (templates/dirs/non-env excluded)", got, want)
	}
}

func TestDetectExcludesSymlinkToDirAndEmptySuffix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "A=1")
	writeFile(t, filepath.Join(dir, ".env."), "garbage")

	target := filepath.Join(dir, "somedir")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".env.todir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, ".env.tofile")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := DetectEnvFiles(dir)
	if err != nil {
		t.Fatalf("DetectEnvFiles: %v", err)
	}
	want := []string{".env", ".env.tofile"}
	if !equal(got, want) {
		t.Errorf("DetectEnvFiles = %v, want %v (.env. empty-suffix and symlink-to-dir excluded, symlink-to-file kept)", got, want)
	}
}

func TestInferProjectFromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "my-pkg", "version": "1.0.0"}`)
	got, err := InferProject(dir, "")
	if err != nil {
		t.Fatalf("InferProject: %v", err)
	}
	if got != "my-pkg" {
		t.Errorf("InferProject = %q, want my-pkg", got)
	}
}

func TestInferProjectScopedPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "@acme/widget"}`)
	got, err := InferProject(dir, "")
	if err != nil {
		t.Fatalf("InferProject: %v", err)
	}
	if got != "acme-widget" {
		t.Errorf("InferProject = %q, want acme-widget", got)
	}
}

func TestInferProjectFallsBackToFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Project")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := InferProject(dir, "")
	if err != nil {
		t.Fatalf("InferProject: %v", err)
	}
	if got != "my-project" {
		t.Errorf("InferProject = %q, want my-project", got)
	}
}

func TestInferProjectOverrideWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "from-pkg"}`)
	got, err := InferProject(dir, "Override Name")
	if err != nil {
		t.Fatalf("InferProject: %v", err)
	}
	if got != "override-name" {
		t.Errorf("InferProject = %q, want override-name", got)
	}
}

func TestInferProjectMalformedPackageJSONFallsBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fallbackdir")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "package.json"), `{ not valid json `)
	got, err := InferProject(dir, "")
	if err != nil {
		t.Fatalf("InferProject: %v", err)
	}
	if got != "fallbackdir" {
		t.Errorf("InferProject = %q, want fallbackdir (fallback on bad json)", got)
	}
}

func TestResolveStage(t *testing.T) {
	cases := map[string]string{
		"":       "default",
		"   ":    "default",
		"prod":   "prod",
		"Prod":   "prod",
		"My/Env": "my-env",
	}
	for in, want := range cases {
		if got := ResolveStage(in); got != want {
			t.Errorf("ResolveStage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"MyApp":        "myapp",
		"My App":       "my-app",
		"  spaced  ":   "spaced",
		"@acme/widget": "acme-widget",
		"a--b__c":      "a-b__c",
		"v1.2.3":       "v1.2.3",
		"a..b":         "a.b",
		".env":         "env",
		"...":          "",
		"!!!":          "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyOutputIsVaultSafe(t *testing.T) {
	// Boundary contract: whatever Slugify emits must be a name the vault layer
	// accepts (no separators/NUL, never "." or ".."). Mirrors vault.validateName.
	inputs := []string{
		"normal", "My App", "@acme/widget", "a..b", "...", "!!!", "  ",
		"../escape", "a/b/c", "a\\b", "..", ".", "x..", "..x", "a.tmp b",
		"con/../trol", "../../etc/passwd", "v1.2.3", "_leading", "trailing_",
	}
	for _, in := range inputs {
		got := Slugify(in)
		if got == "" {
			continue
		}
		if got == "." || got == ".." {
			t.Errorf("Slugify(%q) = %q — vault rejects this", in, got)
		}
		if strings.ContainsAny(got, "/\\\x00") {
			t.Errorf("Slugify(%q) = %q — contains a separator/NUL", in, got)
		}
		for _, seg := range strings.Split(got, ".") {
			if seg == "" && got != "." {
				// an empty segment means consecutive dots survived
				if strings.Contains(got, "..") {
					t.Errorf("Slugify(%q) = %q — has a '..' segment", in, got)
				}
			}
		}
	}
}

func TestInferProjectErrorsOnDegenerateName(t *testing.T) {
	dir := t.TempDir()
	if _, err := InferProject(dir, "!!!"); err == nil {
		t.Error("InferProject with degenerate override returned no error")
	}
}

func TestResolveAggregates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Demo App")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, ".env"), "A=1")
	writeFile(t, filepath.Join(dir, ".env.example"), "A=")

	ctx, err := Resolve(dir, "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ctx.Project != "demo-app" {
		t.Errorf("Project = %q, want demo-app", ctx.Project)
	}
	if ctx.Stage != "default" {
		t.Errorf("Stage = %q, want default", ctx.Stage)
	}
	if !equal(ctx.Files, []string{".env"}) {
		t.Errorf("Files = %v, want [.env]", ctx.Files)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
