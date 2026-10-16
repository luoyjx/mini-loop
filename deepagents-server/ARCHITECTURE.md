# Agent Server 分层架构

这张图解释 `6328007` 中的 `deepagents-server/`：标准 Agent Server 负责服务与调度，
Deep Agents 负责模型和工具循环，本模块提供 graph、认证、用户隔离及存储绑定。
它是 [README canonical architecture](../README.md#architecture) 中独立模块泳道的展开视图，
不新增运行能力，也不改变原 Python/Go runtime。

![当前 Agent Server 分层架构](docs/agent-server-layered-architecture.png)

## 从上往下读

| 层 | 回答的问题 | 当前实现 |
| --- | --- | --- |
| L1 客户端与接入 | 用户怎样调用？ | SDK 或 REST 发请求，SSE 接收事件；`scripts/chat.py` 是可运行示例 |
| L2 身份与访问控制 | 这个用户能访问什么？ | Bearer token 映射到可信 owner；Thread/Run 按 owner 过滤；Store namespace 首段必须匹配 owner |
| L3 服务与调度 | 一次任务怎样被管理？ | 标准 Agent Server 提供 Threads、Runs、排队、取消、状态和 SSE；本模块注册无副作用 graph factory |
| L4 Agent 执行 | 任务具体怎样完成？ | `create_deep_agent` 构造 graph，框架执行 planning、general-purpose 子代理和模型/虚拟文件工具循环；ChatOpenAI 调用外部模型 |
| L5 状态、文件与 Memory | 数据属于谁，怎样保存？ | StateBackend 保存线程虚拟文件；StoreBackend 将 `/memories/` 路由到 `(owner, memories)` |

Thread 是一段有身份的对话，Run 是这段对话中的一次执行。一个 Thread 的 Run 串行，
不同 Thread 可以并发。当前 Compose 只有一个 Agent Server API 服务，没有拆出独立 Worker 池；
图中的分层表示逻辑职责，不表示每层各占一个容器。

## 一条消息的路径

1. 客户端携带 token 创建或选择 Thread，再提交 Run。
2. `auth.py` 验证 token，得出 owner，检查对话归属。请求中的 `context.user_id` 或伪造的认证 config 不会成为可信身份。
3. Agent Server 调度 Run，调用 `graph.agent(runtime)` 构建 graph。factory 也用于 schema/state inspection，因此不能在其中创建业务资源或写库。
4. Deep Agents 调用模型，由模型选择 planning、子代理或虚拟文件工具，工具结果再进入循环。
5. Server 管理 Checkpoint/Store，并通过 SSE 返回事件。Checkpoint 不构成逐 token 持久日志，也不提供外部副作用 exactly-once 保证。

## 两种文件作用域

假设 Alice 有两个 Thread：A 和 B，Bob 有 Thread C。

- Alice 在 A 写 `/plan.md`：文件属于 A 的虚拟状态，B 和 C 都不会自动看到它。
- Alice 在 A 写 `/memories/preference.txt`：它进入 `(alice, memories)`，B 可以读取，C 仍不能读取。namespace 从 verified server user 得出，不由模型输入选择。
- `/memories/AGENTS.md` 会作为用户长期指令/知识载入上下文；它仍是不可信数据，不能改变工具权限或身份。不同 Thread 同时修改同一 memory 文件没有 CAS 合并保证。

这里的文件都是 graph state/store 中的虚拟文件。没有宿主文件系统 backend 或 shell executor；
即使 schema 中出现 `execute`，当前 backend 也会返回 execution unavailable。

## 底层存储是二选一的运行模式

| 模式 | 数据底座 | 当前证据 |
| --- | --- | --- |
| 本地开发 `langgraph dev` | inmem runtime + `.langgraph_api/` 本地快照 | 已用测试模型验证真实 HTTP/SSE、工具、隔离、取消和优雅退出后的恢复 |
| Compose 部署配置 | PostgreSQL 保存 Run/Thread/Checkpoint/Store；Redis 做临时协调和 PubSub | 配置已校验，真实部署和崩溃/断电恢复尚未验收，需平台凭据与适用许可证 |

真实 OpenAI adapter 已完成构造/schema 检查，尚未使用真实 provider key 执行推理。
预安装 Assistant 是共享只读定义；私有的是 Thread/Run 与用户 memory。
Cron 当前拒绝访问，没有 MDA Context Hub、OAuth、Channels 或托管 Sandbox。

## 代码与制图证据

- 接入：[chat.py](scripts/chat.py)。
- 授权：[auth.py](src/mini_loop_deepagents/auth.py)。
- graph/model/backend 绑定：[graph.py](src/mini_loop_deepagents/graph.py)。
- 服务注册：[langgraph.json](langgraph.json)；部署配置：[compose.yaml](compose.yaml)。
- 已有验证结果：[DELIVERY.md](DELIVERY.md)，82 项模块检查及 10 组真实 HTTP 检查；本次制图没有重新运行实现测试。
- Built-in imagegen 以仓库完整 layered base 为编辑输入，聚焦并展开独立模块泳道；[exact prompt](docs/agent-server-layered-architecture.prompt.txt) 与图片同目录保存。
- 逐层核对当前代码及 canonical Mermaid；区分框架能力、模块代码、开发证据和部署配置。
