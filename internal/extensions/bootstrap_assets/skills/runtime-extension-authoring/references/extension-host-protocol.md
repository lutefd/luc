# Extension Host Protocol

This is the direct, language-agnostic contract behind `luc.extension/v1`.

Use this document when you are not using a JS/TS SDK and want to implement an
extension host directly in Python, Go, or another language.

## When To Use This

Use a direct protocol implementation when:

- you want a non-Node extension host
- you need `input.transform`, `prompt.context`, `tool.preflight`, or `tool.result`
- you want a long-lived host that owns hosted tool handlers
- you need session or workspace storage managed by luc

If you only need a simple fire-and-forget side effect, use `luc.hook/v1`
instead.

## Manifest

```yaml
schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: message.assistant.final
  - event: tool.preflight
    mode: sync
    failure_mode: closed
```

## Startup Sequence

For each active session, luc launches one child process per enabled extension
host and speaks JSONL over stdin/stdout.

Startup order:

1. luc starts the child process
2. luc sends `hello`
3. the extension replies with `ready`
4. luc sends `storage_snapshot`
5. luc sends `session_start`
6. luc begins sending observe events, sync requests, and hosted tool calls

Shutdown order:

1. luc sends `session_shutdown`
2. the extension should exit promptly

## Message Types

Host to extension:

- `hello`
- `storage_snapshot`
- `session_start`
- `session_shutdown`
- `event`
- `tool_invoke`
- `ping`

Extension to host:

- `ready`
- `decision`
- `tool_result`
- `tools.register`
- `storage_update`
- `client_action`
- `log`
- `progress`
- `error`
- `done`

Rules:

- stdin/stdout is JSONL only
- sync requests always carry `request_id`
- sync responses must echo the same `request_id`
- unknown fields should be ignored
- invalid JSON or invalid message shapes mark the host unhealthy

## Sync Seams

Supported sync seams today:

- `input.transform`
- `prompt.context`
- `tool.preflight`
- `tool.result`

Supported observe events today:

- `session.start`
- `session.reload`
- `message.assistant.final`
- `tool.finished`
- `tool.error`
- `compaction.completed`

`failure_mode: closed` is allowed only for `input.transform` and
`tool.preflight`.

## Hosted Tools

Hosted tools should usually stay declarative: the tool manifest points to the extension host. Advanced integrations whose catalogs are discovered at runtime, such as MCP adapters, may emit `tools.register` when the host advertises `tools.dynamic`. Dynamic tools are session-scoped, owned by the registering extension host, use source `dynamic:<extension-id>`, and cannot replace existing built-in or manifest-declared tools.

```yaml
schema: luc.tool/v2
name: session_audit_status
description: Show extension-collected audit state.
runtime:
  kind: extension
  extension_id: audit
  handler: status
input_schema:
  type: object
  properties: {}
```

luc sends hosted tool calls as `tool_invoke` and expects `tool_result`.

Dynamic tool registration example for MCP-style adapters:

```json
{
  "type": "tools.register",
  "tools": [
    {
      "name": "mcp_fetch_issue",
      "description": "Fetch an issue from the connected MCP server.",
      "handler": "mcp_call_tool",
      "input_schema": {
        "type": "object",
        "properties": {
          "id": { "type": "string" }
        },
        "required": ["id"]
      }
    }
  ]
}
```

The registered tool is invoked through the same `tool_invoke` path as declarative hosted tools.

## Storage

luc manages two whole-value JSON stores per extension:

- session storage
- workspace storage

The extension receives both stores in `storage_snapshot` and can replace either
store by emitting:

```json
{"type":"storage_update","scope":"session","value":{"count":1}}
```

The stored JSON is not sent to the model unless the extension explicitly injects
it through `prompt.context`.

## Restart And Diagnostics

Extension hosts are isolated child processes. If one hangs, crashes, or emits
malformed protocol messages, luc marks it unhealthy, surfaces diagnostics, and
restarts it with bounded backoff.

Current restart policy:

- bounded automatic retries within the active session
- exponential backoff starting at 250 ms and capped at 2 s
- diagnostics stay present while the host is down
- diagnostics clear after the host becomes healthy again
- after the retry budget is exhausted, the host is disabled for that session

Diagnostics show up through runtime diagnostics and inspector logs.

## Minimal Python Host

```python
#!/usr/bin/env python3
import json
import sys

session_store = {}
workspace_store = {}


def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


for raw in sys.stdin:
    raw = raw.strip()
    if not raw:
        continue
    msg = json.loads(raw)
    kind = msg.get("type")

    if kind == "hello":
        emit({"type": "ready", "protocol_version": 1})
    elif kind == "storage_snapshot":
        session_store = msg.get("session") or {}
        workspace_store = msg.get("workspace") or {}
    elif kind == "event" and msg.get("event") == "tool.preflight":
        args = (msg.get("payload") or {}).get("arguments") or {}
        command = str(args.get("command", ""))
        if "rm -rf /" in command:
            emit(
                {
                    "type": "decision",
                    "request_id": msg["request_id"],
                    "decision": "block",
                    "message": "blocked by audit policy",
                }
            )
        else:
            emit(
                {
                    "type": "decision",
                    "request_id": msg["request_id"],
                    "decision": "allow",
                }
            )
    elif kind == "tool_invoke" and msg.get("handler") == "status":
        emit(
            {
                "type": "tool_result",
                "request_id": msg["request_id"],
                "result": {
                    "content": json.dumps(
                        {
                            "session": session_store,
                            "workspace": workspace_store,
                        }
                    )
                },
            }
        )
    elif kind == "session_shutdown":
        break
```

## Minimal Go Host

```go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	var sessionStore map[string]any
	var workspaceStore map[string]any

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return
		}

		switch msg["type"] {
		case "hello":
			_ = enc.Encode(map[string]any{
				"type":             "ready",
				"protocol_version": 1,
			})
		case "storage_snapshot":
			sessionStore, _ = msg["session"].(map[string]any)
			workspaceStore, _ = msg["workspace"].(map[string]any)
		case "event":
			if msg["event"] != "prompt.context" {
				continue
			}
			_ = enc.Encode(map[string]any{
				"type":       "decision",
				"request_id": msg["request_id"],
				"decision":   "system_append",
				"system_append": []string{
					fmt.Sprintf("session=%v workspace=%v", sessionStore, workspaceStore),
				},
			})
		case "session_shutdown":
			return
		}
	}
}
```

## Hybrid Package Layout

A package can combine declarative and programmable assets:

```text
my-package/
  extensions/
    audit.yaml
    host.py
  tools/
    session_audit_status.yaml
  ui/
    session-audit.yaml
```

That is the preferred Phase 4 composition pattern:

- `luc.extension/v1` owns state and sync seams
- `luc.tool/v2` exposes hosted tools declaratively
- `luc.ui/v1` exposes commands, views, and approval policies declaratively
