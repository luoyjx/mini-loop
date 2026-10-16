# 独立 Temporal Task HTTP 服务

这是 `task_server/` 下显式启动的长任务服务。HTTP API 只提交和观察任务，独立
Worker 运行真实 Deep Agents；客户端断开或 API 进程退出不会主动取消已接受任务。
任务身份、状态、结果、取消请求与生命周期事件以 **Temporal 保留的 Workflow
History 为单一权威**。此路径没有另建业务 SQL 台账、Outbox 或第二套任务重试器。

它与标准 LangGraph Agent Server、[离线故障原型](TEMPORAL_PROTOTYPE.md) 以及原
Python/Go 业务分别运行。固定依赖仍为 Deep Agents `0.7.25`、Temporal Python SDK
`1.34.0`（可选 `temporal` extra）；本轮新增经用户批准的固定 Python 基础镜像，
没有增加 Python 包。源码和本地故障测试不等于
生产高可用、持续容量或真实 provider 的调用验收。

## 分层与一次任务的生命周期

| 层 | 当前职责 | 权威与恢复边界 |
| --- | --- | --- |
| 客户端 | 保存 Idempotency-Key、TaskID、RunID；提交后可离线 | HTTP 超时不能推出任务未创建 |
| Task HTTP API 多副本 | Bearer→owner；提交、状态、结果、取消、事件分页 | 无本地任务队列；远端 Start 确认或核对到同内容任务后才返回 202 |
| 镜像与角色 launcher | 同一制品分别运行 API/Worker；按角色读取 Secret 文件；API 提供 namespace readiness | 同进程信号所有权；外接 Temporal/ingress，容器配置不提供 HA 或版本路由 |
| 共享 Client 连接配置 | API/Worker 各自显式连接；CLI 环境配置，library 显式对象 | 可选验证 TLS/mTLS；没有独立代理服务，HTTP owner 与 Temporal ACL 分开 |
| Temporal 服务 | Workflow ID 接受、队列、History、Activity 重试与取消 | 服务器持久化与保留策略决定可恢复范围；本地 SQLite 故障实验不代表集群 HA |
| TaskWorkflowV2 + v1 Worker | Deep Agents 规划、子代理、原生摘要、虚拟文件、todos、messages | V2 完整 public saver 日志跨 Run 迁移；同 Run 经 History 重放重建；不访问外部 checkpointer |
| 模型 Activity | 以 Worker 配置创建 ChatOpenAI，调用外部模型 | 重试可能重复调用模型；固定 alias，HTTP 输入不能选择 provider、模型或 fixture |

同 owner 的 Idempotency-Key 经确定性摘要形成 TaskID，也是 Workflow ID。请求摘要
包含 message、TaskID、owner、协议版本和 task queue。新提交为 `task-v2`，重复旧 key
使用已有任务版本的摘要核对，不会将 v1 自动迁移为 v2。并发 API 副本依赖 Temporal
原生 Workflow ID 冲突保护：同 key 同摘要复用现有任务，不同摘要返回 409。
已完成任务也可复用，前提是服务仍保留该执行。

该去重不是永久承诺。关闭的执行超过 namespace retention 或被管理员删除后，原
key 可能再次创建执行；查询与事件也可能返回 404。调用方必须保存已接受 handle，
并让部署保留期覆盖业务重试和结果读取窗口。当前没有永久幂等台账、任务列表接口
或自动归档服务。

## V2 分段续跑与完整状态

V2 已在 `98de8d2` 交付。本轮以 `06a93d4` 为审查基线，保留相同 Workflow 代码；
新任务启动 `TaskWorkflowV2` / `task-v2`，结果 snapshot
为 schema 2。Worker 同时注册原 `TaskWorkflow` / `task-v1`，v1 运行码保持不变。
部署时先替换为能同时运行两版的 Worker，再切换 API；旧 Worker 无法运行 V2，不能
把新任务直接派给只认识 v1 的 Worker 池。这里没有自动 Worker Versioning 路由。

V2 让 Deep Agents 在父图 `model` 节点前暂停，等待前一轮工具、general-purpose
子图与摘要 middleware 完成，且确认没有未完成子状态、错误或人工 interrupt。
每完成 8 个这样的父 model 边界，在下一次 model 前执行 Continue-As-New（CAN）。
这是同一 Task/Workflow execution chain 的新 Run，不是再发一次用户请求；TaskID、
owner、请求 memo 和 `first_execution_run_id` 保持稳定，RunID 改变。

