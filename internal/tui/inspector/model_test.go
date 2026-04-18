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
	model := New(workspace.Info{Root: "/tmp/work", ProjectID: "proj", HasGit: true}, "sess", "gpt-test", theme.Default(theme.VariantLight))
	model.SetSize(40, 12)
	model.Apply(history.EventEnvelope{Kind: "tool.requested", Payload: history.ToolCallPayload{ID: "call_1", Name: "bash", Arguments: `{"command":"pwd"}`}})
	model.Apply(history.EventEnvelope{Kind: "tool.finished", Payload: history.ToolResultPayload{ID: "call_1", Name: "bash", Content: "/tmp/work"}})
	model.SetLogs([]logging.Entry{{Time: time.Unix(0, 0), Level: "info", Message: "started"}})

	if view := model.SummaryView(); !strings.Contains(view, "bash") {
		t.Fatalf("expected tool tab content, got %q", view)
	}

	if view := model.DetailView(); !strings.Contains(view, "started") {
		t.Fatalf("expected logs tab content, got %q", view)
	}
	if view := model.DetailView(); !strings.Contains(view, `"session_id": "sess"`) {
		t.Fatalf("expected context tab content, got %q", view)
	}
}
