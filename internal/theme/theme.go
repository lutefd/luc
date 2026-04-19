package theme

import (
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	lipgloss "charm.land/lipgloss/v2"
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
	UserBubble       lipgloss.Style
	UserPrefix       lipgloss.Style
	AssistantLabel   lipgloss.Style
	AssistantBody    lipgloss.Style
	AssistantPrefix  lipgloss.Style
	ToolCard         lipgloss.Style
	ToolTitle        lipgloss.Style
	ErrorCard        lipgloss.Style
	Sidebar          lipgloss.Style
	SidebarTitle     lipgloss.Style
	SidebarLabel     lipgloss.Style
	SidebarValue     lipgloss.Style
	SidebarSection   lipgloss.Style
	SidebarTabActive lipgloss.Style
	SidebarTab       lipgloss.Style
	InputFrame       lipgloss.Style
	InputPrompt      lipgloss.Style
	InputPlaceholder lipgloss.Style
	InputText        lipgloss.Style
	Footer           lipgloss.Style
	StatusReady      lipgloss.Style
	StatusBusy       lipgloss.Style
	StatusError      lipgloss.Style
	PaletteFrame     lipgloss.Style
	PaletteActive    lipgloss.Style
	DiffAdd          lipgloss.Style
	DiffDel          lipgloss.Style
	DiffContext      lipgloss.Style
	DiffHunk         lipgloss.Style
	DiffGutter       lipgloss.Style
}

func Default(variant string) Theme {
	p := paletteFor(ResolveVariant(variant))
	panel := lipgloss.Color(p.panel)
	line := lipgloss.Color(p.line)
	accent := lipgloss.Color(p.accent)
	accentAlt := lipgloss.Color(p.accentAlt)
	text := lipgloss.Color(p.text)
	muted := lipgloss.Color(p.muted)
	blue := lipgloss.Color(p.blue)
	cyan := lipgloss.Color(p.cyan)
	success := lipgloss.Color(p.success)
	warn := lipgloss.Color(p.warn)
	errorText := lipgloss.Color(p.errorText)

	return Theme{
		App:              lipgloss.NewStyle().Foreground(text),
		HeaderBrand:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		HeaderMeta:       lipgloss.NewStyle().Foreground(muted),
		HeaderRule:       lipgloss.NewStyle().Foreground(accentAlt),
		Body:             lipgloss.NewStyle().Foreground(text),
		Muted:            lipgloss.NewStyle().Foreground(muted),
		Subtle:           lipgloss.NewStyle().Foreground(lipgloss.Color(p.subtle)),
		UserLabel:        lipgloss.NewStyle().Foreground(cyan).Bold(true),
		UserBubble:       lipgloss.NewStyle().Foreground(text),
		UserPrefix:       lipgloss.NewStyle().Foreground(cyan),
		AssistantLabel:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		AssistantBody:    lipgloss.NewStyle().Foreground(text),
		AssistantPrefix:  lipgloss.NewStyle().Foreground(accent),
		ToolCard:         lipgloss.NewStyle().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		ToolTitle:        lipgloss.NewStyle().Foreground(blue).Bold(true),
		ErrorCard:        lipgloss.NewStyle().Foreground(errorText).Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1),
		Sidebar:          lipgloss.NewStyle().Background(panel).Foreground(text).Padding(1, 2),
		SidebarTitle:     lipgloss.NewStyle().Background(panel).Foreground(accent).Bold(true),
		SidebarLabel:     lipgloss.NewStyle().Background(panel).Foreground(muted),
		SidebarValue:     lipgloss.NewStyle().Background(panel).Foreground(text),
		SidebarSection:   lipgloss.NewStyle().Background(panel).Foreground(line),
		SidebarTabActive: lipgloss.NewStyle().Background(panel).Foreground(accent).Bold(true).Underline(true),
		SidebarTab:       lipgloss.NewStyle().Background(panel).Foreground(muted),
		InputFrame:       lipgloss.NewStyle().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		InputPrompt:      lipgloss.NewStyle().Foreground(cyan).Bold(true),
		InputPlaceholder: lipgloss.NewStyle().Foreground(muted),
		InputText:        lipgloss.NewStyle().Foreground(text),
		Footer:           lipgloss.NewStyle().Foreground(muted),
		StatusReady:      lipgloss.NewStyle().Foreground(success),
		StatusBusy:       lipgloss.NewStyle().Foreground(blue),
		StatusError:      lipgloss.NewStyle().Foreground(warn),
		PaletteFrame:     lipgloss.NewStyle().Background(panel).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		PaletteActive:    lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color(p.bg)).Bold(true),
		DiffAdd:          lipgloss.NewStyle().Background(lipgloss.Color(p.diffAddBG)).Foreground(lipgloss.Color(p.diffAddFG)),
		DiffDel:          lipgloss.NewStyle().Background(lipgloss.Color(p.diffDelBG)).Foreground(lipgloss.Color(p.diffDelFG)),
		DiffContext:      lipgloss.NewStyle().Foreground(text),
		DiffHunk:         lipgloss.NewStyle().Foreground(muted).Italic(true),
		DiffGutter:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.subtle)),
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
			return VariantDark
		}
	default:
		return VariantDark
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
	diffAddBG string
	diffAddFG string
	diffDelBG string
	diffDelFG string
}

func paletteFor(variant string) palette {
	if variant == VariantDark {
		return palette{
			bg:        "#0d1117",
			panel:     "#161b22",
			panelAlt:  "#1c2128",
			line:      "#30363d",
			accent:    "#58a6ff",
			accentAlt: "#6e7681",
			text:      "#e6edf3",
			muted:     "#7d8590",
			subtle:    "#484f58",
			success:   "#3fb950",
			warn:      "#d29922",
			blue:      "#58a6ff",
			cyan:      "#39d2c0",
			errorText: "#f85149",
			diffAddBG: "#1b3a26",
			diffAddFG: "#aff5b4",
			diffDelBG: "#3a1b26",
			diffDelFG: "#ffc2ce",
		}
	}

	return palette{
		bg:        "#ffffff",
		panel:     "#f6f8fa",
		panelAlt:  "#f0f3f6",
		line:      "#d0d7de",
		accent:    "#0969da",
		accentAlt: "#8c959f",
		text:      "#1f2328",
		muted:     "#656d76",
		subtle:    "#8c959f",
		success:   "#1a7f37",
		warn:      "#9a6700",
		blue:      "#0969da",
		cyan:      "#0e7070",
		errorText: "#cf222e",
		diffAddBG: "#d1f2d8",
		diffAddFG: "#0a3416",
		diffDelBG: "#f5d1d8",
		diffDelFG: "#67060c",
	}
}
