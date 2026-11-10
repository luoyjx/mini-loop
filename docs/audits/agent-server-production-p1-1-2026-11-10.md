# P1.1 任务关联图解检查

本图由仓库内置 imagegen 以
[`mini-loop-layered-architecture.png`](../mini-loop-layered-architecture.png) 为底图编辑，
精确提示词见
[`agent-server-production-p1-1-2026-11-10.prompt.txt`](agent-server-production-p1-1-2026-11-10.prompt.txt)。

高亮限定在独立 Task lane：API request UUID 关联 Task、首/当前 Run，模型 Activity
关联 Activity ID、attempt、owner，并记录镜像 digest 与 `task-v2` / schema 2。
callout 明确标注 best-effort stderr 不是持久指标或 trace，且排除 message、key 和
credential。图中没有新增存储、执行器或控制面；Temporal History 仍是执行和结果权威。

视觉检查：原有五层、标准 Deep Agents Server、Temporal 原型、Task、Go assistant
各 lane 均保留；P1.1 callout 位于 Task lane 上方，红色 2026-10-17 历史标记仍清晰，
Task API、Temporal History、Worker/Activity 的高亮路径可辨。底图未被替换，因为本轮
没有新增或删除 canonical 架构组件。

输出：1375 × 1144 RGB PNG；SHA-256
`3be2d9eb3e3aca7b581a43442537f27762e3c06931ff1d2d56fc24e33b47d3b4`。
