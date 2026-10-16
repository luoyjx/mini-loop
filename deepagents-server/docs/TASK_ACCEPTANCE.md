# Task Server 需求与验收边界

本矩阵直接对应用户目标：同时承载多个长任务，异步执行，在失败、部署或重启后
恢复；业务与原 Python/Go 模块独立；参考 Agent Server 等资料设计高并发/高可用
分层；每轮推送代码并给出标记变化的独立架构图。下表与组合故障记录保留
`08b4333` 镜像、2026-10-16 实验的历史验收范围；当前源码的实际验证结果单独见
[交付记录](../DELIVERY.md)，不从历史记录推定本轮已重新执行。

当前 Worker 只有 `runtime.py:TaskWorkflow` 一份实现，早期未生产使用的 Task v1
与双版本接受分支已删除。Temporal type `TaskWorkflowV2`、`task-v2` 和 schema 2
仍是当前持久格式的标识。当前验收只针对这份实现；旧 v1 的接管、复用与旧 128 步
任务上限测试不再是当前支持项。历史 evidence JSON 与交付章节保留原事实，不改写。

“机制通过”只覆盖列出的真实进程、持久状态与故障场景；配置存在、单元测试和
生产 HA/容量是不同层次的证据。当前没有用户指定的部署环境、并发数量、任务时长
分布或可用性/RPO/RTO/吞吐 SLO，因而不能声明已满足某个生产等级。

| 用户要求 | 应当看到的权威证据 | 历史证据与状态 | 边界 |
| --- | --- | --- | --- |
| 多个任务同时运行 | 多项已接受任务，至少两个 Worker Activity 同时执行，溢出项在持久队列等待，最终各自完成 | 历史 rollout：12 Task/3 owner/3 Worker，原生观察 3 活动/9 排队并全部完成；容器基线：2 API/2 Worker 的两 Task 强杀替换后完成。该次 6 Task/3 owner，两个原生模型 Activity 同时在门控处运行，另外四项首 Activity 为原生 SCHEDULED；全栈冷停后六项全部完成 | 不给定生产并发承诺；进程槽不是全局/owner 配额 |
| 长任务异步执行 | 远端接受确认后返回 TaskID；提交连接退出后任务继续，另一 API 可查询同一身份 | Task HTTP 原生 Start 后才 202；独立提交进程退出、跨 API 查询和同 key 复用已有故障证据。V2 的 CAN/摘要/子图与完整状态迁移已分别验证 | 当前原始总期限 24 小时、最多 512 父 model 边界；fixture 的短时机制实验不证明连续运行数小时或真实模型调用 |
| 失败、部署和重启后恢复 | 已接受任务在运行进程消失后仍由持久服务识别；兼容新 Worker 重建状态，保持 Task/首 Run 与请求身份；运行与排队任务均可继续 | 该次全部 API、Worker、Temporal 与 observer 同时停止 3.189 秒，再用全新容器/进程/连接恢复；运行/排队任务均保持身份，全部完成多次 CAN，完整门控状态和重试请求摘要一致 | 恢复依赖保留的 Temporal History 与兼容代码；本机 SQLite 文件未损坏，不等于磁盘损坏恢复、主备切换或跨故障域 HA |
| 与原 Python/Go 业务完全独立 | 独立包/锁/CLI/镜像，运行时入口不调用原 SessionManager、Go 服务或标准 Agent Server 调度 | `deepagents-server` 独立包与锁，Task API/Worker 直接调用 Temporal；容器仅装此模块包源码。原 Python/Go 业务不参与 Task 执行 | 复用设计及测试思路，不把共享仓库解释为共享任务权威；标准 LangGraph 入口仍是另一条路径 |
| 参考高并发/高可用架构 | 有版本和来源的职责拆分；明确状态权威、接入/执行扩缩、重试与恢复边界 | [设计报告](../../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md) 记录 LangSmith、Temporal、DBOS 和 PostgreSQL 原始资料；现有 API/Worker 分离，Temporal 保留历史为单一执行权威 | 架构参考与恢复机制已交付；生产 HA/容量尚未验收，需在目标部署与负载下测量 |
| 每轮推送与独立架构图 | 本地提交与实时远端 SHA 相同；独立图片、精确提示词、变更标记与 canonical/JSON/HTML 一致 | 该次制品为 [冷重启验收图](../../docs/audits/task-server-cold-restart-2026-10-16.png)、同名精确 prompt 与更新后的交互图；最终提交/推送以本地 HEAD、origin/main 与实时远端 SHA 核对记录为准 | 文档中的计划或图片路径不证明推送/生图已完成；最终状态以 [交付记录](../DELIVERY.md) 和实际制品为准 |

