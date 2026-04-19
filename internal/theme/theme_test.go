package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDefaultThemeRendersStyles(t *testing.T) {
	th := Default(VariantLight)
	if got := th.HeaderBrand.Render("luc"); !strings.Contains(got, "luc") {
		t.Fatalf("expected rendered header to contain content, got %q", got)
	}
	if got := th.AssistantBody.Render("assistant"); !strings.Contains(got, "assistant") {
		t.Fatalf("expected rendered agent bubble to contain content, got %q", got)
	}
}

func TestNewMarkdownRenderer(t *testing.T) {
	renderer, err := NewMarkdownRenderer(40, VariantLight)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := renderer.Render("# Title\n\nbody")
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "Title") {
		t.Fatalf("expected rendered markdown to include content, got %q", rendered)
	}
	if strings.Contains(plain, "# Title") {
		t.Fatalf("expected markdown heading to be rendered, got %q", plain)
	}
}

func TestResolveVariant(t *testing.T) {
	t.Setenv("LUC_THEME", "dark")
	if got := ResolveVariant("auto"); got != VariantDark {
		t.Fatalf("expected env-driven dark variant, got %q", got)
	}
	if got := ResolveVariant("light"); got != VariantLight {
		t.Fatalf("expected explicit light variant, got %q", got)
	}
}

func TestLoadCustomThemeFromGlobalLucDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".luc", "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `inherits: light
colors:
  accent: "#ff5500"
  panel: "#fef4ef"
`
	if err := os.WriteFile(filepath.Join(home, ".luc", "themes", "sunrise.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	th, variant, err := Load("sunrise", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if variant != VariantLight {
		t.Fatalf("expected inherited light variant, got %q", variant)
	}
	if got := th.HeaderBrand.Render("luc"); !strings.Contains(got, "luc") {
		t.Fatalf("expected loaded custom theme to render content, got %q", got)
	}
}
