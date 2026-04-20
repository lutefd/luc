---
name: runtime-extension-authoring
description: How luc expands itself at runtime through tools, providers, UI, hooks, themes, prompts, and skills.
---
When the task is about extending luc, prefer runtime extension mechanisms before
proposing core code changes.

Use this lookup order:

1. Global base layer in `~/.luc/...`
2. Installed package layer in `<workspace>/.luc/packages/*/...`
3. Project-local override in `<workspace>/.luc/...`

Supported runtime extension types:

- Tools in `~/.luc/tools` and `<workspace>/.luc/tools`
- Extension hosts in `~/.luc/extensions`, `<workspace>/.luc/packages/*/extensions`, and `<workspace>/.luc/extensions`
- Providers in `~/.luc/providers` and `<workspace>/.luc/providers`
- UI manifests in `~/.luc/ui`, `<workspace>/.luc/packages/*/ui`, and `<workspace>/.luc/ui`
- Hook manifests in `~/.luc/hooks`, `<workspace>/.luc/packages/*/hooks`, and `<workspace>/.luc/hooks`
- Skills in `~/.luc/skills`, `<workspace>/.luc/skills`, `~/.agents/skills`, and `<workspace>/.agents/skills`
- Themes in `~/.luc/themes` and `<workspace>/.luc/themes`
- System prompt overrides in `~/.luc/prompts/system.md` and `<workspace>/.luc/prompts/system.md`
- Prompt extension manifests in `~/.luc/prompts`, `<workspace>/.luc/packages/*/prompts`, and `<workspace>/.luc/prompts`

Authoring workflow:

1. Decide which runtime surfaces own the capability.
2. Prefer `~/.luc` when the capability should apply across projects.
3. Use project `.luc` only for repo-specific overrides or specialized workflow.
4. Compose host-owned runtime surfaces instead of pushing behavior into core:
   - tools/providers/hooks/extension hosts handle execution and protocol translation
   - UI manifests handle commands, views, and approval policies
   - skills/prompts teach the model how to use the new capability
5. Remind the user they can reload with `luc reload` or `ctrl+r`.

Rules:

- Do not suggest recompiling for tools, providers, skills, themes, UI, or hooks unless runtime limits make that unavoidable.
- For a simple shell-style runtime tool, provide `name`, `description`, `command`, `schema`, and optional `ui`.
- For a capability-enabled tool, use `schema: luc.tool/v1` with `runtime.kind: exec`, optional `runtime.capabilities`, and `input_schema`.
- For a stateful hosted tool, use `schema: luc.tool/v2` with `runtime.kind: extension`, `runtime.extension_id`, `runtime.handler`, and `input_schema`.
- `structured_io` means luc writes a JSON request envelope to stdin and expects JSONL events on stdout.
- `client_actions` means the tool, provider, or hook may emit host-owned `client_action` events and receive `client_result` responses.
- Use `luc.extension/v1` when the capability needs session-scoped state, selected sync interception seams, or hosted tool handlers.
- Supported sync extension seams today are `input.transform`, `prompt.context`, `tool.preflight`, and `tool.result`.
- Use hooks for async side effects only. If the user wants to mutate the active turn loop, guard tools, or inject prompt context synchronously, prefer `luc.extension/v1` instead of `luc.hook/v1`.
- Supported `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Capability-enabled tools and providers should be paired with `luc.ui/v1` manifests when they need reusable commands, inspector/page views, or approval policy wiring.
- Runtime UI stays host-owned in this slice. Views are declarative and read-only.
- luc does support creating brand-new runtime views with `luc.ui/v1`. New `inspector_tab` and `page` views are valid runtime extension targets.
- Built-in inspector tabs such as `Overview` are core-owned. Runtime UI can add new `inspector_tab` or `page` views, but cannot inject a new field directly into the built-in `Overview` tab.
- If the user asks for an overview/sidebar addition, prefer a new runtime `inspector_tab` or `page` as the supported extension path. Only edit core TUI code when they explicitly want to change the built-in `Overview` implementation itself.
- If creating a runtime provider, use either:
  `type: openai-compatible` with `id`, `name`, `base_url`, optional `api_key_env`, and `models`; or
  `type: exec` with `id`, `name`, `command`, optional `args`, optional `env`, optional `capabilities`, and `models`.
- For `type: exec` providers, assume the adapter receives one JSON request on stdin and emits JSONL provider events on stdout. The adapter translates upstream API semantics into luc provider events; luc still executes the actual tools and renders the UI cards.
- If creating hooks, use `luc.hook/v1` with `runtime.kind: exec` and optional `runtime.capabilities`; hooks are async side effects over stdio, not turn-loop mutations.
- If creating an extension host, use `luc.extension/v1` with `runtime.kind: exec`, `protocol_version: 1`, and explicit `subscriptions`. Extension hosts speak JSONL over stdin/stdout and can also own hosted tool handlers declared separately by `luc.tool/v2` manifests.
- If creating a runtime skill, treat `skill-name/SKILL.md` as the canonical instruction body.
- Use `skill-name/luc.yaml` only for metadata such as `interface.display_name` and `interface.short_description`.
- If a skill needs bundled references or scripts, keep them in the same skill directory and assume they will be read through `read_skill_resource`.
- If creating a runtime theme, inherit from `light` or `dark` and override only the necessary colors.
- If creating prompt tuning without replacing the whole base prompt, use `schema: luc.prompt/v1` with a short `prompt` block and optional `match.providers`, `match.models`, or `match.model_prefixes`.
- Prompt extensions are appended after the base system prompt, so keep them compact and targeted to the provider/model behavior you want to change.
- For a one-stop surface-selection guide, read `references/extension-model.md` first.

Read bundled references when you need exact manifest shapes or end-to-end composition:

- `references/extension-model.md` for deciding which runtime surface to use and how the pieces fit together.
- `references/capability-tools.md` for `luc.tool/v1`, `luc.tool/v2`, `structured_io`, `client_actions`, hosted tools, and tool request envelopes.
- `references/provider-ui-composition.md` for `type: exec` providers, host capabilities, and matching `luc.ui/v1` manifests.
- `references/runtime-ui-views.md` for new runtime `inspector_tab` and `page` views with concrete generic examples.
- `references/runtime-ui-actions.md` for host-owned UI actions such as `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- `references/hook-patterns.md` for `luc.hook/v1` manifests, async event delivery, and the boundary between hooks and `luc.extension/v1`.

Current limits:

- Runtime tools, extension hosts, providers, UI manifests, hooks, skills, themes, and prompts are supported.
- Runtime views are read-only in this slice.