`checkpoint.PortableSaver` 记录 public `put` / `put_writes` 操作，包含原生 channel、
pending writes、task_path、路由、DeltaChannel 祖先和 middleware 私有状态。每次写入
立即序列化，后续对象修改不能改变已记录内容。完整日志经过 msgpack/zlib/base64
进入下一 Run 的受信任参数，恢复时通过同样的 public saver 操作重建；没有读取 saver
私有 storage/blob/writes 字段，也不增加磁盘或 SQL checkpointer。迁移数据只接受
内部 Workflow History，公开 HTTP 不能提交 checkpoint。

这项协议保留原生摘要状态，而不只复制可见的 messages/files/todos。摘要仍由真实
`SummarizationMiddleware` 完成：40 messages **或** 16000 tokens 触发，保留 12
messages；摘要模型 I/O 同样经过 Activity。session ID、summary message ID 与虚拟
归档文件时间来自 Workflow 确定性时钟/ID。原生 PatchToolCalls slot 保留 dangling-call
修复，父图及 GP 子图的真实 ToolMessage ID 被稳定化。没有替换成测试专用摘要算法。

CAN 缩短单个 Run 的 History，但迁移 journal 累积此前写入；它不是恒定大小状态或
无限长执行承诺。原始 24 小时总执行期限（含排队）不重置，最多 512 个**父 model
边界**。子代理与摘要会产生额外模型调用，因此不能把 512 写成整个任务的模型调用上限。
每次父图 invocation 的 recursion limit 为 128；默认 GP 子图仍沿用原生 9999，
没有被父图 128 限制覆盖。还有以下显式失败边界：

| 边界 | 行为 |
| --- | --- |
| 完整 journal 原始字节 > 16 MiB | `task_checkpoint_limit`；不继续扩大保存的数据 |
| 压缩 checkpoint > 384 KiB 或 continuation JSON envelope > 512 KiB | `task_checkpoint_limit`；不发出超限 CAN |
| checkpoint 编码、摘要、schema、身份或累计预算不匹配 | `task_checkpoint_invalid`；不接受损坏迁移 |
| 不满足安全父 model 边界 | `task_checkpoint_boundary`；不跳过尚未完成工作 |
| 父 model 总预算或一次图 invocation 步数耗尽 | `task_step_limit`；进入原生 FAILED，而不是无限 Workflow Task 重试 |

这些类型保存在受信任 Temporal History；公开 result 对失败仅返回稳定的通用
`workflow_failed`，不暴露内部异常或模型正文。Memory、审批恢复和不兼容 graph
升级不属于本协议。新协议的运行验收与旧 v1 故障记录分开，见文末交付证据。

## 启动与配置

独立应用镜像与 Compose 部署见 [Task 容器部署](TASK_DEPLOYMENT.md)。该 profile 使用
`launch api|worker` 在同进程进入以下 CLI，按角色加载 Secret 文件；外接 Temporal，
不启动标准 Agent Server/SQL/Redis。以下命令仍是直接运行 Python 入口。

以下命令在 `deepagents-server/` 下执行。先准备已校验的 Temporal CLI 或可访问的
受信任 Temporal 服务；固定本地 CLI/Server 校验值见 [原型记录](TEMPORAL_PROTOTYPE.md)。
服务端地址、namespace 和队列必须由部署方统一配置。示例保留原队列名
`mini-loop-task-v1` 以兼容既有请求摘要；队列名称不决定 Workflow 版本，新任务仍为 V2。

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
  --activity-slots 1 --workflow-slots 2 \
  --shutdown-grace-seconds 2 --shutdown-timeout-seconds 15
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
| Worker CLI 停机 | `--shutdown-grace-seconds=2`，有限数 0–300；`--shutdown-timeout-seconds=15`，有限数、大于 grace、最多 360 秒 |
| V2 单任务 | 原始 Workflow execution timeout 24 小时；512 个父 model 边界；每 8 个安全边界 CAN；不是全部模型调用上限 |
| 图 invocation | 父图 128 步；原生 GP 子图 9999 步；v1 仍沿用原 128 步任务行为 |
| V2 状态迁移 | 16 MiB raw journal、384 KiB compressed checkpoint、512 KiB continuation envelope |
| 单模型 Activity | 每次 start-to-close 上限 330 秒、heartbeat timeout 5 秒、最多 3 次尝试；OpenAI SDK 重试关闭 |
| 提交与事件 | HTTP JSON body 上限 16 KiB；事件每页 1–100 条（默认 50）；Temporal RPC 默认 5 秒 |

