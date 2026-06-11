// Package tui implements the interactive vault browser: a project list on the
// left, a stage-tabbed preview on the right. Launched by `envault` with no args.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/fmilioni/envault/internal/vault"
)

const (
	minWidth  = 50
	minHeight = 12

	leftMinWidth = 16
	leftMaxWidth = 30

	// lipgloss draws the border outside the .Width/.Height box and Padding(0,1)
	// eats 1 column each side — both panels share these so the math stays exact.
	panelBorder = 2
	panelHPad   = 2
	tabsHeight  = 2 // tab strip + one spacer row before the preview
	titleRows   = 1
	helpRows    = 1
)

// Selection is the (project, stage) a user picked in selector mode.
type Selection struct {
	Project string
	Stage   string
}

// Run opens the browser TUI over the given vault and blocks until the user quits.
func Run(v *vault.Vault) error {
	m, err := newModel(v)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// Select runs the browser as an interactive picker for `load`, pre-selecting the
// inferred project/stage. Returns the chosen snapshot, nil if the user cancelled,
// or nil when the vault is empty (nothing to pick).
func Select(v *vault.Vault, project, stage string) (*Selection, error) {
	return selectWith(v, project, stage, "load")
}

// SelectForDelete is Select with the picker labelled for the destructive
// `delete` flow, so the title/help read "delete" instead of "load".
func SelectForDelete(v *vault.Vault, project, stage string) (*Selection, error) {
	return selectWith(v, project, stage, "delete")
}

func selectWith(v *vault.Vault, project, stage, verb string) (*Selection, error) {
	m, err := newModel(v)
	if err != nil {
		return nil, err
	}
	if len(m.projects) == 0 {
		return nil, nil
	}
	m.selecting = true
	m.selectVerb = verb
	m.preselect(project, stage)
	out, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	return out.(model).chosen, nil
}

// SelectMulti runs the browser as a multi-project picker for `export`: the user
// toggles whole projects (each carries all its stages) while previewing them.
// preProject is pre-checked. Returns the chosen project names in vault order,
// nil if the user cancelled, or nil when the vault is empty.
func SelectMulti(v *vault.Vault, preProject string) ([]string, error) {
	m, err := newModel(v)
	if err != nil {
		return nil, err
	}
	if len(m.projects) == 0 {
		return nil, nil
	}
	m.multiSelecting = true
	m.checked = make(map[string]bool, len(m.projects))
	m.preselect(preProject, "")
	if preProject != "" {
		m.checked[preProject] = true
	}
	out, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	fm := out.(model)
	if !fm.confirmed {
		return nil, nil
	}
	var chosen []string
	for _, p := range fm.projects {
		if fm.checked[p] {
			chosen = append(chosen, p)
		}
	}
	return chosen, nil
}

type model struct {
	vault *vault.Vault

	projects []string
	projIdx  int

	stages   []string
	stageIdx int

	snap    *vault.Snapshot
	loadErr error

	vp     viewport.Model
	ready  bool
	width  int
	height int

	selecting  bool
	selectVerb string
	chosen     *Selection

	multiSelecting bool
	checked        map[string]bool
	confirmed      bool
}

func newModel(v *vault.Vault) (model, error) {
	projects, err := v.Projects()
	if err != nil {
		return model{}, err
	}
	m := model{vault: v, projects: projects}
	m.refreshStages()
	return m, nil
}

func (m model) Init() tea.Cmd { return nil }

// refreshStages reloads the stage list for the selected project and its
// snapshot. Keeps stageIdx in range when the new project has fewer stages.
func (m *model) refreshStages() {
	if m.loadStages() {
		m.refreshSnapshot()
	}
}

// loadStages loads the stage list for the selected project, resetting stageIdx.
// Returns false (with loadErr set) so callers skip the snapshot load on failure.
func (m *model) loadStages() bool {
	m.stages = nil
	m.stageIdx = 0
	if len(m.projects) == 0 {
		return true
	}
	stages, err := m.vault.Stages(m.projects[m.projIdx])
	if err != nil {
		m.loadErr = err
		m.snap = nil
		return false
	}
	m.stages = stages
	return true
}

// preselect moves the cursor to the inferred project/stage before the program
// starts; a stage with no match falls back to the first one (index 0).
func (m *model) preselect(project, stage string) {
	for i, p := range m.projects {
		if p == project {
			m.projIdx = i
			break
		}
	}
	if !m.loadStages() {
		return
	}
	for i, s := range m.stages {
		if s == stage {
			m.stageIdx = i
			break
		}
	}
	m.refreshSnapshot()
}

func (m *model) refreshSnapshot() {
	m.snap = nil
	m.loadErr = nil
	if len(m.projects) == 0 || len(m.stages) == 0 {
		return
	}
	snap, err := m.vault.Load(m.projects[m.projIdx], m.stages[m.stageIdx])
	if err != nil {
		m.loadErr = err
		return
	}
	m.snap = snap
	if m.ready {
		m.vp.SetContent(m.previewBody())
		m.vp.GotoTop()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit

		case "enter":
			if m.selecting && len(m.stages) > 0 && m.snap != nil {
				m.chosen = &Selection{Project: m.projects[m.projIdx], Stage: m.stages[m.stageIdx]}
				return m, tea.Quit
			}
			if m.multiSelecting && m.anyChecked() {
				m.confirmed = true
				return m, tea.Quit
			}

		case " ":
			if m.multiSelecting && len(m.projects) > 0 {
				p := m.projects[m.projIdx]
				m.checked[p] = !m.checked[p]
			}

		case "a":
			if m.multiSelecting {
				all := !m.allChecked()
				for _, p := range m.projects {
					m.checked[p] = all
				}
			}

		case "up", "k":
			if len(m.projects) > 0 {
				m.projIdx = (m.projIdx - 1 + len(m.projects)) % len(m.projects)
				m.refreshStages()
			}
		case "down", "j":
			if len(m.projects) > 0 {
				m.projIdx = (m.projIdx + 1) % len(m.projects)
				m.refreshStages()
			}

		case "left", "h", "shift+tab":
			if len(m.stages) > 0 {
				m.stageIdx = (m.stageIdx - 1 + len(m.stages)) % len(m.stages)
				m.refreshSnapshot()
			}
		case "right", "l", "tab":
			if len(m.stages) > 0 {
				m.stageIdx = (m.stageIdx + 1) % len(m.stages)
				m.refreshSnapshot()
			}

		case "pgup", "ctrl+u":
			m.vp.HalfPageUp()
		case "pgdown", "ctrl+d":
			m.vp.HalfPageDown()
		case "g", "home":
			m.vp.GotoTop()
		case "G", "end":
			m.vp.GotoBottom()
		}
	}
	return m, nil
}

