package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	manifestVersion = 1
	dirPerm         = 0o700
	filePerm        = 0o600
	manifestName    = "manifest.json"
	blobsDir        = "blobs"
)

// File is one .env file inside a snapshot, identified by its origin path
// relative to the folder it was saved from.
type File struct {
	Path    string
	Content []byte
}

// Snapshot is the saved state of a single (project, stage): one or more files
// plus the time they were captured.
type Snapshot struct {
	Project string
	Stage   string
	SavedAt time.Time
	Files   []File
}

type fileEntry struct {
	Path string `json:"path"`
	Blob string `json:"blob"`
}

type manifest struct {
	Version int         `json:"version"`
	SavedAt time.Time   `json:"savedAt"`
	Files   []fileEntry `json:"files"`
}

// NotFoundError is returned by Load/Delete when no snapshot exists for the
// requested (project, stage).
type NotFoundError struct {
	Project string
	Stage   string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no snapshot for %s/%s", e.Project, e.Stage)
}

// IsNotFound reports whether err is a NotFoundError.
func IsNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

// Vault is the global store rooted at ~/.envault (or a custom root in tests).
type Vault struct {
	root string
}

// Open resolves the vault at ~/.envault, creating it with 0700 if absent.
func Open() (*Vault, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(filepath.Join(home, ".envault"))
}

// OpenAt resolves the vault at an explicit root — used by tests.
func OpenAt(root string) (*Vault, error) {
	if root == "" {
		return nil, errors.New("vault root is empty")
	}
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, dirPerm); err != nil {
		return nil, err
	}
	recoverOrphans(root)
	return &Vault{root: root}, nil
}

func (v *Vault) Root() string { return v.root }

// Save writes files as the snapshot for (project, stage), overwriting any
// existing one. The snapshot is staged in a temp dir; the swap moves the old
// snapshot aside, renames the new one into place, then drops the old — so a
// crash leaves either the old or the new snapshot intact, never neither.
func (v *Vault) Save(project, stage string, files []File) error {
	if err := validateName("project", project); err != nil {
		return err
	}
	if err := validateName("stage", stage); err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no files to save")
	}
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		if err := ValidateRelPath(f.Path); err != nil {
			return err
		}
		key := filepath.ToSlash(filepath.Clean(f.Path))
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate file path %q", f.Path)
		}
		seen[key] = struct{}{}
	}

	projectDir := filepath.Join(v.root, project)
	if err := os.MkdirAll(projectDir, dirPerm); err != nil {
		return err
	}
	if err := os.Chmod(projectDir, dirPerm); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp(projectDir, "."+stage+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, dirPerm); err != nil {
		return err
	}

	blobs := filepath.Join(tmp, blobsDir)
	if err := os.Mkdir(blobs, dirPerm); err != nil {
		return err
	}

	m := manifest{
		Version: manifestVersion,
		SavedAt: time.Now().UTC(),
		Files:   make([]fileEntry, len(files)),
	}
	for i, f := range files {
		blob := fmt.Sprintf("%03d", i)
		if err := os.WriteFile(filepath.Join(blobs, blob), f.Content, filePerm); err != nil {
			return err
		}
		m.Files[i] = fileEntry{Path: filepath.ToSlash(f.Path), Blob: blob}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(tmp, manifestName), data, filePerm); err != nil {
		return err
	}

	final := filepath.Join(projectDir, stage)
	if _, err := os.Stat(final); err == nil {
		backup := filepath.Join(projectDir, "."+stage+".old")
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
		if err := os.Rename(final, backup); err != nil {
			return err
		}
		if err := os.Rename(tmp, final); err != nil {
			_ = os.Rename(backup, final)
			return err
		}
		return os.RemoveAll(backup)
	}
	return os.Rename(tmp, final)
}

// recoverOrphans cleans up after a Save interrupted mid-swap. A staging dir
// (".<stage>.tmp-*") is an uncommitted write and is always discarded. A backup
// (".<stage>.old") is the previous snapshot: if its final slot is missing the
// crash hit between the two renames, so restore it; otherwise it's stale.
// Assumes no concurrent Save on the same vault (single-user CLI).
func recoverOrphans(root string) {
	projects, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		pdir := filepath.Join(root, p.Name())
		entries, err := os.ReadDir(pdir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || !strings.HasPrefix(name, ".") {
				continue
			}
			switch {
			// `.old` first: a staging dir is ".<stage>.tmp-<rand>" and never ends
			// in ".old", so a backup whose stage slug contains ".tmp-" isn't
			// mistaken for staging and discarded.
			case strings.HasSuffix(name, ".old"):
				stage := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".old")
				final := filepath.Join(pdir, stage)
				if _, err := os.Stat(final); os.IsNotExist(err) {
					_ = os.Rename(filepath.Join(pdir, name), final)
				} else {
					_ = os.RemoveAll(filepath.Join(pdir, name))
				}
			case strings.Contains(name, ".tmp-"):
				_ = os.RemoveAll(filepath.Join(pdir, name))
			}
		}
	}
}

// Load reads the snapshot for (project, stage). Returns *NotFoundError when it
// doesn't exist.
func (v *Vault) Load(project, stage string) (*Snapshot, error) {
	if err := validateName("project", project); err != nil {
		return nil, err
	}
	if err := validateName("stage", stage); err != nil {
		return nil, err
	}

	dir := filepath.Join(v.root, project, stage)
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &NotFoundError{Project: project, Stage: stage}
		}
		return nil, err
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("corrupt manifest for %s/%s: %w", project, stage, err)
	}

	snap := &Snapshot{
		Project: project,
		Stage:   stage,
		SavedAt: m.SavedAt,
		Files:   make([]File, len(m.Files)),
	}
	for i, fe := range m.Files {
		content, err := os.ReadFile(filepath.Join(dir, blobsDir, fe.Blob))
		if err != nil {
			return nil, fmt.Errorf("reading blob for %q: %w", fe.Path, err)
		}
		snap.Files[i] = File{Path: fe.Path, Content: content}
	}
	return snap, nil
}

// Exists reports whether a snapshot exists for (project, stage).
func (v *Vault) Exists(project, stage string) (bool, error) {
	if err := validateName("project", project); err != nil {
		return false, err
	}
	if err := validateName("stage", stage); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(v.root, project, stage, manifestName))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Projects lists project names that hold at least one snapshot, sorted.
func (v *Vault) Projects() ([]string, error) {
	entries, err := os.ReadDir(v.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Stages lists the stages saved under a project, sorted. A directory only
// counts as a stage when it holds a manifest.
func (v *Vault) Stages(project string) ([]string, error) {
	if err := validateName("project", project); err != nil {
		return nil, err
	}
	dir := filepath.Join(v.root, project)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), manifestName)); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Delete removes the snapshot for (project, stage), pruning the project dir if
// it becomes empty. Returns *NotFoundError when nothing is there.
func (v *Vault) Delete(project, stage string) error {
	if err := validateName("project", project); err != nil {
		return err
	}
	if err := validateName("stage", stage); err != nil {
		return err
	}
	dir := filepath.Join(v.root, project, stage)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{Project: project, Stage: stage}
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	pruneIfEmpty(filepath.Join(v.root, project))
	return nil
}

func pruneIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

func validateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s name is empty", kind)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	if strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	return nil
}

// ValidateRelPath rejects empty, absolute, or folder-escaping paths. Shared with
// the CLI so the same traversal guard protects both storage and restore writes.
func ValidateRelPath(p string) error {
	if p == "" {
		return errors.New("file path is empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("file path must be relative: %q", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("file path escapes the folder: %q", p)
	}
	return nil
}
