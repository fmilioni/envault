package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newVault(t *testing.T) *Vault {
	t.Helper()
	v, err := OpenAt(filepath.Join(t.TempDir(), ".envault"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return v
}

func TestSaveLoadRoundTrip(t *testing.T) {
	v := newVault(t)
	in := []File{
		{Path: ".env", Content: []byte("A=1\nB=2\n")},
		{Path: ".env.local", Content: []byte("SECRET=xyz\n")},
	}
	before := time.Now().Add(-time.Second)
	if err := v.Save("myapp", "default", in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snap, err := v.Load("myapp", "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Project != "myapp" || snap.Stage != "default" {
		t.Errorf("identity = %s/%s, want myapp/default", snap.Project, snap.Stage)
	}
	if snap.SavedAt.Before(before) || snap.SavedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("SavedAt = %v, not within the save window", snap.SavedAt)
	}
	if len(snap.Files) != len(in) {
		t.Fatalf("got %d files, want %d", len(snap.Files), len(in))
	}
	for i, f := range snap.Files {
		if f.Path != in[i].Path || string(f.Content) != string(in[i].Content) {
			t.Errorf("file %d = %q/%q, want %q/%q", i, f.Path, f.Content, in[i].Path, in[i].Content)
		}
	}
}

func TestMultipleFilesPreserveRelativePaths(t *testing.T) {
	v := newVault(t)
	in := []File{
		{Path: ".env", Content: []byte("ROOT=1")},
		{Path: "apps/web/.env", Content: []byte("WEB=1")},
	}
	if err := v.Save("myapp", "dev", in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	snap, err := v.Load("myapp", "dev")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Files[1].Path != "apps/web/.env" {
		t.Errorf("nested path = %q, want apps/web/.env", snap.Files[1].Path)
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms not meaningful on Windows")
	}
	v := newVault(t)
	if err := v.Save("myapp", "default", []File{{Path: ".env", Content: []byte("X=1")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dirs := []string{
		v.root,
		filepath.Join(v.root, "myapp"),
		filepath.Join(v.root, "myapp", "default"),
		filepath.Join(v.root, "myapp", "default", blobsDir),
	}
	for _, d := range dirs {
		assertPerm(t, d, 0o700)
	}
	files := []string{
		filepath.Join(v.root, "myapp", "default", manifestName),
		filepath.Join(v.root, "myapp", "default", blobsDir, "000"),
	}
	for _, f := range files {
		assertPerm(t, f, 0o600)
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s perm = %o, want %o", path, got, want)
	}
}

func TestOverwriteReplacesSnapshot(t *testing.T) {
	v := newVault(t)
	if err := v.Save("myapp", "default", []File{
		{Path: ".env", Content: []byte("OLD=1")},
		{Path: ".env.extra", Content: []byte("E=1")},
	}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := v.Save("myapp", "default", []File{
		{Path: ".env", Content: []byte("NEW=1")},
	}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	snap, err := v.Load("myapp", "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Files) != 1 || string(snap.Files[0].Content) != "NEW=1" {
		t.Errorf("overwrite kept stale data: %+v", snap.Files)
	}
}

func TestProjectsAndStages(t *testing.T) {
	v := newVault(t)
	save := func(p, s string) {
		if err := v.Save(p, s, []File{{Path: ".env", Content: []byte("X=1")}}); err != nil {
			t.Fatalf("Save %s/%s: %v", p, s, err)
		}
	}
	save("alpha", "default")
	save("alpha", "prod")
	save("beta", "default")

	projects, err := v.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if want := []string{"alpha", "beta"}; !equal(projects, want) {
		t.Errorf("Projects = %v, want %v", projects, want)
	}

	stages, err := v.Stages("alpha")
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if want := []string{"default", "prod"}; !equal(stages, want) {
		t.Errorf("Stages(alpha) = %v, want %v", stages, want)
	}
}

func TestLoadNotFound(t *testing.T) {
	v := newVault(t)
	_, err := v.Load("ghost", "default")
	if !IsNotFound(err) {
		t.Errorf("Load missing = %v, want NotFoundError", err)
	}
}

func TestDeletePrunesEmptyProject(t *testing.T) {
	v := newVault(t)
	if err := v.Save("solo", "default", []File{{Path: ".env", Content: []byte("X=1")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := v.Delete("solo", "default"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := v.Exists("solo", "default"); ok {
		t.Error("Exists true after Delete")
	}
	if _, err := os.Stat(filepath.Join(v.root, "solo")); !os.IsNotExist(err) {
		t.Error("empty project dir not pruned")
	}
	if err := v.Delete("solo", "default"); !IsNotFound(err) {
		t.Errorf("re-Delete = %v, want NotFoundError", err)
	}
}

func TestRejectsUnsafeNames(t *testing.T) {
	v := newVault(t)
	files := []File{{Path: ".env", Content: []byte("X=1")}}
	for _, name := range []string{"", "..", "a/b", `a\b`} {
		if err := v.Save(name, "default", files); err == nil {
			t.Errorf("Save(project=%q) accepted, want error", name)
		}
	}
	if err := v.Save("ok", "default", []File{{Path: "../escape", Content: []byte("X=1")}}); err == nil {
		t.Error("Save with traversal path accepted, want error")
	}
}

func TestRejectsDuplicatePaths(t *testing.T) {
	v := newVault(t)
	err := v.Save("myapp", "default", []File{
		{Path: ".env", Content: []byte("A=1")},
		{Path: "./.env", Content: []byte("B=2")},
	})
	if err == nil {
		t.Error("Save with duplicate paths accepted, want error")
	}
}

func TestOverwriteLeavesNoLitter(t *testing.T) {
	v := newVault(t)
	files := []File{{Path: ".env", Content: []byte("X=1")}}
	if err := v.Save("myapp", "default", files); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := v.Save("myapp", "default", files); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(v.root, "myapp"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "default" {
			t.Errorf("leftover entry after overwrite: %q", e.Name())
		}
	}
}

func TestRecoverRestoresInterruptedSwap(t *testing.T) {
	v := newVault(t)
	if err := v.Save("myapp", "default", []File{{Path: ".env", Content: []byte("GOOD=1")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Simulate a crash between the two renames of an overwrite: the previous
	// snapshot sits in the backup slot and the final slot is gone.
	pdir := filepath.Join(v.root, "myapp")
	if err := os.Rename(filepath.Join(pdir, "default"), filepath.Join(pdir, ".default.old")); err != nil {
		t.Fatalf("simulate crash: %v", err)
	}
	// A leftover staging dir from the same interrupted write.
	if err := os.MkdirAll(filepath.Join(pdir, ".default.tmp-123", blobsDir), dirPerm); err != nil {
		t.Fatalf("simulate staging: %v", err)
	}

	if _, err := OpenAt(v.root); err != nil {
		t.Fatalf("re-Open: %v", err)
	}

	snap, err := v.Load("myapp", "default")
	if err != nil {
		t.Fatalf("Load after recover: %v", err)
	}
	if string(snap.Files[0].Content) != "GOOD=1" {
		t.Errorf("recovered content = %q, want GOOD=1", snap.Files[0].Content)
	}
	if _, err := os.Stat(filepath.Join(pdir, ".default.tmp-123")); !os.IsNotExist(err) {
		t.Error("staging dir not discarded on recover")
	}
}

func TestRecoverHandlesStageSlugWithTmpToken(t *testing.T) {
	v := newVault(t)
	// A stage slug can legitimately contain ".tmp-" (envctx Slugify of e.g.
	// "a.tmp b"). Its backup dir ".a.tmp-b.old" must not be mistaken for a
	// staging dir and discarded during recovery.
	const stage = "a.tmp-b"
	if err := v.Save("myapp", stage, []File{{Path: ".env", Content: []byte("KEEP=1")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	pdir := filepath.Join(v.root, "myapp")
	if err := os.Rename(filepath.Join(pdir, stage), filepath.Join(pdir, "."+stage+".old")); err != nil {
		t.Fatalf("simulate crash: %v", err)
	}

	if _, err := OpenAt(v.root); err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	snap, err := v.Load("myapp", stage)
	if err != nil {
		t.Fatalf("Load after recover: %v", err)
	}
	if string(snap.Files[0].Content) != "KEEP=1" {
		t.Errorf("recovered content = %q, want KEEP=1", snap.Files[0].Content)
	}
}

func TestRecoverDropsStaleBackup(t *testing.T) {
	v := newVault(t)
	if err := v.Save("myapp", "default", []File{{Path: ".env", Content: []byte("LIVE=1")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Backup present while the final slot also exists → stale, must be dropped.
	pdir := filepath.Join(v.root, "myapp")
	if err := os.MkdirAll(filepath.Join(pdir, ".default.old", blobsDir), dirPerm); err != nil {
		t.Fatalf("simulate stale backup: %v", err)
	}
	if _, err := OpenAt(v.root); err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pdir, ".default.old")); !os.IsNotExist(err) {
		t.Error("stale backup not dropped on recover")
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
