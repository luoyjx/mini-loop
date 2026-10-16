# Deep Agents Server delivery — 2026-10-16

当前迭代以已推送的 `9af554a` 为基线，为独立 Task HTTP 服务增加 **task-v2 分段续跑**。
新任务在安全的父模型边界每 8 次执行 Continue-As-New，把完整 checkpoint 写入日志
传给下一个 Run；TaskID、owner、初始执行链和原生 24 小时总期限保持不变。
真实 Deep Agents 的文件、计划、子代理及摘要状态一起恢复。Worker 同时注册不可变的
v1 与新 V2，新 API 能复用既有 v1 幂等键。本轮没有新增依赖。
详细合同见 [TASK_SERVER.md](TASK_SERVER.md)，故障摘要见
[task-can-evidence.json](docs/task-can-evidence.json)。

## Task V2 Continue-As-New 迭代验证

本轮新旧故障验收使用固定 Temporal CLI 1.9.1 / Server 1.32.0、SDK 1.34.0、
Deep Agents 0.7.25。生产 Workflow、原生工具和子代理真实运行；模型 Activity 使用
确定性离线 fixture，因此不将本轮结果当作真实 provider 或生产 HA 验收。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **373 passed in 3.45s** | wrapper 6.16 秒；采样峰值 176.5 MiB；最低 memory-free 51% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，106.74 秒 | wrapper 108.47 秒；采样峰值 331.1 MiB；最低 memory-free 45% |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅覆盖原 Python package，不替代 Task V2 验收 |
| V2 真故障验收 | **4 组通过**，4 次 Continue-As-New / 5 个 Run | 45.86 秒；797.3 MiB 峰值；最低 memory-free 37%；87 次采样；19 个进程退出 |
| 原生工具 / 子代理 / 摘要恢复 | 37 次父工具步骤、2 个真实 general-purpose 子代理、2 次跨 Run 摘要 | 子代理 Activity 中、分段前、分段后及第三次 CAN 后整组强杀；快照与重放模型请求逐字节相符；5 个 Run 的 WorkflowTaskFailed 均为 0 |
| 原 8 组 HTTP 故障与 v1 兼容 | **8 组通过** | 22.18 秒；827.9 MiB 峰值；最低 memory-free 40%；17 个进程退出；默认 V2 冷重放完整状态一致 |
| 已有 v1 合同 | 当前 API 复用 v1；冲突 409 / 跨 owner 404；原 128 步失败终态 | 43 个模型 Activity 完成；task_step_limit non-retryable；WorkflowTaskFailed=0；不修改 v1 Workflow 源码 |
| Lock / offline wheel | `uv lock --check --offline`、离线构建与内容审计通过 | 105 packages；21 个包文件逐字节匹配（20 个 Python 模块含 8 个 Task 模块，以及 instructions.md）；README metadata 一致；package/lock 不变 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | 冻结 JSON 生成 HTML；CAN 回流标签修正后明暗主题静态目检，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build 与 HTML **5/5** 通过 | wrapper 10.23 秒；采样峰值 889.5 MiB；最低 memory-free 50%；只更新目标设计报告的生成数据 |
| Markdown / whitespace | 168 个本地引用、fence、README outline 与 `git diff --check` 通过 | 6 份文档；21 个交付源码摘要匹配；v1 runtime 与 9af554a 逐字节一致 |

主验收 run_id 为 `caffa4578ab348a3bc7198eb430bcbfb` 与
`d252d1bc60c9470b84747c5dcbda28bc`。原始快照、History、日志与进程采样留在忽略目录
`var/task-can-prototype/`、`var/task-server-prototype/`；发布证据保留内容摘要和源码
SHA-256，不发布完整任务内容或凭据。所有重验证串行并降低优先级，普通 gate 预算
1.5 GiB / 300 秒 / memory-free 15%，故障脚本预算 2 GiB / 180 秒。
没有资源超限中止；短 lock/wheel/invariants gate 的启动采样不能当作真实峰值。
完整门禁日志索引在 `var/deepagents-delivery/task-can-final-validation.receipt.json`。
未改 Go、source scanner 或原有 guarded mutation anchors，未运行相应 gate。
证据中的源码摘要是两次验收后的冻结清单；旧 HTTP harness 在两次运行之间调整过
v1 兼容场景，不能倒推每个辅助脚本在两个运行时都与当前版本相同。生产源码保持一致。

