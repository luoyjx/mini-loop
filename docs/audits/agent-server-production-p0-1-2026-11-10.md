# Agent Server 生产化 P0.1 交付证据

日期：2026-11-10

评审基线：`305ead5`

计划项：P0.1

## 本轮结论

首个试点固定为内部、受控、无外部副作用的长时研究与文本产物任务。该任务只走
Task HTTP API、Temporal `TaskWorkflow` 和 model Activity；标准 Agent Server 会话路径
保持独立，不接收同一业务提交。负责人和当前本地离线环境已记录，目标部署、生产运营
责任、独立复核人、预算与 SLA 继续作为 P0 未满足项。

权威范围见 [P0.1 试点范围决策](../../deepagents-server/docs/PRODUCTION_PILOT_SCOPE.md)。
本轮没有修改运行代码、依赖、拓扑、权限、持久化或默认功能，因而 canonical README
Mermaid、架构 JSON、生成 HTML 和分层基础图均保持不变。

## 制图证据

- 工具：内置 imagegen，编辑模式。
- 编辑目标：`docs/mini-loop-layered-architecture.png`。
- 本轮图片：[agent-server-production-p0-1-2026-11-10.png](agent-server-production-p0-1-2026-11-10.png)。
- SHA-256：`f4d5fa84ee71f4db576241ddbffc68e497e02f261d5855989aa6cd0bffbe43c0`。
- 尺寸：1375 × 1144。
- 逐字提示词：[agent-server-production-p0-1-2026-11-10.prompt.txt](agent-server-production-p0-1-2026-11-10.prompt.txt)。
- 生成后已删除 `$CODEX_HOME/generated_images/01a1c2ce-3072-7e12-ab5f-233e33802f71`
  工作副本；仓库内正式 PNG 是唯一任务产物。

视觉检查确认完整画布和原有层次仍可见；新增青色强调只指向现有 Task API -> Temporal ->
Worker -> model Activity 路径，callout 同时标明会话路径保持独立、目标部署待 P0.2。图片
用于说明范围决策，不是部署、故障、容量或真实 provider 的验收证据。

## 环境与未执行项

盘点环境为 Apple arm64、macOS 27.0、Docker 29.4.0 客户端；Docker Server 未连接，
`temporal` CLI 不可用。本轮是文档/范围迭代，不执行容器、Temporal、真实 provider、
故障恢复或容量测试，也不据此改变历史验收状态。

## 门禁

- `ast-outline README.md`：退出 0；README 架构段仍为唯一 `## Architecture`，Mermaid 仍为
  一段，结构未被本轮说明破坏。
- `ast-outline docs/AGENT_SERVER_PRODUCTION_EXECUTION_PLAN.md`：退出 0；17 个标题保持可读。
- `ast-outline deepagents-server/docs/PRODUCTION_PILOT_SCOPE.md`：退出 0；新决策包含 5 个主题段。
- `cd deepagents-server && /usr/bin/time -l nice -n 10 .venv/bin/python -m pytest -q -p no:cacheprovider`：
  退出 0，599 passed，4.10 秒，最大 RSS 199,835,648 bytes，0 swaps。
- `/usr/bin/time -l nice -n 10 .venv/bin/python -m pytest -q`：退出 0，2256 passed、
  28 skipped、104 warnings，74.32 秒，最大 RSS 370,655,232 bytes，0 swaps。warnings 为
  既有依赖弃用与 SDK 边界序列化样本告警；本轮没有新增代码告警。
- `git diff --check`：退出 0。

Archify 可执行文件 `/Users/yanjixiong/.codex/skills/archify/bin/archify.mjs` 当前不存在，
所以没有伪报交互图重新生成或浏览器检查。本轮没有修改 canonical Mermaid、架构 JSON 或
生成 HTML；三者的运行拓扑与上轮一致。最终提交和推送 SHA 以 Git/远端状态为权威证据。
