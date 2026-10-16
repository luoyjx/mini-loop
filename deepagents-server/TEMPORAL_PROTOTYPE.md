# Temporal 长任务故障原型

这是与标准 Agent Server 分开启动的可选实验。真实 Deep Agents 循环在 Temporal
Workflow 中执行；模型与 Memory I/O 通过 Activities 调度。模型是离线、无状态的
`TranscriptModel`，按已有消息决定下一步，避免 Worker 重启使本地计数器归零。
本原型用于验证恢复机制，生产 Task 控制面仍按
[持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md) 单独实施。

用户已批准 `temporalio==1.34.0` 与本地临时 Temporal 服务。依赖作为 `temporal`
extra 固定；标准 Agent Server profile 和原 Python/Go 模块不自动接入此路径。

## 运行边界与数据归属

| 组件 | 当前实现 | 数据归属 |
| --- | --- | --- |
| PrototypeClient | 受控本地 Python 调用，固定 owner；稳定 Workflow ID；保留期内拒绝重复执行 ID | 本机应用 SQLite 保存不可变 Workflow→owner 及请求摘要绑定 |
| Temporal 服务 | CLI 1.9.1 / OSS Server 1.32.0，本机 SQLite dev server | Workflow History、活动重试、执行身份、Continue-as-New 链 |
| Worker | SDK 1.34.0 实验 Deep Agents 插件；独立进程、显式小并发 | Workflow cache 设为 0，冷重放不能依赖旧进程内存 |
| Agent 循环 | `create_temporal_deep_agent`，真实 planning、虚拟文件与工具节点 | Workflow 内 InMemorySaver 通过 History 重建；不在 Workflow 中访问外部 checkpointer |
| 模型 Activity | 根据 transcript 生成确定性的模型响应 | 不调用真实 provider，不评估模型质量或 provider 限流 |
| Memory Activities | 按可信 Workflow 绑定定位 owner；命名 Activity，无动态 backend 对象引用 | 单独的应用 SQLite：memory、操作去重回执、故障 gate 和测试观察记录 |

Temporal History 数据库与应用数据库是两个独立文件。后者的 SQLite 路径由 Worker
启动参数配置，不由模型或 Workflow payload 选择；共享本机文件验证跨进程定位，
并不能证明多机共享存储或数据库高可用。`PrototypeClient` 是可信进程内入口；
直接连接本地 Temporal 的调用方并不受此类的 owner 检查约束，它不是生产认证服务。

`REJECT_DUPLICATE` 的已关闭执行去重受 Temporal Namespace retention 限制；本地绑定
不等于永久 admission 台账。SDK 可重试同一个 Start RPC/request ID，wrapper 不创建
新的业务重提。最终响应不确定时，应按已有 Workflow ID 核对结果。

## 固定版本与启动

从本模块目录执行：

```sh
uv sync --locked --extra temporal
.venv/bin/python scripts/temporal_fault_smoke.py \
  --temporal-binary /absolute/path/to/temporal \
  --binary-sha256 EXPECTED_EXTRACTED_BINARY_SHA256
```

脚本要求已经存在的 CLI 可执行文件，不会自动下载，也不会读取用户已有 Temporal
配置。测试服务只监听 loopback；每次运行使用独立目录、端口、队列和 Workflow ID。
默认输出位于仓库忽略目录 `var/temporal-prototype/<run_id>/`，保留 receipt、日志和
两个 SQLite 数据库供审查，不发布其中的数据。

