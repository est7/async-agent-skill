---
name: async-agent-skill
description: Uses the packaged async-agent-backend binary to run Claude, Codex, or Gemini tasks asynchronously or synchronously from this repository. Use whenever the user asks to submit background agent jobs, run headless provider tasks, wait for completion, stream logs, cancel tasks, or expose the local backend over MCP.
---

# Async Agent Skill

Use this skill to drive the local `async-agent-backend` bundle that ships with this repository.

## Before running commands

1. Use the packaged binary at `./assets/async-agent-backend` relative to this `SKILL.md`.
2. If the packaged binary is missing or not executable on the current machine, rebuild from `../async-agent-backend/` relative to this `SKILL.md`.
3. This file is intended to be self-contained for normal binary usage. Do not assume a second doc will be consulted first.
4. If a request depends on exact backend-supported flags, verify against `../async-agent-backend/internal/app/app.go` and `../async-agent-backend/internal/task/task.go` instead of guessing.

## Common commands

后台提交：

```bash
./assets/async-agent-backend submit --provider <claude|codex|gemini> --task "<prompt>" --working-dir <dir>
```

同步执行：

```bash
./assets/async-agent-backend run --provider <claude|codex|gemini> --task "<prompt>"
```

查询状态：

```bash
./assets/async-agent-backend status <task-id>
./assets/async-agent-backend status --result-mode normalized <task-id>
```

等待任务结束：

```bash
./assets/async-agent-backend wait <task-id>
./assets/async-agent-backend wait --timeout-seconds 60 --result-mode normalized <task-id>
```

跟随日志：

```bash
./assets/async-agent-backend logs <task-id>
./assets/async-agent-backend logs --follow <task-id>
./assets/async-agent-backend logs --follow --event-mode normalized <task-id>
```

取消任务：

```bash
./assets/async-agent-backend cancel <task-id>
```

启动 MCP：

```bash
./assets/async-agent-backend mcp
```

## Command model

- `submit` and `run` share the same execution flag surface.
- `status`, `wait`, `logs`, `cancel`, and `mcp` operate on persisted task state under the task store.
- Prefer `--result-mode normalized` when the caller only wants the backend's unified result envelope.
- Prefer `logs --follow --event-mode normalized` when the caller wants structured stream events instead of raw stdout/stderr chunks.

## Execution flags for `submit` and `run`

所有 provider 通用：

- `--provider <claude|codex|gemini>`
- `--task <text>`
- `--working-dir <dir>`
- `--model <name>`
- `--output-format <format>`
- `--result-mode <normalized>`
- `--subcommand <name>`
- `--store-root <dir>`
- `--timeout-seconds <n>`
- `--arg <value>` repeatable passthrough flag for provider-native args not modeled as first-class backend fields

通用 repeatable flags：

- `--add-dir <dir>` additional provider-accessible directory
- `--allowed-tool <name>` allowed provider tool

Claude-mapped flags：

- `--skip-permissions <bool>`
- `--json-schema <json>`
- `--continue`
- `--resume <session>`
- `--max-turns <n>`
- `--disallowed-tool <name>` repeatable
- `--tools-mode <mode>`
- `--permission-mode <mode>`
- `--max-budget-usd <amount>`
- `--permission-prompt-tool <tool>`

Codex-mapped flags：

- `--approval-mode <mode>`
- `--sandbox-mode <read-only|workspace-write|danger-full-access>`
- `--ephemeral`
- `--search`
- `--resume-last`
- `--resume-all`
- `--full-auto`
- `--image <path>` repeatable
- `--profile <name>`
- `--config-override <key=value>` repeatable
- `--output-last-message <path>`
- `--output-schema <path>`

Gemini-mapped flags：

- `--approval-mode <mode>`
- `--sandbox`
- `--include-directory <dir>` repeatable
- `--allowed-mcp-server-name <name>` repeatable
- `--extension <name>` repeatable
- `--debug`
- `--experimental-acp`
- `--screen-reader`
- `--list-sessions`
- `--delete-session <session>`

## Provider cheat sheet inside this skill

This section embeds the headless parameter knowledge needed to decide how to call the packaged binary. The binary itself exposes a normalized flag surface; when the user asks for a provider-native capability that is not first-class yet, use `--arg`.

### Claude Code

headless upstream entry:

