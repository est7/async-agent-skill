---
name: async-agent-skill
description: Uses the packaged async-agent-backend binary to run Claude, Codex, or Gemini tasks asynchronously or synchronously from this repository. Use whenever the user asks to submit background agent jobs, run headless provider tasks, wait for completion, stream logs, cancel tasks, or expose the local backend over MCP.
---

# Async Agent Skill

Use this skill to drive the local `async-agent-backend` bundle that ships with this repository.

## Before running commands

1. Prefer the packaged binary at `./assets/async-agent-backend` relative to this `SKILL.md`.
2. If the packaged binary is missing or not executable on the current machine, rebuild from `../async-agent-backend/` relative to this `SKILL.md`.
3. Unless the task explicitly needs a different working directory, run commands from `../async-agent-backend/` relative to this `SKILL.md`; from there, the packaged binary path is `../async-agent-skill/assets/async-agent-backend`.

## Common commands

后台提交：

```bash
../async-agent-skill/assets/async-agent-backend submit --provider <claude|codex|gemini> --task "<prompt>" --working-dir <dir>
```

同步执行：

```bash
../async-agent-skill/assets/async-agent-backend run --provider <claude|codex|gemini> --task "<prompt>"
```

等待任务结束：

```bash
../async-agent-skill/assets/async-agent-backend wait <task-id>
```

跟随日志：

```bash
../async-agent-skill/assets/async-agent-backend logs --follow <task-id>
../async-agent-skill/assets/async-agent-backend logs --follow --event-mode normalized <task-id>
```

取消任务：

```bash
../async-agent-skill/assets/async-agent-backend cancel <task-id>
```

启动 MCP：

```bash
../async-agent-skill/assets/async-agent-backend mcp
```

## Notes

- `submit/status/wait/logs/cancel` operate on persisted task state, not in-memory handles.
- Prefer `--event-mode normalized` when the caller wants structured stream events.
- Provider overrides are available through `ASYNC_AGENT_CLAUDE_BIN`, `ASYNC_AGENT_CODEX_BIN`, `ASYNC_AGENT_GEMINI_BIN`, and `ASYNC_AGENT_TASK_DIR`.