## Task HTTP 历史验证（9af554a）

该轮以 `1fd010d` 为基线，新增独立可选的 **Task HTTP 服务**：
无状态 API 确认 Temporal Start 后返回 202，独立 Worker 执行真实 Deep Agents，
保留的 Temporal History 是任务状态、恢复和结果的唯一权威。新增路径不调用原
Python/Go 业务，不经过标准 Agent Server API，也不引入第二套业务数据库或 outbox。
命令、HTTP 合同与边界见 [TASK_SERVER.md](TASK_SERVER.md)。本轮没有新增依赖。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **323 passed in 7.13s** | wrapper 10.24 秒；采样峰值 175.8 MiB；最低 memory-free 49% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，196.12 秒 | wrapper 197.91 秒；采样峰值 321.2 MiB；最低 memory-free 44% |
| 原 Python invariants | **77 modules checked**，退出 0 | 只覆盖既有 Python package，不代替新服务验收 |
| Task HTTP 真故障验收 | **8 组通过**，32.45 秒 | 双 API、双 Worker；峰值 780.0 MiB；最低 memory-free 44%；61 次采样；16 个进程均退出 |
| 步数上限失败终态 | 真实循环完成 **43 个模型 Activity**，128 步耗尽返回 `failed` / `ready=true` | 原生 `task_step_limit` non-retryable failure；`WorkflowTaskFailed=0`，没有以 Workflow Task 重试掩盖任务失败 |
| 定向 runtime / harness | **20 passed in 2.39s** | 含导入和监控 19.81 秒；峰值 202.9 MiB；最低 memory-free 49% |
| Lock / offline wheel | `uv lock --check --offline`、离线构建与内容审计通过 | 105 packages；18 个包文件逐字节匹配（17 个 Python 模块含新增 5 个 Task 模块，以及 instructions.md）；不含测试、.env 或运行数据 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | 冻结 JSON 生成 HTML；新增泳道深浅主题静态目检，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build 与 HTML **5/5** 通过 | wrapper 8.14 秒；峰值 697.9 MiB；最低 memory-free 45%；生成 JSON 由命令更新 |
| Markdown / whitespace | **160 个本地引用**、fence、README outline 与 `git diff --check` 通过 | 6 份文档检查；12 个受测源码摘要匹配；Research 仅改变目标报告 |

直接故障证据：[task-server-evidence.json](docs/task-server-evidence.json)，运行
`792fc91351654e1ba8ff5f3b7fa35c32`。它验证跨 API 的幂等竞争与 owner 隔离、生产者退出后
双任务并发、API 全部强杀重启后的身份及事件游标、Temporal 与全部 Worker 强杀后的
冷重放、单任务取消、另一任务完整 messages/files/todos 恢复与终态复用，以及真实
步骤耗尽失败。首轮 `2fdf96d9fce9426da998d8aa1b3a6c30` 在步骤上限修复之前通过七组，
仅保留为历史记录，不作为最终代码的主验收。

所有重验证串行、`nice -n 10` 运行；普通 gate 预算 1.5 GiB / 300 秒 / memory-free
15%，故障脚本 2 GiB / 180 秒。无资源超限中止。完整收据、日志和快照留在忽略的
`var/task-server-prototype/`、`var/deepagents-delivery/task-http-final-*`；发布摘要含来源
SHA-256，不含 token、密钥或完整任务内容。未改 Go、source scanner 或原有 guarded
mutation anchors，未运行相应 gate。真实 provider、生产集群 HA 与容量仍未验收。

## Temporal 原型历史验证（1fd010d）

