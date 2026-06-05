package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fmilioni/envault/internal/vault"
)

func newTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.OpenAt(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return v
}

func key(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestEmptyVault(t *testing.T) {
	m, err := newModel(newTestVault(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.projects) != 0 {
		t.Fatalf("expected no projects, got %v", m.projects)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.View(); !strings.Contains(got, "empty") {
		t.Errorf("empty vault view missing empty-state hint:\n%s", got)
	}
}

func TestProjectsListedSorted(t *testing.T) {
	v := newTestVault(t)
	mustSave(t, v, "zeta", "dev", vault.File{Path: ".env", Content: []byte("Z=1\n")})
	mustSave(t, v, "alpha", "dev", vault.File{Path: ".env", Content: []byte("A=1\n")})

	m, err := newModel(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "zeta"}; !equal(m.projects, want) {
		t.Fatalf("projects = %v, want %v", m.projects, want)
	}
}

func TestNavigationAndPreview(t *testing.T) {
	v := newTestVault(t)
	mustSave(t, v, "alpha", "dev",
		vault.File{Path: ".env", Content: []byte("ALPHA_DEV=1\n")},
		vault.File{Path: ".env.local", Content: []byte("LOCAL=secret\n")},
	)
	mustSave(t, v, "alpha", "prod", vault.File{Path: ".env", Content: []byte("ALPHA_PROD=1\n")})
	mustSave(t, v, "beta", "dev", vault.File{Path: ".env", Content: []byte("BETA_DEV=1\n")})

	m, err := newModel(v)
	if err != nil {
		t.Fatal(err)
	}
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})

	// alpha selected: dev stage shows both stacked files.
	body := m.previewBody()
	for _, want := range []string{"ALPHA_DEV=1", ".env.local", "LOCAL=secret", "saved "} {
		if !strings.Contains(body, want) {
			t.Errorf("dev preview missing %q:\n%s", want, body)
		}
	}

	// Switch stage tab → prod.
	m = send(m, key("right"))
	if m.stages[m.stageIdx] != "prod" {
		t.Fatalf("stageIdx not on prod: %v", m.stages[m.stageIdx])
	}
	if body := m.previewBody(); !strings.Contains(body, "ALPHA_PROD=1") {
		t.Errorf("prod preview missing content:\n%s", body)
	}

	// Move to next project → beta; stageIdx resets and stages reload.
	m = send(m, key("down"))
	if m.projects[m.projIdx] != "beta" {
		t.Fatalf("projIdx not on beta: %v", m.projects[m.projIdx])
	}
	if m.stageIdx != 0 {
		t.Errorf("stageIdx not reset on project change: %d", m.stageIdx)
	}
	if want := []string{"dev"}; !equal(m.stages, want) {
		t.Errorf("beta stages = %v, want %v", m.stages, want)
	}
	if body := m.previewBody(); !strings.Contains(body, "BETA_DEV=1") {
		t.Errorf("beta preview missing content:\n%s", body)
	}
}

func TestProjectWithoutStages(t *testing.T) {
	v := newTestVault(t)
	// A project dir with no stage manifest (e.g. a pruned/half-written vault).
	if err := os.MkdirAll(filepath.Join(v.Root(), "ghost"), 0o700); err != nil {
		t.Fatal(err)
	}
	m, err := newModel(v)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(m.projects, []string{"ghost"}) {
		t.Fatalf("projects = %v, want [ghost]", m.projects)
	}
	if len(m.stages) != 0 {
		t.Fatalf("stages = %v, want none", m.stages)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if body := m.previewBody(); !strings.Contains(body, "no saved stages") {
		t.Errorf("preview missing no-stages hint:\n%s", body)
	}
	if tabs := m.tabsView(40); !strings.Contains(tabs, "no stages") {
		t.Errorf("tabs missing no-stages hint:\n%s", tabs)
	}
}

// TestTabStripStaysOneLine guards the tabsHeight budget: even with many stages
// on a narrow terminal the tab strip must render as a single line within width,
// always including the active tab — otherwise wrapped rows clip the preview.
func TestTabStripStaysOneLine(t *testing.T) {
	v := newTestVault(t)
	stages := []string{"dev", "prod", "staging", "qa", "preview", "sandbox", "demo"}
	for _, s := range stages {
		mustSave(t, v, "alpha", s, vault.File{Path: ".env", Content: []byte("K=1\n")})
	}
	m, err := newModel(v)
	if err != nil {
		t.Fatal(err)
	}
	m = send(m, tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	_, _, previewW, _, _ := m.dims()

	// Walk every stage as the active one; the strip stays 1 line and fits, and
	// the active tab is always present (m.stages is sorted, so index into it).
	for i, active := range m.stages {
		m.stageIdx = i
		strip := m.tabsView(previewW)
		if h := lipgloss.Height(strip); h != 1 {
			t.Errorf("stage %q: tab strip is %d lines, want 1:\n%s", active, h, strip)
		}
		if w := lipgloss.Width(strip); w > previewW {
			t.Errorf("stage %q: tab strip width %d exceeds %d", active, w, previewW)
		}
		if !strings.Contains(strip, active) {
			t.Errorf("stage %q: active tab not visible in strip:\n%s", active, strip)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	m, err := newModel(newTestVault(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"q", "ctrl+c"} {
		var msg tea.KeyMsg
		if k == "ctrl+c" {
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		} else {
			msg = key(k)
		}
		if _, cmd := m.Update(msg); cmd == nil {
			t.Errorf("key %q did not return a command (expected Quit)", k)
		}
	}
}

// TestViewNeverOverflows locks in the responsive layout: the rendered View must
// always fit within the terminal, populated or empty, down to the min size.
func TestViewNeverOverflows(t *testing.T) {
	v := newTestVault(t)
	mustSave(t, v, "alpha", "dev",
		vault.File{Path: ".env", Content: []byte("A=1\n")},
		vault.File{Path: ".env.local", Content: []byte("LONG=some-fairly-long-value-here\n")},
	)
	mustSave(t, v, "alpha", "prod", vault.File{Path: ".env", Content: []byte("A=9\n")})

	sizes := [][2]int{{minWidth, minHeight}, {51, 13}, {80, 24}, {120, 40}, {200, 60}, {minWidth, 50}}
	for _, tv := range []*vault.Vault{v, newTestVault(t)} {
		m, err := newModel(tv)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sizes {
			mm := send(m, tea.WindowSizeMsg{Width: s[0], Height: s[1]})
			view := mm.View()
			if w := lipgloss.Width(view); w > s[0] {
				t.Errorf("width %d exceeds terminal %d at %dx%d", w, s[0], s[0], s[1])
			}
			if h := lipgloss.Height(view); h > s[1] {
				t.Errorf("height %d exceeds terminal %d at %dx%d", h, s[1], s[0], s[1])
			}
		}
	}
}

func mustSave(t *testing.T, v *vault.Vault, project, stage string, files ...vault.File) {
	t.Helper()
	if err := v.Save(project, stage, files); err != nil {
		t.Fatalf("Save(%s/%s): %v", project, stage, err)
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
