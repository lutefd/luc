# RPC Mode

`luc` can run headlessly over a JSONL protocol on stdin/stdout.

This mode is designed for embedding `luc` in editors, IDEs, services, and custom UIs without going through the TUI.

`luc` RPC is intentionally `luc`-native. It streams existing `history.EventEnvelope` values instead of translating them into a second event model.

## Starting RPC Mode

```bash
luc rpc
luc rpc --continue
luc rpc --session <session-id>

luc --mode rpc
luc --mode rpc --continue
luc --mode rpc --session <session-id>
```

Defaults:

- No flags: start a fresh session.
- `--continue`: resume the latest session in the current workspace.
- `--session <session-id>`: open a specific saved session.
- `--session` and `--continue` together are rejected.

## Framing

RPC mode uses strict JSONL:

- one JSON object per line
- LF (`\n`) is the record delimiter
- trailing `\r` is accepted on input
- stdout contains JSON only

## Stdout Frames

Responses:

```json
{"id":"req-1","type":"response","command":"get_state","success":true,"data":{...}}
```

Errors:

```json
{"id":"req-1","type":"response","command":"get_state","success":false,"error":"..."}
```

Events:

```json
{"type":"event","event":{...}}
```

The `event` payload is the existing `history.EventEnvelope` shape from `luc`.

Common event kinds include:

- `message.user`
- `message.assistant.delta`
- `message.assistant.final`
- `message.assistant.tool_calls`
- `tool.requested`
- `tool.finished`
- `reload.finished`
- `session.compaction`
- `ui.action`
- `ui.result`

## Commands

### `get_state`

Returns:

- protocol version
- workspace info
- current session meta and replay counts
- provider/model
- current turn state
- approvals mode
- host capabilities

Example:

```json
{"id":"state-1","type":"get_state"}
```

### `get_events`

Returns replayable session events.

Default scope is `visible`.

```json
{"id":"events-1","type":"get_events"}
{"id":"events-2","type":"get_events","scope":"raw"}
{"id":"events-3","type":"get_events","scope":"visible","since_seq":42}
```

Scopes:

- `visible`: uses compacted replay state
- `raw`: uses full session history

### `get_logs`

Returns the current in-memory log ring.

```json
{"id":"logs-1","type":"get_logs"}
```

### `list_sessions`

Returns saved sessions for the current project.

```json
{"id":"sessions-1","type":"list_sessions"}
```

### `new_session`

Starts a fresh session and returns:

- updated state
- replay events for the new active session

```json
{"id":"new-1","type":"new_session"}
```

### `open_session`

Opens a saved session by session ID and returns:

- updated state
- replay events for that session

```json
{"id":"open-1","type":"open_session","session_id":"sess_123"}
```

### `list_models`

Returns runtime-available models from the current provider registry.

```json
{"id":"models-1","type":"list_models"}
```

### `set_model`

Switches the active model using `luc`'s normal controller logic.

```json
{"id":"model-1","type":"set_model","model_id":"gpt-5.4"}
```

### `prompt`

Accepts a user message and optional image attachments.

The command response only means the prompt was accepted. Progress continues on the event stream.

```json
{"id":"prompt-1","type":"prompt","message":"Hello"}
```

With images:

```json
{
  "id":"prompt-2",
  "type":"prompt",
  "message":"Describe this image",
  "attachments":[
    {
      "type":"image",
      "name":"photo.png",
      "media_type":"image/png",
      "data":"base64..."
    }
  ]
}
```

Only image attachments are supported in v1.

### `abort`

Requests cancellation of the current turn.

```json
{"id":"abort-1","type":"abort"}
```

### `reload`

Reloads runtime assets and returns updated state.

```json
{"id":"reload-1","type":"reload"}
```

### `compact`

Runs manual compaction and returns updated state.

```json
{"id":"compact-1","type":"compact"}
{"id":"compact-2","type":"compact","instructions":"Focus on changed files"}
```

### `get_runtime_ui`

Returns runtime UI contributions:

- commands
- views
- diagnostics

```json
{"id":"ui-1","type":"get_runtime_ui"}
```

### `render_view`

Runs a runtime view's source tool and returns:

- the `RuntimeView`
- the raw `tools.Result`
- rendered text using the same rendering path as the TUI

```json
{"id":"view-1","type":"render_view","view_id":"provider.status"}
```

### `ui_response`

Delivers the host's answer for a blocking runtime UI action.

```json
{
  "id":"ui-response-1",
  "type":"ui_response",
  "action_id":"confirm_123",
  "accepted":true,
  "choice_id":"approve",
  "data":{"value":"ok"}
}
```

## Busy State

`luc` does not queue prompts in RPC mode.

These commands are rejected while the agent is busy:

- `prompt`
- `new_session`
- `open_session`
- `set_model`
- `reload`
- `compact`
- `render_view`

These remain allowed while busy:

- `abort`
- `get_state`
- `get_events`
- `get_logs`
- `ui_response`

## Runtime UI Flow

Blocking runtime UI actions still work in RPC mode.

The flow is:

1. `luc` emits an `event` frame whose `event.kind` is `ui.action`
2. the host inspects the `history.UIActionPayload`
3. the host replies with `ui_response`
4. `luc` resumes the blocked tool/provider/hook
5. `luc` emits a `ui.result` event

That applies to:

- structured runtime tools
- exec providers with `client_actions`
- runtime hooks with `client_actions`
- approval-policy dialogs during runtime view rendering

## Example: Go Client

```go
package main

import (
	"bufio"
	"encoding/json"
	"os/exec"
)

func main() {
	cmd := exec.Command("luc", "rpc")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	_ = cmd.Start()

	_ = json.NewEncoder(stdin).Encode(map[string]any{
		"id": "state-1",
		"type": "get_state",
	})

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var frame map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &frame)
	}
}
```

## Example: Node Client

```js
const { spawn } = require("child_process");

const luc = spawn("luc", ["rpc"]);
luc.stdin.write(JSON.stringify({ id: "state-1", type: "get_state" }) + "\n");

let buffer = "";
luc.stdout.on("data", (chunk) => {
  buffer += chunk.toString("utf8");
  while (true) {
    const idx = buffer.indexOf("\n");
    if (idx === -1) break;
    const line = buffer.slice(0, idx);
    buffer = buffer.slice(idx + 1);
    if (!line) continue;
    const frame = JSON.parse(line);
    console.log(frame);
  }
});
```