## 历史组合验收（2026-10-16）

运行 `c0a4afaaf20c48e5b65fc51b7e9a7580` 的 **3 组通过**，脱敏
[原始观测摘要](task-cold-restart-evidence.json) 与 [交付记录](../DELIVERY.md) 保存证据。
复用 `08b4333` 的 arm64 镜像，两个 API、两个单 Activity 槽 Worker，6 Task/3 owner。
两项先运行任务在父工具第 24 步、已完成 3 次 CAN 后停在模型门控点，此时年龄分别
45.200/45.097 秒；另四项首个 Activity 为原生 SCHEDULED，没有模型执行。

强杀 Temporal 后先实测 readyz=503、healthz=200，再停止所有旧 API/Worker/observer。
连续 3.189 秒观察到四旧容器和旧 Temporal/observer 全部不存活。随后创建全新的
Temporal、observer、API 和 Worker；新 Worker 启动前六项已接受任务可查、同 key
全部复用、四项仍 SCHEDULED。其首个原生 ActivityStarted 都属于新 Worker、attempt 1，
晚于新 Worker 启动。两项运行中任务的 Run/Activity/request SHA 和完整 native 状态
一致，只有这两个受故障影响的模型 Activity 从 attempt 1→2。

六项最终各完成 37 个父工具步骤、4 次 CAN/5 Run、2 个真实 GP 子图、2 次跨 Run
原生摘要；30 份 History 为 0 WorkflowTaskFailed。共 276 个模型 Activity、278 个
尝试，只有两次重试。旧事件游标保留，TaskID/首 Run/owner 身份保持。接受至观察
到终态为 63.764–68.624 秒，包含人工 pacing、门控、排队和停机，不能解释为 provider
速度、持续容量或全程并发峰值。

运行 93.63 秒、142 个采样点，所属 RSS+cgroup 观测峰值 912.6 MiB；最低宿主空闲
42%、VM 可用内存 86.14%，无 OOM，VM PSI 不可用。9 个容器、新旧 2 Temporal 与
2 observer 以及驱动均清理。旧四容器注入和最后四业务容器清理都是 SIGKILL/137，
不代表全部正常 drain。10 个测试凭据片段扫描 30 History/13 日志零命中，限于这些值。

生产源码、锁文件与镜像内基线一致；该次仓库文档晚于镜像内的构建 README，不宣称
二者逐字节相同。已有 576 项模块测试保留为 `08b4333` 历史门禁，该次最终门禁单列。
实验为单机 OrbStack、plaintext CLI SQLite 与确定性模拟模型；真实 Deep Agents/
Temporal 控制流并不等于已测试真实 provider、Cloud/数据库 HA、mTLS 或宿主故障。

## 完成结论应如何表述

可以分别报告独立 Task 服务的实现、已测的异步并发与恢复机制，以及本轮推送/制图
交付。生产并发量、HA 等级与持续任务时长则需目标环境和负载证据，不能从单机
fixture 推定。真实 provider、原生 Temporal TLS/ACL、数据库主备与故障域、入口
负载均衡及外部副作用都应保留与各自证据相称的状态。

设计报告的永久业务台账、全局配额、版本路由、Memory、审批与外部工具 fencing 是
后续设计选项或在相应业务场景下才需要的能力；用户当前目标没有逐项要求实现它们。
不能把全部 Phase C/D 设想扩张为本次必须交付，也不能因这些选项暂未实现而掩盖已
完成的能力。反过来，现有组件全部测试通过也不等于未定义的生产 HA/容量已达成。
