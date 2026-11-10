# Agent Server P0.1 试点范围决策

状态：**P0.1 已固定，P0 仍在执行中**

决策日期：2026-11-10

评审源码基线：`305ead5`

本文只完成生产化计划的 P0.1：固定首个任务类型、唯一主路径、负责人和本轮运行环境。
它不批准生产 SLA、模型费用、云资源或新依赖；这些内容必须在 P0.2 的验收配置中定版。

## 首个任务类型

首个用例是**内部、受控、无外部副作用的长时研究与文本产物任务**。已认证的内部操作者
提交一个可检查的研究/分析目标，Agent 使用现有 planning、general-purpose 子代理、摘要
和虚拟文件工具完成工作，并把最终状态和文本产物留在同一个 Task 的持久执行链中。

第一阶段任务必须同时满足以下约束：

- 输入只包含 Task API 已接受的 owner、`Idempotency-Key` 和 UTF-8 message；owner 来自服务端
  Bearer 映射，不从 message 推断。
- 允许工具只有现有 Deep Agents 规划、子代理、todo 和 Task 内虚拟文件操作。
- 输出是 Task 结果与 Task 内文本文件；不把虚拟文件描述为宿主文件或对象存储产物。
- 禁止宿主 shell、代码执行、任意网络访问、跨 Task Memory、外部系统写入、人工审批、
  Cron、Webhook 和渠道发送。
- 当前 24 小时执行期限、512 个父 model 边界、16 MiB 原始日志、384 KiB 压缩 checkpoint
  和 512 KiB continuation envelope 仍是硬边界，不因试点而放宽。

P1.4 会把该用例收窄为固定输入和机器可检查的产物断言，并分别覆盖规划、虚拟文件、
子代理、摘要及 Continue-As-New。P0.1 不把尚未运行的真实 provider 用例标为通过。

## 唯一主路径

该用例只走 `Task HTTP API -> Temporal TaskWorkflow -> model Activity` 路径。Temporal 是
任务调度、History、重试和恢复的唯一权威；API 只有在远端 Workflow Start 已确认，或已
核对为相同 owner/key/内容的现有 Task 时才返回 202。当前唯一 Workflow 实现为
`runtime.py:TaskWorkflow`，持久 type/protocol/schema 标识仍分别是 `TaskWorkflowV2`、
`task-v2` 和 schema 2。

标准 LangGraph Agent Server 继续服务需要 Thread、Run、SSE 和 owner Store 的会话型业务，
但不承接这个试点任务。一个业务提交不能同时进入两条路径，也不增加镜像队列、永久业务
台账或第二个 graph 调度器。若后续把首个用例改成会话型业务，必须先修订本决策、P0.2
验收矩阵和后续故障证据，不能在运行时静默回退。

## 负责人

| 责任 | 当前负责人 | 完成边界 |
| --- | --- | --- |
| 实现、证据与发布 | `yanjixiong`，仓库维护者 | 只提交任务所属文件；每轮门禁后提交并推送 |
| P0/P1 初验 | `yanjixiong`，以另一轮独立执行承担 | 按原始命令、配置、日志和退出码复验，不用作者结论代替证据 |
| M1 独立复核 | **未指定，P0 未满足项** | 另一维护者必须能按记录复现；指定前不得结束 P0 或批准 M1 |
| Temporal、入口 TLS、备份运营 | **未指定，P0.2 未满足项** | 目标部署选定后分别记录服务 owner 与值班/恢复责任 |

计划允许实现与验收角色由同一维护者承担，但 M1 仍要求另一人可复现。这里明确保留未指定项，
不把代码作者自动解释为生产运营负责人。

## 本轮运行环境

P0.1 文档与离线门禁固定在当前仓库开发机：Apple arm64、macOS 27.0，仓库 `.venv`，
源码提交 `305ead5`。2026-11-10 盘点时只有 Docker 29.4.0 客户端，未连接 Docker Server，
且没有可用的 `temporal` CLI；因此本轮不执行容器、Temporal、真实 provider 或故障验收。

后续本地回归仍可使用已有的单机 Temporal CLI/SQLite 与 fixture，但它只证明开发/故障机制，
不是 M1 试点部署。P0.2 已在 [验收配置](PRODUCTION_ACCEPTANCE.md) 固定单地域外部 Temporal
预生产环境类别、TLS/ACL/持久存储门槛和模型/资源上限；具体供应商、地域、namespace、
备份运营责任与消费批准仍是 P0 未满足条件，在记录这些条件前保持离线。

## 进入下一项的合同

P0.2 已以本文范围为输入，固定负载与持续时间、状态规模、提交/查询/取消延迟目标、各故障的
RPO/RTO、History/结果/产物保留期、真实模型费用上限、停止条件和总 RSS 预算。后续配置
可以收紧范围；若改变任务类型、主路径或允许的副作用，则必须重新打开 P0.1。

本决策不改变运行拓扑、默认功能或权限边界。当前状态仍是“本地开发证据 + 待选目标部署”，
不能对外表述为生产高可用、生产容量或已完成内部试点。
