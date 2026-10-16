# 长任务：提交、并发、恢复与部署

本文描述标准 Agent Server 的后台 Run。`AsyncJobsClient` 与 CLI 负责
提交、保存标识和观察；服务端的原生调度负责执行。Compose 已拆开 API 与 Worker，
PostgreSQL 是持久任务与 Checkpoint 的来源，Redis 做临时协调。

**已存在的部署配置不等于已通过生产恢复验收。** 本地真实 HTTP 故障检查使用
`langgraph dev` 的周期性快照；生产 PostgreSQL/Redis 的 Worker 强杀、API/Worker
重新部署与 Redis 重启仍须在有适用许可证的环境验收。最新结果见
[交付记录](DELIVERY.md)。下一阶段持久任务控制面的完整设计见
[持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)。

另一条已实现的独立 Temporal 路径见 [Task Server](TASK_SERVER.md)。两条入口的代码与
验收布局见 [项目结构](docs/PROJECT_STRUCTURE.md)：标准服务的 smoke/fault 实现现位于
`tooling/acceptance/scenarios/langgraph_smoke.py`、`langgraph_fault.py`，离线 graph 位于
`tooling/acceptance/fixtures/`；原 `scripts/smoke.py`、`scripts/fault_smoke.py` 命令保持。

## 1. 提交后可以离开客户端

设置 `MINILOOP_DEEPAGENTS_CLIENT_TOKEN` 后，在模块目录运行：

```sh
uv run python scripts/jobs.py submit '分析并完成一个长任务' --output job.json
uv run python scripts/jobs.py status --job-file job.json
uv run python scripts/jobs.py wait --job-file job.json --timeout 30
uv run python scripts/jobs.py state --job-file job.json
```

提交返回的 `JobHandle` 包含版本、可信 server URL、graph/assistant ID、Thread ID、
Run ID 与 request ID，不包含 token。CLI 以 0600 权限原子保存 handle，默认拒绝覆盖；
它在保存文件前也输出完整 handle。远端已接受任务但保存文件失败时，应保留输出，
不要重新提交。handle 是任务标识，不是访问凭据；所有请求仍使用当前配置的 token，
并检查 handle 的 URL 与调用方配置的可信 URL 相同。使用其他部署时，为每条命令显式
设置相同的 `--url https://your-server.example`。

- 每次 `submit` 默认创建独立 Thread；有空闲 Worker 槽时，不同 Thread 可以并发。
- `submit --thread THREAD_ID` 复用已有 Thread，采用原生 `enqueue`；同一 Thread 的 Run 串行，不能靠增加 Worker 绕过。
- `status` 读取 handle 中那个 Run。`state` 是整个 Thread 的当前状态，可能已经包含后续 Run，不能把它当作旧 Run 的固定结果快照。
- `wait --timeout 30` 只限制本地等待。超时退出码为 2，服务端 Run 继续；客户端进程退出、关闭连接也不主动取消 Run。
- `wait` 成功观察到终态后，success 退出 0，其他终态退出 1；提交成功后应保留 handle，之后查询同一 Run。

Python 使用同一个客户端合同：

```python
import asyncio
import os
from mini_loop_deepagents.jobs import AsyncJobsClient, JobWaitTimeout

async def main():
    async with AsyncJobsClient(
        "http://127.0.0.1:2024",
        os.environ["MINILOOP_DEEPAGENTS_CLIENT_TOKEN"],
    ) as client:
        job = await client.submit("分析并完成一个长任务")
        print(job.to_json())  # 先持久保存，再开始等待。
        try:
            result = await client.wait(job, timeout=30)
            print(result["status"])
        except JobWaitTimeout:
            print("本地等待结束；稍后用同一 handle 继续查询")

asyncio.run(main())
```

## 2. 响应丢失：先核对发生在哪一步

客户端的 HTTP transport 不自动重试请求。`wait` 可以重试暂时失败的只读状态查询，
遇到明确的 4xx 则结束；创建 Thread、创建 Run、取消和续跑等写请求从不自动重发。
网络异常、408、5xx 或无法校验的写响应可能发生在服务端已经接受请求之后，统一返回
`SubmissionUncertain`；CLI 退出 3，stderr 输出 `submission_uncertain`、`operation`
与当前已知标识。核对方法随阶段变化：

| operation | 已知信息 | 下一步 |
| --- | --- | --- |
| `create_thread` | 客户端预生成的 Thread ID；此调用没有继续提交 Run | 读取该 Thread 是否已存在。只知道 Thread 创建结果，不代表任务已经提交 |
| `submit` | Thread ID 与该次 request ID；可能尚不知道 Run ID | 列出该 Thread 的 Runs，匹配 `metadata.request_id`，定位已接受 Run |
| `resume` | 原 Run ID、Thread ID，以及新 continuation 的 request ID | 查询 Runs，匹配新的 request ID。原 Run ID 只是恢复来源，不能当作新 Run ID |
| `cancel` | 已知 Thread/Run ID | 查询那个 Run 的状态，判断取消是否生效；提交时的 request metadata 不能证明取消已执行 |