// dims derives every panel size from the terminal size so layout() and View()
// never disagree. Returns the .Width/.Height values for the two panels plus the
// viewport's text size; all clamped to ≥1 (the View guards tiny terminals).
func (m model) dims() (leftW, rightW, previewW, bodyH, previewH int) {
	leftW = clamp(m.width/4, leftMinWidth, leftMaxWidth)
	rightW = atLeast1(m.width - (leftW + panelBorder) - panelBorder)
	previewW = atLeast1(rightW - panelHPad)
	bodyH = atLeast1(m.height - titleRows - helpRows - panelBorder)
	previewH = atLeast1(bodyH - tabsHeight)
	return
}

// layout (re)computes the viewport size on resize and is where it is first
// constructed — its content is set only once dimensions are known.
func (m *model) layout() {
	if m.width < minWidth || m.height < minHeight {
		m.ready = false
		return
	}
	_, _, previewW, _, previewH := m.dims()
	if !m.ready {
		m.vp = viewport.New(previewW, previewH)
		m.vp.KeyMap = viewport.KeyMap{} // driven manually to avoid key clashes
		m.ready = true
	} else {
		m.vp.Width = previewW
		m.vp.Height = previewH
	}
	m.vp.SetContent(m.previewBody())
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal too small — need at least %d×%d.\nPress q to quit.", minWidth, minHeight)
	}
	if len(m.projects) == 0 {
		return m.emptyView()
	}

	leftW, rightW, previewW, bodyH, _ := m.dims()

	left := panelStyle.Width(leftW).Height(bodyH).Render(m.projectsView(leftW))
	right := panelStyle.Width(rightW).Height(bodyH).Render(m.rightView(previewW))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(m.title()), body, m.helpView())
}

func (m model) title() string {
	switch {
	case m.selecting:
		return "Envault · " + m.selectVerb + " — pick a snapshot"
	case m.multiSelecting:
		return "Envault · export — pick projects"
	}
	return "Envault"
}

