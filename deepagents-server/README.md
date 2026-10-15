# mini-loop Deep Agents Server

这是使用 **Deep Agents + 标准 LangGraph Agent Server** 的独立模块，依据
[MDA Agent Server 参考设计](../docs/MANAGED_DEEP_AGENTS_AGENT_SERVER_DESIGN.md)
的分层构建。它有独立环境、依赖锁和启动配置，不调用已有 `mini_loop`
Python/Go runtime，也不替换原服务。

## 已实现与边界

| 层 | 实现 | 状态 |
| --- | --- | --- |
| Agent harness | `create_deep_agent`，planning、文件工具、默认 general-purpose subagent | 真实 Deep Agents graph |
| Server | 标准 Threads、Runs、状态/历史、取消、同线程 Run 调度、SSE API | 使用 Agent Server，自行实现的调度器为零 |
| 身份 | 服务端配置的随机 Bearer token → owner；禁用 Studio 旁路 | 默认拒绝未认证访问 |
| 资源授权 | Thread/Run owner metadata filter；Store namespace 首段必须为 owner | 模型输入和客户端 context 不授予身份 |
| 文件 | StateBackend，线程虚拟文件，由 Checkpoint 保存 | 无宿主文件访问 |
| Memory | `/memories/` → StoreBackend `(owner, "memories")` | 同 owner 跨 Thread 可用；不跨 owner 共享 |
| Model | 服务端 `ChatOpenAI` 实例；模型、超时、输出上限来自环境 | 真实 provider 调用需要自己的 API key |
| 本地开发 | `langgraph dev`，inmem runtime + 本地快照 | 可检查优雅退出后的恢复；非生产持久性保证 |
| 部署配置 | 标准 Agent Server image + PostgreSQL + Redis | 需平台凭据/许可证，验证状态见交付记录 |

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

## 部署配置

`Dockerfile` 由固定版本 CLI 从 `langgraph.json` 生成；`compose.yaml` 配置
PostgreSQL 持久卷及 Redis 临时协调，不公开数据库端口，API 只绑定宿主 loopback。
API、PG、Redis 分别限为 1 GiB、512 MiB、128 MiB 内存；这些是开发验证/小型部署的起点，
并非已测量的生产容量。没有另行拆分 API/Worker 池。

标准 Agent Server 及其自定义认证具有独立的许可条件。必须自行配置有效的
`LANGSMITH_API_KEY`、`LANGGRAPH_CLOUD_LICENSE_KEY` 和 `POSTGRES_PASSWORD`；
数据库密码请用 `secrets.token_urlsafe(32)` 生成（Compose 直接将它放入 URI，
只使用 URL-safe 字符，不使用 `@`、`#`、`?`、`%` 等需要编码的字符）。
Compose 缺少它们时会在启动前失败。检查适用的
[Standalone 部署要求](https://docs.langchain.com/langsmith/deploy-standalone-server)
和[自定义认证许可说明](https://docs.langchain.com/langsmith/custom-auth)。
本仓库不提供这些凭据，不通过关闭认证绕过许可。

```sh
uv run langgraph dockerfile Dockerfile --api-version 0.16.0
docker compose config --quiet
docker compose up --build -d
docker compose logs -f api
docker compose down
```

`docker compose down` 保留 PG 卷；`down -v` 会删除持久数据。开发快照不会自动迁入 PG。
基础镜像及 PG/Redis 镜像已固定 registry digest；重新生成 Dockerfile 后需重新固定
并验证 digest。生产镜像受平台基础镜像 constraints 管理，本地安装使用 `uv.lock`；镜像构建与许可证
验证应在实际部署环境单独验收。PostgreSQL 是 Run/Thread/Checkpoint/Store 的持久来源，
Redis 用于唤醒、取消、实时 PubSub。SSE 不能因此获得逐 token 持久重放保证。
恢复不保证外部副作用 exactly-once；未来增加外部工具时必须设计幂等/审批和执行隔离。

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

## 验证

```sh
uv run pytest -q
uv run python scripts/smoke.py
```

Smoke 启动实际 `langgraph dev` 和真实 Deep Agents graph，使用仅位于 `tests/`
的无网络模型，不需要 provider key。它检查 token/owner、真实文件工具、跨线程 memory、
伪造身份、跨用户读写、Store API、HTTP SSE、禁用执行和优雅重启恢复，并监测进程树 RSS。
它在私有临时目录保存开发状态并在退出时清理，仅在仓库 `var/deepagents-delivery/`
留下日志；生产配置不会加载测试模型。模型 API 和持久 PG 部署需在对应环境另行验证。

版本基线：`deepagents==0.7.25`、`langgraph-api==0.16.0`、
`langgraph-cli==0.4.35`、`langgraph-sdk==0.4.7`、`langchain-openai==1.7.2`。
完整结果及架构图说明见 [交付记录](DELIVERY.md)。
