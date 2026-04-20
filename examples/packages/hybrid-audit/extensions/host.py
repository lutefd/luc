#!/usr/bin/env python3
import json
import sys


session_store = {}
workspace_store = {}


def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def save(scope, value):
    emit({"type": "storage_update", "scope": scope, "value": value})


def increment(store, key):
    store[key] = int(store.get(key, 0)) + 1


for raw in sys.stdin:
    raw = raw.strip()
    if not raw:
        continue

    msg = json.loads(raw)
    kind = msg.get("type")

    if kind == "hello":
        emit({"type": "ready", "protocol_version": 1})
        continue

    if kind == "storage_snapshot":
        session_store = msg.get("session") or {}
        workspace_store = msg.get("workspace") or {}
        continue

    if kind == "event":
        event = msg.get("event")
        payload = msg.get("payload") or {}

        if event == "tool.preflight":
            arguments = payload.get("arguments") or {}
            command = str(arguments.get("command", ""))
            if "rm -rf /" in command:
                emit(
                    {
                        "type": "decision",
                        "request_id": msg["request_id"],
                        "decision": "block",
                        "message": "blocked by session-audit",
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
            continue

        if event == "tool.finished":
            increment(session_store, "tool_calls")
            increment(workspace_store, "tool_calls")
            save("session", session_store)
            save("workspace", workspace_store)
            continue

        if event == "message.assistant.final":
            increment(session_store, "assistant_turns")
            save("session", session_store)
            continue

    if kind == "tool_invoke" and msg.get("handler") == "status":
        emit(
            {
                "type": "tool_result",
                "request_id": msg["request_id"],
                "result": {
                    "content": json.dumps(
                        {
                            "session": session_store,
                            "workspace": workspace_store,
                        },
                        indent=2,
                    ),
                    "collapsed_summary": "Session audit state",
                },
            }
        )
        continue

    if kind == "session_shutdown":
        break