本机 macOS ARM64 使用 [Temporal CLI v1.9.1 官方 release](https://github.com/temporalio/cli/releases/tag/v1.9.1)：

- Archive：`temporal_cli_1.9.1_darwin_arm64.tar.gz`，43,195,714 bytes。
- Archive SHA-256：`41e0425378fcb4fb5766340b97435e20fe47bbff2d7bf644ec2d51f7662b7c56`。
- 提取后 executable SHA-256：`33c298d41f6eefebf01a28a3511f444480e939e21d3518cb0c12a4abad467c4b`。
- `temporal --version` 已核对 CLI 1.9.1、Server 1.32.0。其他平台需要其对应 artifact 和校验值。

手动启动 Worker 的入口为：

```sh
.venv/bin/python -m mini_loop_deepagents.temporal_prototype.worker \
  --address 127.0.0.1:7233 --task-queue local-prototype \
  --database /absolute/path/to/application.sqlite --identity worker-a
```

仅在已启动的隔离本地服务上使用该命令。完整故障脚本自行管理服务、Worker、数据库
和任务绑定，适合复现下面的验收场景。

## 三项实验集成风险如何处理

1. **Checkpoint 所有权。**Agent loop 在 Workflow 中，模型/Memory 操作各自形成
   Activity History。外部数据库 I/O 不放进 Workflow，也不注入标准 Agent Server 的
   PostgreSQL checkpointer；不把整个 Agent 包成一个没有中间恢复点的大 Activity。
2. **可迁移资源。**Memory 工具只调度显式命名的 Activities，每个 Worker 注册相同
   函数，Activity 从持久绑定得到 owner。没有把进程内 backend ref 传给另一 Worker。
   当前 memory 是专用工具，尚未提供标准 profile 的 `/memories/*` 挂载语义。
3. **完整状态迁移。**显式 Continue-as-New 携带 messages、files、todos、owner/Workflow
   identity、schema/code version 和阶段。它替代官方 helper 只携带部分状态的默认路径。
   这里只覆盖受控阶段边界，尚无审批、signal handler 或任意图状态迁移协议。

固定版本的 fixture model 不会默认获得 `write_todos`，因此 factory 显式安装公开的
`TodoListMiddleware`，使用它原生的异步工具。`todos` 被标为 `OmitFromInput`，
直接把快照放进下一次 `ainvoke` 会丢失待办状态；恢复器先通过固定图的 `model`
writer 写回完整 state，再用公开的 `END` 更新清掉已完成 turn 的待运行路由，
随后输入下一阶段消息。这是固定图结构的适配，不是任意 graph 的迁移器。

`DeterministicStateBackend` 基于固定 Deep Agents 0.7.25 的私有 backend seam，
write/edit/upload 使用 Workflow 时间生成文件元数据，保留原有读取和替换语义。
这是需要跟随上游版本复查的实验适配，不能据此承诺未来版本兼容。

原生 ToolMessage 未提供 ID 时，公开 `AgentMiddleware` 在 reducer 之前按
Workflow ID、tool_call_id 和消息序号补稳定 ID；已有 ID 与 Command 的其他字段保持。
不在最终快照比较时删除或改写字段。SQLite Activities 在事务成功或失败后显式关闭
连接，避免故障 gate 的轮询将连接生命周期交给垃圾回收。

Memory 副作用先在一个 SQLite 事务内写入数据和稳定的逻辑操作回执，再进入故障 gate。
同 `(workflow_id, operation_id)`、同内容重试复用结果；相同 key 不同内容拒绝。
这个保证只覆盖该数据库内的受保护操作，不能推出任意外部 HTTP 工具只执行一次。

## 验收与资源限制

2026-10-16 真实故障验收 **6 组通过**，run_id
`a21cf53c8ef04a54911debb5e0e4e277`。耗时 **16.10 秒**，owned 进程树采样峰值
**663.9 MiB**，最低 memory-free **41%**，30 次资源采样；7 个注册进程均已退出。
以下条件已由本次服务与 Worker 运行观察，不是由脚本存在推定通过：

| 场景 | 必须观察 |
| --- | --- |
| 两个长任务并发 | 两个独立 Worker PID 已生成计划/文件/消息并在副作用确认前阻塞 |
| 副作用提交后 Worker 强杀 | 新 Worker 接管同一 Activity 的后续 attempt；逻辑操作与效果行不重复 |
| Temporal 服务强杀重启 | 保留同一磁盘数据库，服务 PID 改变，原 Workflow/Run ID 和客户端仍有效 |
| 丢弃全部旧 Worker | 新进程从 History 冷重放，不能依赖旧 cache 或 backend 对象 |
| Continue-as-New | 读取真实 History 事件与新 Run 输入，逐项比较完整文件数据、消息前缀、todos、身份和版本 |
| owner 隔离 | 受控本地 adapter 拒绝另一个 owner 的 handle、查询、结果和取消访问 |
| 重复执行 ID | 两个 Continue-as-New 链完成后，相同请求与 Workflow ID 重提均被拒绝；仅证明本次 History 保留期内的行为 |
| 清理与证据 | 只停止本轮拥有的进程；receipt 记录唯一 run_id 和终态，失败不会保留旧 pass |

真实 History 中观察到 `deepagents.invoke_model`、`prototype_memory_put` 与
`prototype_memory_get` 三种 Activity；Agent 循环没有被打包成单个大 Activity。
服务 PID `35406` → `35454`；原 Run ID 在强杀重启后保持，两个新的 Run ID 仅在
Continue-as-New 边界产生，旧 → 新链由实际 History 事件核对。新 Worker PID
`35455`、`35456` 从 History 重建状态；Memory 逻辑效果仍各一条。

完整 receipt、前后 snapshot、History 中实际 CAN 输入、数据库观察和进程日志保存在
`var/temporal-prototype/a21cf53c8ef04a54911debb5e0e4e277/`，不随代码发布。
receipt 的 `production_ha_acceptance` 明确为 `false`。
可提交的 [精简验收证据](docs/temporal-prototype-evidence.json) 保存固定版本、源文件
hash、六组结果、真实 Run 链与资源数据；完整 receipt 的 SHA-256 也在其中。

脚本将所有拥有的进程树合计限制在 **2 GiB RSS**、系统 memory-free 不低于 **15%**，
总时间最多 **180 秒**。服务使用 `GOMAXPROCS=2`、`GOMEMLIMIT=512MiB`；Worker
Activity slot 为 1、Workflow slot 为 2，关闭 eager Activity，显式限制 poller。
这些是测试资源限制，不是吞吐测量或生产容量承诺。

生产集群 HA、数据库主备切换、物理断电、真实模型、长时间负载、HTTP 身份服务、
租户公平配额、Task/Outbox/Event 控制面以及不兼容部署的版本路由仍需独立实现与验收。

## 架构与失败证据

完整分层图与本轮标注见 [Temporal 迭代图](../docs/audits/temporal-prototype-2026-10-16.png)
和 [exact prompt](../docs/audits/temporal-prototype-2026-10-16.prompt.txt)。
橙色泳道表示新增的独立原型，图中仍明确标注本地 SQLite、离线模型与未实现的 Task 控制面。
canonical [README Mermaid](../README.md#architecture) 与
[交互地图](../docs/mini-loop-system.architecture.html) 同步维护。

首次真实运行 `2e116ae89ec441278ad418df3f50f93e` 在进入强杀阶段之前失败：
实际 factory 未注册 `write_todos`。运行耗时 29.74 秒，采样峰值 672.4 MiB，
owned 服务与 Worker 已清理，失败 receipt 保留于忽略目录，不能当作故障恢复通过。
随后增加显式 Todo middleware 与完整状态恢复检查；最终验收以新的唯一运行记录为准。

第二次运行 `81c9416a8b634305ab3092f23552280f` 已经过强杀与重启，但严格 transcript
检查失败（16.83 秒、637.2 MiB，清理完成）。离线解码 `history_node` 确认：两个任务
共 8 个原生 ToolMessage 的 ID 随冷重放变化，content 与 tool_call_id 均保持。该失败
促成 reducer 前稳定消息 ID 的修复；没有删除 snapshot 字段或放宽比较来获得通过。

第三次运行 `eb65ab7700e24923a49a71251fcdc7ec` 通过（15.70 秒、591.2 MiB）。
主审随后补强 todos、schema/version 和最终身份/终态的显式跨 Run 相等断言；对该次
receipt 离线逐项审查通过，并由上方主交付 run 再次在线通过全部断言。