```bash
claude -p "your prompt"
claude --print "your prompt"
```

backend invocation shape:

```bash
./assets/async-agent-backend run \
  --provider claude \
  --task "<prompt>"
```

backend behavior:

- always calls Claude in `--print` mode
- defaults Claude `output_format` to `stream-json` if you do not pass `--output-format`
- automatically adds `--verbose` when `--output-format stream-json`
- defaults to `--dangerously-skip-permissions` unless `--skip-permissions false`

most useful Claude headless flags:

- `--output-format text|json|stream-json`
- `--input-format text|stream-json`
- `--include-partial-messages`
- `--json-schema <schema>`
- `--verbose`
- `-c, --continue`
- `-r, --resume <session>`
- `--fork-session`
- `--session-id <uuid>`
- `--no-session-persistence`
- `--allowedTools`
- `--disallowedTools`
- `--tools`
- `--permission-mode <mode>`
- `--permission-prompt-tool <mcp_tool>`
- `--dangerously-skip-permissions`
- `--allow-dangerously-skip-permissions`
- `--max-turns <n>`
- `--max-budget-usd <amount>`
- `--fallback-model <model>`
- `--model <alias|full-name>`
- `--add-dir <dir...>`
- `--mcp-config <json-file-or-json>`
- `--strict-mcp-config`
- `--plugin-dir <dir>`
- `--system-prompt`
- `--system-prompt-file`
- `--append-system-prompt`
- `--append-system-prompt-file`

backend first-class mappings for Claude:

- use `--output-format`, `--model`, `--json-schema`, `--continue`, `--resume`, `--max-turns`, `--add-dir`, `--allowed-tool`, `--disallowed-tool`, `--tools-mode`, `--permission-mode`, `--max-budget-usd`, `--permission-prompt-tool`
- use `--skip-permissions false` only when you explicitly do not want the default skip-permissions behavior
- use `--arg` for currently unmapped Claude-native flags like `--system-prompt`, `--append-system-prompt`, `--mcp-config`, `--strict-mcp-config`

examples:

```bash
./assets/async-agent-backend run \
  --provider claude \
  --task "Extract APIs" \
  --output-format json \
  --json-schema '{"type":"object"}' \
  --result-mode normalized
```

```bash
./assets/async-agent-backend run \
  --provider claude \
  --task "Use this custom system prompt" \
  --arg --system-prompt \
  --arg "You are a strict API extractor."
```

### Codex CLI

headless upstream entry:

```bash
codex exec "your prompt"
```

backend invocation shape:

```bash
./assets/async-agent-backend run \
  --provider codex \
  --task "<prompt>"
```

backend behavior:

- defaults `subcommand` to `exec`
- if `--subcommand resume` is used, backend builds `codex exec resume`
- appends `--skip-git-repo-check` automatically unless already present in `--arg`
- passes `--working-dir` through as `codex -C <dir>`

most useful Codex headless flags:

- `--add-dir <path>`
- `--ask-for-approval, -a untrusted|on-request|never`
- `--cd, -C <path>`
- `--config, -c key=value`
- `--dangerously-bypass-approvals-and-sandbox`
- `--disable <feature>`
- `--enable <feature>`
- `--full-auto`
- `--image, -i <path[,path...]>`
- `--model, -m <string>`
- `--oss`
- `--profile, -p <string>`
- `--sandbox, -s read-only|workspace-write|danger-full-access`
- `--search`
- `--color always|never|auto`
- `--ephemeral`
- `--json`
- `--output-last-message, -o <path>`
- `--output-schema <path>`
- `--skip-git-repo-check`
- `PROMPT <string | ->`
- `codex exec resume [SESSION_ID]`
- `--last`
- `--all`

backend first-class mappings for Codex:

- use `--subcommand`, `--sandbox-mode`, `--approval-mode`, `--search`, `--full-auto`, `--ephemeral`, `--resume-last`, `--resume-all`, `--add-dir`, `--image`, `--profile`, `--config-override`, `--output-last-message`, `--output-schema`
- use `--resume <session>` together with `--subcommand resume` when the user wants a specific resumable session id
- use `--arg` for currently unmapped Codex-native flags like `--json`, `--oss`, `--color`, `--enable`, `--disable`, `--dangerously-bypass-approvals-and-sandbox`

examples:

