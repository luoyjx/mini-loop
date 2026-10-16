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

本轮审查基线为 `9af554a`。新任务启动 `TaskWorkflowV2` / `task-v2`，结果 snapshot
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
| V2 单任务 | 原始 Workflow execution timeout 24 小时；512 个父 model 边界；每 8 个安全边界 CAN；不是全部模型调用上限 |
| 图 invocation | 父图 128 步；原生 GP 子图 9999 步；v1 仍沿用原 128 步任务行为 |
| V2 状态迁移 | 16 MiB raw journal、384 KiB compressed checkpoint、512 KiB continuation envelope |
| 单模型 Activity | 每次 start-to-close 上限 330 秒、heartbeat timeout 5 秒、最多 3 次尝试；OpenAI SDK 重试关闭 |
| 提交与事件 | HTTP JSON body 上限 16 KiB；事件每页 1–100 条（默认 50）；Temporal RPC 默认 5 秒 |

排队时间包含在 Workflow execution timeout 内，重启与 CAN 都不会重置这项总期限。
各层预算分别生效；CAN 不放宽 provider 时限、Activity 重试或父/子图自己的限制。

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

## 部署、版本与数据边界

- Temporal RPC 只向可信 API/Worker 开放。HTTP Bearer 的 owner 检查不等于 Temporal
  ACL；直接访问 Temporal 的管理员可看到 History 中的输入与执行数据。当前 CLI
  未接入 TLS/mTLS 参数，跨主机生产部署的传输与访问控制仍待实现和验收。
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

本轮 V2 专用故障运行 `caffa4578ab348a3bc7198eb430bcbfb` 的 **4 组验收通过**：
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

此前 HTTP 故障脚本也在当前代码重新运行：`d252d1bc60c9470b84747c5dcbda28bc`，
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

本轮独立 Agent Server [架构图](../docs/audits/task-server-can-2026-10-16.png) 与
[精确生图提示词](../docs/audits/task-server-can-2026-10-16.prompt.txt) 标注 V2 分段续跑、
原生状态迁移与跨 Run 观察；全仓库规范仍以 [README 架构图](../README.md#architecture) 为准。
具体运行证据与提交范围由 [DELIVERY.md](DELIVERY.md) 汇总。
