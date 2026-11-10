# P1.2 基础指标图解检查

本图使用内置 imagegen，以
[`mini-loop-layered-architecture.png`](../mini-loop-layered-architecture.png) 为底图编辑；
精确提示词见
[`agent-server-production-p1-2-2026-11-10.prompt.txt`](agent-server-production-p1-2-2026-11-10.prompt.txt)。

高亮限定在既有独立 Task lane：Task API 与 Worker 分别指向内部 `:9464/metrics`
说明，API、TaskWorkflow 和模型 Activity 原框保留，Temporal History 仍处于执行与结果
权威位置。callout 列出 admission/status/cancel、Activity queue/attempt/retry/inflight、
parent boundary、CAN 和状态大小，并明确 process-local、重启归零和低基数隐私边界。
图中没有新增数据库、指标服务、dashboard、trace backend 或第二执行器。

视觉检查：原有五层、标准 Deep Agents Server、Temporal 原型、Task、Go assistant 各 lane
均保留；P1.2 callout 位于 Task lane 上方并以虚线连接既有 API/Worker，未遮挡红色
2026-10-17 历史标记。现有底图未替换，因为本轮没有新增或删除 canonical 架构组件。

输出：1375 × 1144 RGB PNG；SHA-256
`da27716aba6c45d4b7bbde76c90c6ec62eaa377bb45c254c63d2c87349241724`。
