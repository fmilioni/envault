// Package envctx resolves the context of a folder for save/load: which .env
// files are present, which project they belong to, and which stage is in use.
// It is the single place that policy lives, shared by every command.
package envctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultStage = "default"

var envTemplates = map[string]bool{
	".env.example": true,
	".env.sample":  true,
}

// Context is the resolved view of a folder.
type Context struct {
	Dir     string
	Project string
	Stage   string
	Files   []string
}

// Resolve composes detection + inference for dir, honoring the optional
// --project / --stage overrides. An empty Files slice is valid (the command
// decides what to do with it).
func Resolve(dir, projectOverride, stageOverride string) (Context, error) {
	files, err := DetectEnvFiles(dir)
	if err != nil {
		return Context{}, err
	}
	project, err := InferProject(dir, projectOverride)
	if err != nil {
		return Context{}, err
	}
	return Context{
		Dir:     dir,
		Project: project,
		Stage:   ResolveStage(stageOverride),
		Files:   files,
	}, nil
}

// DetectEnvFiles returns the .env candidate file names directly in dir, sorted.
// Candidates are `.env` and `.env.*`; the `.env.example`/`.env.sample`
// templates and any directories are excluded.
func DetectEnvFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !isEnvCandidate(name) {
			continue
		}
		// Stat (not e.IsDir) so a symlink resolves to its target: a symlinked
		// .env file counts, a symlink to a directory does not.
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func isEnvCandidate(name string) bool {
	if envTemplates[name] {
		return false
	}
	return name == ".env" || (strings.HasPrefix(name, ".env.") && len(name) > len(".env."))
}

// InferProject resolves the project name for dir, slugified for safe use as a
// vault directory. Priority: override > package.json "name" > folder base name.
func InferProject(dir, override string) (string, error) {
	name := override
	if name == "" {
		name = projectFromPackageJSON(dir)
	}
	if name == "" {
		name = folderName(dir)
	}
	slug := Slugify(name)
	if slug == "" {
		return "", fmt.Errorf("could not infer a project name from %q", dir)
	}
	return slug, nil
}

// ResolveStage returns the slugified override, or DefaultStage when empty.
func ResolveStage(override string) string {
	if slug := Slugify(override); slug != "" {
		return slug
	}
	return DefaultStage
}

// Slugify normalizes a name into a filesystem-safe slug: lowercased, keeping
// [a-z0-9_.], with runs of any other characters collapsed to a single dash and
// trimmed off the ends. May return "" for a degenerate input.
//
// Invariant the vault relies on: any non-empty output passes vault.validateName
// — no path separators or NUL (stripped), and never "." or ".." (dot runs are
// collapsed and dots are trimmed off the ends). See TestSlugifyOutputIsVaultSafe.
func Slugify(name string) string {
	var b strings.Builder
	pendingDash := false
	last := rune(0)
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' {
			if r == '.' && last == '.' {
				continue
			}
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			last = r
		} else {
			pendingDash = true
		}
	}
	return strings.Trim(b.String(), "-._")
}

func projectFromPackageJSON(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return pkg.Name
}

func folderName(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(dir)
}