`request_id` 是核对线索，不是 Agent Server 强制的幂等键。查不到匹配也不自动证明
原写请求没有到达；分页、在途提交和短暂不可见必须纳入判断。当前没有应用级
admission 唯一约束、事务 outbox 或原子 reconcile/submit 操作。出现未知结果时由单一
操作者协调核对，不能依赖无限重试获得“恰好一次”。

## 3. 取消与显式续跑

```sh
uv run python scripts/jobs.py cancel --job-file job.json
uv run python scripts/jobs.py status --job-file job.json
uv run python scripts/jobs.py resume --job-file job.json --output continued-job.json
```

取消使用原生 `action="interrupt"`、`wait=False`，请求返回不等于任务已经停止；之后
继续查状态。取消已执行的外部副作用不会被自动撤销。

`resume` 只在原 Run 为 error、timeout 或 interrupted 时继续检查：它必须仍是该 Thread
的最新 Run，Checkpoint 必须还有 `next` 未完成任务，且没有需要人工回答的 graph
interrupt。满足条件后用 `input=None`、`multitask_strategy="reject"` 新建 Run，返回
**新的 handle**，不会把原用户消息重复加入对话。需要 `Command(resume=...)` 的人工
中断不由这个通用命令代答，客户端会拒绝。

这些“读取状态 → 检查最新 Run → 创建 continuation”步骤存在 TOCTOU 窗口。原生
`reject` 能拒绝当时仍在执行的冲突，不能防止另一个 continuation 已经迅速完成后
再次续跑。当前恢复命令要求单一操作者协调，不具备跨进程的幂等 continuation admission。
若已有较新 Run，先检查它；不要为了通过检查改写 handle。

## 4. Checkpoint 与身份策略

长任务客户端显式请求 `durability="sync"`。服务端 `auth.py` 的 `create_run` policy
还会写入 `durability="sync"` 和 `checkpoint_during=True`，使经认证的 stateful
Thread Run（包括直接 REST 调用）请求同步步骤 Checkpoint，客户端不能用 `exit`
参数降低这一策略。此 hook 的参数写入由原生 runtime 消费；它不是另一个 checkpointer。

恢复约定针对保留的 stateful Thread/Run。stateless Run、delete-on-completion 或显式
删除 Thread/Run 具有另外的原生保留规则，不承诺保留可供恢复的状态。持久化也不代表
业务数据永不删除。Bearer token→owner、Thread/Run owner filter、Store owner namespace
和 Cron 拒绝策略保持一致，伪造 `context.user_id` 不改变身份。

`/plan.md` 等虚拟文件属于 Thread Checkpoint；`/memories/` 进入 owner Store，允许同
owner 跨 Thread 读取。Memory 不可信、不能存凭据；并发改同一文件没有 CAS 合并保证。
没有宿主文件系统或 shell executor，Worker isolated loops 不等于工具 Sandbox。

## 5. API、Worker 与数据底座

| 服务 | 当前配置 | 生命周期与职责 |
| --- | --- | --- |
| API | `N_JOBS_PER_WORKER=0`；512 MiB / 0.5 CPU | 认证、Run admission、状态与 SSE 接口；不承载后台执行槽 |
| Worker | 标准 queue entrypoint；默认 2 槽；1 GiB / 1 CPU | 独立进程执行 Run；`BG_JOB_ISOLATED_LOOPS=true`；同 Thread 串行 |
| PostgreSQL | 512 MiB / 0.5 CPU；命名持久卷 | pending Runs、执行认领/状态、Checkpoint、Thread 和 owner Store 的持久来源 |
| Redis | 128 MiB / 0.25 CPU；关闭快照/AOF | 临时唤醒、取消、事件协调；不作为持久任务来源 |

单 Worker 实例总内存上限 2.125 GiB、CPU 上限 2.25 核，只是小规模验证配置。增加
Worker 副本或并发槽前，需同步核对模型限流、数据库连接上限与实际内存。isolated
Worker 线程的连接池按并发槽分摊；当前 Worker 池上限默认 32，API 为 10。更大并发
并未做容量验收。

