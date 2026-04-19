package inspector

import (
	"strings"
	"testing"
	"time"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/workspace"
)

func TestInspectorViewAcrossTabs(t *testing.T) {
	model := New(
		workspace.Info{Root: "/tmp/work", ProjectID: "proj", HasGit: true, Branch: "main"},
		history.SessionMeta{
			SessionID: "sess",
			Title:     "Fix scrolling",
			Provider:  "openai",
			Model:     "gpt-test",
			CreatedAt: time.Unix(0, 0),
			UpdatedAt: time.Unix(3600, 0),
		},
		theme.Default(theme.VariantLight),
	)
	model.SetSize(44, 24)
	model.Apply(history.EventEnvelope{Kind: "tool.requested", Payload: history.ToolCallPayload{ID: "call_1", Name: "bash", Arguments: `{"command":"pwd"}`}})
	model.Apply(history.EventEnvelope{Kind: "tool.finished", Payload: history.ToolResultPayload{ID: "call_1", Name: "bash", Content: "/tmp/work"}})
	model.SetStatus("Thinking...")
	model.SetLogs([]logging.Entry{{Time: time.Unix(0, 0), Level: "info", Message: "started"}})

	// Summary view shows session info.
	if view := model.SummaryView(); !strings.Contains(view, "gpt-test") || !strings.Contains(view, "Thinking") || !strings.Contains(view, "bash") || !strings.Contains(view, "main") {
		t.Fatalf("expected enriched overview content in summary view, got %q", view)
	}

	// Detail view starts on Overview tab (session info).
	if view := model.DetailView(); !strings.Contains(view, "Fix scrolling") || !strings.Contains(view, "openai") {
		t.Fatalf("expected session info in overview tab, got %q", view)
	}

	// Switch to Tool tab.
	model.NextTab()
	if view := model.DetailView(); !strings.Contains(view, "bash") {
		t.Fatalf("expected tool content in tool tab, got %q", view)
	}

	// Switch to Logs tab.
	model.NextTab()
	model.SetLogs([]logging.Entry{{Time: time.Unix(0, 0), Level: "info", Message: "started"}})
	model.refreshContent()
	if view := model.DetailView(); !strings.Contains(view, "started") {
		t.Fatalf("expected logs content in logs tab, got %q", view)
	}

	// Switch to Context tab.
	model.NextTab()
	model.refreshContent()
	if view := model.DetailView(); !strings.Contains(view, `"session_id"`) {
		t.Fatalf("expected context content in context tab, got %q", view)
	}
}

func TestInspectorHidesListTools(t *testing.T) {
	model := New(
		workspace.Info{Root: "/tmp/work", ProjectID: "proj", HasGit: true, Branch: "main"},
		history.SessionMeta{SessionID: "sess", Model: "gpt-test"},
		theme.Default(theme.VariantLight),
	)
	model.SetSize(44, 24)
	model.Apply(history.EventEnvelope{Kind: "tool.requested", Payload: history.ToolCallPayload{ID: "call_1", Name: "list_tools", Arguments: `{}`}})
	model.Apply(history.EventEnvelope{Kind: "tool.finished", Payload: history.ToolResultPayload{ID: "call_1", Name: "list_tools", Content: "read\nwrite\nedit\nbash"}})
	model.NextTab()

	if view := model.DetailView(); !strings.Contains(view, "No tool activity yet.") {
		t.Fatalf("expected list_tools to stay hidden from tool tab, got %q", view)
	}
}

func TestInspectorHidesSyntheticUserMessages(t *testing.T) {
	model := New(
		workspace.Info{Root: "/tmp/work", ProjectID: "proj", HasGit: true, Branch: "main"},
		history.SessionMeta{SessionID: "sess", Model: "gpt-test"},
		theme.Default(theme.VariantLight),
	)
	model.SetSize(44, 24)
	model.Apply(history.EventEnvelope{
		Kind: "message.user",
		Payload: history.MessagePayload{
			ID:        "u1",
			Content:   "continue",
			Synthetic: true,
		},
	})
	model.Apply(history.EventEnvelope{
		Kind: "message.user",
		Payload: history.MessagePayload{
			ID:      "u2",
			Content: "real question",
		},
	})

	view := model.SummaryView()
	if strings.Contains(view, "continue") {
		t.Fatalf("expected synthetic user message to stay hidden, got %q", view)
	}
	if !strings.Contains(view, "1 user") || strings.Contains(view, "2 user") {
		t.Fatalf("expected only the real user turn to count in summary, got %q", view)
	}
}
