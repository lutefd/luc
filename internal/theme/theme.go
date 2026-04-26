package theme

import (
	"image/color"
	"os"
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/extensions"
)

const (
	VariantLight = "light"
	VariantDark  = "dark"
)

// Theme holds the per-surface styles used by the TUI.
//
// Design: the theme does NOT paint container backgrounds. Every style in this
// struct sets only foreground colors (and, where useful, border colors). The
// only exceptions are genuine highlights that must visually differ from the
// base surface:
//
//   - PaletteActive — inverted selection row in pickers.
//   - DiffAdd / DiffDel — content-level color blocks.
//
// The terminal's default background is set via tea.View.BackgroundColor (OSC
// 11). Every cell the lipgloss styles don't paint picks up that color for
// free. Mixing OSC 11 with per-cell Background(bg) on the same hex produced
// visibly different shades (terminals apply the two rendering paths with
// different blending/profile handling), so we pick one source of truth — OSC
// 11 — and keep container surfaces transparent.
type Theme struct {
	Background color.Color // target for tea.View.BackgroundColor (OSC 11)

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
	// PaletteSurface is kept for API compatibility with the picker views; it
	// is now a plain foreground-only style (no panel paint), since we rely on
	// the terminal bg from OSC 11 to fill the palette interior.
	PaletteSurface lipgloss.Style
	PaletteActive  lipgloss.Style
	DiffAdd        lipgloss.Style
	DiffDel        lipgloss.Style
	DiffContext    lipgloss.Style
	DiffHunk       lipgloss.Style
	DiffGutter     lipgloss.Style
}

func Default(variant string) Theme {
	return fromPalette(paletteFor(ResolveVariant(variant)))
}

func Load(name, workspaceRoot string) (Theme, string, error) {
	if def, found, err := extensions.LoadTheme(workspaceRoot, name); err != nil {
		return Default(ResolveVariant(name)), ResolveVariant(name), err
	} else if found {
		variant := ResolveVariant(def.Inherits)
		p := applyThemeColors(paletteFor(variant), def.Colors)
		return fromPalette(p), variant, nil
	}
	variant := ResolveVariant(name)
	return Default(variant), variant, nil
}

func fromPalette(p palette) Theme {
	bg := lipgloss.Color(p.bg)
	accent := lipgloss.Color(p.accent)
	accentAlt := lipgloss.Color(p.accentAlt)
	line := lipgloss.Color(p.line)
	text := lipgloss.Color(p.text)
	muted := lipgloss.Color(p.muted)
	subtle := lipgloss.Color(p.subtle)
	blue := lipgloss.Color(p.blue)
	cyan := lipgloss.Color(p.cyan)
	success := lipgloss.Color(p.success)
	warn := lipgloss.Color(p.warn)
	errorText := lipgloss.Color(p.errorText)

	fg := func(c color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(c)
	}

	return Theme{
		// OSC 11 — terminal repaints its default bg to the theme's color.
		// Every unpainted cell then inherits this color naturally.
		Background: bg,

		// All fg-only styles: no Background() call. Cells fall through to
		// the terminal's OSC 11 bg.
		App:              fg(text),
		HeaderBrand:      fg(accent).Bold(true),
		HeaderMeta:       fg(muted),
		HeaderRule:       fg(accentAlt),
		Body:             fg(text),
		Muted:            fg(muted),
		Subtle:           fg(subtle),
		UserLabel:        fg(cyan).Bold(true),
		UserBubble:       fg(text),
		UserPrefix:       fg(cyan),
		AssistantLabel:   fg(accent).Bold(true),
		AssistantBody:    fg(text),
		AssistantPrefix:  fg(accent),
		ToolCard:         fg(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		ToolTitle:        fg(blue).Bold(true),
		ErrorCard:        fg(errorText).Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1),
		Sidebar:          fg(text).Padding(1, 2),
		SidebarTitle:     fg(accent).Bold(true),
		SidebarLabel:     fg(muted),
		SidebarValue:     fg(text),
		SidebarSection:   fg(line),
		SidebarTabActive: fg(accent).Bold(true).Underline(true),
		SidebarTab:       fg(muted),
		InputFrame:       fg(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		InputPrompt:      fg(cyan).Bold(true),
		InputPlaceholder: fg(muted),
		InputText:        fg(text),
		Footer:           fg(muted),
		StatusReady:      fg(success),
		StatusBusy:       fg(blue),
		StatusError:      fg(warn),
		PaletteFrame:     fg(text).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		PaletteSurface:   fg(text),

		// Highlights — these INTENTIONALLY paint bg because that's the point:
		// they need to contrast with the surrounding (OSC-11) surface.
		PaletteActive: lipgloss.NewStyle().Background(accent).Foreground(bg).Bold(true),
		DiffAdd:       lipgloss.NewStyle().Background(lipgloss.Color(p.diffAddBG)).Foreground(lipgloss.Color(p.diffAddFG)),
		DiffDel:       lipgloss.NewStyle().Background(lipgloss.Color(p.diffDelBG)).Foreground(lipgloss.Color(p.diffDelFG)),

		DiffContext: fg(text),
		DiffHunk:    fg(muted).Italic(true),
		DiffGutter:  fg(subtle),
	}
}

func NewMarkdownRenderer(width int, variant string) (*glamour.TermRenderer, error) {
	style := headingMarkdownStyle(styles.LightStyleConfig)
	if ResolveVariant(variant) == VariantDark {
		style = headingMarkdownStyle(styles.TokyoNightStyleConfig)
	}
	return glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(max(20, width)),
	)
}