排队时间包含在 Workflow execution timeout 内，重启与 CAN 都不会重置这项总期限。
各层预算分别生效；CAN 不放宽 provider 时限、Activity 重试或父/子图自己的限制。

## Temporal 连接与 TLS/mTLS

API 与 Worker 共用 `task_server/connection.py` 中的不可变
`TemporalConnectionSettings` 与 `connect_temporal`。生产 CLI 才调用 `from_env`；
library `serve` / `run_process` / `connect_temporal` 的 `connection=None` 明确使用
明文默认值，**不读取宿主环境中的安全变量**。可信库调用方需要显式构造并传入设置。
地址仍用现有 API `--temporal-address` / Worker `--address`，格式为 `host:port`
（支持带方括号的 IPv6），不接受含 URL scheme、路径或凭据的地址。

所有变量以前缀 `MINILOOP_TASK_TEMPORAL_` 开头：

| 后缀 | 用途与约束 |
| --- | --- |
| `TLS` | 缺省 `false`；只接受小写 `true` / `false`，不自动推断 |
| `CA_CERT_FILE` | 自定义服务端信任 CA 的 PEM 文件；未提供时使用 SDK 默认根证书策略 |
| `CLIENT_CERT_FILE` / `CLIENT_KEY_FILE` | mTLS 客户端 PEM 证书/私钥，必须同时提供 |
| `SERVER_NAME` | 可选 `TLSConfig.domain`：同时设置 SNI、HTTP/2 authority 和默认证书校验名；不是跳过校验 |
| `API_KEY` / `API_KEY_FILE` | SDK API-key metadata，两者只能选一个；与 Task HTTP Bearer token 不同 |

安全字段要求 `TLS=true`；即使字段为空，`TLS=false` 与任何安全变量同时出现也拒绝。
证书/私钥各最多 1 MiB，API key 最多 16 KiB；key 文件只容许一个末尾 LF 或 CRLF，
不把多行或空白无条件裁掉。文件必须为可读的非空普通文件（允许 Secret 挂载符号链接），
FIFO、目录和超限文件被拒绝。没有 skip-verify 开关，也不在握手失败后退回明文。
配置与连接异常只公开固定错误码，隐藏原始异常内容；这些保护不改变操作系统对文件
权限的责任。请由部署的 Secret 管理机制提供证书与凭据，不把它们放入 Task message。

例如在启动两个 CLI 前为各进程配置下列环境；示例路径是部署方预置的 Secret 文件：

```sh
export MINILOOP_TASK_TEMPORAL_TLS=true
export MINILOOP_TASK_TEMPORAL_CA_CERT_FILE=/run/secrets/temporal-ca.pem
export MINILOOP_TASK_TEMPORAL_CLIENT_CERT_FILE=/run/secrets/temporal-client.pem
export MINILOOP_TASK_TEMPORAL_CLIENT_KEY_FILE=/run/secrets/temporal-client.key
export MINILOOP_TASK_TEMPORAL_SERVER_NAME=temporal.example.internal
```

API-key 部署可改用 `MINILOOP_TASK_TEMPORAL_API_KEY_FILE`，仍需显式 `TLS=true`。
是否需要 mTLS、API key 或服务端支持的组合，由 Temporal 部署策略决定；本模块不配置
Cloud ACL 或服务端 Authorizer。共享工厂保留 namespace/Worker identity，采用
`lazy=False`，首次连接有 10 秒总预算。连接成功只证明该连接完成，不等于所有 namespace
RPC 已获授权；服务端拒绝授权仍由后续 RPC 返回。HTTP Bearer→owner 校验只保护本
Task API，不能保护绕过 HTTP 直接调用 Temporal 的主体，服务端管理员也能读取 History。

