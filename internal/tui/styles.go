package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent = lipgloss.AdaptiveColor{Light: "#0f766e", Dark: "#2dd4bf"} // teal
	subtle = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#6b7280"}
	border = lipgloss.AdaptiveColor{Light: "#d4d4d8", Dark: "#3f3f46"}

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(accent).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)

	itemStyle         = lipgloss.NewStyle()
	selectedItemStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)

	tabBg = lipgloss.AdaptiveColor{Light: "#e4e4e7", Dark: "#27272a"}
	tabFg = lipgloss.AdaptiveColor{Light: "#3f3f46", Dark: "#d4d4d8"}

	tabStyle = lipgloss.NewStyle().
			Foreground(tabFg).
			Background(tabBg).
			Padding(0, 1).
			MarginRight(1)
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(accent).
			Padding(0, 1).
			MarginRight(1)

	fileHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle        = lipgloss.NewStyle().Foreground(subtle)
	errStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))

	helpStyle = lipgloss.NewStyle().Foreground(subtle).Padding(0, 1)
)
