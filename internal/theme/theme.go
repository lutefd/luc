package theme

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	AppBorder   lipgloss.Style
	Header      lipgloss.Style
	Muted       lipgloss.Style
	UserBubble  lipgloss.Style
	AgentBubble lipgloss.Style
	ToolBubble  lipgloss.Style
	ErrorBubble lipgloss.Style
	Footer      lipgloss.Style
}

func Default() Theme {
	return Theme{
		AppBorder:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#4a6d7c")),
		Header:      lipgloss.NewStyle().Foreground(lipgloss.Color("#8ecae6")).Bold(true),
		Muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		UserBubble:  lipgloss.NewStyle().Foreground(lipgloss.Color("#edf6f9")).Background(lipgloss.Color("#344e41")).Padding(0, 1),
		AgentBubble: lipgloss.NewStyle().Foreground(lipgloss.Color("#edf6f9")).Background(lipgloss.Color("#264653")).Padding(0, 1),
		ToolBubble:  lipgloss.NewStyle().Foreground(lipgloss.Color("#edf6f9")).Background(lipgloss.Color("#1d3557")).Padding(0, 1),
		ErrorBubble: lipgloss.NewStyle().Foreground(lipgloss.Color("#f8edeb")).Background(lipgloss.Color("#9d0208")).Padding(0, 1),
		Footer:      lipgloss.NewStyle().Foreground(lipgloss.Color("#adb5bd")),
	}
}

func NewMarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(width),
	)
}