func (m model) projectsView(w int) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Projects"))
	b.WriteString("\n\n")
	for i, p := range m.projects {
		label := p
		if m.multiSelecting {
			box := "[ ] "
			if m.checked[p] {
				box = "[x] "
			}
			label = box + p
		}
		line := truncate(label, w-panelHPad)
		if i == m.projIdx {
			b.WriteString(selectedItemStyle.Render("› " + line))
		} else {
			b.WriteString(itemStyle.Render("  " + line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) anyChecked() bool {
	for _, v := range m.checked {
		if v {
			return true
		}
	}
	return false
}

func (m model) allChecked() bool {
	for _, p := range m.projects {
		if !m.checked[p] {
			return false
		}
	}
	return len(m.projects) > 0
}

func (m model) rightView(width int) string {
	return lipgloss.JoinVertical(lipgloss.Left, m.tabsView(width), "", m.vp.View())
}

// tabsView renders the stage tabs as a single row no wider than width. When the
// full strip doesn't fit, it shows a window around the active tab with ‹ › for
// the hidden ones — staying one line keeps the layout budget (tabsHeight) honest.
func (m model) tabsView(width int) string {
	if len(m.stages) == 0 {
		return dimStyle.Render("no stages")
	}
	chips := make([]string, len(m.stages))
	total := 0
	for i, s := range m.stages {
		if i == m.stageIdx {
			chips[i] = activeTabStyle.Render(s)
		} else {
			chips[i] = tabStyle.Render(s)
		}
		total += lipgloss.Width(chips[i])
	}
	if total <= width {
		return lipgloss.JoinHorizontal(lipgloss.Top, chips...)
	}

	lo, hi := m.stageIdx, m.stageIdx
	used := lipgloss.Width(chips[m.stageIdx])
	for {
		grew := false
		if hi+1 < len(chips) && used+lipgloss.Width(chips[hi+1])+2 <= width {
			hi++
			used += lipgloss.Width(chips[hi])
			grew = true
		}
		if lo-1 >= 0 && used+lipgloss.Width(chips[lo-1])+2 <= width {
			lo--
			used += lipgloss.Width(chips[lo])
			grew = true
		}
		if !grew {
			break
		}
	}
	strip := lipgloss.JoinHorizontal(lipgloss.Top, chips[lo:hi+1]...)
	if lo > 0 {
		strip = dimStyle.Render("‹") + strip
	}
	if hi < len(chips)-1 {
		strip = strip + dimStyle.Render("›")
	}
	return ansi.Truncate(strip, width, "›")
}

// previewBody renders the selected snapshot for the viewport: a saved-at line,
// then each file as a labeled block, stacked one above the other.
func (m model) previewBody() string {
	if m.loadErr != nil {
		return errStyle.Render("failed to read snapshot: " + m.loadErr.Error())
	}
	if len(m.stages) == 0 {
		return dimStyle.Render("This project has no saved stages.")
	}
	if m.snap == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("saved " + m.snap.SavedAt.Local().Format("2006-01-02 15:04")))
	b.WriteString("\n\n")
	for i, f := range m.snap.Files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fileHeaderStyle.Render("── " + f.Path + " ──"))
		b.WriteString("\n")
		content := string(f.Content)
		b.WriteString(content)
		if len(content) == 0 || content[len(content)-1] != '\n' {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m model) emptyView() string {
	hint := dimStyle.Width(atLeast1(m.width - 4)).Align(lipgloss.Center).
		Render("Run `envault save` in a project to store its .env files.")
	msg := lipgloss.JoinVertical(lipgloss.Center,
		headerStyle.Render("Your vault is empty"),
		"",
		hint,
	)
	box := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, msg)
	return lipgloss.JoinVertical(lipgloss.Left, box, m.helpView())
}

func (m model) helpView() string {
	keys := "↑/↓ project · ←/→ stage · pgup/pgdn scroll · q quit"
	switch {
	case m.selecting:
		keys = "↑/↓ project · ←/→ stage · enter " + m.selectVerb + " · q/esc cancel"
	case m.multiSelecting:
		keys = "↑/↓ project · space toggle · a all · enter export · q/esc cancel"
	}
	return helpStyle.Render(truncate(keys, m.width-panelHPad))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// truncate cuts s to max display cells, rune- and width-aware (ansi.Truncate
// counts cells, so accented/CJK names never split mid-rune).
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	return ansi.Truncate(s, max, "…")
}
