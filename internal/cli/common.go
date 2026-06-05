package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fmilioni/envault/internal/vault"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// openVault opens the vault, honoring ENVAULT_HOME as a root override so tests
// and power users can point it away from ~/.envault.
func openVault() (*vault.Vault, error) {
	if home := os.Getenv("ENVAULT_HOME"); home != "" {
		return vault.OpenAt(home)
	}
	return vault.Open()
}

// stdinIsTTY is the seam for deciding whether the interactive TUI / password
// prompt can run; tests point it away from a real terminal.
var stdinIsTTY = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// resolvePassword obtains a bundle password without ever echoing it. ENVAULT_PASSWORD
// wins when set (the scripting/CI/test path); otherwise a TTY is prompted (twice when
// confirm is set, e.g. on export). A non-TTY with no env var is an error so we never
// silently produce an unencrypted-by-accident or hung command.
func resolvePassword(confirm bool) (string, error) {
	if pw := os.Getenv("ENVAULT_PASSWORD"); pw != "" {
		return pw, nil
	}
	if !stdinIsTTY() {
		return "", errors.New("no password available: set ENVAULT_PASSWORD or run in an interactive terminal")
	}
	pw, err := readSecret("Password: ")
	if err != nil {
		return "", err
	}
	if pw == "" {
		return "", errors.New("password is empty")
	}
	if confirm {
		again, err := readSecret("Confirm password: ")
		if err != nil {
			return "", err
		}
		if pw != again {
			return "", errors.New("passwords do not match")
		}
	}
	return pw, nil
}

func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// readFilesFromDir loads each path (relative to dir) into a vault.File, with the
// stored Path normalized the same way the vault stores it, so later diffs line up.
func readFilesFromDir(dir string, paths []string) ([]vault.File, error) {
	files := make([]vault.File, 0, len(paths))
	for _, p := range paths {
		if err := vault.ValidateRelPath(p); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			return nil, err
		}
		files = append(files, vault.File{Path: filepath.ToSlash(filepath.Clean(p)), Content: content})
	}
	return files, nil
}

func snapshotsEqual(a, b []vault.File) bool {
	if len(a) != len(b) {
		return false
	}
	byA := byPath(a)
	for _, f := range b {
		c, ok := byA[f.Path]
		if !ok || !bytes.Equal(c, f.Content) {
			return false
		}
	}
	return true
}

// confirm reads a y/N answer; empty input or EOF is a safe no.
func confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	fmt.Fprintf(w, "%s [y/N]: ", prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// selectFiles prompts a numbered multi-select over candidates. Accepts "a"/"all"
// or a comma-separated list of 1-based indices. Returns the chosen paths.
func selectFiles(r io.Reader, w io.Writer, candidates []string) ([]string, error) {
	fmt.Fprintln(w, "Multiple .env files found:")
	for i, c := range candidates {
		fmt.Fprintf(w, "  %d) %s\n", i+1, c)
	}
	fmt.Fprint(w, "Select files to save (comma-separated numbers, 'a' for all): ")
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "a" || answer == "all" {
		return candidates, nil
	}
	var chosen []string
	seen := make(map[int]struct{})
	for _, tok := range strings.Split(answer, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n < 1 || n > len(candidates) {
			return nil, fmt.Errorf("invalid selection %q", tok)
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		chosen = append(chosen, candidates[n-1])
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no files selected")
	}
	return chosen, nil
}
