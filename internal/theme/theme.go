package theme

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

const (
	VariantLight = "light"
	VariantDark  = "dark"
)

type Theme struct {
	App              lipgloss.Style
	HeaderBrand      lipgloss.Style
	HeaderMeta       lipgloss.Style
	HeaderRule       lipgloss.Style
	Body             lipgloss.Style
	Muted            lipgloss.Style
	Subtle           lipgloss.Style
	UserLabel        lipgloss.Style
	AssistantLabel   lipgloss.Style
	AssistantBody    lipgloss.Style
	ToolCard         lipgloss.Style
	ToolTitle        lipgloss.Style
	ErrorCard        lipgloss.Style
	Sidebar          lipgloss.Style
	SidebarTitle     lipgloss.Style
	SidebarLabel     lipgloss.Style
	SidebarValue     lipgloss.Style
	SidebarSection   lipgloss.Style
	InputFrame       lipgloss.Style
	InputPrompt      lipgloss.Style
	InputPlaceholder lipgloss.Style
	InputText        lipgloss.Style
	Footer           lipgloss.Style
	StatusReady      lipgloss.Style
	StatusBusy       lipgloss.Style
	StatusError      lipgloss.Style
}

func Default(variant string) Theme {
	palette := paletteFor(ResolveVariant(variant))
	bg := lipgloss.Color(palette.bg)
	panel := lipgloss.Color(palette.panel)
	panelAlt := lipgloss.Color(palette.panelAlt)
	line := lipgloss.Color(palette.line)
	accent := lipgloss.Color(palette.accent)
	accentAlt := lipgloss.Color(palette.accentAlt)
	text := lipgloss.Color(palette.text)
	muted := lipgloss.Color(palette.muted)
	blue := lipgloss.Color(palette.blue)
	cyan := lipgloss.Color(palette.cyan)
	success := lipgloss.Color(palette.success)
	warn := lipgloss.Color(palette.warn)
	errorText := lipgloss.Color(palette.errorText)

	return Theme{
		App:              lipgloss.NewStyle().Background(bg).Foreground(text),
		HeaderBrand:      lipgloss.NewStyle().Background(bg).Foreground(accent).Bold(true),
		HeaderMeta:       lipgloss.NewStyle().Background(bg).Foreground(muted),
		HeaderRule:       lipgloss.NewStyle().Background(bg).Foreground(accentAlt),
		Body:             lipgloss.NewStyle().Background(bg).Foreground(text),
		Muted:            lipgloss.NewStyle().Background(bg).Foreground(muted),
		Subtle:           lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(palette.subtle)),
		UserLabel:        lipgloss.NewStyle().Background(bg).Foreground(cyan).Bold(true),
		AssistantLabel:   lipgloss.NewStyle().Background(bg).Foreground(accent).Bold(true),
		AssistantBody:    lipgloss.NewStyle().Background(bg).Foreground(text),
		ToolCard:         lipgloss.NewStyle().Background(panelAlt).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		ToolTitle:        lipgloss.NewStyle().Background(panelAlt).Foreground(blue).Bold(true),
		ErrorCard:        lipgloss.NewStyle().Background(panelAlt).Foreground(errorText).Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1),
		Sidebar:          lipgloss.NewStyle().Background(panel).Foreground(text).Padding(1, 2),
		SidebarTitle:     lipgloss.NewStyle().Background(panel).Foreground(accent).Bold(true),
		SidebarLabel:     lipgloss.NewStyle().Background(panel).Foreground(muted),
		SidebarValue:     lipgloss.NewStyle().Background(panel).Foreground(text),
		SidebarSection:   lipgloss.NewStyle().Background(panel).Foreground(line),
		InputFrame:       lipgloss.NewStyle().Background(panel).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		InputPrompt:      lipgloss.NewStyle().Background(panel).Foreground(cyan).Bold(true),
		InputPlaceholder: lipgloss.NewStyle().Background(panel).Foreground(muted),
		InputText:        lipgloss.NewStyle().Background(panel).Foreground(text),
		Footer:           lipgloss.NewStyle().Background(bg).Foreground(muted),
		StatusReady:      lipgloss.NewStyle().Background(bg).Foreground(success),
		StatusBusy:       lipgloss.NewStyle().Background(bg).Foreground(blue),
		StatusError:      lipgloss.NewStyle().Background(bg).Foreground(warn),
	}
}

func NewMarkdownRenderer(width int, variant string) (*glamour.TermRenderer, error) {
	style := styles.LightStyle
	if ResolveVariant(variant) == VariantDark {
		style = styles.TokyoNightStyle
	}
	return glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
}

func ResolveVariant(variant string) string {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case VariantDark:
		return VariantDark
	case VariantLight:
		return VariantLight
	case "", "auto":
		switch strings.ToLower(strings.TrimSpace(os.Getenv("LUC_THEME"))) {
		case VariantDark:
			return VariantDark
		case VariantLight:
			return VariantLight
		default:
			return VariantLight
		}
	default:
		return VariantLight
	}
}

type palette struct {
	bg        string
	panel     string
	panelAlt  string
	line      string
	accent    string
	accentAlt string
	text      string
	muted     string
	subtle    string
	success   string
	warn      string
	blue      string
	cyan      string
	errorText string
}

func paletteFor(variant string) palette {
	if variant == VariantDark {
		return palette{
			bg:        "#16131f",
			panel:     "#1d1828",
			panelAlt:  "#221c2f",
			line:      "#51456b",
			accent:    "#ff4fd8",
			accentAlt: "#6d5cff",
			text:      "#f3efff",
			muted:     "#8e86a3",
			subtle:    "#6f6684",
			success:   "#38d39f",
			warn:      "#ff7aab",
			blue:      "#5dc8ff",
			cyan:      "#42e6cf",
			errorText: "#ffd3df",
		}
	}

	return palette{
		bg:        "#f6f1ff",
		panel:     "#ebe4f7",
		panelAlt:  "#f2ebfb",
		line:      "#c8bddb",
		accent:    "#c214b8",
		accentAlt: "#6a43d3",
		text:      "#20192d",
		muted:     "#756a8d",
		subtle:    "#8f84a8",
		success:   "#157f61",
		warn:      "#c62f68",
		blue:      "#2160d8",
		cyan:      "#0f9f92",
		errorText: "#5b1730",
	}
}
