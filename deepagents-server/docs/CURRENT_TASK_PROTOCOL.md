# 当前 Temporal Task 协议

本文只描述 `deepagents-server` 当前可执行的 Temporal Task 协议。历史运行记录可以保留
当时的 V1/V2 事实，但不能据此推导当前 Worker 仍兼容或注册旧 V1。

审阅日期：2026-11-10。源码基线：`74ed76e`。

## 唯一当前实现

| 合同项 | 当前值 | 权威源码 |
| --- | --- | --- |
| Python Workflow 实现 | `task_server/runtime.py:TaskWorkflow` | `src/mini_loop_deepagents/task_server/runtime.py` |
| Worker 注册 | 仅 `workflows=[TaskWorkflow]` | `src/mini_loop_deepagents/task_server/worker.py` |
| Temporal Workflow type | `TaskWorkflowV2` | `task_server/protocol.py:WORKFLOW_TYPE` |
| 请求协议版本 | `task-v2` | `task_server/protocol.py:TASK_VERSION` |
| continuation snapshot schema | `2` | `task_server/protocol.py:SNAPSHOT_SCHEMA` |

`TaskWorkflowV2` 和 `task-v2` 是当前持久 wire identity，不表示源码中还存在两个
Workflow 实现。旧 `TaskWorkflow`/`task-v1` 执行路径已删除：当前 Worker 不注册它，
API 不复用或迁移旧执行，也没有版本路由或旧 Worker 池。已超过 Temporal retention、
无法由当前 type 与摘要核对的执行，不能被猜测为可复用。

## 固定执行与状态预算

| 预算 | 当前值 | 语义 |
| --- | --- | --- |
| CAN 分段 | 8 个父 model 边界 | 只在安全父 model 边界 Continue-As-New |
| 全链上限 | 512 个父 model 边界 | 不包含所有子代理或摘要模型调用 |
| 父图单次调用 | recursion limit 128 | 不等于全链 model 调用总量 |
| 原始 saver journal | 16 MiB | continuation 编码前上限 |
| 压缩 checkpoint | 384 KiB | 压缩 transport 上限 |
| continuation envelope | 512 KiB | 传给下一 Run 的完整信封上限 |
| 原始总期限 | 24 小时 | Continue-As-New 不重置 |

状态超限进入明确的不可重试失败；当前协议没有无限执行或无限状态保证。更细的状态
迁移语义见 `runtime.py`、`checkpoint.py` 和 [Task Server](../TASK_SERVER.md)。

`protocol.py` 也是 snapshot schema 与观测关联 wire identity 的唯一常量来源。新执行在
Temporal patch marker `task-observation-correlation-v1` 之后，把已核对的 owner、TaskID、
首 Run、当前 Run、`task-v2` 与 schema 2 作为 content-free metadata 传给模型 Activity。
旧 History replay 保留旧 config 命令；缺少关联 metadata 的 Activity 明确记录
`correlation_status=unavailable`，不改变既有 Workflow 命令或伪造完整关联。

## 名称消歧

- HTTP 路由 `/v1/tasks` 是 HTTP API 的版本，不是 `task-v1` Workflow 支持声明。
- `mini-loop:task-id:v1` 是稳定 Task ID 的哈希域分隔符；修改它会改变既有 idempotency
  key 的 Task ID，因此它独立于 Workflow 版本。
- `temporalio.api.*.v1` 是 Temporal SDK 的 protobuf 包命名，不是本服务的 Workflow 版本。
- `temporal_prototype` 的 `prototype-v1` 属于隔离的历史技术原型，不由当前 Task Worker 注册。
- 交付记录中的 “v1 历史基线” 与旧 V1/V2 故障运行是不可改写的历史证据，不是当前支持矩阵。

## 变更规则

任何修改 Workflow type、请求版本、snapshot schema、Worker 注册、CAN 边界或状态大小
预算的变更，都必须同时更新本文、相关恢复测试与交付证据。需要重新支持旧执行时，
必须显式设计版本路由、兼容 Worker 和验收范围；不能仅恢复旧名称或依据历史通过项
宣称兼容。

请求/Activity 观测字段与脱敏边界见 [Task 关联合同](TASK_OBSERVABILITY.md)。这些记录
不是新的执行、结果或持久状态权威。
