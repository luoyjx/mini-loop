# P0.3 当前协议标注图

- 基图：`docs/mini-loop-layered-architecture.png`
- 本轮图：`docs/audits/agent-server-production-p0-3-2026-11-10.png`
- 精确提示词：`docs/audits/agent-server-production-p0-3-2026-11-10.prompt.txt`
- PNG SHA-256：`45b1a779bc246694cdd071eb8bf907e4348b2f5ebd4f8562eea59943287b5d5c`
- 尺寸：1375 × 1144，RGB PNG

图中只高亮既有独立 Task 容器泳道，并标注当前唯一
`runtime.py:TaskWorkflow`、`TaskWorkflowV2` / `task-v2` / schema 2 以及旧 V1
仅属历史证据。它没有增加或移除组件，没有修改箭头、权威边界、部署路径或默认状态；
README Mermaid、架构 JSON、生成 HTML 与分层基图因此保持不变。

生成后已目检：Task 泳道保持原位置和连接，新增标注位于右侧并未覆盖主路径。
底图本身含早期生成式文字的视觉噪声；本图只作为本轮变更定位证据，精确协议文字以
[当前 Task 协议](../../deepagents-server/docs/CURRENT_TASK_PROTOCOL.md) 为权威。

## 验证

- 源码合同静态核对：唯一 Worker 注册、`TaskWorkflowV2`、`task-v2`、schema 2、
  8/512/128 边界与 16 MiB / 384 KiB / 512 KiB 状态预算一致。
- 相关单元测试：6 passed；覆盖当前 versioned Start、拒绝旧协议、24 小时总期限和
  checkpoint 三层大小上限。
- `ast-outline`：README 与 7 份本轮 Markdown 结构通过。
- 本地 Markdown 引用：8 份文件全部存在；`git diff --check` 通过。
- canonical README Mermaid 未改；架构 JSON、生成 HTML 和分层基图无差异。
  本机无 Archify 工具，因此未做无变化产物的重新生成；本轮 PNG 已以原始分辨率目检。
