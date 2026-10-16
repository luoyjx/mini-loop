# mini-loop Deep Agents Server

这是独立的 Deep Agents 服务模块，提供 **标准 LangGraph Agent Server** 与显式启用的
**Temporal Task HTTP 服务** 两个入口；它们分别拥有自己的调度与持久化路径，默认
不会互相转发任务。分层参考 [MDA Agent Server 设计](../docs/MANAGED_DEEP_AGENTS_AGENT_SERVER_DESIGN.md)。
模块有独立环境、依赖锁和启动配置，不调用已有 `mini_loop` Python/Go runtime。

标准入口图解：[五层架构与长任务的完整路径](ARCHITECTURE.md)。
长任务运行约束：[提交、并发、恢复与部署](LONG_TASKS.md)。
独立 Temporal HTTP 服务：[Task Server](TASK_SERVER.md) · [本轮分层图与修改标注](../docs/audits/task-server-can-2026-10-16.png)。

## 已实现与边界

下表与后续启动命令描述标准 LangGraph 入口；Temporal 入口见后文专节。

| 层 | 实现 | 状态 |
| --- | --- | --- |
| Agent harness | `create_deep_agent`，planning、文件工具、默认 general-purpose subagent | 真实 Deep Agents graph |
| Server | 标准 Threads、Runs、状态/历史、取消、同线程 Run 调度、SSE API | 原生后台 Run 调度，不自行实现队列 |
| 长任务客户端 | `AsyncJobsClient` 和 `scripts/jobs.py`：提交后返回 handle，查询、等待、取消、显式续跑 | 等待超时/客户端断线不取消 Run；每次提交默认新 Thread |
| 身份 | 服务端配置的随机 Bearer token → owner；禁用 Studio 旁路 | 默认拒绝未认证访问 |
| 资源授权 | Thread/Run owner metadata filter；Store namespace 首段必须为 owner | 模型输入和客户端 context 不授予身份 |
| 文件 | StateBackend，线程虚拟文件，由 Checkpoint 保存 | 无宿主文件访问 |
| Memory | `/memories/` → StoreBackend `(owner, "memories")` | 同 owner 跨 Thread 可用；不跨 owner 共享 |
| Model | 服务端 `ChatOpenAI` 实例；模型、超时、输出上限来自环境 | 真实 provider 调用需要自己的 API key |
| 本地开发 | `langgraph dev`，inmem runtime + 本地快照 | 可检查优雅退出后的恢复；非生产持久性保证 |
| 部署配置 | 同一不可变 image 分为 API-only + Worker；PostgreSQL 持久数据 + Redis 临时协调 | 默认 Worker 两个并发槽；生产崩溃/部署恢复仍待带许可证验收 |

没有实现 MDA Compiler、Context Hub、OAuth Connections、Channels 或托管 Sandbox。
用户不得创建/修改 Assistant，预安装 graph 的 Assistant 定义可供已认证用户读取；
不要在其 config/metadata 中放用户秘密，也不要复用包含历史私有 Assistant 的数据库。
Cron API 当前拒绝访问。Memory 是不可信知识，不是权限配置或凭据仓库。

Deep Agents 的 `execute` 名称仍可能出现在工具 schema 中，但当前虚拟 backend
始终返回 execution unavailable，没有 shell executor。其他文件操作均在虚拟文件中。
本模块没有移植原 mini-loop 的 ToolGate、审批、SQLite schema 或 AG-UI 协议。

## 本地启动

