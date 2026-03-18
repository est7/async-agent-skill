# async-agent-backend CLI 调用手册

这份文档只讲怎么调用 `async-agent-backend`，默认你已经在模块目录下：

```bash
cd /Users/est8/scripts/async-agent/async-agent-backend
go build -o async-agent-backend ./cmd/async-agent-backend
```

## 一句话用法

- `submit`
  启动后台任务，立即返回 `task_id`
- `run`
  同步执行，直接返回最终结果
- `status`
  查询任务状态
- `wait`
  等到任务结束再返回
- `logs`
  读取 stdout/stderr，或实时跟随输出
- `cancel`
  取消运行中的任务
- `mcp`
  以 stdio 方式运行 MCP server

## 最小示例

### 1. 后台执行

```bash
./async-agent-backend submit \
  --provider claude \
  --task "分析这个仓库" \
  --working-dir /path/to/repo
```

### 2. 同步执行

```bash
./async-agent-backend run \
  --provider codex \
  --task "review this diff"
```

### 3. 查询状态

```bash
./async-agent-backend status <task-id>
./async-agent-backend wait --timeout-seconds 60 <task-id>
./async-agent-backend cancel <task-id>
```

## 统一结果层

默认 `run/status/wait` 返回完整载荷，里面既有兼容字段，也有统一结果层：

- `result`
- `normalized_result`
- `result_path`
- `normalized_result_path`

如果调用方只想消费统一结果层：

```bash
./async-agent-backend run \
  --provider claude \
  --task "Summarize this repo" \
  --output-format json \
  --result-mode normalized
```

返回会裁剪成：

- `task_id`
- `provider`
- `status`
- `exit_code`
- `error`
- `normalized_result`
- `normalized_result_path`

## 实时事件流

### 原始 follow 模式

```bash
./async-agent-backend logs \
  --follow \
  --poll-ms 100 \
  <task-id>
```

输出是 JSONL：

- `{"stream":"stdout","chunk":"..."}`
- `{"stream":"stderr","chunk":"..."}`
- `{"stream":"status","status":"succeeded"}`

### 标准化事件模式

```bash
./async-agent-backend logs \
  --follow \
  --event-mode normalized \
  --poll-ms 100 \
  <task-id>
```

输出是 JSONL：

- `{"stream":"event","event":{...}}`
- `{"stream":"status","status":"succeeded"}`

`event` 现在统一成这几个字段：

- `event_type`
- `raw_type`
- `text`
- `tool_name`
- `is_error`
- `is_terminal`

## 常见 provider 示例

### Claude

```bash
./async-agent-backend run \
  --provider claude \
  --task "Extract APIs" \
  --output-format json \
  --json-schema '{"type":"object"}' \
  --result-mode normalized
```

### Codex

```bash
./async-agent-backend run \
  --provider codex \
  --task "Review this diff" \
  --subcommand exec \
  --arg=--json \
  --result-mode normalized
```

### Gemini

```bash
./async-agent-backend run \
  --provider gemini \
  --task "Summarize these logs" \
  --output-format stream-json \
  --result-mode normalized
```

## MCP 模式

直接运行：

```bash
./async-agent-backend mcp
```

当前比较实用的 normalized 入口：

- `async_agent_run_normalized`
- `async_agent_wait_normalized`

如果你是从外层 skill/script 调用，优先用这两个。

## 结果文件

每个任务目录默认包含：

- `meta.json`
- `stdout.log`
- `stderr.log`
- `result.txt`
- `normalized_result.json`

任务根目录默认在系统 cache 下，也可以覆盖：

```bash
ASYNC_AGENT_TASK_DIR=/tmp/async-agent-tasks ./async-agent-backend run ...
./async-agent-backend run --store-root /tmp/async-agent-tasks ...
```

## Provider 可执行文件覆盖

```bash
ASYNC_AGENT_CLAUDE_BIN=/custom/bin/claude
ASYNC_AGENT_CODEX_BIN=/custom/bin/codex
ASYNC_AGENT_GEMINI_BIN=/custom/bin/gemini
```
