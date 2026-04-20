package scrollbar

import (
	"testing"
	"time"
)

func TestStateDebouncesHideTimer(t *testing.T) {
	var state State

	first := state.Activate()
	if first == nil {
		t.Fatal("expected initial activate to schedule a hide timer")
	}
	deadline := state.hideAt

	second := state.Activate()
	if second != nil {
		t.Fatal("expected repeated activate to reuse the existing timer")
	}
	if !state.hideAt.After(deadline) {
		t.Fatal("expected repeated activate to extend the hide deadline")
	}

	handled, cmd := state.Update(hideMsg{
		id: state.id,
		at: deadline,
	})
	if handled {
		t.Fatal("expected stale hide tick to be ignored")
	}
	if cmd == nil {
		t.Fatal("expected stale hide tick to reschedule the timer")
	}

	handled, cmd = state.Update(hideMsg{
		id: state.id,
		at: state.hideAt.Add(time.Millisecond),
	})
	if !handled {
		t.Fatal("expected final hide tick to hide the scrollbar")
	}
	if cmd != nil {
		t.Fatal("expected final hide tick not to reschedule")
	}
	if state.visible {
		t.Fatal("expected scrollbar to become hidden")
	}
}