需要 Python 3.12、[uv](https://docs.astral.sh/uv/)；模块环境与仓库根 `.venv` 隔离。

```sh
cd deepagents-server
uv sync --locked
cp .env.example .env
uv run python -c 'import secrets; print(secrets.token_urlsafe(32))'
```

编辑 `.env`：用刚生成的随机值替换 TOKENS 对象的示例 key，并设置
`OPENAI_API_KEY`。每个 token 绑定一个 owner；多个 token 可以绑定同一 owner。
token 必须至少 32 个无空白 ASCII 字符，示例占位值会被拒绝；长度校验不能验证随机性。

```sh
uv run langgraph validate
uv run langgraph dev --host 127.0.0.1 --port 2024 --no-browser --n-jobs-per-worker 2
```

默认访问地址 `http://127.0.0.1:2024`，API 文档 `/docs`。API 通过
`Authorization: Bearer <token>` 认证。`/ok` 等平台健康探针可能公开。
首次创建 graph 时会校验模型配置和 provider 凭据；服务健康并不代表 provider 调用成功。
本地 `.langgraph_api/` 保存快照，删除它会丢失本地状态；不要把开发服务暴露到公网。

在第二个终端设置自己的 token，创建 Thread 并读取 SSE：

```sh
export MINILOOP_DEEPAGENTS_CLIENT_TOKEN='your-generated-token'
uv run python scripts/chat.py '规划一个简短任务，并将计划写入 /plan.md'
uv run python scripts/chat.py --thread '<上次输出的 thread_id>' '读取 /plan.md'
```

通用 HTTP 示例：

```sh
curl -sS http://127.0.0.1:2024/threads \
  -H "Authorization: Bearer $MINILOOP_DEEPAGENTS_CLIENT_TOKEN" \
  -H 'Content-Type: application/json' -d '{}'
curl -N http://127.0.0.1:2024/threads/THREAD_ID/runs/stream \
  -H "Authorization: Bearer $MINILOOP_DEEPAGENTS_CLIENT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"assistant_id":"agent","input":{"messages":[{"role":"user","content":"你好"}]},"stream_mode":["values"],"multitask_strategy":"enqueue"}'
```

## 提交异步长任务

交互式 `scripts/chat.py` 用 SSE 等待结果；长任务使用 `scripts/jobs.py`，先拿到
包含 `server_url`、`thread_id`、`run_id`、`request_id` 的 handle，随后可以退出终端。
文件不包含 token，仍应视作用户任务标识妥善保存；CLI 用 0600 权限原子写入。

```sh
uv run python scripts/jobs.py submit '规划并完成一个长任务' --output job.json
uv run python scripts/jobs.py status --job-file job.json
uv run python scripts/jobs.py wait --job-file job.json --timeout 30
uv run python scripts/jobs.py state --job-file job.json
uv run python scripts/jobs.py cancel --job-file job.json
uv run python scripts/jobs.py resume --job-file job.json --output continued-job.json
```

每次 `submit` 默认创建新 Thread，以便独立任务并发；加 `--thread THREAD_ID` 会在
该 Thread 排队串行。`wait` 超时只结束本地等待（退出码 2），Run 继续执行；
`status` 查询该 Run，`state` 读取整个 Thread 的当前状态，可能包含后续 Run。
取消采用 `interrupt`，保留 Checkpoint。`resume` 只接受最近一个 error/timeout/
interrupted Run 且仍有未完成 Checkpoint 任务的情况，以 `input=None` 新建 Run；
它拒绝需要人工答案的 graph interrupt。预检查不是跨客户端原子去重锁，续跑必须由
单一操作者协调。SDK 用法和失败判定见 [长任务运行约束](LONG_TASKS.md)。

创建 Thread/Run 不自动重试。请求发送后响应丢失时，CLI 以退出码 3 输出
`submission_uncertain` 与已知 Thread/Run/request 标识；应先查询服务端，避免直接重复
提交。`request_id` 是核对 metadata，不是服务端强制的幂等键。保存 handle 失败时仍会先
输出已接受的 handle；不要因此重新提交。客户端提交请求指定 `durability="sync"`。

## 部署配置

`Dockerfile` 由固定版本 CLI 从 `langgraph.json` 生成。`compose.yaml` 将 API 接入与
后台执行拆成两个进程，两者必须使用**同一个不可变应用镜像 ID 或 registry digest**：
API 的执行槽固定为 0；Worker 用标准 `/storage/queue_entrypoint.sh` 启动，默认两个
并发槽并开启 isolated loops。Worker 可按 `--scale worker=N` 横向增加；每个实例默认
两个槽，同一 Thread 仍串行。增加实例前按模型配额、数据库连接与内存重新配置容量。

PostgreSQL 持久卷保存 pending Run、执行状态、Checkpoint 和 owner Store；Redis
只承载临时唤醒、取消与事件，不是持久任务来源。数据库不公开宿主端口，API 只绑定
loopback。默认内存上限为 API 512 MiB、每个 Worker 1 GiB、PG 512 MiB、Redis 128 MiB，
单 Worker 实例的合计为 2.125 GiB；CPU 上限合计 2.25 核。这是小规模验证配置，
不是生产容量测量。进程隔离不提供工具 Sandbox；阻塞工具仍须审查或隔离执行。

标准 Agent Server 与自定义认证有独立许可条件。必须配置有效的
`LANGSMITH_API_KEY`、`LANGGRAPH_CLOUD_LICENSE_KEY`、`POSTGRES_PASSWORD` 和
`MINILOOP_DEEPAGENTS_IMAGE`。数据库密码使用 `secrets.token_urlsafe(32)` 生成；
Compose 将它放入 URI，只使用 URL-safe 字符。缺少必需配置会在启动前失败。检查
[Standalone 部署要求](https://docs.langchain.com/langsmith/deploy-standalone-server)
和[自定义认证许可说明](https://docs.langchain.com/langsmith/custom-auth)。

```sh
# Use the checked-in Dockerfile with its pinned base image digest.
docker build -t mini-loop-deepagents:local .
export MINILOOP_DEEPAGENTS_IMAGE="$(docker image inspect mini-loop-deepagents:local --format '{{.Id}}')"
docker compose config --quiet
docker compose up -d
docker compose logs -f api worker
```

默认每次执行 attempt 上限为 24 小时，原生执行 attempt 总数上限为 3（0.16.0 的
`BG_JOB_MAX_RETRIES` 包含第一次执行）。这约束可重试执行故障，不代表任意模型错误都
会自动重试。Worker 优雅停机窗口为 180 秒，容器停机窗口为 210 秒。部署时先停止旧
Worker 接新任务并等待退出，再用同一新镜像重建 API/Worker；未完成任务的恢复以
PostgreSQL 中的 Run 与 Checkpoint 为依据。示例操作和强杀验收见
[长任务运行约束](LONG_TASKS.md)。

`docker compose down` 保留 PG 卷；`down -v` 删除持久数据。开发快照不会自动迁入 PG。
发布须保持 graph ID、Checkpoint schema、包/工具兼容和 token→owner 映射；不兼容
更新需要迁移或先排空任务。同步 Checkpoint 仍会重放未完成步骤，不保证外部副作用
exactly-once；未来添加外部工具时必须有业务幂等键。SSE 也不是逐 token 持久日志。
**真实 PostgreSQL/Redis 部署及 Worker 崩溃、滚动部署恢复尚未通过生产栈验收**，需要
适用许可证与该栈的故障注入结果；本地快照恢复不能替代这项证据。

## 配置与扩展

| 配置 | 默认 / 约束 |
| --- | --- |
| `MINILOOP_DEEPAGENTS_TOKENS` | 必填 JSON token→owner；owner 为 1–128 位安全 namespace 字符 |
| `MINILOOP_DEEPAGENTS_MODEL` | `openai:gpt-4.1-mini`；当前仅开放 OpenAI adapter |
| `MINILOOP_DEEPAGENTS_MAX_TOKENS` | `4096`；范围 1–32768 |
| `MINILOOP_DEEPAGENTS_TIMEOUT_SECONDS` | `60`；范围 1–300 秒，必须有限 |
| `OPENAI_API_KEY` | 由 provider SDK 读取，不接受来自 Run context 的 key |
| `LANGSMITH_TRACING` | 示例为 `false`；启用前评估 trace 中的输入/工具内容 |

`graph.agent(runtime)` 是无副作用 factory，inspection 时也能调用；不依赖
`execution_runtime.context`。构造不写库，不 provision 资源，不自行配置服务端
checkpointer/store。Memory namespace 只读取执行 runtime 的 verified `server_info.user`。
服务端 instructions 随 package 发布；修改模型/工具/指令后重新部署，没有 Context Hub 热更新。
自定义工具应通过受信任代码加入 `build_agent`，普通 Python 工具在 Worker 进程执行，
不会自动进入 Sandbox。当前内存文件读改写没有跨 Thread CAS/事务合并保证；同 owner
两个 Thread 同时改一个 memory 文件可能发生覆盖，重要业务状态应放外部业务数据库。

## 可选 Temporal Task HTTP 服务

[Task Server](TASK_SERVER.md) 是独立 opt-in 服务，使用 Temporal Python SDK 1.34.0；它不经过
上面的标准 Agent Server API，也不改变原 Python/Go 业务。HTTP Bearer token 绑定
owner，`POST /v1/tasks` 接受 `Idempotency-Key` 和 message。只有远程 Workflow Start
已确认，或已核对为同一 owner/相同内容的现有任务，才返回 202。相同 owner/key
生成稳定 TaskID，跨 API 副本重复相同内容复用任务，不同内容返回 409；该去重受
Temporal retention 约束，不是永久业务台账。Start 结果未知时返回稳定 TaskID 供核对，
不把本地请求接收当作持久接受。

新提交使用 `TaskWorkflowV2` / `task-v2`；Worker 同时保留不变的 v1 运行码，旧 key
重试按其原版本摘要复用。真实 Deep Agents 每经过 8 个安全父 model 边界执行
Continue-As-New，完整 public saver 写日志携带路由、DeltaChannel 祖先、files/todos、
messages 和摘要私有状态进入下一 Run；没有新增 checkpoint 数据库。固定原生摘要
以 40 messages 或 16000 tokens 触发并保留 12 messages，使用 Workflow 确定性时钟/ID。

模型 I/O 经 Activity，在 Worker 环境中把固定 alias 解析为 ChatOpenAI。公开
HTTP/生产 CLI 无 fixture 或 provider 选择参数，未启用 owner Memory。24 小时总期限
不随 CAN 重置，最多 512 个父 model 边界；不等于全部模型调用上限。原生 GP 子图
保留 9999 步上限，父图每次 invocation 为 128 步。状态迁移受 16 MiB 原始日志、
384 KiB 压缩 checkpoint、512 KiB continuation envelope 限制，超限明确失败。

状态、结果与取消跟随经身份核对的执行链；签名事件 cursor 仍属于某个 Run，
`run_id` 与 first/previous/next 字段支持跨 Run 导航。相邻 History 已被清理时显式标注
导航不完整；不拼造任务级统一事件序号。事件是 History metadata，不是 token 重放。
API 副本共享 namespace、队列、token 映射和 cursor secret；取消响应只确认请求。

当前不增加业务 SQL admission/outbox/quotas 层；Temporal 是独立服务，负责 History
持久性。Temporal 端点只应向受信任 API/Worker 开放，HTTP Bearer 不等于 Temporal
服务 ACL。API/Worker 的进程内限额不等于全局配额。原型与这条 Task 服务分开：下节
实验仍使用离线模型和自己的 SQLite memory/effect ledger。新 Task HTTP 路径的故障
实测结果见专门说明；生产 HA、外部模型推理与容量未经源码存在本身证明。

## 可选 Temporal 故障原型

[Temporal 原型说明](TEMPORAL_PROTOTYPE.md) 提供独立的实验路径，需显式安装
`temporal` extra；固定 SDK 1.34.0，不在标准 Agent Server graph、认证或 Compose
profile 中启用，也不接入原 Python/Go runtime。

真实 Deep Agents loop 在 Temporal Workflow 中执行，DeepAgentsPlugin 将模型 I/O
放进 Activity；当前只配置无网络的 TranscriptModel fixture。Memory put/get 是命名
Activities，访问本机共享的应用 SQLite；CLI dev server 的另一份 SQLite 保存 Temporal
History。Workflow 的虚拟文件、计划和消息通过显式完整 snapshot 跨 Continue-As-New。
这是本地故障实验，不是生产 Task API、Temporal ACL 或 HA 部署。原型验收结果和
运行命令由专门说明列出；高层 Task control plane 仍是设计与库合同。

## 验证

```sh
uv run pytest -q
uv run python scripts/smoke.py
uv run python scripts/fault_smoke.py
```

Smoke 启动实际 `langgraph dev` 和真实 Deep Agents graph，使用仅位于 `tests/`
的无网络模型，不需要 provider key。它检查 token/owner、真实文件工具、跨线程 memory、
伪造身份、跨用户读写、Store API、HTTP SSE、禁用执行和优雅重启恢复，并监测进程树 RSS。
它在私有临时目录保存开发状态并在退出时清理，仅在仓库 `var/deepagents-delivery/`
留下日志；生产配置不会加载测试模型。模型 API 和持久 PG 部署需在对应环境另行验证。

`scripts/fault_smoke.py` 是单独的开发模式故障检查：覆盖并发与同 Thread 排队、
客户端等待超时、终态错误后显式续跑、取消后续跑，以及缓存已刷盘后 SIGKILL 开发
进程再用兼容 graph 代码重启。它仍使用本地快照，不能证明 PostgreSQL/Redis 的
生产恢复；执行结果与尚待生产验收项见 [长任务运行约束](LONG_TASKS.md)。

版本基线：`deepagents==0.7.25`、`langgraph-api==0.16.0`、
`langgraph-cli==0.4.35`、`langgraph-sdk==0.4.7`、`langchain-openai==1.7.2`。
完整结果及架构图说明见 [交付记录](DELIVERY.md)；本轮长任务验收边界见
[长任务运行约束](LONG_TASKS.md)。
