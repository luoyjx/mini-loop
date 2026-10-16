# 独立 Temporal Task HTTP 服务

这是 `task_server/` 下显式启动的长任务服务。HTTP API 只提交和观察任务，独立
Worker 运行真实 Deep Agents；客户端断开或 API 进程退出不会主动取消已接受任务。
任务身份、状态、结果、取消请求与生命周期事件以 **Temporal 保留的 Workflow
History 为单一权威**。此路径没有另建业务 SQL 台账、Outbox 或第二套任务重试器。

它与标准 LangGraph Agent Server、[离线故障原型](TEMPORAL_PROTOTYPE.md) 以及原
Python/Go 业务分别运行。固定依赖仍为 Deep Agents `0.7.25`、Temporal Python SDK
`1.34.0`（可选 `temporal` extra）；本轮没有增加依赖。源码和本地故障测试不等于
生产高可用、持续容量或真实 provider 的调用验收。

## 分层与一次任务的生命周期

| 层 | 当前职责 | 权威与恢复边界 |
| --- | --- | --- |
| 客户端 | 保存 Idempotency-Key、TaskID、RunID；提交后可离线 | HTTP 超时不能推出任务未创建 |
| Task HTTP API 多副本 | Bearer→owner；提交、状态、结果、取消、事件分页 | 无本地任务队列；远端 Start 确认或核对到同内容任务后才返回 202 |
| Temporal 服务 | Workflow ID 接受、队列、History、Activity 重试与取消 | 服务器持久化与保留策略决定可恢复范围；本地 SQLite 故障实验不代表集群 HA |
| TaskWorkflow Worker | Deep Agents 规划、虚拟文件、todos、messages | Workflow 内 InMemorySaver 经 History 重放重建；不直接访问外部 checkpointer |
| 模型 Activity | 以 Worker 配置创建 ChatOpenAI，调用外部模型 | 重试可能重复调用模型；固定 alias，HTTP 输入不能选择 provider、模型或 fixture |

同 owner 的 Idempotency-Key 经确定性摘要形成 TaskID，也是 Workflow ID。请求摘要
包含 message、TaskID、owner、协议版本和 task queue。并发 API 副本依赖 Temporal
原生 Workflow ID 冲突保护：同 key 同摘要复用现有任务，不同摘要返回 409。
已完成任务也可复用，前提是服务仍保留该执行。

该去重不是永久承诺。关闭的执行超过 namespace retention 或被管理员删除后，原
key 可能再次创建执行；查询与事件也可能返回 404。调用方必须保存已接受 handle，
并让部署保留期覆盖业务重试和结果读取窗口。当前没有永久幂等台账、任务列表接口
或自动归档服务。

## 启动与配置

以下命令在 `deepagents-server/` 下执行。先准备已校验的 Temporal CLI 或可访问的
受信任 Temporal 服务；固定本地 CLI/Server 校验值见 [原型记录](TEMPORAL_PROTOTYPE.md)。
服务端地址、namespace 和队列必须由部署方统一配置。

```sh
uv sync --locked --extra temporal
```

本地机制验证可在单独终端启动有持久文件的 dev server。`TASK_TEMPORAL_DIR` 必须是
本次实验专用且持久保留的绝对目录；重启使用同一目录，不能同时启动两个进程写同一文件。
此命令不是生产 Temporal 集群部署方案。

```sh
mkdir -p "${TASK_TEMPORAL_DIR:?set an isolated absolute directory}"
"${TEMPORAL_BIN:?set the verified Temporal CLI path}" \
  --disable-config-env --disable-config-file --log-level warn \
  server start-dev --ip 127.0.0.1 --port 7233 --headless \
  --db-filename "$TASK_TEMPORAL_DIR/history.sqlite"
```

通过本地凭据管理设置 `TASK_TOKEN`、`MINILOOP_TASK_SERVER_CURSOR_SECRET` 和 Worker 的
`OPENAI_API_KEY`，不要写入仓库或提交记录。前两者至少 32 位无空白 ASCII 字符，应
由安全随机生成器产生。下面只引用环境变量，不提供实际凭据。API token→owner 映射
可从已设置的 token 生成；所有 API 副本和重启必须使用同一映射与 cursor secret。

```sh
export TASK_OWNER=local-owner
export MINILOOP_DEEPAGENTS_TOKENS="$(.venv/bin/python -c \
  'import json,os; print(json.dumps({os.environ["TASK_TOKEN"]: os.environ["TASK_OWNER"]}))')"
.venv/bin/python -m mini_loop_deepagents.task_server.api \
  --address 127.0.0.1:8123 --temporal-address 127.0.0.1:7233 \
  --namespace default --task-queue mini-loop-task-v1 --max-inflight 32
```

在另一终端以 Worker 凭据环境启动执行进程。相同队列可运行多个 Worker，各自使用
不同 identity；扩容前须考虑 provider 额度。Worker 不需要 API Bearer token。

