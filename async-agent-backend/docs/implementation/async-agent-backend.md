# Async Agent Backend

## Summary

`async-agent-backend` 是一个本地无头执行后端，不做任务编排，只负责：

- 启动 `claude` / `codex` / `gemini` CLI
- 保存 stdout/stderr/result
- 跟踪任务状态
- 以 CLI 或 MCP 方式暴露统一接口

`fork/join`、任务拆分、策略路由仍应放在 skill 或外层脚本里。

## Lifecycle

任务生命周期是：

1. `submit` 创建任务目录和元数据
2. `submit` 启动同一二进制的隐藏 `worker` 子命令
3. `worker` 再调用真实 provider CLI，并写回退出码、结果和终态
4. `status` / `wait` / `logs` 读取持久化状态，而不是直接依赖父进程内存

这样做的好处是：

- 没有常驻守护进程
- 任务可以跨命令查询
- 可以拿到真实退出码
- 不需要用“文件停止写入”作为唯一完成信号

## Interfaces

CLI 子命令：

- `submit`
- `run`
- `status`
- `wait`
- `logs`
- `cancel`
- `mcp`

MCP 工具：

- `async_agent_submit`
- `async_agent_run`
- `async_agent_status`
- `async_agent_wait`
- `async_agent_logs`
- `async_agent_cancel`

返回结果分两层：

- 兼容字段：`result`
  继续表示最终文本结果
- 统一字段：`normalized_result`
  包含：
  - `output_mode`: `text` / `json` / `stream-json`
  - `final_text`
  - `structured_output`（当 provider 输出是 JSON 时）
  - `stream_event_count`
  - `stream_event_types`
  - `events`

`events` 当前统一成以下字段：

- `event_type`
- `raw_type`
- `text`
- `tool_name`
- `is_error`
- `is_terminal`

并且会额外暴露 `normalized_result_path`，指向落盘的 `normalized_result.json`。

如果调用方只想消费统一结果层，可以在 `run/status/wait` 上指定 `result_mode=normalized`。

该模式下返回会裁剪成轻量载荷，只保留：

- `task_id`
- `provider`
- `status`
- `exit_code`
- `error`
- `normalized_result`
- `normalized_result_path`

如果调用方需要实时消费统一事件层，可以使用：

- CLI: `logs --follow --event-mode normalized`
- MCP: `async_agent_run_normalized`
- MCP: `async_agent_wait_normalized`

其中 CLI follow 会输出 JSONL：

- `stream = event`
- `stream = status`

## Task Storage

默认任务目录在系统 cache 目录下的 `async-agent-backend/tasks`。

可以通过环境变量 `ASYNC_AGENT_TASK_DIR` 或命令行参数 `--store-root` 覆盖。

单个任务目录当前包含：

- `meta.json`
- `stdout.log`
- `stderr.log`
- `result.txt`
- `normalized_result.json`

Codex 的 `--json` / `--experimental-json` 现在也会进入统一结果层，并被标记为 `output_mode = stream-json`。

Claude / Codex / Gemini 的流式事件也都会映射进统一 `events` 数组，目的是让上层 skill/script 不再按 provider 分支写事件解析器。
