package execprovider

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

type providerTestBroker struct {
	action luruntime.UIAction
}

func (b *providerTestBroker) Publish(action luruntime.UIAction) error {
	b.action = action
	return nil
}

func (b *providerTestBroker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	_ = ctx
	b.action = action
	return luruntime.UIResult{ActionID: action.ID, Accepted: true, ChoiceID: "approve"}, nil
}

func TestExecProviderSupportsClientActions(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	resultPath := filepath.Join(dir, "client_result.json")
	scriptPath := filepath.Join(dir, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
IFS= read -r request
printf '%s' "$request" > "`+requestPath+`"
printf '%s\n' '{"type":"client_action","action":{"id":"confirm_1","kind":"confirm.request","blocking":true,"title":"Run?","body":"Proceed?","options":[{"id":"approve","label":"Approve","primary":true}]}}'
IFS= read -r result
printf '%s' "$result" > "`+resultPath+`"
printf '%s\n' '{"type":"text_delta","text":"approved"}'
printf '%s\n' '{"type":"done","completed":true}'
`), 0o755); err != nil {
		t.Fatal(err)
	}

	client, err := New(config.ProviderConfig{}, Spec{
		Command:      "./adapter.sh",
		Dir:          dir,
		Capabilities: []string{luruntime.CapabilityClientAction},
	})
	if err != nil {
		t.Fatal(err)
	}
	broker := &providerTestBroker{}
	client.SetRuntimeOptions(broker, []string{luruntime.HostCapabilityUIConfirm})

	stream, err := client.Start(t.Context(), provider.Request{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "text_delta" || ev.Text != "approved" {
		t.Fatalf("unexpected event %#v", ev)
	}
	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "done" || !ev.Completed {
		t.Fatalf("unexpected done event %#v", ev)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
	if broker.action.Kind != "confirm.request" {
		t.Fatalf("expected provider client action to reach broker, got %#v", broker.action)
	}

	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw execRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.HostCapabilities) != 1 || raw.HostCapabilities[0] != luruntime.HostCapabilityUIConfirm {
		t.Fatalf("expected host capabilities in provider request, got %#v", raw)
	}

	data, err = os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var clientResult luruntime.ClientResultEnvelope
	if err := json.Unmarshal(data, &clientResult); err != nil {
		t.Fatal(err)
	}
	if clientResult.Result.ChoiceID != "approve" || !clientResult.Result.Accepted {
		t.Fatalf("unexpected provider client result %#v", clientResult)
	}
}
