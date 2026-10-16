# Deep Agents Server delivery — 2026-10-16

本轮在实现 `6328007`、架构说明 `9ba2a49` 的基础上，补充异步长任务客户端、
API/Worker 分离部署、stateful Run 同步 Checkpoint 策略和可测试的持久任务库合同。
原 Python/Go runtime 保持原入口；本模块继续使用标准 Agent Server 调度。
**生产 PostgreSQL/Redis 崩溃与部署恢复尚未验收，不能据此宣布生产恢复保证。**

运行合同见 [LONG_TASKS.md](LONG_TASKS.md)，下一阶段控制面设计见
[持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)。

## 实现与验收依据

| 要求 | 当前实现与直接证据 | 边界 |
| --- | --- | --- |
| 独立 Deep Agents server | `graph.py`、`langgraph.json`、独立 `pyproject.toml` / `uv.lock` | 不调用原 mini-loop runtime |
| 异步提交与任务观察 | `jobs.py` / `scripts/jobs.py`；返回可保存的 JobHandle，支持 status/wait/state/cancel/resume | wait 超时不取消；state 是当前 Thread，不是旧 Run 的固定快照 |
| 多任务并发 | 提交默认独立 Thread；同 Thread `enqueue` 串行；Worker 默认两个槽 | 未做生产容量或多副本负载验收 |
| 创建结果未知 | 写请求不自动重试；`SubmissionUncertain` 区分 create_thread/submit/resume/cancel 的核对步骤 | request metadata 不是原子去重键；不存在应用级 admission/outbox |
| 显式续跑 | 校验终态、最新 Run、未完成任务和 graph interrupt 后，`input=None` 创建新 Run | 单一操作者协调；预检查存在 TOCTOU，不能当作跨进程锁 |
| 同步 Checkpoint | 客户端请求 sync；认证 create_run hook 强制 sync/checkpoint_during | 恢复约定针对保留的 stateful Run；stateless/delete 不承诺保留 |
| 身份隔离 | Bearer→owner；Thread/Run owner filter；Store namespace 首段为 owner；Cron 拒绝 | context/config 不能授予身份；memory 不是凭据仓库 |
| 部署分离 | 同一不可变镜像的 API=0 槽、Worker=2 槽；PG 持久卷、Redis 临时协调；资源和停机窗口显式配置 | 需要适用许可证，实际生产栈未运行 |
| 持久任务抽象 | `task_contracts.py` 的不可变类型、状态迁移、admission/fencing 纯函数、adapter Protocol | 仅库合同，没有实际队列、数据库、outbox、lease 或 Task API |
| 架构同步 | canonical Mermaid、边界与 baseline、Archify JSON/generated HTML、分层图和 prompt | 图不能替代生产恢复实测 |

版本保持 Deep Agents 0.7.25、Agent Server 0.16.0、CLI 0.4.35、SDK 0.4.7、
OpenAI adapter 1.7.2；Python 3.12.10。没有新增依赖，传递依赖与包 hashes 保持在 `uv.lock`。

## 本轮验证记录

| Gate | 已读取结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest | **218 passed in 1.77s** | wrapper 4.09 秒，进程树采样峰值 144.6 MiB，最低 memory-free 48%；含 graph、auth、jobs、部署结构和 task contracts |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，pytest 98.86 秒 | wrapper 100.03 秒；采样峰值 323.1 MiB；最低 memory-free 38%；不包含新模块 |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅既有 Python package，不验证新模块的持久执行 |
| 长任务 HTTP fault smoke | **6 组通过**，22.86 秒 | run_id `18414d1d-7444-4524-a52c-f587c36d9832`；峰值 177.2 MiB，memory-free 47%→46%；`production_acceptance=false` |
| Archify validate + deliver | **9/9 showcase**，0 errors / 0 warnings | 生成 HTML 的变更泳道已静态渲染并目检深浅主题；未运行浏览器交互验收 |
| Research Atlas | content 索引 21 篇；build 与 5/5 HTML tests 通过 | 恢复现有 lockfile 的 463 个依赖；package/lock hashes 不变；最终测试阶段峰值 530 MiB |
| Wheel / Compose | offline wheel 构建、包内容核对与 Compose config 检查通过 | 包含 jobs/task_contracts/instructions；不含测试 graph/.env；配置检查仅使用临时合成凭据 |
| Markdown / whitespace | README/模块 Markdown outline、`git diff --check` 通过 | 最终暂存内容由主交付流程复核 |

