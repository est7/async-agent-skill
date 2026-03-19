# async-agent

这个仓库当前只保留一套主实现：

- [`async-agent-backend/`](/Users/est8/scripts/async-agent/async-agent-backend)
  统一的 Go 无头执行后端。它负责调用 `claude`、`codex`、`gemini` 三种 CLI，提供稳定的 CLI 子命令和一个薄 MCP 模式。

它的职责边界很明确：

- 启动 provider CLI
- 持久化 stdout / stderr / result
- 跟踪任务状态
- 通过 CLI 和 MCP 暴露统一入口

它不负责任务编排。`fork/join`、任务拆分、路由策略这些都应该放在外层脚本、skill 或调用方里。

## Repository Layout

- [`async-agent-backend/`](/Users/est8/scripts/async-agent/async-agent-backend)
  主代码、测试和模块级说明。
- [`async-agent-skill/`](/Users/est8/scripts/async-agent/async-agent-skill)
  面向本地调用方的 skill 封装，包含 `SKILL.md`、说明文档和打包后的 backend 二进制资源。
- [`async-agent-backend/docs/`](/Users/est8/scripts/async-agent/async-agent-backend/docs)
  后端模块的长期文档，包含实现说明、调用手册和参考资料。
- [`CLAUDE.md`](/Users/est8/scripts/async-agent/CLAUDE.md)
  仓库级协作约束。

## Quick Start

```bash
cd /Users/est8/scripts/async-agent/async-agent-backend
go build -o ./async-agent-backend ./cmd/async-agent-backend
./async-agent-backend help
```

最小后台执行示例：

```bash
./async-agent-backend submit \
  --provider codex \
  --task "分析这个仓库" \
  --working-dir .
```

最小同步执行示例：

```bash
./async-agent-backend run \
  --provider claude \
  --task "总结这个仓库"
```

## Main Entry Points

- 模块说明：[`async-agent-backend/README.md`](/Users/est8/scripts/async-agent/async-agent-backend/README.md)
- skill 入口：[`async-agent-skill/README.md`](/Users/est8/scripts/async-agent/async-agent-skill/README.md)
- CLI 手册：[`async-agent-backend/docs/user/async-agent-backend-cli.md`](/Users/est8/scripts/async-agent/async-agent-backend/docs/user/async-agent-backend-cli.md)
- 实现说明：[`async-agent-backend/docs/implementation/async-agent-backend.md`](/Users/est8/scripts/async-agent/async-agent-backend/docs/implementation/async-agent-backend.md)
- 参数速查：[`async-agent-backend/docs/reference/headless-cli-cheatsheet.md`](/Users/est8/scripts/async-agent/async-agent-backend/docs/reference/headless-cli-cheatsheet.md)
- 文档索引：[`async-agent-backend/docs/README.md`](/Users/est8/scripts/async-agent/async-agent-backend/docs/README.md)

## Current Model

当前后端支持：

- CLI 子命令：`submit`、`run`、`status`、`wait`、`logs`、`cancel`、`mcp`
- Provider：`claude`、`codex`、`gemini`
- 结果层：兼容 `result` 与统一 `normalized_result`
- 跟随输出：`logs --follow` 和 `logs --follow --event-mode normalized`

任务完成判定以持久化状态为真值，不依赖内存态；本地同机消费时会优先使用文件系统事件唤醒，并保留 polling fallback。
