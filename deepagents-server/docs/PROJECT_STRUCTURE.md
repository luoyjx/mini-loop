# Deep Agents 服务项目结构

本模块使用 `src` 布局区分可安装的运行代码与仓库工具。标准 LangGraph Agent Server、
Temporal Task 服务和历史 Temporal 原型各有入口；Task 服务内部按 HTTP、任务编排和
Temporal 后端分工。源码目录不代表新增部署服务，API 与 Worker 仍是两个进程角色。

## 目录与职责

```text
deepagents-server/
├── pyproject.toml / uv.lock          包边界、固定依赖与可选 temporal extra
├── langgraph.json                   标准 Agent Server 的 graph/auth 入口
├── Dockerfile / compose.yaml        标准 Agent Server 部署
├── Dockerfile.task / compose.task.yaml  独立 Task API/Worker 部署
├── src/mini_loop_deepagents/         wheel 中的运行代码
│   ├── config.py / auth.py / graph.py   标准 graph 的配置、认证、构造
│   ├── jobs.py                      标准 Agent Server 的异步客户端
│   ├── task_contracts.py            未来控制面合同；不参与当前任务持久化
│   ├── task_server/                 当前 Temporal Task 服务
│   │   ├── api.py / http.py         CLI 与兼容入口 / ASGI HTTP 层
│   │   ├── protocol.py              错误、稳定标识、摘要与操作接口
│   │   ├── service.py               submit/get/result/cancel/events 编排
│   │   ├── backend/                 Temporal SDK 访问及其协议适配
│   │   │   ├── runs.py              owner 校验、Run 查找与执行链校验
│   │   │   ├── history.py           History 读取、事件页与 Run 导航
│   │   │   ├── cursors.py           有签名的每 Run 分页 cursor
│   │   │   └── readiness.py         有界 namespace 探活与并发合并
│   │   ├── worker.py                model provider、Worker 装配及 CLI
│   │   ├── runtime.py / runtime_v2.py   已发布的 Workflow 版本
│   │   ├── checkpoint.py            跨 Continue-As-New 的 saver 日志
│   │   ├── summarization.py         原生摘要中间件的确定性适配
│   │   └── connection.py / lifecycle.py / launch.py
│   │                               连接、进程生命周期、角色启动
│   ├── temporal_support/           生产 Workflow 与原型共用的窄适配
│   │   └── determinism.py          Workflow 确定性时钟和 ID 相关适配
│   └── temporal_prototype/          历史离线实验；自己的入口与状态合同
├── tooling/                         仅在源码仓库使用，不打进应用 wheel
│   ├── paths.py                     仓库根、fixture 等路径的集中定义
│   ├── build_task_bundle.py         固定依赖及源文件一致性审计、构建上下文
│   └── acceptance/
│       ├── scenarios/              各故障场景的执行流程
│       ├── support/                子进程、资源预算、Docker/Temporal 辅助
│       └── fixtures/               离线模型、测试 Worker 与观察器
├── scripts/                         故障/构建兼容薄入口；chat.py/jobs.py 客户端示例
├── tests/                           单元和合同测试；不承载运行时 fixture
└── docs/                            源码导航与已记录的验收证据
```

`task_contracts.py` 是独立设计合同，不是本轮拆分后的 `protocol.py`，也不是已部署的
SQL admission、outbox 或配额实现。当前任务身份、状态与事件仍由保留中的 Temporal History
提供。`temporal_prototype/` 的本地 SQLite memory/effect ledger 也没有接入 Task 服务。

## 跟随一个 Task 请求

1. `api.py` 解析启动参数并装配连接；`http.py` 负责 ASGI 生命周期、Bearer 身份、
   请求大小/并发限制、字段校验和 HTTP 错误映射。
2. HTTP 层依赖 `protocol.py` 中的任务操作接口，不直接拼装 Temporal RPC。
   `service.py` 根据 owner、Idempotency-Key 和消息执行提交、查询、取消和分页编排。
3. `backend/runs.py` 核对 owner、不可变请求 memo 和执行链；`history.py` 读取保留的
   History 并构造事件页，`cursors.py` 验签和绑定 namespace/owner/TaskID/RunID。
   `readiness.py` 单独管理探活的超时、并发合并和短缓存。
4. Temporal 服务持久接受 Workflow 后，API 才能返回 202。Worker 独立轮询并执行
   `runtime.py` 或 `runtime_v2.py`；HTTP 连接的生命期不拥有任务执行。

