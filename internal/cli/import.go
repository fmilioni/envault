package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fmilioni/envault/internal/bundle"
	"github.com/fmilioni/envault/internal/vault"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "import <bundle>",
		Short: "Import a bundle into the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "overwrite differing snapshots without confirmation")
	return cmd
}

func runImport(cmd *cobra.Command, path string, yes bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	b, err := bundle.Parse(data)
	if err != nil {
		return err
	}

	password := ""
	if b.IsEncrypted() {
		password, err = resolvePassword(false)
		if err != nil {
			return err
		}
	}

	// Decode (and thus authenticate) the whole payload before touching the vault,
	// so a wrong password or corrupt bundle never leaves a partial import.
	payload, err := b.Decode(password)
	if err != nil {
		return err
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	// Pre-flight every item (names, paths, non-empty) before the first write, so a
	// bad/tampered bundle is rejected whole rather than leaving a partial import.
	for _, p := range payload.Projects {
		for _, s := range p.Stages {
			if err := v.ValidateSnapshot(p.Name, s.Name, toVaultFiles(s.Files)); err != nil {
				return fmt.Errorf("refusing to import %s/%s: %w", p.Name, s.Name, err)
			}
		}
	}

	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())
	var added, overwritten, unchanged, skipped int
	applyAll := yes

	for _, p := range payload.Projects {
		for _, s := range p.Stages {
			files := toVaultFiles(s.Files)

			exists, err := v.Exists(p.Name, s.Name)
			if err != nil {
				return err
			}
			if !exists {
				if err := v.SaveAt(p.Name, s.Name, files, s.SavedAt); err != nil {
					return err
				}
				added++
				fmt.Fprintf(out, "+ %s/%s (new)\n", p.Name, s.Name)
				continue
			}

			current, err := v.Load(p.Name, s.Name)
			if err != nil {
				return err
			}
			if snapshotsEqual(current.Files, files) {
				unchanged++
				continue
			}

			if !applyAll {
				showConflict(out, p.Name, s.Name, current, s.SavedAt, files)
				dec, err := promptOverwrite(reader, out, fmt.Sprintf("Overwrite %s/%s?", p.Name, s.Name))
				if err != nil {
					return err
				}
				switch dec {
				case decAll:
					applyAll = true
				case decNo:
					skipped++
					continue
				case decQuit:
					skipped++
					fmt.Fprintln(out, "Stopped.")
					return importSummary(out, added, overwritten, unchanged, skipped)
				}
			}

			if err := v.SaveAt(p.Name, s.Name, files, s.SavedAt); err != nil {
				return err
			}
			overwritten++
			fmt.Fprintf(out, "~ %s/%s (overwritten)\n", p.Name, s.Name)
		}
	}

	return importSummary(out, added, overwritten, unchanged, skipped)
}

func importSummary(out io.Writer, added, overwritten, unchanged, skipped int) error {
	fmt.Fprintf(out, "Imported: %d new, %d overwritten, %d unchanged, %d skipped.\n",
		added, overwritten, unchanged, skipped)
	return nil
}

// showConflict prints the savedAt dates of both sides and a per-file line diff
// (vault → bundle) over the union of the two file sets.
func showConflict(w io.Writer, project, stage string, current *vault.Snapshot, bundleSavedAt time.Time, incoming []vault.File) {
	fmt.Fprintf(w, "\nConflict on %s/%s:\n", project, stage)
	fmt.Fprintf(w, "  vault saved %s   |   bundle saved %s\n",
		current.SavedAt.Local().Format("2006-01-02 15:04"),
		bundleSavedAt.Local().Format("2006-01-02 15:04"))

	cur := byPath(current.Files)
	next := byPath(incoming)
	for _, path := range unionPaths(cur, next) {
		fmt.Fprintf(w, "  ── %s ──\n", path)
		renderDiff(w, lineDiff(string(cur[path]), string(next[path])))
	}
}

func byPath(files []vault.File) map[string][]byte {
	m := make(map[string][]byte, len(files))
	for _, f := range files {
		m[f.Path] = f.Content
	}
	return m
}

func unionPaths(a, b map[string][]byte) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for p := range a {
		seen[p] = struct{}{}
	}
	for p := range b {
		seen[p] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

type overwriteDecision int

const (
	decNo overwriteDecision = iota
	decYes
	decAll
	decQuit
)

// promptOverwrite reads one y/N/a/q answer. Empty/EOF is a safe no.
func promptOverwrite(r *bufio.Reader, w io.Writer, prompt string) (overwriteDecision, error) {
	fmt.Fprintf(w, "%s [y/N/a/q]: ", prompt)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return decNo, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return decYes, nil
	case "a", "all":
		return decAll, nil
	case "q", "quit":
		return decQuit, nil
	default:
		return decNo, nil
	}
}

func toVaultFiles(fs []bundle.File) []vault.File {
	out := make([]vault.File, len(fs))
	for i, f := range fs {
		out[i] = vault.File{Path: f.Path, Content: f.Content}
	}
	return out
}
