# async-agent-skill

这个目录封装了仓库内的 `async-agent-backend` 可执行能力，目的是给本地 skill / 调用脚本一个稳定入口。

## Layout

- [`SKILL.md`](/Users/est8/scripts/async-agent/async-agent-skill/SKILL.md)
  skill 触发说明和执行约束。
- [`assets/`](/Users/est8/scripts/async-agent/async-agent-skill/assets)
  打包后的本地二进制资源。

## Packaged Binary

当前打包二进制路径：

- [`assets/async-agent-backend`](/Users/est8/scripts/async-agent/async-agent-skill/assets/async-agent-backend)

如果后端实现变更，需要重新从 [`async-agent-backend/`](/Users/est8/scripts/async-agent/async-agent-backend) 编译并刷新这里的二进制。
