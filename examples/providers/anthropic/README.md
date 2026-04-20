# Anthropic provider adapter

Connects luc to the Anthropic Messages API via the exec provider protocol.

## Setup

```
pip install "anthropic>=0.40.0"
chmod +x adapter.py
```

Store your key in the luc keychain or export it:

```
luc auth set anthropic sk-ant-...
# or
export ANTHROPIC_API_KEY=sk-ant-...
```

Copy the provider into your luc runtime:

```
cp -r . ~/.luc/providers/anthropic
```

Then reload:

```
luc reload      # if luc is running
# or just start a new session — providers are loaded on startup
```

Pick a model with `ctrl+m` in the TUI or set it in config:

```yaml
# ~/.config/luc/config.yaml
provider:
  id: anthropic
  model: claude-sonnet-4-6
```

## Extended thinking

To enable extended thinking on supported models (`claude-opus-4-7`,
`claude-sonnet-4-6`), set the thinking token budget before running:

```
export LUC_ANTHROPIC_THINKING_TOKENS=8000
```

The adapter enables extended thinking automatically when this is set and the
active model supports it. Temperature is forced to 1 and `max_tokens` is
bumped to at least `budget + 1024` as required by the API.

## How it works

luc writes one JSON request to the adapter's stdin:

```json
{
  "request": {
    "model": "claude-sonnet-4-6",
    "system": "...",
    "messages": [...],
    "tools": [...],
    "max_tokens": 8192
  }
}
```

The adapter streams JSONL events back to stdout:

```jsonl
{"type": "thinking", "text": "..."}
{"type": "text_delta", "text": "..."}
{"type": "tool_call", "tool_call": {"id": "...", "name": "...", "arguments": "..."}}
{"type": "done", "completed": true, "usage": {"input_tokens": 100, "output_tokens": 200}}
```

luc handles tool execution and re-invokes the adapter for each subsequent turn.
