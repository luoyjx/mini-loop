# Agent Server 生产化执行计划交付记录

## 交付范围

2026-11-09，以仓库 `f7b80e2` 和独立模块代码 `c65073a` 为基线，新增[执行计划](../AGENT_SERVER_PRODUCTION_EXECUTION_PLAN.md)，并在根 README、模块 README 和 Research Atlas 中建立入口。计划按 P0 至 P5 编排范围、可观测性、真实模型、恢复发布、配额容量及可选产品能力；所有实施阶段均待执行。

本次没有修改运行代码、依赖、默认行为、canonical Mermaid 或交互架构 JSON/HTML，不重新宣称生产 HA、模型质量或容量通过。原始底图与其 prompt 保持原有版本。

## 范围标注图

使用 built-in imagegen 编辑 `docs/mini-loop-layered-architecture.png`，保存为[本轮图片](agent-server-production-plan-2026-11-09.png)；[精确 prompt](agent-server-production-plan-2026-11-09.prompt.txt)与实际调用一致。

图中新增 amber 范围线及独立底部路线说明，标明标准 Agent Server 与 Temporal Task 服务是计划对象；没有将路线步骤画成已部署组件。视觉检查确认原有分层及服务带完整可见，新增标签包含日期、阶段和“仅文档计划，尚未实现；现有运行拓扑不变”。图片是辅助说明，当前架构事实仍以 README Mermaid 和源码为准。

## 验证

| 检查 | 结果 |
| --- | --- |
| `ast-outline` 文档与 README 结构 | 通过；执行计划有一个 H1 和 17 个标题，阶段及验收入口完整 |
| 新计划与本记录的相对链接 | 11 个本地链接存在，单 H1 检查通过 |
| README canonical Mermaid | 与基线逐字一致；未新增运行组件，无需重新生成交互架构 HTML |
| `cd research-site && npm run content` | 通过，索引包含 23 份文档，新计划出现一次 |
| `npm test` 内的 `content:check` | 通过，生成索引与源文档一致 |
| `cd research-site && npm test` | 环境阻断，退出 127：本地未提供已声明的 `vinext` 可执行文件；在构建阶段报 `vinext: command not found`，后续 rendered HTML 测试未运行 |
| `git diff --check` | 通过；提交前同时检查暂存差异 |
| imagegen 图片与 prompt | 已视觉检查新增路线及范围标签，保存完整图片；精确 prompt 与工具调用一致 |

没有安装依赖或改动锁文件。完整网站构建与渲染测试需在具备仓库锁定依赖的环境重跑，不能以索引检查通过代替。

Research Atlas 的生成记录包含源文档 Git 提交号；源文档提交后再运行生成器及 `content:check`，将最终索引保存在独立提交中，不手工修改生成 JSON。

验证串行执行，子进程 `nice -n 10`，Node heap 1536 MiB，任务进程树 RSS 预算 2 GiB、宿主空闲阈值 15%、总超时 300 秒。采样峰值 295.42 MiB，最低宿主空闲 93%，6 个采样点，无资源阻断。日志保留在 `/var/folders/kz/fg_z2k3d6msdby3p737trv600000gn/T/mini-loop-server-plan-checks-7a9x2xev/`，包含 `content.log`、`test.log` 和 `summary.json`。

此前同日评估重跑独立模块测试：599 passed，6.05 秒；该次日志位于 `/var/folders/kz/fg_z2k3d6msdby3p737trv600000gn/T/mini-loop-server-review-oqjle2e2/pytest.log`。它是计划基线证据，本次文档编辑不再次运行 runtime 测试。根 pytest、invariants、scans、guards、Go test/vet、真实模型和生产故障演练均未在本次文档交付中执行。