```bash
./assets/async-agent-backend run \
  --provider codex \
  --task "Review this diff" \
  --full-auto \
  --sandbox-mode workspace-write \
  --approval-mode never \
  --arg=--json \
  --result-mode normalized
```

```bash
./assets/async-agent-backend run \
  --provider codex \
  --subcommand resume \
  --resume "<session-id>"
```

### Gemini CLI

headless upstream entry:

```bash
gemini -p "your prompt"
gemini --prompt "your prompt"
```

backend invocation shape:

```bash
./assets/async-agent-backend run \
  --provider gemini \
  --task "<prompt>"
```

backend behavior:

- appends `--prompt <task>` when `--task` is present
- supports session-management commands through `--list-sessions` and `--delete-session`
- `--sandbox` is a boolean switch in backend, not a string enum

most useful Gemini headless flags:

- `--prompt, -p <string>`
- `--output-format, -o text|json|stream-json`
- `--model, -m <string>`
- `--sandbox, -s <boolean>`
- `--approval-mode <default|auto_edit|yolo>`
- `--yolo, -y` deprecated, use `--approval-mode yolo`
- `--allowed-mcp-server-names <array>`
- `--allowed-tools <array>` deprecated
- `--extensions, -e <array>`
- `--include-directories <array>`
- `--resume, -r <session>`
- `--debug, -d`
- `--prompt-interactive, -i`
- `--experimental-acp`
- `--experimental-zed-integration`
- `--list-extensions`
- `--list-sessions`
- `--delete-session`
- `--screen-reader`

backend first-class mappings for Gemini:

- use `--output-format`, `--model`, `--approval-mode`, `--sandbox`, `--resume`, `--include-directory`, `--allowed-mcp-server-name`, `--extension`, `--debug`, `--experimental-acp`, `--screen-reader`, `--list-sessions`, `--delete-session`
- use `--arg` for currently unmapped Gemini-native flags like `--list-extensions`, `--prompt-interactive`, `--experimental-zed-integration`

examples:

```bash
./assets/async-agent-backend run \
  --provider gemini \
  --task "Summarize these logs" \
  --output-format stream-json \
  --approval-mode yolo \
  --result-mode normalized
```

```bash
./assets/async-agent-backend run \
  --provider gemini \
  --list-sessions
```

## Task-state flags

`status`:

- `status [--store-root <dir>] [--result-mode normalized] <task-id>`

`wait`:

- `wait [--store-root <dir>] [--timeout-seconds <n>] [--result-mode normalized] <task-id>`

`logs`:

- `logs [--store-root <dir>] <task-id>`
- `logs [--store-root <dir>] --follow [--poll-ms <n>] [--event-mode raw|normalized] <task-id>`

`cancel`:

- `cancel [--store-root <dir>] <task-id>`

`mcp`:

- `mcp [--store-root <dir>]`

## Parameter selection rules

1. Use backend first-class flags whenever they exist; this keeps requests portable across providers and preserves normalized results.
2. If the user asks for a provider-native capability that is not modeled as a backend field yet, use the provider cheat sheet in this file and pass the native flag through `--arg`.
3. Do not invent flag names from memory. Resolve them from this skill first, then verify against backend parser code when the request is exacting or unusual.
4. When the request is about structured results, explicitly choose both provider output mode and backend result mode:
   - Example: Claude JSON result usually needs `--output-format json` or `stream-json`
   - Example: backend-trimmed return payload needs `--result-mode normalized`
5. When the request is about streaming, prefer `logs --follow --event-mode normalized` unless the caller explicitly wants raw stdout/stderr.

## Notes

- `submit/status/wait/logs/cancel` operate on persisted task state, not in-memory handles.
- Prefer `--event-mode normalized` when the caller wants structured stream events.
- Provider overrides are available through `ASYNC_AGENT_CLAUDE_BIN`, `ASYNC_AGENT_CODEX_BIN`, `ASYNC_AGENT_GEMINI_BIN`, and `ASYNC_AGENT_TASK_DIR`.
- The backend automatically strips `CLAUDECODE` from child processes, so Claude provider works correctly even when invoked from within a Claude Code session.
- The backend help text is intentionally short; the full supported execution flag surface lives in `internal/app/app.go`.
- This skill intentionally embeds the provider headless cheat sheet so the binary can be used from `SKILL.md` alone.
