# luc

A terminal AI agent with a streaming TUI, local tool execution, and a runtime extension system.

## Install

```
go install github.com/lutefd/luc/cmd/luc@latest
```

Requires Go 1.22+.

## Configuration

luc reads config from `~/.config/luc/config.yaml` (user-global) and `.luc/config.yaml` (workspace-local). Workspace config is merged on top.

Minimal setup:

```yaml
provider:
  kind: openai-compatible
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: gpt-4o
```

Set your API key in the environment:

```
export OPENAI_API_KEY=sk-...
```

Any OpenAI-compatible endpoint works — set `base_url` and `api_key_env` to point at it.

## Credentials

Instead of exporting an environment variable, you can store your API key in the OS keychain (macOS Keychain, Windows Credential Manager, Linux libsecret):

```
luc auth set openai-compatible sk-...
luc auth list
luc auth unset openai-compatible
```

The lookup priority is: environment variable → keychain → error.

## Usage

```
luc              # start a new session
luc open <id>    # resume a session by ID
luc doctor       # show workspace, config, and provider info
luc reload       # reload extensions without restarting
luc rpc          # machine-readable JSON mode (stdin/stdout)
```

### Key bindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Shift+Enter` | Newline |
| `Ctrl+O` | Toggle inspector pane |
| `Ctrl+P` | Command palette |
| `Ctrl+M` | Switch model |
| `Ctrl+L` | Session list |
| `Ctrl+R` | Reload extensions |
| `Ctrl+.` | Stop current turn |
| `Ctrl+C` | Quit |

## Built-in tools

luc ships five tools the model can use out of the box:

- `read` — read file contents
- `write` — write a file
- `edit` — apply targeted edits
- `bash` — run shell commands
- `list_tools` — enumerate available tools

## Extensions

Runtime extensions live under `~/.luc` (user-global) or `<workspace>/.luc` (project-local) and load without recompiling.

Supported surfaces:

- `tools/` — shell or structured exec tools (`luc.tool/v1`, `luc.tool/v2`)
- `extensions/` — long-lived extension hosts with sync interception seams (`luc.extension/v1`)
- `hooks/` — async side-effect hooks (`luc.hook/v1`)
- `providers/` — custom AI providers
- `ui/` — commands, views, and approval policies (`luc.ui/v1`)
- `prompts/` — prompt extensions and system prompt overrides
- `skills/` — named instruction sets the model can load on demand
- `themes/` — color themes

### Custom providers

You can wire any OpenAI-compatible or exec-based LLM as a runtime provider — no recompiling needed.

**OpenAI-compatible** (e.g. local Ollama, Groq, Together):

```yaml
# ~/.luc/providers/ollama.yaml
schema: luc.provider/v1
type: openai-compatible
id: ollama
name: Ollama
base_url: http://localhost:11434/v1
api_key_env: ""
models:
  - id: llama3.2
    name: Llama 3.2
```

**Exec provider** (custom adapter process):

```yaml
# ~/.luc/providers/anthropic/luc.provider.yaml
schema: luc.provider/v1
type: exec
id: anthropic
name: Anthropic
command: ./adapter.py
models:
  - id: claude-opus-4-7
    name: Claude Opus 4.7
  - id: claude-sonnet-4-6
    name: Claude Sonnet 4.6
  - id: claude-haiku-4-5
    name: Claude Haiku 4.5
```

A complete Anthropic adapter with streaming and extended thinking support is included in `examples/providers/anthropic/`. Copy it to `~/.luc/providers/anthropic/`, install the `anthropic` Python package, and set your key:

```
luc auth set anthropic sk-ant-...
```

The exec adapter receives a JSON request on stdin and writes JSONL provider events to stdout. See `docs/runtime-extensions.md` for the full protocol, or browse the bundled reference at `~/.luc/docs/` after first launch.

See `docs/runtime-extensions.md` for the full reference.

## Packages

Install a shared extension package:

```
luc pkg install github.com/user/luc-package@v1.0.0
luc pkg install ./my-local-package
luc pkg list
luc pkg remove github.com/user/luc-package
```

See `examples/packages/` for example package layouts.

## Platforms

macOS, Linux, and Windows (Windows Terminal recommended).
