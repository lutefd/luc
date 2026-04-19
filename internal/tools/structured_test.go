package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	luruntime "github.com/lutefd/luc/internal/runtime"
)

type testBroker struct {
	action luruntime.UIAction
	result luruntime.UIResult
}

func (b *testBroker) Publish(action luruntime.UIAction) error {
	b.action = action
	return nil
}

func (b *testBroker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	_ = ctx
	b.action = action
	return b.result, nil
}

func TestStructuredRuntimeToolCanRoundTripClientActions(t *testing.T) {
	root := t.TempDir()
	requestPath := filepath.Join(root, "request.json")
	resultPath := filepath.Join(root, "client_result.json")
	scriptPath := filepath.Join(root, ".luc", "tools", "provider_status.sh")
	manifestPath := filepath.Join(root, ".luc", "tools", "provider_status.yaml")

	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
IFS= read -r request
printf '%s' "$request" > "`+requestPath+`"
printf '%s\n' '{"type":"client_action","action":{"id":"confirm_1","kind":"confirm.request","blocking":true,"title":"Run?","body":"Proceed?","options":[{"id":"run","label":"Run","primary":true}]}}'
IFS= read -r result
printf '%s' "$result" > "`+resultPath+`"
printf '%s\n' '{"type":"result","result":{"content":"approved","metadata":{"status":"ok"}}}'
printf '%s\n' '{"type":"done","done":true}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`schema: luc.tool/v1
name: provider_status
description: Show provider status.
runtime:
  kind: exec
  command: ./.luc/tools/provider_status.sh
  capabilities:
    - structured_io
    - client_actions
input_schema:
  type: object
  properties: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}

	broker := &testBroker{result: luruntime.UIResult{ActionID: "confirm_1", Accepted: true, ChoiceID: "run"}}
	result, err := manager.Run(context.Background(), Request{
		Name:             "provider_status",
		Arguments:        `{}`,
		Workspace:        root,
		SessionID:        "sess_1",
		AgentID:          "root",
		HostCapabilities: []string{luruntime.HostCapabilityUIConfirm},
		UIBroker:         broker,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "approved" {
		t.Fatalf("expected structured tool result, got %#v", result)
	}
	if broker.action.Kind != "confirm.request" {
		t.Fatalf("expected client action to reach broker, got %#v", broker.action)
	}

	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope luruntime.ToolRequestEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ToolName != "provider_status" || envelope.SessionID != "sess_1" {
		t.Fatalf("unexpected structured request %#v", envelope)
	}
	if len(envelope.HostCapabilities) != 1 || envelope.HostCapabilities[0] != luruntime.HostCapabilityUIConfirm {
		t.Fatalf("expected host capabilities in request, got %#v", envelope)
	}

	data, err = os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var clientResult luruntime.ClientResultEnvelope
	if err := json.Unmarshal(data, &clientResult); err != nil {
		t.Fatal(err)
	}
	if clientResult.Result.ChoiceID != "run" || !clientResult.Result.Accepted {
		t.Fatalf("unexpected client result %#v", clientResult)
	}
}