API 与 Worker 必须使用同一不可变 image ID/digest、graph 注册与兼容包。许可证与
配置说明见 [模块 README](README.md#部署配置)。默认 `BG_JOB_TIMEOUT_SECS=86400`
是每次执行 attempt 的 24 小时上限，不是 HTTP 或单次模型请求超时；
`MINILOOP_DEEPAGENTS_TIMEOUT_SECONDS` 默认 60 秒，只约束一次模型请求。
`BG_JOB_MAX_RETRIES=3` 在固定 0.16.0 中是**包括首次在内的 attempt 总数**。优雅停机
窗口 180 秒，Compose 允许 210 秒后才强杀，不代表所有长任务都必须在此窗口完成。

## 6. 故障与部署后的恢复约定

- 客户端断线或 API 重启：继续查原 Run。API 与 Worker 分离后，API 生命周期不直接终止 Worker；这仍需生产栈的重启检查证明。
- Worker 优雅停机、崩溃或部署终止：原生调度根据持久 Run 与 Checkpoint 处理符合条件的未完成执行，受 attempt 上限约束。这个生产恢复路径目前有配置与上游实现依据，尚没有本仓库生产栈验收结果。
- 模型/graph 应用异常：可能直接进入终态 error，不等同于可重试的基础设施或停机故障。检查错误原因后，再决定是否显式 null-input continuation。
- attempt 耗尽或 Run timeout：检查终态与 Checkpoint，不无限重试。Checkpoint 无未完成任务时，通用 resume 会拒绝。
- PostgreSQL 卷丢失、不兼容 schema 更新、owner 映射改变：不能承诺原任务恢复；部署前需要备份、迁移或先排空任务。

部署时先停止旧 Worker 接新工作并等待其退出，在同一版本窗口替换 API/Worker，保留
PostgreSQL 卷。不要执行 `docker compose down -v`。升级至少保持 graph/assistant ID、
Checkpoint schema、工具及指令的兼容性；开发 `.langgraph_api/` 快照不会自动迁入 PG。
本轮本地故障检查只替换兼容的 graph 代码路径，不能证明任意代码升级都可恢复。

同步 Checkpoint 只界定已经提交的步骤。一个外部请求已经产生效果、而步骤完成状态
尚未写入时，恢复仍可能重放工具。支付、发信、写外部数据库等操作必须使用跨 attempt
稳定的业务幂等键和可核对的执行结果；不能用变化的 attempt ID 替代 logical operation ID。
SSE 也不是逐 token 持久日志；客户端恢复观察应首先读 Run/Thread 的权威状态。

## 7. `task_contracts.py` 是库合同

新增 [task_contracts.py](src/mini_loop_deepagents/task_contracts.py) 定义 Task、Attempt、
ExecutionRef、能力声明、状态迁移、admission 决策、fencing 谓词、EventCursor 与
ExecutionAdapter Protocol。它为 [持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)
提供可测试的类型和纯函数边界；**没有安装实际 Task API、调度器、数据库表、事务 outbox、
lease 服务、持久事件日志或具体执行 adapter**。现有 `AsyncJobsClient` 仍直接调用
Agent Server，尚未经过该控制面。

例如 `admission_decision` 不能替代数据库唯一约束，`fence_allows_write` 不能阻止
旧 Worker 写数据库、LangGraph Checkpoint 或外部系统；未来实现必须在权威存储事务
内核对当前 epoch、Worker 和服务端时间。EventCursor 是未来 Task 的持久事件序号，
StreamSequence 是临时流序号，两者都不是当前已交付的 durable SSE replay。

## 8. 验收矩阵

| Gate | 直接证据 | 能证明的范围 |
| --- | --- | --- |
| 模块单元/graph/HTTP client 合同 | `deepagents-server/tests`；最终结果见 DELIVERY | 参数、隔离、错误分类、续跑检查、部署结构和纯函数合同；不等于生产数据库执行 |
| 原 Python 回归与 invariants | 根 pytest / 77 modules | 旧 runtime 未回归；不覆盖新模块的生产恢复 |
| 基础真实 HTTP smoke | `scripts/smoke.py` | 实际 Agent Server + Deep Agents 测试模型、授权、文件、memory、SSE 和优雅开发重启 |
| 长任务真实 HTTP 故障 smoke | `scripts/fault_smoke.py` | 独立 Thread 并发、同 Thread 排队、等待超时、取消/错误后续跑、开发缓存刷盘后 SIGKILL 与兼容 graph 重启；历史 receipt 修复后 6 组通过，具体 run_id 与资源结果见 DELIVERY |
| PostgreSQL/Redis 生产故障验收 | 尚未执行 | 必须证明 Worker 强杀接管、API/Worker 重部署、Redis 重启、PG 状态与 owner 隔离；缺有效许可证及实际栈证据 |
| 真实模型与容量 | 尚未执行 | 无真实 provider 推理、长期负载或多副本容量结果 |

故障 smoke 在开始时原子发布本次 `run_id` 和 running receipt，失败/资源阻断也覆盖
旧结果，避免留下历史 pass。必须读取本次终态与日志；运行中的 receipt 或过往 pass
都不能视作当前通过。日志和资源采样位于忽略目录 `var/deepagents-delivery/`。
