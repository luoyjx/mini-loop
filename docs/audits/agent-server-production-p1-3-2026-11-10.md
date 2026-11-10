# P1.3 Trace 与用量图解检查

本轮因 canonical 架构增加外部诊断 trace 边界，先使用内置 imagegen 更新
[`mini-loop-layered-architecture.png`](../mini-loop-layered-architecture.png) 基础图；其精确
提示词同步保存于
[`mini-loop-layered-architecture.prompt.txt`](../mini-loop-layered-architecture.prompt.txt)。
随后从更新后的基础图生成本轮高亮图，精确提示词见
[`agent-server-production-p1-3-2026-11-10.prompt.txt`](agent-server-production-p1-3-2026-11-10.prompt.txt)。

基础图只重排独立 Task lane 右侧，加入 `LangSmith trace` 外部诊断框与 API/Activity
虚线，保留 client、Task API、Temporal History、TaskWorkflow、model Activity/ChatOpenAI
和 Go assistant 等其他架构。Temporal History 继续以数据库形状和中心位置表达 Task、
Run、重试、状态与结果权威；LangSmith 不是数据库、执行器或恢复源。

本轮图以 cyan/amber 高亮 Task API、model Activity 与 trace，callout 明确列出 parent、
subagent、summary、retry，输入/输出/异常脱敏，缺失 usage 为 unknown，以及 30 天策略仍
待远端 TTL 核验。它没有宣称真实 provider waterfall、远端删除或 trace 可用性已验收。
图中的“输入/输出/异常脱敏”是短标签；实现还在 SDK enqueue 前删除 serialized/runtime、
event 与 attachment，实际 HTTP payload 捕获回归已覆盖这条更严格边界。

视觉检查：输出保留 1375 × 1144 RGB canvas、五层主架构、右侧六条边界说明、标准
Deep Agents Server、Temporal 原型、Task 与 Go assistant lane。P1.3 callout 位于 Task lane
上方并部分覆盖原型下方空白/框区，但 Task 主路径、红色 2026-10-17 标记、trace 框和
“Temporal History 仍是权威”标签均可辨；没有新增第二调度器或存储圆柱。

基础图 SHA-256：`c219a78df123c75da7ce008b6999e47049ea49fcaea84930225c1988b1904503`。
本轮图 SHA-256：`515cb619fa11e24f4de6cce9110cf24999505cfce0d9838953eabd4d59482e4c`。

交互架构图的 JSON 驱动静态检查得到 83 components / 110 connections、无重复 ID。当前环境没有
Archify，Work 浏览器也被禁用，因此没有把生成器 9/9 或浏览器交互检查误记为通过。