模块与根 pytest 通过 `nice -n 10` 串行运行。重测试配置与采样结果保存在
`var/deepagents-delivery/current-longtasks-*.log`、`*.summary.json` 和
`*.resources.jsonl`；没有资源超预算中止。fault smoke 也监测服务进程树 RSS，结果
以本次 receipt 的 `run_id`、终态和对应日志为准。修复后的脚本在开始前原子替换旧
receipt，running/failed/resource_blocked 均不冒充 passed。

本轮没有修改 Go 实现、source scanner 或 guarded mutation anchors，不运行 Go、
scan 或 mutation gate。认证参数策略由新模块测试和真实 HTTP 故障检查单独覆盖，
原仓库的 invariants 不能替代这些检查。

## 初始模块的历史验证

以下结果属于 `6328007` 的初始交付，保留其原范围，不当作本轮代码的新验收：

- 模块 **82 passed**，1.61 秒；production factory 的 ChatOpenAI 构造/schema 检查不发送 provider 请求。
- `scripts/smoke.py`：10 组实际 `langgraph dev` HTTP 检查，使用 `tests/` 的无网络模型；覆盖文件、general-purpose 子代理、owner 隔离、Store、SSE、取消和优雅开发重启。服务树峰值 175.2 MiB，预算 1 GiB。
- 原根 pytest：2256 passed / 28 skipped / 104 warnings，138.98 秒；原 invariants：77 modules。
- `langgraph validate`、`uv lock --check`、`uv sync --locked`、`uv build --wheel` 通过；初始 wheel 包含 `instructions.md`，不包含 `.env` 或测试 graph。本轮新增 package 模块另经上表 wheel 检查。
- 原 Compose 使用临时合成凭据运行结构配置检查；这不证明当前 API/Worker 拆分后的生产服务启动。

原始日志与资源采样位于忽略目录 `var/deepagents-delivery/`，不随代码发布用户凭据。

## 架构产物

- 规范：[README Mermaid](../README.md#architecture)；模块展开：[分层说明](ARCHITECTURE.md)。
- Archify source：[architecture JSON](../docs/mini-loop-system.architecture.json)；generated [interactive HTML](../docs/mini-loop-system.architecture.html)。
  Spec SHA-256 `f91fb2ee07b6fed13b152ef9c1eeac971807062cefd134bbedfeb64d7680bbb8`（176560 bytes）；
  HTML SHA-256 `3fc68eac12e25d52bf8ea2034e23c3ba1c849857eb26438c56549396887d5a89`（850684 bytes）。
- 完整 base：[PNG](../docs/mini-loop-layered-architecture.png) 与 [exact prompt](../docs/mini-loop-layered-architecture.prompt.txt)。
- 初始模块历史标注（6328007）：[PNG](../docs/audits/deepagents-server-2026-10-16.png) 与 [exact prompt](../docs/audits/deepagents-server-2026-10-16.prompt.txt)。
- 本轮模块聚焦与变更标注图：[PNG](docs/agent-server-layered-architecture.png) 与 [exact prompt](docs/agent-server-layered-architecture.prompt.txt)。

本轮 built-in imagegen 更新完整 base，再由该 base 编辑模块聚焦图，橙色标注异步
handle、API/Worker 配置与故障测试。已保存精确 prompt 并目检五层职责、模型连线、
文件与 memory 作用域，以及开发通过/生产未验收标签。Archify HTML 由冻结的 JSON 生成，没有手改 HTML。

## 未执行和能力边界

没有真实模型/平台凭据，本轮**未执行真实 OpenAI 推理、生产 Docker image 构建与
带许可证的 PostgreSQL/Redis 服务启动、Worker 强杀接管、API/Worker 重部署、Redis
重启或断电恢复验收**。生产验收还必须确认 PG 权威状态、Run identity、未完成步骤
接管、用户隔离与兼容 graph 升级；开发快照的 SIGKILL/重启不能替代它。

没有实际 TaskControlPlane、事务 admission、outbox、fenced checkpoint/tool 写入或
持久 Task 事件接口；`task_contracts.py` 只定义和验证未来合同。外部副作用不保证
exactly-once，必须采用跨 attempt 稳定的业务幂等键。SSE 不是持久 token 重放日志。

不实现 MDA Compiler、Context Hub、托管 Sandbox、OAuth 或 Channels；Cron 拒绝。
没有宿主 shell/local filesystem backend；`execute` 名称可能在 schema 中出现，执行
仍由 backend 拒绝。Memory 同 owner 跨 Thread 共享，但没有并发编辑 CAS；不要存
secrets 或把它作为业务唯一事实来源。部署升级须保留 graph/schema/工具兼容性、
owner 映射和持久卷；删除数据后不承诺恢复。