以下是上一轮可选故障原型的验收，范围见 [TEMPORAL_PROTOTYPE.md](TEMPORAL_PROTOTYPE.md)。
其 Memory 和固定阶段 Continue-As-New 属于原型；当前 Task V2 的完整 checkpoint
分段实现及验收另见本页顶部，不把原型证据替代当前入口的验收。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **238 passed in 1.78s** | wrapper 4.08 秒；采样峰值 171.7 MiB；最低 memory-free 46% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，101.87 秒 | wrapper 104.12 秒；采样峰值 287.5 MiB；最低 memory-free 39% |
| 原 Python invariants | **77 modules checked**，退出 0 | 只覆盖原 Python package，不把该结果当作 Temporal 验收 |
| Temporal 真故障验收 | 并发、Worker 强杀接管、服务强杀重启、全部旧 Worker 丢弃、完整状态迁移与副作用去重通过 | 唯一 run_id、实际 History 与清理证据见 [原型记录](TEMPORAL_PROTOTYPE.md#验收与资源限制)；`production_ha_acceptance=false` |
| 最新 harness 单元检查 | **9 passed** | wrapper 6.12 秒，峰值 67.2 MiB；最终状态断言单独复核 |
| Lock / offline wheel | `uv lock --check --offline` 与离线构建、内容审计通过 | 105 packages；12 个 Python 模块与源码逐字节匹配，含 6 个 Temporal 模块；保留 instructions.md，不含测试、日志、数据库或 .env |
| Archify | **9/9 showcase**，0 errors / 0 warnings | JSON 生成 HTML；新增泳道已静态渲染并目检深浅主题，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build、HTML **5 tests passed** | bundled Node 24.19.0；wrapper 10.23 秒；采样峰值 467.8 MiB |
| Markdown / whitespace | 本地链接与 fence 检查、`git diff --check` 通过 | README outline 与新增/修改文档核对，最终暂存另复核 |

验证串行运行，使用 `nice -n 10`；普通 gate 的进程树预算为 1.5 GiB、300 秒、
memory-free 15%，真实故障脚本为 2 GiB、180 秒。无资源超限中止。短 gate 只有启动
采样，不能据此推算实际峰值。日志与逐次采样索引保存在
`var/deepagents-delivery/temporal-final-validation.receipt.json`。
本轮只新增已获批准的可选 Temporal SDK 及其锁定传递依赖，默认依赖与现有包版本保持。
没有改 Go、scanner 或既有 guarded mutation anchors，未运行对应 gate。

## 上一轮交付基线

下列长任务客户端交付记录属于 **`e67e174` 的上一轮基线**；其中测试数字与依赖描述
保留当时范围，不作为新增 Temporal 代码的测试结果。

上一轮在实现 `6328007`、架构说明 `9ba2a49` 的基础上，补充异步长任务客户端、
API/Worker 分离部署、stateful Run 同步 Checkpoint 策略和可测试的持久任务库合同。
原 Python/Go runtime 保持原入口；本模块继续使用标准 Agent Server 调度。
**生产 PostgreSQL/Redis 崩溃与部署恢复尚未验收，不能据此宣布生产恢复保证。**

运行合同见 [LONG_TASKS.md](LONG_TASKS.md)，下一阶段控制面设计见
[持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)。

## 标准入口历史实现与验收依据（e67e174）

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

## 上一轮验证记录（e67e174）

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
  当前 V2 迭代 Spec SHA-256 `e38eed9658d2cfdff97915804a3a0bca0b0be8cf8754c9f2364c8cecfef49cd0`（186846 bytes）；
  HTML SHA-256 `31d6488ebe37fb8f036cc17256c8ae31dff4aea202f5ab009a6c6ff86097e798`（882432 bytes）。
- 完整 base：[PNG](../docs/mini-loop-layered-architecture.png) 与 [exact prompt](../docs/mini-loop-layered-architecture.prompt.txt)。
- 初始模块历史标注（6328007）：[PNG](../docs/audits/deepagents-server-2026-10-16.png) 与 [exact prompt](../docs/audits/deepagents-server-2026-10-16.prompt.txt)。
- 标准模块聚焦图（e67e174）：[PNG](docs/agent-server-layered-architecture.png) 与 [exact prompt](docs/agent-server-layered-architecture.prompt.txt)。
- 上一轮 Temporal 泳道标注：[PNG](../docs/audits/temporal-prototype-2026-10-16.png) 与 [exact prompt](../docs/audits/temporal-prototype-2026-10-16.prompt.txt)。
- 上一轮 Task HTTP 图（9af554a）：[PNG](../docs/audits/task-server-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-2026-10-16.prompt.txt)。
  修正提示词：[接口路径与身份范围](../docs/audits/task-server-2026-10-16.repair.prompt.txt)、[移除歧义回流箭头](../docs/audits/task-server-2026-10-16.arrow-repair.prompt.txt)。
- 本轮 V2 独立 Agent Server 图：[PNG](../docs/audits/task-server-can-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-can-2026-10-16.prompt.txt)。
  [修图提示词](../docs/audits/task-server-can-2026-10-16.repair.prompt.txt) 校正初稿的接口示例、无状态 API 图标、父模型计数措辞与 CAN 回流线。

本轮 built-in imagegen 编辑完整 base 的 Task HTTP 泳道，保留原五层、标准模块和
旧 Temporal 原型；再以更新后的 base 生成聚焦 Task 服务的独立图。橙色专门标注
V2 的分段续跑、完整 checkpoint、确定性摘要、旧 v1 key 兼容与跨 Run 观察。
两份精确 prompt 随图保存。Archify HTML 由冻结 JSON 生成；CAN 回流标签经过一次
位置修正，深浅主题全图与新泳道静态目检通过。未运行浏览器交互验收，本地 SVG
预览使用字体 fallback；PNG 由 built-in imagegen 生成。
最终 PNG 已逐项目检五层职责、无状态 API、Worker 到 Temporal 的 CAN 方向、橙色
修改标注与能力边界。源码和架构产物 SHA-256 一并记录在本轮发布证据中。

## 未执行和能力边界

本轮**未执行真实 OpenAI 推理、生产 Docker image 构建、Temporal 生产集群 HA / 容量、
物理断电、任意兼容代码升级或带许可证的标准 Agent Server PostgreSQL/Redis 恢复验收**。
Task HTTP 的本地 SIGKILL/进程替换已验证；该证据不能扩展为生产数据库故障转移或跨
版本升级保证。保留 v1 原实现及其已有任务合同已单独验证；这不等于任意版本升级可重放。
V2 全链上限为 24 小时、512 次父模型边界；每次父图调用另有 128 步限制。原生子代理
有自己的递归上限，不能把 512 或 128 宣称为包含全部子代理和摘要模型调用的总量。
完整 saver 写入日志有 16 MiB 原始、384 KiB 压缩、512 KiB CAN 信封上限；超出后进入
不可重试失败。日志不做压缩后的语义裁剪，状态较大的任务可能提前触顶，尚无无限长执行保证。

Task HTTP 现在具备直接委托 Temporal 的任务入口及持久生命周期事件接口，但没有
独立业务 SQL TaskControlPlane、事务 admission/outbox、租户/集群全局配额或任意外部
工具的 fenced 写入；`task_contracts.py` 中这些合同仍是未来设计。幂等与事件受 History
保留期约束；外部副作用不保证 exactly-once。事件分页不是持久 token 重放日志。
Task HTTP 路径没有跨任务 Memory；V2 已启用上述受控 Continue-As-New。确定性摘要
适配固定 Deep Agents 版本的内部接点；依赖升级必须重做原生子代理与摘要的冷重放验收。
原生 Temporal RPC
必须留在受信任 API/Worker 边界内，HTTP Bearer 不是 Temporal 原生访问控制。

不实现 MDA Compiler、Context Hub、托管 Sandbox、OAuth 或 Channels；Cron 拒绝。
没有宿主 shell/local filesystem backend；`execute` 名称可能在 schema 中出现，执行
仍由 backend 拒绝。标准入口的 Memory 同 owner 跨 Thread 共享，但没有并发编辑 CAS；不要存
secrets 或把它作为业务唯一事实来源。部署升级须保留 graph/schema/工具兼容性、
owner 映射和持久卷；删除数据后不承诺恢复。