证书与 key 在启动时读取一次，不监听文件轮换。替换 Secret 文件后必须重新创建 Client；
CLI 部署应滚动替换 API/Worker，并继续遵守已有 Activity 重试预算与排空协议。TLS 只覆盖
客户端传输，不提供 History 的静态加密、永久留存或数据库 HA。本地测试所用 stdlib
mTLS 隧道终止 TLS 后连到 loopback dev Temporal，属于测试设施，不是产品部署组件；
原生 Temporal 服务 TLS、Cloud API-key ACL、跨主机 HA 与容量仍需目标环境验收。

## Worker 滚动停机与任务交接

`worker.run_process` 是独立 CLI 的可信进程入口，在连接 Temporal 前安装 SIGTERM /
SIGINT handler；第一个信号触发显式 `Worker.shutdown()`。此前只有
`graceful_shutdown_timeout=2` 配置和 `await worker.run()`，没有将 SIGTERM 接入
SDK 停机流程，因此不能从旧配置推导出滚动排空已经工作。`401e4d5` 的这项修改
不改变 v1/V2 Workflow、CAN 协议或模型 Activity 重试策略，只补进程所有权。

SDK 停机停止新的轮询，继续处理已经领取的工作；已在 Core 内缓冲的 Start 仍可能
交给语言层，因此不能用“信号后绝无新 Activity 开始”作为保证。宽限内已领取的
Activity 可以完成并提交结果，超过宽限则收到 SDK cancellation。固定 SDK 1.34.0
Core 对因 Worker shutdown 而取消的 Activity 返回可重试 `WorkerShutdown` failure，
由同队列存活 Worker 承接；它消耗原本最多 **3 次 Activity 尝试**，不额外增加重试
预算，也不向用户 Task 发送取消请求。若原尝试已经耗尽，Task 仍可能失败。

宽限不是进程总停机时限：Activity 若吞掉 cancellation，SDK 可继续等待。CLI 另设
从首信号开始的硬期限，默认 15 秒；第二次 SIGTERM/SIGINT 或到期通过 `os._exit(70)`
退出此专用进程，跳过可能继续等待的 asyncio Runner cleanup。未提交完成的
Activity 仍依赖原生 heartbeat/timeout 与有限重试恢复；不要把退出码 70 当作
已成功交接或任务完成。部署方容器/进程管理器应给硬期限留出退出与观察余量。

异步库入口 `serve(..., shutdown_grace_seconds=2, stop_event=...)` 接受调用方的
`asyncio.Event` 来请求 SDK 停机；它不安装进程信号、不启动强退 watchdog、不调用
`os._exit`。宿主应用拥有进程关闭策略，取消 library coroutine 保留取消语义。
`run_process` 只用于专用 Worker 进程，不应嵌进承载其他业务的主进程。

`401e4d5` 的受控替换顺序是：保留另外两个 Worker 服务，逐一给旧 Worker 发 SIGTERM，
观察退出后启动相同 namespace、queue、v1/V2 注册与模型配置的替换 Worker；
最多同时保留三个执行进程。生产环境若采用先起新副本再停旧副本，须另外预算临时
增加的执行槽和 provider 额度，并验收该发布方式。保持原 TaskID、first Run、
请求 memo 与队列配置，不重新提交用户输入。
当前停机入口使用固定生命周期日志，不将 Task 输入或凭据放入日志。进程替换不等于
graph schema 迁移、Worker Versioning、零中断服务或跨实例配额。

