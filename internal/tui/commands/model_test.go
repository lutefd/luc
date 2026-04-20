package commands

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lutefd/luc/internal/theme"
)

func TestCommandPaletteSearchInputUsesInputTextStyle(t *testing.T) {
	th := theme.Default(theme.VariantDark)
	registry := NewRegistry()
	registry.Register(Command{ID: "help", Name: "Help"})

	model := New(registry, th)
	model.SetSize(100, 30)
	model.Open()
	model.input.SetValue("dss")

	view := model.View()
	if !strings.Contains(view, th.InputText.Render("dss")) {
		t.Fatalf("expected search query to use input text style, got %q", view)
	}
}

func TestCommandPaletteSelectRunsCommand(t *testing.T) {
	th := theme.Default(theme.VariantDark)
	registry := NewRegistry()
	registry.Register(Command{
		ID:   "help",
		Name: "Help",
		Run: func() tea.Cmd {
			return func() tea.Msg { return "ok" }
		},
	})

	model := New(registry, th)
	model.Open()
	cmd, closed, handled := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || !closed || cmd == nil {
		t.Fatalf("expected enter to select command, handled=%v closed=%v cmd=%v", handled, closed, cmd != nil)
	}
	if got := cmd(); got != "ok" {
		t.Fatalf("expected command result, got %#v", got)
	}
}