```sh
export MINILOOP_DEEPAGENTS_MODEL=openai:gpt-4.1-mini
export MINILOOP_DEEPAGENTS_MAX_TOKENS=4096
export MINILOOP_DEEPAGENTS_TIMEOUT_SECONDS=60
export LANGSMITH_TRACING=false
.venv/bin/python -m mini_loop_deepagents.task_server.worker \
  --address 127.0.0.1:7233 --namespace default \
  --task-queue mini-loop-task-v1 --identity task-worker-a \
  --activity-slots 1 --workflow-slots 2
```

生产 CLI 只使用真实 ChatOpenAI adapter，没有切换测试 fixture 的参数。缺少 provider
凭据会在 Worker 初始化模型时失败。网络推理、模型质量、费用和 provider 429 行为
仍需在部署环境单独验收；不要把离线模型故障测试当成已执行这些调用。

| 配置 | 默认或约束 |
| --- | --- |
| API token 映射 | `MINILOOP_DEEPAGENTS_TOKENS` 必填 JSON；owner 为安全 namespace 字符，1–128 位 |
| 事件游标签名 | `MINILOOP_TASK_SERVER_CURSOR_SECRET` 必填；轮换后旧 cursor 失效 |
| Worker 模型 | `MINILOOP_DEEPAGENTS_MODEL=openai:gpt-4.1-mini`；只接受 `openai:` 模型标识 |
| 模型输出与调用超时 | `MAX_TOKENS=4096`（1–32768）；`TIMEOUT_SECONDS=60`（1–300 秒）；均带 `MINILOOP_DEEPAGENTS_` 前缀 |
| 单 API 进程 | `--max-inflight=32`（1–1024）；应用层超限 429，底层连接过载也可能返回 503；请求时限 20 秒 |
| 单 Worker 进程 | Activity 槽默认 1，Workflow task 槽默认 2，均为 1–32；Workflow cache 为 0 |
| 单任务 | Workflow execution timeout 24 小时；Deep Agents recursion limit 为 128 个图步骤，不等于 128 次模型请求 |
| 单模型 Activity | 每次 start-to-close 上限 330 秒、heartbeat timeout 5 秒、最多 3 次尝试；OpenAI SDK 重试关闭 |
| 提交与事件 | HTTP JSON body 上限 16 KiB；事件每页 1–100 条（默认 50）；Temporal RPC 默认 5 秒 |

排队时间包含在 Workflow execution timeout 内。重启不会重置这项总期限，128 步
也限制了本版本能承载的任务长度。达到图步骤上限会以 `task_step_limit` 结束为
FAILED，避免把业务预算耗尽误当成可无限重试的 Workflow Task 故障。

## HTTP 使用与恢复语义

提交正文只允许非空字符串 `message`。owner 由认证得出；重复 JSON 字段、未知字段
及用户指定 owner/model 等均拒绝。每个逻辑任务保存自己的 key，遇到结果未知时
重试相同 key 和正文；新业务任务使用新 key。

```sh
export TASK_KEY=report-20261016-001
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer ${TASK_TOKEN:?set the API token}" \
  -H "Idempotency-Key: $TASK_KEY" -H 'Content-Type: application/json' \
  --data '{"message":"先规划步骤，再写一份关于任务恢复机制的说明到 /report.md。"}' \
  http://127.0.0.1:8123/v1/tasks
```

202 响应含 `task_id`、`run_id`、`status`、`version`、`reused` 和
`write_confirmed=true`，`Location` 指向状态地址。`running` 表示 Temporal 执行尚未
终结，可能仍在排队，并不证明某个 Worker 正占用 CPU。Start 结果不确定时返回
503、`submission_unknown`、稳定 TaskID 和 `outcome=unknown`；HTTP 总超时
也可能只返回 503 `request_timeout`。两种情况都不能改 key 盲目创建另一项任务。

保存返回的 TaskID 后执行下列调用。取消请求单独列出，按需要运行。

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $TASK_TOKEN" \
  "http://127.0.0.1:8123/v1/tasks/${TASK_ID:?set the returned task_id}"
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $TASK_TOKEN" \
  "http://127.0.0.1:8123/v1/tasks/$TASK_ID/result"
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $TASK_TOKEN" \
  "http://127.0.0.1:8123/v1/tasks/$TASK_ID/events?limit=50"
```

```sh
curl --fail-with-body --silent --show-error -X POST \
  -H "Authorization: Bearer $TASK_TOKEN" \
  "http://127.0.0.1:8123/v1/tasks/$TASK_ID/cancel"
