package sessions

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/theme"
)

func TestRenderSessionRowOmitsVisibleSessionID(t *testing.T) {
	when := time.Date(2026, 4, 18, 20, 42, 0, 0, time.UTC)
	row := renderSessionRow(theme.Default(theme.VariantLight), history.SessionMeta{
		SessionID: "sess_123",
		Title:     "first prompt title",
		Model:     "gpt-5.4",
		UpdatedAt: when,
	}, 80, false, false)
	row = ansi.Strip(row)

	if strings.Contains(row, "sess_123") {
		t.Fatalf("expected session id to be hidden, got %q", row)
	}
	if !strings.Contains(row, "gpt-5.4") || !strings.Contains(row, when.Local().Format("2006-01-02 15:04")) {
		t.Fatalf("expected model and date in row, got %q", row)
	}
}

func TestRenderSessionRowActiveKeepsMetadataReadable(t *testing.T) {
	when := time.Date(2026, 4, 18, 20, 42, 0, 0, time.UTC)
	row := renderSessionRow(theme.Default(theme.VariantLight), history.SessionMeta{
		SessionID: "sess_123",
		Title:     "first prompt title",
		Model:     "gpt-5.4",
		UpdatedAt: when,
	}, 80, true, true)
	row = ansi.Strip(row)

	if !strings.Contains(row, "gpt-5.4") || !strings.Contains(row, when.Local().Format("2006-01-02 15:04")) {
		t.Fatalf("expected active row metadata to remain visible, got %q", row)
	}
}