func headingMarkdownStyle(style glamouransi.StyleConfig) glamouransi.StyleConfig {
	style.H1.StylePrimitive.Prefix = ""
	style.H1.StylePrimitive.Suffix = ""
	style.H1.StylePrimitive.BackgroundColor = nil
	style.H2.StylePrimitive.Prefix = ""
	style.H2.StylePrimitive.Suffix = ""
	style.H2.StylePrimitive.BackgroundColor = nil
	style.H3.StylePrimitive.Prefix = ""
	style.H3.StylePrimitive.Suffix = ""
	style.H3.StylePrimitive.BackgroundColor = nil
	style.H4.StylePrimitive.Prefix = ""
	style.H4.StylePrimitive.Suffix = ""
	style.H4.StylePrimitive.BackgroundColor = nil
	style.H5.StylePrimitive.Prefix = ""
	style.H5.StylePrimitive.Suffix = ""
	style.H5.StylePrimitive.BackgroundColor = nil
	style.H6.StylePrimitive.Prefix = ""
	style.H6.StylePrimitive.Suffix = ""
	style.H6.StylePrimitive.BackgroundColor = nil
	return style
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

func applyThemeColors(p palette, c extensions.ThemeColors) palette {
	if c.Bg != "" {
		p.bg = c.Bg
	}
	if c.Panel != "" {
		p.panel = c.Panel
	}
	if c.PanelAlt != "" {
		p.panelAlt = c.PanelAlt
	}
	if c.Line != "" {
		p.line = c.Line
	}
	if c.Accent != "" {
		p.accent = c.Accent
	}
	if c.AccentAlt != "" {
		p.accentAlt = c.AccentAlt
	}
	if c.Text != "" {
		p.text = c.Text
	}
	if c.Muted != "" {
		p.muted = c.Muted
	}
	if c.Subtle != "" {
		p.subtle = c.Subtle
	}
	if c.Success != "" {
		p.success = c.Success
	}
	if c.Warn != "" {
		p.warn = c.Warn
	}
	if c.Blue != "" {
		p.blue = c.Blue
	}
	if c.Cyan != "" {
		p.cyan = c.Cyan
	}
	if c.ErrorText != "" {
		p.errorText = c.ErrorText
	}
	if c.DiffAddBG != "" {
		p.diffAddBG = c.DiffAddBG
	}
	if c.DiffAddFG != "" {
		p.diffAddFG = c.DiffAddFG
	}
	if c.DiffDelBG != "" {
		p.diffDelBG = c.DiffDelBG
	}
	if c.DiffDelFG != "" {
		p.diffDelFG = c.DiffDelFG
	}
	return p
}