依赖方向是 `api → http → protocol` 与 `api → service → backend`。
HTTP 与实际 service 通过操作接口连接；`backend` 复用纯协议定义，既不导入 HTTP 层，
也不导入 service。这里的接口只描述当前实际使用的操作，没有通用 repository 框架。

`temporal_support/` 只承载共享的确定性适配。生产 Task Workflow 不再通过历史实验模块
获取这些能力。模型 I/O 仍由 `DeepAgentsPlugin` 移入 Activity；这个目录不是新的模型服务。

## 按变更类型选择位置

| 想扩展什么 | 从哪里开始 | 需要保留的边界 |
| --- | --- | --- |
| 一个 HTTP 字段或错误响应 | `task_server/http.py`；操作变化时同步 `protocol.py`、`service.py` | owner 只能来自认证；输入不选择 provider 或 fixture |
| 任务提交、查询或取消规则 | `task_server/service.py` | 远端 Start 确认、旧 key 内容核对、首 Run 身份校验 |
| History 导航或 cursor | `task_server/backend/history.py`、`cursors.py` | 保留期限、每 Run 的事件编号与现有签名格式 |
| Temporal 连接或探活 | `connection.py`、`backend/readiness.py` | CLI 环境读取与 library 显式配置分开；不降低 TLS 验证 |
| 模型部署设置 | `config.py`、`task_server/worker.py` | 凭据留在 Worker；固定 alias 不由 HTTP 输入覆盖 |
| Workflow / 工具执行行为 | 对应版本的 `runtime*.py`、`checkpoint.py`、`summarization.py` | 先考虑历史 replay 和状态格式，再决定新版本；不能只通过改模块名迁移旧任务 |
| Worker 退出行为 | `lifecycle.py` 与 `worker.py` | library `serve` 不接管宿主信号；CLI 才拥有强退责任 |
| 新故障验收场景 | `tooling/acceptance/scenarios/`；共用设施放 `support/` | 离线模型与故障注入留在 `fixtures/`，不进入生产输入或 wheel |
| 打包规则 | `tooling/build_task_bundle.py`、`Dockerfile.task` | wheel 源文件和锁文件一致性；不从测试目录拼装生产包 |

能由现有职责容纳的小变化直接放入对应模块；只有出现独立责任或重复设施时再拆文件。
不要为了目录对称引入空层，也不要让 `support` 逐渐变成业务逻辑的收纳目录。

## 保留的入口和持久协议

- `langgraph.json` 的 graph/auth 引用与 `mini_loop_deepagents.jobs.AsyncJobsClient` 保持。
- `python -m mini_loop_deepagents.task_server.api`、`worker`、`launch` 保持原启动方式；
  `api.create_app` 和已使用的 service 导出保留兼容入口。
- 原 `scripts/*.py` 命令保持可调用；故障验收和构建脚本通过普通包导入进入 `tooling/`，
  不依赖按文件路径动态加载其他脚本，也不从 `tests/` 导入运行组件。
  `scripts/chat.py` 与 `scripts/jobs.py` 仍是原位的客户端示例和客户端 CLI。
- Workflow 名称 `TaskWorkflow` / `TaskWorkflowV2`、固定模型 alias、Activity 名称、
  request memo、状态 payload、checkpoint envelope 与 cursor 编码保持。
  代码重排不等于任意 Workflow 版本升级已通过 replay 验证。
- 生产依赖仍由现有 `pyproject.toml` / `uv.lock` 管理；`tooling` 和验收 fixtures 不发布到
  应用 wheel。它们只在受控验收时以明确的仓库路径运行。

## 测试与证据怎么读

`tests/` 验证协议、HTTP、后端、Workflow 适配以及验收设施自身的合同。
`tooling/acceptance/scenarios/` 运行真实服务和故障流程，输出单独的验收收据；
单元测试通过不能替代真实 Temporal 的恢复实验。

已有完整环境可以直接运行 `.venv/bin/python -m pytest -q`。安装或同步环境时需区分
标准 profile 和显式 `temporal` extra，避免不带 extra 的同步移除已有 SDK。
重型测试、镜像构建和故障实验串行运行，并按仓库要求限制、观察 CPU 与进程树 RSS。

[验收矩阵](TASK_ACCEPTANCE.md) 和现有 evidence JSON 保留各自原始源码、镜像及实验基线。
新目录结构不回写历史收据；本轮重新运行的检查另记在 [交付记录](../DELIVERY.md)。
这些本地证据仍不证明生产 Temporal HA、跨主机故障域、真实 provider 吞吐或任意版本迁移。
