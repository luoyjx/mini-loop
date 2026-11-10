# Agent Server 生产化 P0.2 交付证据

日期：2026-11-10

评审基线：`c0fff9f`

计划项：P0.2

## 本轮结论

P0.2 已把负载、真实任务时长、状态边界、p95、RPO/RTO、保留、provider 费用护栏和
资源停止条件落入版本化 JSON，并增加 stdlib 校验器和 runtime 常量合同测试。配置明确
不授权真实 provider 消费，所有数值仍是待证明目标。

权威说明见 [P0.2 验收配置](../../deepagents-server/docs/PRODUCTION_ACCEPTANCE.md)，机器配置见
[PRODUCTION_ACCEPTANCE_PROFILE.json](../../deepagents-server/docs/PRODUCTION_ACCEPTANCE_PROFILE.json)。
本轮没有修改生产 runtime、依赖、拓扑、权限、持久化或默认功能，因而 canonical README
Mermaid、架构 JSON、生成 HTML 和分层基础图均保持不变。

## 制图证据

- 工具：内置 imagegen，编辑模式。
- 编辑目标：`docs/mini-loop-layered-architecture.png`。
- 本轮图片：[agent-server-production-p0-2-2026-11-10.png](agent-server-production-p0-2-2026-11-10.png)。
- SHA-256：`8512f2d32388ad6cd62b38ebba896c36f2216c3d377d2038546b54a852029890`。
- 尺寸：1375 × 1144。
- 逐字提示词：[agent-server-production-p0-2-2026-11-10.prompt.txt](agent-server-production-p0-2-2026-11-10.prompt.txt)。
- 生成后已删除 `$CODEX_HOME/generated_images/01a1c2ce-3072-7e12-ab5f-233e33802f71`
  工作副本；仓库内正式 PNG 是唯一任务产物。

视觉检查确认完整画布和原有层次仍可见；新增青色强调只指向现有 Task API -> Temporal
History -> Worker -> model Activity 路径。callout 显示固定拓扑、5/30/120 分钟与 24 小时
矩阵、RPO/RTO/成本/资源，并以红色标明“目标未证明”。图片不是部署、故障、容量或真实
provider 的通过证据。

## 环境与未执行项

本轮只执行离线配置校验和测试，不连接 Temporal、容器或真实 provider，不产生模型费用。
具体 Temporal 供应商/地域/namespace、运营负责人、独立复核人和消费批准仍是 P0 未满足项。

## 门禁

- `.venv/bin/python -m json.tool docs/PRODUCTION_ACCEPTANCE_PROFILE.json`：退出 0。
- `nice -n 10 .venv/bin/python -m pytest -q -p no:cacheprovider tests/test_production_acceptance_profile.py tests/test_project_structure.py`：
  退出 0，18 passed，0.72 秒。
- `.venv/bin/python -m tooling.acceptance.production_profile`：退出 0，`status=valid`、
  `provider_execution_authorized=false`、4 项 open P0 condition。
- `cd deepagents-server && /usr/bin/time -l nice -n 10 .venv/bin/python -m pytest -q -p no:cacheprovider`：
  退出 0，614 passed，3.61 秒，最大 RSS 199,983,104 bytes，0 swaps。
- `/usr/bin/time -l nice -n 10 .venv/bin/python -m pytest -q`：退出 0，2256 passed、
  28 skipped、104 warnings，72.95 秒，最大 RSS 370,278,400 bytes，0 swaps。warnings 为
  既有依赖弃用与 SDK 边界序列化样本告警；本轮新增测试没有告警。
- `ast-outline` 已检查新校验器、测试、P0.2 文档、P0.1 文档、Task 验收矩阵、执行计划和
  根 README；根 README 仍只有一段 canonical Mermaid。
- `git diff --check`：退出 0。

Archify 可执行文件 `/Users/yanjixiong/.codex/skills/archify/bin/archify.mjs` 当前不存在，
所以没有伪报交互图重新生成或浏览器检查。本轮没有修改 canonical Mermaid、架构 JSON 或
生成 HTML；三者的运行拓扑与上轮一致。最终提交和推送 SHA 以 Git/远端状态为权威证据。
