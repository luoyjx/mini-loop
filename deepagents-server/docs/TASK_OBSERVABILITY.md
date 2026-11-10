# Task 请求与模型 Activity 关联

P1.1 在不引入第二个执行权威的前提下，为 Task API 请求和模型 Activity attempt
输出有界、机器可读的 JSON 行。Temporal History 仍负责 Task、Run、状态与结果；
这些 process-local stderr 记录只用于关联诊断，不是持久索引、指标后端或 trace。

## 关联路径

API 为每个非探针请求生成 UUID，并同时放入 `X-Request-ID` 响应头与
`task.api_request` 记录。成功的 service 返回值经过格式核对后，补充 TaskID、首 Run、
当前 Run（History 导航则为所选 Run）、任务状态或结果 readiness。调用方由此保存：

```text
request_id → task_id → first_execution_run_id + current_run_id
```

Workflow 只从已认证 owner、已核对 memo 和 SDK execution info 构造 Activity metadata。
模型 Activity 的 `task.model_activity` 记录再加入 Activity ID、Activity type、attempt、
结果和哈希后的 Worker identity。跨 Continue-As-New 时 TaskID 与首 Run 不变、当前 Run
改变；Activity 重试或 Worker 替换时 Task/Run 保持、attempt 或 Worker hash 改变。

## 记录字段

两类记录均包含 `schema_version=1`、`event`、`image_digest`、
`protocol_version=task-v2` 与 `snapshot_schema=2`。

- `task.api_request`：`request_id`、operation、HTTP status，以及可验证时的 owner、
  TaskID、首/当前 Run、History `selected_run_id`、任务状态或 `result_ready`。
- `task.model_activity`：TaskID、当前 Run、Activity ID/type、attempt、outcome、
  Worker identity SHA-256；完整 Workflow metadata 可验证时再加入 owner 与首 Run，
  并将 `correlation_status` 设为 `complete`，否则明确为 `unavailable`。终态记录还包含
  provider 标准 usage 的 input/output/total token；缺失值写 `unknown` 而不是 0，
  `usage_status` 为 complete、partial 或 unknown。

outcome 只取 `started`、`completed` 或 `failed`。失败记录不包含异常文本。
sink 失败是 best effort，不会改变 HTTP 或 provider 的返回、异常与取消语义。

## 身份、版本与兼容边界

生产 CLI 从部署环境读取 `MINILOOP_TASK_IMAGE_REF`，只接受
`repository@sha256:<64 lowercase hex>`，记录仅保存 digest。Compose 将同一个不可变
引用传给 API 与 Worker。显式 library 装配不会读取 ambient environment；未提供时写
`image_digest=unavailable`，不伪装版本已知。

Workflow 使用 Temporal patch marker `task-observation-correlation-v1`。新执行把关联
metadata 传入模型 Activity；旧 History replay 沿原 config 路径，不凭空增加命令。
旧执行或缺少/不匹配 metadata 的 Activity 仍记录 SDK 可验证的 Task/当前 Run，完整
关联标记为 unavailable。协议常量由 `task_server/protocol.py` 统一提供。

## 明确不记录

观测记录不包含 Task message、Idempotency-Key、Authorization token、Temporal task
token、模型输入/输出、工具 payload、provider credential 或异常正文。Worker identity
只保留 SHA-256，部署镜像只保留 digest。

默认 sink 直接向 stderr 写一行、限制 4096 bytes，并独立于应用 log level。部署方可
采集这些行，但当前没有持久日志后端、查询 API、保留承诺或跨进程交付保证；接受/拒绝、
排队、重试、CAN 与状态规模的机器可读聚合属于 P1.2。P1.3 的 trace、上传前脱敏、
用量和保留边界见 [Task trace 与模型用量合同](TASK_TRACING.md)。