SDK 行为参见 [Worker API 文档](https://python.temporal.io/temporalio.worker.Worker.html)
与安装的固定 `temporalio==1.34.0` 源码：`worker/_worker.py` 的 `run/shutdown`，
`bridge/sdk-core/crates/sdk-core/src/worker/activities.rs` 的 shutdown cancellation
转换及 `worker_shutdown_failure`。真实交接、硬退和重试预算的历史验收见下文 rollout 收据。

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

202 响应含 `task_id`、`run_id`、`first_execution_run_id`、`status`、`version`、`reused` 和
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
| `GET /v1/tasks/{id}` | 200；跟随已核对 owner/版本/memo/first Run 的执行链，返回当前 Run；不要求执行 Worker 在线 |
| `GET /v1/tasks/{id}/result` | 未终结为 202、`ready=false`；终结为 200、`ready=true`；成功含 messages/files/todos 的版本化快照，失败含稳定通用错误码 |
| `POST /v1/tasks/{id}/cancel` | 以 first execution Run 约束原生取消请求，使 CAN 换 Run 后仍属于同一任务；202 仅确认请求，已终结时为 false |
| `GET /v1/tasks/{id}/events` | 单 Run History metadata 按 event_id 倒序；每项仅 ID、类型、时间；`next_cursor=null` 结束当前 Run 的分页 |
| `GET /healthz` | 无需认证的进程存活检查，不是 Temporal、Worker 或 provider readiness |

状态、结果和提交复用核对原生 execution chain；读到 CAN 切换边界时有限刷新。若
仍观察到旧 Run，`continued_as_new` 是非终态，result 为 `ready=false`，不能当成任务完成。取消使用
`first_execution_run_id` 保护执行链，不能误取消相同 TaskID 下另一次独立执行。

事件 cursor 绑定 namespace、owner、TaskID、RunID 和排他 event_id 边界并经过 HMAC
签名。将 `next_cursor` URL 编码后传入 `cursor`；API 重启、换副本或 History 增长
后可继续读取**同一 Run**的更早事件。`run_id` 可选：没有 cursor/run_id 时从当前
Run 开始；两者同时提供必须匹配。响应中的 `first_execution_run_id`、`previous_run_id`
和 `next_run_id` 支持沿链导航，每个相邻 Run 都核对 owner、请求 memo 和 first Run。
相邻 Run 已被保留策略删除时，对应导航为 null，并置
`history_navigation_incomplete=true`；不能把保留缺口解释成完整历史。

每个 Run 的 event_id 都从自己的 History 编号，没有 Task 全局 event_id；读取下一
Run 时清除旧 cursor，使用该 RunID。当前每次从选定 Run 的 History 头部扫描，限制
64 个服务端页和请求期限；历史过大时可能明确返回不可用，尚无独立事件索引。
这些事件不是 SSE 或逐 token 日志，不会暴露 Workflow 输入、模型正文或工具 payload。

## 容器入口与 API 就绪探测

`06a93d4` 之后新增 `launch api|worker`、固定基础镜像 Dockerfile 与独立 Compose
profile。launcher 按角色读取 `*_FILE`，全部校验通过后才加载到进程环境，同进程
调用真实 CLI，Worker 原信号所有权不变。API 挂载 Bearer 映射/cursor secret，Worker
挂载模型 key，两者使用各自 Temporal 客户端身份；无公共 fixture 选择入口。

`GET /healthz` 是纯进程 liveness；`GET /readyz` 是无认证的 namespace 只读检查，
仅在 `DescribeNamespace` 返回配置中的名称和 REGISTERED 状态时给出 200，否则为
503。单次 RPC 1 秒，singleflight 合并并发探测，成功/失败均缓存 1 秒；取消一个
HTTP 请求不会取消共享、独立受限的 RPC。公开响应只有 ready/not_ready，不能证明
Workflow Start 权限、Worker 槽、真实模型或端到端执行可用。HEAD 不享有 GET 探测
的认证豁免。

镜像使用 UID/GID 10001，Compose 只读 root、64 MiB `/tmp`、20 秒 stop grace，
超过 Worker 默认 15 秒硬退出期限。Secret 宿主权限必须允许此身份读取。API 只在
容器网络 expose 8123，需要已有 ingress；Compose healthcheck 状态不会自行触发
重启，`restart: unless-stopped` 是另一项进程退出策略。完整命令和发布边界见
[部署说明](TASK_DEPLOYMENT.md)。

## 部署、版本与数据边界

- Temporal RPC 只向可信 API/Worker 开放。HTTP Bearer 的 owner 检查不等于 Temporal
  ACL；直接访问 Temporal 的管理员可看到 History 中的输入与执行数据。CLI 已接入
  验证 TLS/mTLS 与 API-key metadata，但服务端 ACL 和跨主机生产部署仍需单独验收。
- API 副本共享 Temporal namespace、task queue、协议版本、token→owner 映射和
  cursor secret。不要把既有 token 改绑给其他 owner。输入摘要包括队列，滚动发布时
  随意换队列会使原 key 重试发生 409；当前没有按旧任务自动选择旧队列的路由器。
- Worker 必须保留兼容的代码与模型配置。`task-v2`/schema 2 与 `task-v1`/schema 1
  是协议标签，不是镜像 pinning；当前没有 Worker Versioning 部署、构建摘要路由或
  不兼容 graph 自动迁移。新 Worker 注册两版，旧 key 保留旧版本语义。
- V2 默认启用上文限定的 CAN，没有 owner Memory、审批恢复、宿主 shell 或外部
  Artifact 服务。旧原型的 SQLite Memory 回执不被此服务继承。files 为虚拟图状态；
  CAN 迁移和完整结果仍占用 Temporal payload/History，当前没有大结果外置。
- V2 复用固定版 `DeterministicStateBackend`，并为原生摘要、patch-tool middleware
  增加确定性适配。私有 backend/summary hooks 依赖 Deep Agents 0.7.25；升级须
  重验文件时间、消息 ID、摘要私有状态、原生子图及冷重放。受控 fixture 覆盖范围
  不等于任意自定义工具、深层子图或外部 provider 的通用兼容承诺。
- API/Worker 槽是进程内限制，增加副本会增加总额度。跨 owner 公平调度、全局
  provider 限流、积压上限、集群 HA、备份恢复及 RPO/RTO/吞吐 SLO 仍待实现或验收。
  Activity 可重试，新增外部副作用工具必须自行实现稳定幂等与权限协议。

## 验证与交付证据

本轮容器运行 `0c189c0de2614b5c8d4db07c4b4268be` **4 组通过**。使用最终固定
arm64 镜像：两 API、两受信任 fixture Worker 容器分别强杀重建后，两项真实 V2
Task 均完成各 10 个 native 工具步骤和 1 次 CAN；TaskID/首 Run 保持，4 份 History
为 0 WorkflowTaskFailed。两个门控 Activity attempt 1→2，原生身份、request SHA
与完整 native 状态相同。任务完成后同 SQLite Temporal 重启，原 API 容器保留，
readyz 200→503→200，healthz 始终 200，结果不变。

独立的默认生产 `launch worker` 空队列验收中，SIGTERM 后出现 DRAINING/STOPPED，
0.914 秒退出 0，没有调用 provider。首轮旧镜像失败揭示 Linux 空闲 selector 未被
`Event.set()` 唤醒；本轮增加 `call_soon_threadsafe` 唤醒并重新构建，最小 Linux
无周期 timer 探针 0.318611 秒退出 0，完整容器测试随后通过。失败运行
`74a3ec4f9711414a8d41b1ad7d463d4c` 保留，不算通过。

最终完整运行耗时 39.57 秒，61 个资源样本，宿主所属 RSS 加 cgroup 采样峰值
596.2 MiB，最低宿主可用内存 40%、VM 可用内存 87.44%；10 容器、2 个 Temporal
进程和驱动已清理，末尾四个业务容器是 SIGKILL/137，不能称为全部优雅退出。
六个测试凭据片段扫描 4 History/12 日志零匹配。该实验是单机 OrbStack、CLI SQLite
与离线模型，不是整个 mTLS Compose profile、真实 provider、跨机 HA 或容量验收。
完整镜像/manifest 摘要、失败原因、复跑命令与边界见 [Task 部署](TASK_DEPLOYMENT.md)。


已提交 `06a93d4` 的 TLS 历史基线：运行 `cfdf11707eb142be9c8af50e11cb5dae` 的 **3 组验收通过**，使用真实
SDK、生产 API 入口与 Worker 连接/执行代码、两个 API、两个 Worker 和两项 V2 Task。
测试设施是 stdlib mTLS 隧道，TLS 后端仅为 loopback 明文 dev Temporal；模型仍为
确定性 fixture。API、Worker、observer 分别使用测试客户端身份，未部署生产代理。

| 注入场景 | 实际观察 |
| --- | --- |
| 错 CA、错服务名、缺客户端证书、不受信任的客户端证书 | 四项真实 SDK 连接均返回固定 `temporal_connection_failed`；每个负例前后均有新的有效 RPC 成功控制，失败连接未进入隧道的已接受计数 |
| SIGKILL mTLS 隧道并在同端口重建 | API 返回真实 503 `task_service_unavailable`；6.133 秒后原两个 API、两个 Worker 与 observer 重新连接，四个业务进程 PID 保持不变；门控点完整 native 状态、Task/Run/Activity 身份与请求摘要一致 |
| 重连后执行至完成 | 两项 Task 全部 succeeded，各 10 个真实工具步骤、1 次 CAN；4 个 Run History，0 WorkflowTaskFailed；模型 Activity 总尝试记录 24，最多使用 attempt 2，仍受原最多 3 次预算约束 |

离线 attempt 后检进一步确认：22 个原生模型 Activity 共执行 24 次，其中两个门控
Activity 均从 attempt 1 重试到 2，由两个原 Worker 互换承接；每个 Activity 的
Workflow/Run/Activity ID、step 与 request SHA 保持一致。这是连接恢复和真实 Activity
重试共同完成的恢复，不意味着从头到尾保持同一次 attempt，也不保证外部模型只被调用一次。

四个 Run 的原生 History 和本次 23 份进程日志未匹配到测试凭据内容。这是针对所用
凭据与 PEM 片段的检查，不是任意未来日志的无泄漏证明；API key 只使用合成值验证
SDK metadata 配置，**没有验证真实 Cloud API-key ACL**。
运行耗时 **13.8 秒**，owned 进程树采样峰值 **822.0 MiB**，最低可用内存 **43%**，
26 个资源样本，23 个所属进程全部退出（包含故障注入及清理强停，不代表全部正常排空）。
脱敏 [TLS 证据](docs/task-tls-evidence.json) 与 [交付记录](DELIVERY.md) 保存最终范围；
原始收据位于忽略的 `var/task-tls-prototype/<run_id>/receipt.json`。

```sh
.venv/bin/python scripts/task_tls_fault_smoke.py \
  --temporal-binary /absolute/path/to/temporal \
  --binary-sha256 EXPECTED_EXTRACTED_BINARY_SHA256
```

以上验证 SDK→测试隧道的真实 TLS/mTLS 与连接中断恢复，不是原生 Temporal 服务 TLS、
多机或多区域 HA、生产证书轮换、数据库 HA、真实 provider 或容量验收。

`401e4d5` 的滚动停机历史基线运行 `7058dd0bb0f84158bb1d8a76155b59e4` 的 **4 组验收通过**。两个真实
HTTP API 接受 3 个 owner 的 12 项 V2 任务，三个生产 Worker 进程各有一个 Activity
槽。初始有 3 个 model Activity 执行、9 个原生 pending Activity 为 SCHEDULED；
替换 Worker 接续这 9 项排队任务，最终 **12 项全部 succeeded，各有 10 个真实工具
步骤和 1 次 CAN**。这一固定规模实验不代表生产吞吐或时延指标。

| 注入场景 | 实际观察 |
| --- | --- |
| SIGTERM 后在宽限内释放 Activity | 原 Worker 退出 0，已领取 Activity 完成 |
| SIGTERM 后超过宽限，Activity 配合 cancellation | 原 Worker 退出 0；另一 Worker 以同 Activity ID / RunID / request SHA 从 attempt 1 接到 2 |
| Activity 吞掉 cancellation | 原 Worker 在首信号后 5.087 秒退出 70；替换 Worker 以同身份与 request SHA 从 attempt 1 接到 2 |
| 两个已认证慢请求占满 API 的 2 个 admission 槽 | 下一次提交返回真实 HTTP 429 `api_capacity_exceeded`；原生查询为 NOT_FOUND，释放占用后同 key 重试接受 |

故障验收显式使用 **grace 1 秒 / hard deadline 5 秒**；生产默认仍是 **2 / 15 秒**。
观察到 134 个 model Activity 执行区间：132 完成、1 配合取消、1 因进程退出结束；
最大同时执行为 3。两次交接仅使用到 attempt 2，未穷尽原最多 3 次预算，不能据此
宣称无限次滚动替换都能恢复。本脚本也不作“信号后没有 buffered Start”的断言。
跨 API 同 key 重试、不同 owner 同 key 的独立身份，以及跨 owner 的状态/结果/事件/
取消 404 均已覆盖。API 429 是进程 admission 背压，不是 provider 429 或全局租户限额。

本次耗时 **20.95 秒**，owned 进程树采样峰值 **1059.5 MiB**，最低可用内存 **47%**，
40 个资源样本，45 个所属进程已退出。使用真实生产 `run_process`、Workflow、工具与
Temporal 服务，模型仍为确定性 fixture；本地 Temporal SQLite 不是集群 HA。
脱敏 [滚动停机证据](docs/task-rollout-evidence.json) 与 [交付记录](DELIVERY.md) 保存
范围与门禁，私有原始收据位于忽略的 `var/task-rollout-prototype/<run_id>/receipt.json`。

```sh
.venv/bin/python scripts/task_worker_rollout_smoke.py \
  --temporal-binary /absolute/path/to/temporal \
  --binary-sha256 EXPECTED_EXTRACTED_BINARY_SHA256
```

以下为 `98de8d2` 的 V2 历史基线。V2 专用故障运行 `caffa4578ab348a3bc7198eb430bcbfb` 的 **4 组验收通过**：
主任务经过 **4 次 CAN、5 个 Run、37 个父工具步骤、2 个真实 general-purpose
子代理和 2 次原生摘要 Activity**。在子代理 Activity 中、第一次 CAN 前、CAN 后
以及第三次 CAN 后分别强杀 Worker，后两组还替换全部 API，最后一组重启同数据库
Temporal。每次恢复核对完整语义状态与模型请求字节摘要；5 个 Run 都是
**0 WorkflowTaskFailed**。原始 cursor 可读取旧 Run，导航覆盖完整链；第二项已
CAN 的任务被取消而不影响主任务完成。私有摘要状态和归档虚拟文件跨 Run 保留。

本次耗时 **45.86 秒**，owned 进程树采样峰值 **797.3 MiB**，最低可用内存 **37%**，
87 个资源样本，19 个所属进程均已退出。私有完整收据保存在忽略目录
`var/task-can-prototype/<run_id>/receipt.json`。此测试使用真实生产 Workflow/Deep
Agents 工具、子图和摘要控制流，模型由确定性 fixture 提供，不是外部 provider 验收。

```sh
.venv/bin/python scripts/task_can_fault_smoke.py \
  --temporal-binary /absolute/path/to/temporal \
  --binary-sha256 EXPECTED_EXTRACTED_BINARY_SHA256
```

同一 V2 历史基线的 HTTP 故障脚本运行：`d252d1bc60c9470b84747c5dcbda28bc`，
**8 组通过**；22.18 秒、峰值 827.9 MiB、最低可用内存 40%，17 个所属进程退出。
它覆盖新 V2 API 的基础恢复，并以受信任原生 Start 创建旧 v1，验证 HTTP 同 key
复用、变更正文 409、外 owner 404，以及 v1 原 128 步限制仍进入失败终态。
完整门禁与脱敏证据由 [DELIVERY.md](DELIVERY.md) 汇总；上述受控实验仍不证明生产
集群 HA、容量、公平性、真实 provider 或任意 graph 升级兼容。

以下是已提交 `9af554a` 的 **v1 历史基线**。2026-10-16 专用本地故障验收 **8 组通过**，run_id
`792fc91351654e1ba8ff5f3b7fa35c32`。耗时 **32.45 秒**，owned 进程树采样峰值
**780 MiB**，最低可用内存 **44%**，共 61 个资源样本，16 个任务所属进程均已退出。
脱敏 [证据摘要](docs/task-server-evidence.json) 保存验证范围与固定源码摘要；私有完整
收据留在忽略的 `var/task-server-prototype/<run_id>/receipt.json`。

最后一组使用真实 model/tool 循环触发图步骤上限：43 个模型 Activity 完成后，
原生 History 出现不可重试的 `task_step_limit` WorkflowExecutionFailed；
WorkflowTaskFailed 为 0，HTTP 结果为 `failed/ready=true`。这证明预算耗尽能进入
可观察终态，不证明所有程序异常都已被分类。

v1 基线全模块 **323 passed**，根 Python **2256 passed / 28 skipped**，原包 **77 modules**
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

本轮独立 Agent Server [架构图](../docs/audits/task-server-container-2026-10-16.png) 与
[精确生图提示词](../docs/audits/task-server-container-2026-10-16.prompt.txt) 标注容器入口、
角色 Secret、API readiness 与 Linux 信号唤醒；全仓库规范仍以 [README 架构图](../README.md#architecture) 为准。
具体运行证据与提交范围由 [DELIVERY.md](DELIVERY.md) 汇总。
