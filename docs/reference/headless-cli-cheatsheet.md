# Headless CLI 命令速查

这份速查只覆盖 **非交互 / headless** 相关参数，不试图抄录三个 CLI 的全部交互命令。

目标是回答三件事：

- headless 入口是什么
- 哪些参数最适合脚本 / CI / 管道
- 当前 `async-agent-backend` 已支持到哪一层

## 一句话结论

- **Claude Code**：标准 headless 入口是 `claude -p` / `claude --print`
- **Codex CLI**：标准 headless 入口是 `codex exec`
- **Gemini CLI**：标准 headless 入口是 `gemini -p` / `gemini --prompt`

## Claude Code

### 推荐入口

```bash
claude -p "your prompt"
claude --print "your prompt"
```

### 最常用 headless 参数

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

### 最小模板

```bash
claude -p "Summarize this repo" --output-format json
```

## Codex CLI

### 推荐入口

```bash
codex exec "your prompt"
```

虽然 `codex "prompt"` 也能单次执行后退出，但自动化和 CI 语义更明确的入口仍然是 `codex exec`。

### 最常用 headless 参数

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

### 最小模板

```bash
codex exec --json "Review this diff"
```

## Gemini CLI

### 推荐入口

```bash
gemini -p "your prompt"
gemini --prompt "your prompt"
```

### 最常用 headless 参数

- `--prompt, -p <string>`
- `--output-format, -o text|json|stream-json`
- `--model, -m <string>`
- `--sandbox, -s <boolean>`
- `--approval-mode <default|auto_edit|yolo>`
- `--yolo, -y`（已废弃，改用 `--approval-mode=yolo`）
- `--allowed-mcp-server-names <array>`
- `--allowed-tools <array>`（已废弃）
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

### 最小模板

```bash
gemini -p "Summarize this repo" -o json
```

## 当前 backend 参数面映射

`async-agent-backend` 当前已经暴露的统一字段在 [`task.go`](/Users/est8/scripts/async-agent/async-agent-backend/internal/task/task.go)：

- `provider`
- `task`
- `working_dir`
- `model`
- `output_format`
- `subcommand`
- `args`
- `timeout_seconds`
- `skip_permissions`
- `json_schema`
- `continue_session`
- `resume_session`
- `max_turns`
- `add_dirs`
- `allowed_tools`
- `disallowed_tools`
- `tools_mode`
- `permission_mode`
- `approval_mode`
- `sandbox_mode`
- `sandbox`
- `ephemeral`
- `search`
- `resume_last`
- `resume_all`
- `max_budget_usd`
- `permission_prompt_tool`
- `full_auto`
- `images`
- `profile`
- `config_overrides`
- `output_last_message_path`
- `output_schema_path`
- `include_directories`
- `allowed_mcp_server_names`
- `extensions`
- `debug`
- `experimental_acp`
- `screen_reader`
- `list_sessions`
- `delete_session`

这意味着当前版本已经能覆盖的高频 headless 能力是：

- Claude：`-p/--print`、`--output-format`、`--model`、`--dangerously-skip-permissions`、`--json-schema`、`--continue`、`--resume`、`--max-turns`、`--add-dir`、`--allowedTools`、`--disallowedTools`、`--tools`、`--permission-mode`、`--max-budget-usd`、`--permission-prompt-tool`
- Codex：`exec`、`exec resume`、`-C`、`--sandbox`、`--ask-for-approval`、`--search`、`--ephemeral`、`--full-auto`、`--image`、`--profile`、`--config`、`--add-dir`、`--output-last-message`、`--output-schema`、`--last`、`--all`、`--skip-git-repo-check`
- Gemini：`-p/--prompt`、`--output-format`、`--model`、`--approval-mode`、`--sandbox`、`--resume`、`--include-directories`、`--allowed-mcp-server-names`、`--extensions`、`--debug`、`--experimental-acp`、`--screen-reader`、`--list-sessions`、`--delete-session`

当前仍然主要依赖 `args` 透传、尚未做成统一一等字段的参数面：

- Claude：`mcp-config`、更细的 system prompt 定制
- Codex：`--oss`、`--color`、更细的 `resume` 组合行为
- Gemini：扩展管理命令类参数、更多 session 管理命令

## 我建议的下一步参数扩展优先级

先补最影响自动化稳定性的：

1. Claude 系统层参数：`system-prompt`、`append-system-prompt`、`mcp-config`
2. Codex 输出与 provider 选择：`color`、`oss`
3. Gemini 会话/扩展管理命令：更细的 `list/delete` 行为与返回解析
4. 跨 provider 统一结果字段：结构化输出产物路径、事件流模式、stderr/stdout 语义