```

| 接口 | 成功语义 |
| --- | --- |
| `GET /v1/tasks/{id}` | 200；原生执行状态映射，直接查询 Temporal，不要求执行 Worker 在线 |
| `GET /v1/tasks/{id}/result` | 未终结为 202、`ready=false`；终结为 200、`ready=true`；成功含 messages/files/todos 的版本化快照，失败含稳定通用错误码 |
| `POST /v1/tasks/{id}/cancel` | 202 仅确认取消请求；`cancel_requested=true` 不代表已经停止，继续查询终态；已终结时为 false |
| `GET /v1/tasks/{id}/events` | History metadata 按 event_id 倒序；每项仅 ID、类型、时间；`next_cursor` 为 null 时结束 |
| `GET /healthz` | 无需认证的进程存活检查，不是 Temporal、Worker 或 provider readiness |

事件 cursor 绑定 namespace、owner、TaskID、RunID 和排他 event_id 边界并经过 HMAC
签名。将 `next_cursor` URL 编码后传入 `cursor`；API 重启、换副本或 History 增长
后可继续读取同一执行的更早事件。当前每次从 History 头部扫描，限制 64 个服务端页
和请求期限；历史过大时可能明确返回不可用，尚无独立事件索引。它不是 SSE 或逐 token
日志，也不会把 Workflow 输入、模型正文或工具 payload 放进事件响应。

## 部署、版本与数据边界

- Temporal RPC 只向可信 API/Worker 开放。HTTP Bearer 的 owner 检查不等于 Temporal
  ACL；直接访问 Temporal 的管理员可看到 History 中的输入与执行数据。当前 CLI
  未接入 TLS/mTLS 参数，跨主机生产部署的传输与访问控制仍待实现和验收。
- API 副本共享 Temporal namespace、task queue、协议版本、token→owner 映射和
  cursor secret。不要把既有 token 改绑给其他 owner。输入摘要包括队列，滚动发布时
  随意换队列会使原 key 重试发生 409；当前没有按旧任务自动选择旧队列的路由器。
- Worker 必须保留兼容的代码与模型配置。`task-v1`/schema 1 是协议标签，不是镜像
  pinning；当前没有 Worker Versioning 部署、构建摘要路由或不兼容 graph 自动迁移。
  停掉旧 Worker 前应证明新版本可重放，或保留旧池直至任务完成。
- 此 TaskWorkflow 默认没有 owner Memory、Continue-as-New、审批恢复、宿主 shell
  或外部 Artifact 服务。旧原型的 SQLite Memory 回执与完整状态迁移不被此服务继承。
  files 为虚拟图状态，完整结果与 Activity payload 会增长 History；当前没有大结果
  外置或不受限长任务承诺。
- `runtime.py` 复用旧原型的 `DeterministicStateBackend` 和
  `StableToolMessageIDs`。前者依赖 Deep Agents 0.7.25 的私有 backend seam；升级须
  重验文件时间戳、消息 ID 与冷重放。复杂子代理、摘要压缩及任意自定义工具组合未被
  此次受控故障测试完整覆盖。
- API/Worker 槽是进程内限制，增加副本会增加总额度。跨 owner 公平调度、全局
  provider 限流、积压上限、集群 HA、备份恢复及 RPO/RTO/吞吐 SLO 仍待实现或验收。
  Activity 可重试，新增外部副作用工具必须自行实现稳定幂等与权限协议。

## 验证与交付证据

2026-10-16 专用本地故障验收 **8 组通过**，run_id
`792fc91351654e1ba8ff5f3b7fa35c32`。耗时 **32.45 秒**，owned 进程树采样峰值
**780 MiB**，最低可用内存 **44%**，共 61 个资源样本，16 个任务所属进程均已退出。
脱敏 [证据摘要](docs/task-server-evidence.json) 保存验证范围与固定源码摘要；私有完整
收据留在忽略的 `var/task-server-prototype/<run_id>/receipt.json`。

最后一组使用真实 model/tool 循环触发图步骤上限：43 个模型 Activity 完成后，
原生 History 出现不可重试的 `task_step_limit` WorkflowExecutionFailed；
WorkflowTaskFailed 为 0，HTTP 结果为 `failed/ready=true`。这证明预算耗尽能进入
可观察终态，不证明所有程序异常都已被分类。

本轮全模块 **323 passed**，根 Python **2256 passed / 28 skipped**，原包 **77 modules**
不变量检查通过；lock/offline wheel 及源码内容审计通过。完整门禁记录见
[DELIVERY.md](DELIVERY.md)。已有离线原型的通过记录
保留在 [TEMPORAL_PROTOTYPE.md](TEMPORAL_PROTOTYPE.md)。此轮使用真实 Task API、
TaskWorkflow 与 Deep Agents，但测试 Worker 注入确定性模型，未调用外部 provider。

```sh
.venv/bin/python scripts/task_server_fault_smoke.py \
  --temporal-binary /absolute/path/to/temporal \
  --binary-sha256 EXPECTED_EXTRACTED_BINARY_SHA256 \
  --restart-temporal
```

脚本只启动自身持有的隔离服务和 Worker，生成私有运行目录；它对进程树施加 2 GiB
RSS、180 秒和最低可用内存比例的观察预算，超限标为 resource-blocked。验收要覆盖
双 API 幂等竞争、独立生产者退出、双 Worker 并行、全部 API 替换、同数据库 Temporal
重启、Worker 冷重放、跨 owner 拒绝、事件续页、取消隔离和真实步骤上限终态。

本轮独立 Agent Server [架构图](../docs/audits/task-server-2026-10-16.png) 与
[精确生图提示词](../docs/audits/task-server-2026-10-16.prompt.txt) 标注新增 Task HTTP
路径；全仓库规范仍以 [README 架构图](../README.md#architecture) 为准。
具体运行证据与提交范围由 [DELIVERY.md](DELIVERY.md) 汇总。
