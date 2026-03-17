# async-agent-backend

Go 实现的统一无头执行后端，用来调用三种非交互式 CLI：

- `claude`
- `codex`
- `gemini`

它只负责执行、状态跟踪和输出持久化，不负责 fork/join 编排。

三家 CLI 的 headless 官方参数速查和当前 backend 的参数映射见：

- [`docs/reference/headless-cli-cheatsheet.md`](/Users/est8/scripts/async-agent/docs/reference/headless-cli-cheatsheet.md)
- 完整 CLI 调用手册见：
- [`docs/user/async-agent-backend-cli.md`](/Users/est8/scripts/async-agent/docs/user/async-agent-backend-cli.md)

## Build

```bash
go build ./cmd/async-agent-backend
```

## CLI Usage

提交后台任务：

```bash
./async-agent-backend submit \
  --provider claude \
  --task "分析这个仓库" \
  --working-dir /path/to/repo
```

同步执行：

```bash
./async-agent-backend run \
  --provider codex \
  --task "write a summary" \
  --arg=--full-auto
```

查询状态：

```bash
./async-agent-backend status <task-id>
./async-agent-backend wait --timeout-seconds 30 <task-id>
./async-agent-backend logs <task-id>
./async-agent-backend cancel <task-id>
```

结果返回现在分两层：

- `result`
  向后兼容的最终文本结果
- `normalized_result`
  统一结果对象，包含 `output_mode`、`final_text`、可选的 `structured_output`、流式事件摘要，以及标准化 `events` 数组

如果只想拿统一结果层，可以对 `run/status/wait` 使用：

```bash
./async-agent-backend run \
  --provider claude \
  --task "Summarize this repo" \
  --output-format json \
  --result-mode normalized
```

这会返回一个更轻量的载荷，只保留：

- `task_id`
- `provider`
- `status`
- `exit_code`
- `error`
- `normalized_result`
- `normalized_result_path`
  指向统一结果对象的落盘文件

如果要实时消费统一事件层，可以直接用：

```bash
./async-agent-backend logs \
  --follow \
  --event-mode normalized \
  <task-id>
```

这会输出 JSONL，每行一个标准化事件或终态状态：

- `{"stream":"event","event":{...}}`
- `{"stream":"status","status":"succeeded"}`

`events` 的统一字段当前是：

- `event_type`
- `raw_type`
- `text`
- `tool_name`
- `is_error`
- `is_terminal`

对应的持久化文件：

- `result.txt`
  最终文本结果
- `normalized_result.json`
  统一结果对象

运行 MCP：

```bash
./async-agent-backend mcp
```

## Provider Overrides

测试或自定义安装路径时，可以覆盖底层可执行文件：

- `ASYNC_AGENT_CLAUDE_BIN`
- `ASYNC_AGENT_CODEX_BIN`
- `ASYNC_AGENT_GEMINI_BIN`
- `ASYNC_AGENT_TASK_DIR`

## Design Notes

- `submit` 启动的是当前二进制自己的隐藏 `worker` 命令，而不是直接把 provider 挂成孤儿进程。
- `worker` 负责等待真实 provider 退出并写回 `exit_code`、`status`、`result`。
- `worker` 现在会同时写回 `result.txt` 和 `normalized_result.json`。
- Codex 在 `--json` / `--experimental-json` 下也会被归一化成 `stream-json` 结果。
- Claude / Codex / Gemini 的流式事件现在都会映射成同一套 `events` 字段，上层不需要再按 provider 分支解析。
- MCP 现在额外提供了 `async_agent_run_normalized` 和 `async_agent_wait_normalized` 两个轻量入口。
- `status` 的主真值来自元数据和 worker PID；`last_activity_at` 才来自 stdout/stderr 文件更新时间。
