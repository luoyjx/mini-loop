# Agent Server P0.2 验收配置

状态：**P0.2 已固定，数值尚未在目标环境证明**

决策日期：2026-11-10

机器权威配置：[PRODUCTION_ACCEPTANCE_PROFILE.json](PRODUCTION_ACCEPTANCE_PROFILE.json)

本配置把 P0.1 的受控长任务范围变成可执行的验收目标。数值是后续 P1–P3 的通过门槛，
不是当前能力、生产 SLA、容量承诺或消费授权。配置状态
`fixed-p0-targets-not-yet-proven` 必须保留到相应原始证据通过；校验器只证明配置完整，
不证明环境或任务通过。

## 环境与拓扑

目标环境类别是单地域、外部 Temporal 的预生产环境：History 使用后端持久存储，连接使用
验证 TLS，Temporal namespace ACL 与 Task Bearer→owner 授权保持独立。第一阶段只接收
合成或非敏感内部数据。执行前必须把具体供应商、地域、namespace、许可和数据驻留写入
验收收据；当前 `provisioning_status=not-provisioned`，所以不能声称已经部署。

固定拓扑为 3 owner、2 API、2 Worker。每个 Worker 只有 1 个 Activity 槽和 2 个 Workflow
Task 槽，每个 API 最多 32 个进程内请求。这个形状用于验收，不是全局或 owner 配额；P3
仍须建立集群级准入、公平和背压合同。

## 负载、时长与状态

| 维度 | 固定目标 |
| --- | --- |
| 空闲 | 600 秒无提交，进程健康且模型调用为零 |
| 满槽 | 10 秒内提交 2 Task，观察两个 Activity 同时运行 |
| 排队 | 10 秒内提交 6 Task，观察 2 运行、4 持久排队 |
| 突发 | 10 秒内提交 12 Task，全部被接受并可观察，不能重复执行 |
| 真实长任务 | 2 个约 5 分钟、1 个约 30 分钟、1 个约 2 小时 |
| 持续运行 | 24 小时确定性服务观察；不能替代真实 2 小时任务 |

真实长任务必须包含规划、虚拟文件、子代理、摘要和跨段恢复的真实步骤；仅 sleep 不计入
业务执行时长。状态用消息、摘要、general-purpose 子图、虚拟文本文件和 todos 分别增长，
覆盖各边界的 50%、90% 与 105%。105% 必须显式终止，不能无限重放。

配置中的状态限制与当前实现绑定：请求体 16 KiB、原始 checkpoint 16 MiB、压缩 checkpoint
384 KiB、continuation envelope 512 KiB、24 小时总期限、512 个父模型边界、每段 8 个边界
及 graph recursion 128。合同测试直接读取 runtime 常量，任何一侧变化都要求同步评审。

## 延迟、恢复与保留

所有延迟目标使用 p95：提交确认 2 秒、状态查询 1 秒、取消确认 2 秒、排队可见 5 秒。
收据必须分别报告排队、执行和完成时间，不能用一个端到端数字掩盖瓶颈。

| 故障或数据 | 固定目标 |
| --- | --- |
| Worker 丢失后任务恢复进展 | RTO 120 秒 |
| 后端恢复后服务/任务恢复 | RTO 各 300 秒 |
| 隔离备份恢复后的服务/任务恢复 | RTO 各 3600 秒 |
| 已确认 Task、History、终态结果、安全边界虚拟产物 | RPO 0 秒 |
| 整体备份灾难恢复 | RPO 900 秒 |
| 正在执行的 Activity | 可在原重试预算内重放，不宣称保留进程内进度 |

History、结果、事件、虚拟产物和脱敏 trace 统一保留 30 天，删除请求目标 24 小时内完成。
trace 启动配置只校验声明，远端 project TTL 和过期删除必须另存管理面证据。幂等范围跟随
Temporal History 保留期，不是永久业务台账；过期后只有权威核对为不存在时，相同 key 才
可创建新执行。Start 结果未知时返回稳定 TaskID 并先核对；取消 202 只表示请求被接受。

以上 RPO/RTO 是待证明目标。未配置复制、备份或目标集群时，配置文件本身不提供这些保证。

## 费用与资源停止条件

允许模型固定为当前部署默认 `openai:gpt-4.1-mini`，单次输出上限 4096 tokens，调用超时
60 秒。每次真实模型验收最多 600 次 provider 调用、200 万总 tokens、10 美元；自然月总额
最多 50 美元。连续 5 次 provider 失败即停止。用量未知时停止并标记 inconclusive，不能按
零费用继续。

提交的配置明确设置 `execution_authorized=false` 和 `operator_approval_required=true`。
上述上限是保护边界，不授权购买、使用凭据或发生费用；P1.4 执行前仍需操作者批准。

目标宿主至少 16 GiB 内存；验收拥有的进程树与容器合计 RSS 上限 6 GiB，宿主可用内存
不得低于 25%，最多使用 4 个 CPU 核，单个 API/Worker 容器分别保持 512 MiB/1 GiB 上限。
重型验收串行执行，RSS 超限或内存压力升高时停止本轮拥有的进程树，保留证据且不杀无关进程。

## 校验与后续条件

从模块目录运行：

```sh
.venv/bin/python -m tooling.acceptance.production_profile
```

校验器拒绝重复 JSON key、非有限数字、未知字段、缺失矩阵、与固定拓扑/时长/状态不一致的
值，以及任何把真实 provider 执行改为已授权的提交。成功只输出小型摘要，不回显未来凭据。

P0 仍有四项未满足条件：配置并记录具体 Temporal 供应商/地域/namespace；指定 Temporal、
TLS 与备份运营负责人；指定 M1 独立复核人；批准真实 provider 费用与凭据使用。在这些条件
满足并记录前，P0 和 M1 都不能结束。
