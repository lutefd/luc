package models

import (
	"strings"
	"testing"

	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/theme"
)

func TestModelPickerSearchInputUsesInputTextStyle(t *testing.T) {
	th := theme.Default(theme.VariantDark)
	registry := provider.NewRegistry()
	registry.Register(provider.ProviderDef{
		ID:   "openai",
		Name: "OpenAI",
		Models: []provider.ModelDef{
			{ID: "gpt-5.4", Name: "GPT-5.4"},
		},
	})

	model := New(registry, th)
	model.SetSize(100, 30)
	model.Open("gpt-5.4")
	model.input.SetValue("gpt")

	view := model.View()
	if !strings.Contains(view, th.InputText.Render("gpt")) {
		t.Fatalf("expected filter query to use input text style, got %q", view)
	}
}
