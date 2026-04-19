package theme

import (
	"image/color"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/extensions"
)

const (
	VariantLight = "light"
	VariantDark  = "dark"
)

type Theme struct {
	// Background is the terminal background color to apply via
	// tea.View.BackgroundColor, which triggers an OSC 11 sequence that
	// repaints every cell of the alt-screen — including cells that no
	// lipgloss style explicitly paints. Nil means "don't touch terminal
	// background" (used for built-in light/dark so the user's terminal
	// theme shows through). Set for custom themes whose declared bg
	// differs from the user's terminal.
	Background       color.Color
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
	// PaletteSurface is the "canvas" the palette/picker renders content onto.
	// Wrapping the JoinVertical'd body in this style (with an explicit Width)
	// forces every inner cell — including JoinVertical's right-padding spaces
	// between short rows and the widest row — to carry the panel background.
	// Without this, those padded spaces have no ANSI bg and fall through to
	// the terminal's OSC 11 bg, which shows as a visibly different shade.
	PaletteSurface   lipgloss.Style
	PaletteActive    lipgloss.Style
	DiffAdd          lipgloss.Style
	DiffDel          lipgloss.Style
	DiffContext      lipgloss.Style
	DiffHunk         lipgloss.Style
	DiffGutter       lipgloss.Style
}

func Default(variant string) Theme {
	// Built-in light/dark variants intentionally leave the background
	// unpainted so the user's terminal background (often a warmer/darker
	// tone than pure #ffffff or #0d1117) shows through. Custom themes go
	// through the painting path below.
	p := paletteFor(ResolveVariant(variant))
	return fromPalette(p, false)
}

func Load(name, workspaceRoot string) (Theme, string, error) {
	if def, found, err := extensions.LoadTheme(workspaceRoot, name); err != nil {
		return Default(ResolveVariant(name)), ResolveVariant(name), err
	} else if found {
		variant := ResolveVariant(def.Inherits)
		p := applyThemeColors(paletteFor(variant), def.Colors)
		// Custom themes must paint bg on every surface — otherwise their
		// chosen background only shows in the sidebar while the rest of
		// the UI leaks the terminal's default bg.
		return fromPalette(p, true), variant, nil
	}
	variant := ResolveVariant(name)
	return Default(variant), variant, nil
}

// fromPalette builds a Theme from a resolved palette. When paintBg is false,
// every non-sidebar surface leaves the background unset so the terminal's
// default background (which the user has presumably tuned to their taste)
// shows through. When true, every surface explicitly paints `bg` — required
// for custom themes whose chosen background differs from the terminal.
func fromPalette(p palette, paintBg bool) Theme {
	bg := lipgloss.Color(p.bg)
	panel := lipgloss.Color(p.panel)
	line := lipgloss.Color(p.line)
	accent := lipgloss.Color(p.accent)
	accentAlt := lipgloss.Color(p.accentAlt)
	text := lipgloss.Color(p.text)
	muted := lipgloss.Color(p.muted)
	subtle := lipgloss.Color(p.subtle)
	blue := lipgloss.Color(p.blue)
	cyan := lipgloss.Color(p.cyan)
	success := lipgloss.Color(p.success)
	warn := lipgloss.Color(p.warn)
	errorText := lipgloss.Color(p.errorText)

	// IMPORTANT: fg-only styles intentionally leave Background UNSET, even
	// when paintBg is true. For custom themes we set Theme.Background so the
	// program emits OSC 11 and the terminal repaints its default bg; every
	// cell we don't explicitly style then inherits that color for free.
	//
	// Setting a per-cell Background(bg) in addition to OSC 11 looks like it
	// should be a no-op (same hex), but in practice terminals apply OSC 11
	// and SGR 48;2 through different rendering paths — blending, transparency,
	// profile conversion — producing visibly different shades. The result
	// was darker bands behind every text run I'd painted bg on (labels,
	// tool titles, system notes, transcript lines). Only genuine contrast
	// surfaces (sidebar, palette frame, diff backgrounds) keep explicit
	// Background calls because they *need* to differ from the base bg.
	//
	// For built-in themes paintBg is false, Theme.Background stays nil, and
	// the user's terminal bg shows uniformly across the whole UI.
	_ = bg
	_ = paintBg

	onBg := func() lipgloss.Style {
		return lipgloss.NewStyle()
	}
	onPanel := func() lipgloss.Style {
		return lipgloss.NewStyle().Background(panel)
	}

	var termBg color.Color
	if paintBg {
		termBg = bg
	}

	return Theme{
		Background:       termBg,
		App:              onBg().Foreground(text),
		HeaderBrand:      onBg().Foreground(accent).Bold(true),
		HeaderMeta:       onBg().Foreground(muted),
		HeaderRule:       onBg().Foreground(accentAlt),
		Body:             onBg().Foreground(text),
		Muted:            onBg().Foreground(muted),
		Subtle:           onBg().Foreground(subtle),
		UserLabel:        onBg().Foreground(cyan).Bold(true),
		UserBubble:       onBg().Foreground(text),
		UserPrefix:       onBg().Foreground(cyan),
		AssistantLabel:   onBg().Foreground(accent).Bold(true),
		AssistantBody:    onBg().Foreground(text),
		AssistantPrefix:  onBg().Foreground(accent),
		ToolCard:         onBg().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		ToolTitle:        onBg().Foreground(blue).Bold(true),
		ErrorCard:        onBg().Foreground(errorText).Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1),
		Sidebar:          onPanel().Foreground(text).Padding(1, 2),
		SidebarTitle:     onPanel().Foreground(accent).Bold(true),
		SidebarLabel:     onPanel().Foreground(muted),
		SidebarValue:     onPanel().Foreground(text),
		SidebarSection:   onPanel().Foreground(line),
		SidebarTabActive: onPanel().Foreground(accent).Bold(true).Underline(true),
		SidebarTab:       onPanel().Foreground(muted),
		InputFrame:       onBg().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(line).Padding(0, 1),
		InputPrompt:      onBg().Foreground(cyan).Bold(true),
		InputPlaceholder: onBg().Foreground(muted),
		InputText:        onBg().Foreground(text),
		Footer:           onBg().Foreground(muted),
		StatusReady:      onBg().Foreground(success),
		StatusBusy:       onBg().Foreground(blue),
		StatusError:      onBg().Foreground(warn),
		PaletteFrame:     lipgloss.NewStyle().Background(panel).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		PaletteSurface:   lipgloss.NewStyle().Background(panel).Foreground(text),
		PaletteActive:    lipgloss.NewStyle().Background(accent).Foreground(bg).Bold(true),
		DiffAdd:          lipgloss.NewStyle().Background(lipgloss.Color(p.diffAddBG)).Foreground(lipgloss.Color(p.diffAddFG)),
		DiffDel:          lipgloss.NewStyle().Background(lipgloss.Color(p.diffDelBG)).Foreground(lipgloss.Color(p.diffDelFG)),
		DiffContext:      onBg().Foreground(text),
		DiffHunk:         onBg().Foreground(muted).Italic(true),
		DiffGutter:       onBg().Foreground(subtle),
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
