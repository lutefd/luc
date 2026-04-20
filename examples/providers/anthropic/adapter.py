#!/usr/bin/env python3
"""
Anthropic provider adapter for luc.

Reads a JSON request from stdin, streams from the Anthropic Messages API,
and writes JSONL provider events to stdout.

Protocol
--------
stdin:  single JSON object  { "request": {...}, "host_capabilities": [...] }
stdout: JSONL stream of     { "type": "thinking"|"text_delta"|"tool_call"|"done", ... }
                            { "error": "..." }  on failure

Setup
-----
pip install anthropic>=0.40.0
export ANTHROPIC_API_KEY=sk-ant-...
"""

import json
import os
import sys
from typing import Any

import anthropic

DEFAULT_MAX_TOKENS = 8192

THINKING_MODELS = {
    "claude-opus-4-7",
    "claude-sonnet-4-6",
    "claude-3-7-sonnet-20250219",
    "claude-3-7-sonnet-latest",
}


def emit(obj: dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def error(message: str) -> None:
    emit({"error": message})
    sys.exit(1)


# ---------------------------------------------------------------------------
# Message translation: luc → Anthropic
# ---------------------------------------------------------------------------

def convert_content_parts(parts: list[dict]) -> list[dict]:
    """Convert luc content parts to Anthropic content blocks."""
    blocks = []
    for part in parts:
        t = part.get("type", "")
        if t == "text":
            text = part.get("text", "")
            if text:
                blocks.append({"type": "text", "text": text})
        elif t == "image":
            blocks.append({
                "type": "image",
                "source": {
                    "type": "base64",
                    "media_type": part.get("media_type", "image/png"),
                    "data": part.get("data", ""),
                },
            })
    return blocks


def convert_messages(luc_messages: list[dict]) -> list[dict]:
    """
    Convert luc messages to Anthropic format.

    Key differences:
    - luc role "tool" (tool results) must become role "user" with tool_result
      blocks. Consecutive tool messages are merged into a single user turn.
    - luc assistant messages with tool_calls get tool_use content blocks.
    """
    anthropic_messages = []

    i = 0
    while i < len(luc_messages):
        msg = luc_messages[i]
        role = msg.get("role", "")

        if role == "user":
            parts = msg.get("parts", [])
            content_str = msg.get("content", "")
            if parts:
                blocks = convert_content_parts(parts)
            elif content_str:
                blocks = [{"type": "text", "text": content_str}]
            else:
                blocks = []
            if blocks:
                anthropic_messages.append({"role": "user", "content": blocks})
            i += 1

        elif role == "assistant":
            blocks = []
            content_str = msg.get("content", "")
            if content_str:
                blocks.append({"type": "text", "text": content_str})
            for tc in msg.get("tool_calls", []):
                try:
                    input_obj = json.loads(tc.get("arguments", "{}"))
                except json.JSONDecodeError:
                    input_obj = {}
                blocks.append({
                    "type": "tool_use",
                    "id": tc["id"],
                    "name": tc["name"],
                    "input": input_obj,
                })
            if blocks:
                anthropic_messages.append({"role": "assistant", "content": blocks})
            i += 1

        elif role == "tool":
            # Collect all consecutive tool result messages into one user turn.
            tool_blocks = []
            while i < len(luc_messages) and luc_messages[i].get("role") == "tool":
                t = luc_messages[i]
                tool_blocks.append({
                    "type": "tool_result",
                    "tool_use_id": t.get("tool_call_id", ""),
                    "content": t.get("content", ""),
                })
                i += 1
            anthropic_messages.append({"role": "user", "content": tool_blocks})

        else:
            i += 1

    return anthropic_messages


def convert_tools(luc_tools: list[dict]) -> list[dict]:
    """Convert luc ToolSpec list to Anthropic tool definitions."""
    tools = []
    for t in luc_tools:
        schema = t.get("schema", {})
        if isinstance(schema, str):
            try:
                schema = json.loads(schema)
            except json.JSONDecodeError:
                schema = {}
        tools.append({
            "name": t["name"],
            "description": t.get("description", ""),
            "input_schema": schema or {"type": "object", "properties": {}},
        })
    return tools


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    raw = sys.stdin.read()
    try:
        envelope = json.loads(raw)
    except json.JSONDecodeError as exc:
        error(f"invalid request envelope: {exc}")

    req = envelope.get("request", {})
    model: str = req.get("model", "claude-sonnet-4-6")
    system: str = req.get("system", "")
    max_tokens: int = req.get("max_tokens") or DEFAULT_MAX_TOKENS
    temperature: float = req.get("temperature", 1.0)
    luc_messages: list[dict] = req.get("messages", [])
    luc_tools: list[dict] = req.get("tools", [])

    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not api_key:
        error("ANTHROPIC_API_KEY is not set")

    client = anthropic.Anthropic(api_key=api_key)

    messages = convert_messages(luc_messages)
    tools = convert_tools(luc_tools)

    kwargs: dict[str, Any] = {
        "model": model,
        "max_tokens": max_tokens,
        "messages": messages,
    }
    if system:
        kwargs["system"] = system
    if tools:
        kwargs["tools"] = tools
    if temperature != 1.0:
        kwargs["temperature"] = temperature

    # Extended thinking: opt-in for supported models via env var.
    thinking_budget = int(os.environ.get("LUC_ANTHROPIC_THINKING_TOKENS", "0"))
    if thinking_budget > 0 and model in THINKING_MODELS:
        kwargs["thinking"] = {"type": "enabled", "budget_tokens": thinking_budget}
        # Extended thinking requires temperature=1 and a higher max_tokens floor.
        kwargs.pop("temperature", None)
        kwargs["max_tokens"] = max(max_tokens, thinking_budget + 1024)

    try:
        with client.messages.stream(**kwargs) as stream:
            # Track tool_use blocks being accumulated across deltas.
            current_tool: dict[str, Any] | None = None
            input_parts: list[str] = []
            # input_tokens come from message_start, output_tokens from message_delta.
            usage: dict[str, Any] = {}

            for event in stream:
                etype = event.type

                if etype == "message_start":
                    if hasattr(event, "message") and event.message:
                        u = getattr(event.message, "usage", None)
                        if u and getattr(u, "input_tokens", None):
                            usage["input_tokens"] = u.input_tokens

                elif etype == "content_block_start":
                    block = event.content_block
                    if block.type == "tool_use":
                        current_tool = {"id": block.id, "name": block.name}
                        input_parts = []

                elif etype == "content_block_delta":
                    delta = event.delta
                    if delta.type == "text_delta":
                        emit({"type": "text_delta", "text": delta.text})
                    elif delta.type == "thinking_delta":
                        emit({"type": "thinking", "text": delta.thinking})
                    elif delta.type == "input_json_delta" and current_tool is not None:
                        input_parts.append(delta.partial_json)

                elif etype == "content_block_stop":
                    if current_tool is not None:
                        emit({
                            "type": "tool_call",
                            "tool_call": {
                                "id": current_tool["id"],
                                "name": current_tool["name"],
                                "arguments": "".join(input_parts),
                            },
                        })
                        current_tool = None
                        input_parts = []

                elif etype == "message_delta":
                    u = getattr(event, "usage", None)
                    if u and getattr(u, "output_tokens", None):
                        usage["output_tokens"] = u.output_tokens
                    emit({"type": "done", "completed": True, "usage": usage or None})

    except anthropic.APIStatusError as exc:
        error(f"Anthropic API error {exc.status_code}: {exc.message}")
    except anthropic.APIConnectionError as exc:
        error(f"Anthropic connection error: {exc}")
    except Exception as exc:
        error(str(exc))


if __name__ == "__main__":
    main()
