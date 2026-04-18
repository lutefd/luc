package theme

import (
	"strings"
	"testing"
)

func TestDefaultThemeRendersStyles(t *testing.T) {
	th := Default()
	if got := th.Header.Render("luc"); !strings.Contains(got, "luc") {
		t.Fatalf("expected rendered header to contain content, got %q", got)
	}
	if got := th.AgentBubble.Render("assistant"); !strings.Contains(got, "assistant") {
		t.Fatalf("expected rendered agent bubble to contain content, got %q", got)
	}
}

func TestNewMarkdownRenderer(t *testing.T) {
	renderer, err := NewMarkdownRenderer(40)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := renderer.Render("# Title\n\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "Title") {
		t.Fatalf("expected rendered markdown to include content, got %q", rendered)
	}
}
