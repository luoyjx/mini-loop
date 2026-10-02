# Pi v1.0.0 源码调研与 mini-loop 采用建议

> 调研日期：2026-10-02，Asia/Shanghai。上游：[earendil-works/pi](https://github.com/earendil-works/pi)。
> 最新稳定版：[v1.0.0](https://github.com/earendil-works/pi/releases/tag/v1.0.0)，
> 发布时间 2026-10-01 19:20:55 UTC，即北京时间 10-02 03:20:55。
> 验证源码固定于 [`a13d35a742c6ef8462812a28fbe1d8c8b7431c32`](https://github.com/earendil-works/pi/tree/a13d35a742c6ef8462812a28fbe1d8c8b7431c32)。
> 调研时 main 为 [`7fbbd5f4a1d982bb02d63472dde0774fa639f99b`](https://github.com/earendil-works/pi/tree/7fbbd5f4a1d982bb02d63472dde0774fa639f99b)，比 release 多两个提交。
> mini-loop 对照基线：已提交的 `87f5c7469f3084558a9526bca21e68a7c05a8420`。

本报告更新 [八月调研](PI_RESEARCH.md)，不修改 mini-loop 运行时、不引入上游依赖。
“事实”来自固定版本的文档与实现；“验证”来自独立检出的实际命令；
“判断”和“建议”不等同于上游承诺。除明确注明 main 差异外，源码链接均固定于 release SHA。

## 1. 结论先行

Pi 已不只是“成熟 CLI 加未完成的下一代 harness”：v1.0.0 具备内建 MCP、
QuickJS codemode，以及真正可运行和恢复的 pi-durable。但正式 CLI 仍采用原有
JSONL 会话，durable 集成与远程服务仍是实验路径。对 mini-loop，最值得吸收的是
恢复状态机、按需工具发现和嵌套调用合同，而不是整体替换运行时或移植 Pi 的信任模型。

结论变化最大的三点：

- 旧 `AgentHarness` 已移除，新 `pi-durable` 有实际任务调度、存储和恢复实现；
  “只是 scaffold”不再成立，但“已全面接管正式产品”也不成立。
- MCP 已是内建扩展，并与 codemode、deferred discovery、OAuth 和会话恢复联动；
  旧报告“未内建 MCP”的判断必须停用。
- Chord、protocol、server/client 已形成具体的服务与状态复制路径；
  它们仍不提供跨进程多写者存储、分布式 fencing 或租户权限边界。

本地离线构建通过；本次执行的 99 个测试文件中，**1,756 个测试通过，1 个跳过**。
这证明选取路径在固定环境下通过测试，不代表全部 provider、真实 OAuth 或生产多租户部署已验证。

## 2. 版本变化：不是一次小升级

旧报告分析 main `086c32e74530564922d011ade23ff582c9d63116` 与 release v0.84.2。
旧 main 到 v1.0.0 的 Git 差异涉及 1,439 个文件，约新增 179,841 行、删除 60,966 行；
这些是文本差异规模，不是有效代码量或质量指标。当前 main 相比 release 的两个提交主要涉及
终端彩蛋与对应记录，不改变本报告的核心运行时结论。

| 领域 | 八月快照 | v1.0.0 复核 | 状态与采用含义 |
|---|---|---|---|
| 下一代 harness | `AgentHarness` 与旧实验子路径，核心有未实现处 | 旧接口移除；独立 `pi-durable` | 实验实现已落地，旧 API 迁移不能当成兼容升级 |
| 存储后端 | 独立 `session-backends` | 后端整合到 durable | Memory、SQLite、JSONL 各有明确保证 |
| MCP | 未内建 | 内建扩展，四种 exposure、OAuth | 产品路径，不再只是外部桥接 |
| codemode | 非主要产品能力 | 独立 QuickJS 包与 CLI 扩展 | 脚本隔离与工具权限必须分开看 |
| 服务与视图 | server/client/protocol 实验基础件 | Chord facets、服务 facade、复制视图与连接生命周期 | 已有实现与测试；仍非租户控制面 |
| AI API | 主要统一聊天 provider | chat、image、classifier 类型；中途 system/tool 更新 | 更宽的 provider 合同，不只是新增模型名称 |
| 普通 CLI 会话 | JSONL v3 | 仍为 JSONL v3 | durable 并未默认替换 |

13 个主要包目录为 agent、ai、chord、client、codemode、coding-agent、durable、evals、
mcp、protocol、server、telemetry、tui；许可证仍是 MIT。
直接证据：[根 README](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/README.md)、
[agent breaking changes](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/agent/CHANGELOG.md)、
[包与构建配置](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/package.json)。

v1.0.0 本次发布重点另包括默认 fullscreen、Radius 登录与 MCP 配置、Anthropic copy-code 登录、
codemode 图像生成和 MCP OAuth 修复。上游声称 codemode 提示开销约下降 40%；
具体示例是 GPT-5.6 请求由约 5,300 降至 3,300 tokens，约 37.7%。
这是上游示例，不是本次性能实测，也不能外推到所有工具集和 provider。
来源：[coding-agent changelog](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/CHANGELOG.md)。

## 3. 正式 CLI 与 durable 必须分开看

| 入口 | 实际运行时与持久化 | 当前边界 |
|---|---|---|
| 已发布 `pi` | `cli.ts → main.ts → SessionManager + createAgentSessionRuntime`；JSONL session version 3 | 成熟 CLI 的扩展、会话树与资源系统 |
| 源码 durable TUI | `experimental/durable/main.ts → Harness + SQLite + TUI` | 单进程小型 coding agent，支持恢复与任务图 |
| 源码 server/client | development dispatcher、session worker、durable Harness、Chord services | 开关控制的实验路径，非正式 CLI 默认入口 |

源码依据：[主入口](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/main.ts)、
[正式 SessionManager](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/core/session-manager.ts)、
[发布文件清单](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/package.json)、
[开发命令 dispatch](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/experimental/commands.ts)。
仅设置 `PI_EXPERIMENTAL=1` 不等于已发布 binary 自动获得源码实验入口。

durable TUI 已有模型选择、thinking、compact、steer/follow-up、子会话切换和 task graph。
宿主用文件锁阻止第二进程打开同一会话，崩溃遗留锁约 10 秒后视为 stale。
但它明确缺少 session picker、fork/tree navigation、扩展、prompt templates、图像和自身的 `/login`。
因此“库可恢复”与“完整 CLI 功能可恢复”是两件事。
来源：[durable TUI 说明](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/experimental/durable/README.md)。

服务集成也有未完成项：导航被暂时移除，部分服务只绑定 root conversation，
Transcript view 仅覆盖当前 reset/compaction 后上下文，历史分页尚缺。
来源：[experimental services 的 TODO](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/experimental/services/README.md)。

## 4. pi-durable：最重要的架构进展

### 4.1 从骨架变成运行时

`Harness.open()` 实际打开任务系统；scheduler 在重开时扫描 pending、running、waiting、
completing 任务，将遗留 running 转回 pending 并保留 checkpoint，再由 resume 驱动。
缺少 task definition 或不兼容版本的任务会等待修复/迁移，不凭空跳过。
这是实际实现，不只是设计文档中的 API：
[Harness](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/src/harness/harness.ts)、
[scheduler](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/src/harness/scheduler.ts)。

核心模型是同一 Harness 的单 mutation line：不可变 transcript entries、文档状态、
task checkpoints 在一个 commit 中变更；存储成功后才发布变化。外部工具效果不在事务内部。
`pi.live`、`pi.inbox`、`pi.agent`、`pi.usage` 分别描述运行、输入队列、选择和计量状态。
事务表操作还有读取/写入顺序约束，不能照搬普通数据库“随意读写”的假设。
依据：[完整规范](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/docs/spec.md)。

generation checkpoint 区分 prepare/request/retry/poll/tools；重开可恢复退避时间、
deferred handle 和工具阶段。普通流式请求中断可能重发请求，并为已记录的 partial 收尾；
它不是恢复原 TCP/provider token stream。源码按约 100 ms 节流提交 partial，
view 以已提交状态为基础；不要把它解释成每个 provider token 各形成一个 durable commit。
测试依据：
[generation recovery](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/test/harness-generation-recovery.test.ts)、
[generation 实现](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/src/harness/generation.ts)。

### 4.2 工具恢复不是 exactly-once

工具执行采用“先记 intent → 外部效果 → 再记 outcome”。在效果与 outcome 之间崩溃，
运行时无法自动证明效果有没有发生；默认 replay 为 `unsafe`。
重开时只有 checkpoint 中与当前工具定义**都标为 `safe`**，且工具仍在当前可用集合内，
才会重新执行；否则生成 interrupted 结果，并保留已持久化的部分输出。
before-tool 重写后会重新验证参数，且检查发生在记录执行 intent 前。
依据：[ToolTask](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/src/harness/tool.ts)、
[tools recovery tests](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/test/harness-tools-recovery.test.ts)。

`requestId` 去重是 conversation 范围的 submission 去重。同 ID、同 submission type
返回旧记录；实现没有比较新内容的 digest。它不能直接替代 mini-loop 的 owner 绑定、
批准 receipt 或“同请求必须同参数”的合同，更不能证明外部工具 exactly-once。
依据：[admitSubmission](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/src/harness/submissions.ts)。

### 4.3 队列、子任务和存储边界

steer 在当前工具轮后消费，follow-up 在运行结束边界消费；输入提交与撤回有独立状态。
父任务拥有子任务/子会话时，取消和失败会传播；background 模式明确切断这种默认 ownership，
不会保持父会话 busy，取消时需显式决定是否包含 background。
structured tasks 支持 allSettled/failFast，父任务可能进入 completing 等待所属子工作结束。
这些是可借鉴的生命周期合同，不是“任意并发都安全”的保证。
依据：[任务恢复测试](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/test/harness-tasks-recovery.test.ts)、
[durable API 文档](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/README.md)。

存储保证应分别解读：Memory 不持久；SQLite 使用 WAL、`synchronous=NORMAL`，
进程崩溃恢复不等于断电时最后写入不丢；JSONL 可配置 fsync。
核心库要求单进程独占一个 storage，不提供跨进程锁。实验 TUI/worker 的宿主锁是另一层，
不能解释成核心库具有分布式租约/fencing。其规范也明确排除 CRDT/offline 多写者。
这比旧报告笼统讨论 writer lease 更准确；采用时必须保留 mini-loop 自己的 lease/epoch 合同。
依据：[storage 文档](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/README.md#storage)、
[规范的 non-goals](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/durable/docs/spec.md)。

## 5. MCP 与 codemode：工具规模管理进入产品主路径

MCP 是内建、可替换的扩展；支持 stdio 和 streamable HTTP，不支持旧 SSE transport。
工具暴露有四种策略：codemode（默认间接调用）、deferred（按需搜索）、direct、hidden。
间接工具不需要把所有 schema 一次塞入模型请求；搜索后的可见性会随 transcript 记录，
在 resume/fork 中恢复。服务器 instructions 是 prompt 内容，不是权限授予。
这里的“内建”针对 CLI；SDK sessions 不自动加载内建扩展，宿主仍需显式安装 MCP、
codemode/tool-search 扩展。codemode 连接后可自动激活，也有配置关闭该行为。
依据：[MCP 文档](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/docs/mcp.md)、
[内建扩展注册](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/extensions/index.ts)。

codemode 使用 QuickJS WASM，每次执行有独立 worker/VM，脚本本身没有 Node、文件系统、
网络与计时器，依赖显式 host bridge。CLI 配置 256 MB VM 内存，worker 可在超时/取消时终止。
这是真实脚本隔离，但不是 host 工具隔离：桥接的 bash、文件工具仍按宿主能力运行。
已发生的工具效果不会因脚本失败回滚；成功返回才提交的 store 数据也不是外部效果事务。
依据：[codemode 包](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/codemode/README.md)、
[CLI 执行器](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/extensions/codemode/execute.ts)、
[用户文档](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/docs/codemode.md)。

值得借鉴的是嵌套调用没有直接跳过普通执行流程：codemode 的 `tools.*` 经 `ctx.executeTool()`，
进入 session 的 `runToolCall` 与 before/after hooks，并记录 `parentToolCallId`。
不过 Pi 普通产品默认没有 mini-loop 式权限/批准系统；“经过 pipeline”不等于“经过授权”。
`models.classify()`、`models.generateImages()` 又是额外的模型桥接能力；若 mini-loop 引入，
必须单独纳入凭证、成本、owner 和取消预算合同。
依据：[AgentSession 的 nested runner](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/core/agent-session.ts)、
[nested-tool tests](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/test/nested-tool-calls.test.ts)。

v1 OAuth 修复包括按服务器名和 URL 隔离凭证、授权响应 `iss` 检查、可配置 metadata URL、
step-up 时保留既有 scopes。本次相关测试通过，但未用真实身份提供者登录。
不要将 OAuth 认证扩展成“服务器返回工具可以自动获准”的假设。
依据：[MCP OAuth 实现与用法](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/docs/mcp.md#authenticate-with-oauth)。

## 6. Chord 与远程服务：新的组合层，不是新的授权层

Chord 将同步安装的 facets、依赖关系、service facade 和 replicated views 组合起来。
服务可 singleton 或 keyed；remote 服务用严格 JSON，view 支持 immutable structural sharing。
远端订阅先取 snapshot 再接收有序更新，断线期间 view 不 ready，重连需 hydrate。
慢消费者缓冲上限 100，溢出时重发 snapshot 并重置序列/路径字典，而不是默默丢增量。
这种 delta 应用不等于多写者 CRDT。
依据：[Chord](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/chord/README.md)。

server 负责路由 opaque service calls；application 提供目录、session factory 和存储；
protocol 负责 CBOR/framing。route identity 包含 serverId/sessionId/attachmentId，
用于拒绝 stale/mismatched route，多 presentation 可附着同一会话。
连接关闭会先等已接纳调用结束，再释放 attachment；worker 生命周期同时参考 client demand
与 Harness 的 live activity。它们是生命周期与路由一致性，不是 owner authentication。
peer 认证仍由 application policy 决定，实验 Unix transport 不提供该边界。
依据：[server](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/server/README.md)、
[session worker](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/coding-agent/src/experimental/session-worker.ts)。

## 7. pi-ai 与轻量 Agent：仍有独立借鉴价值

AI registry 现区分 chat、image、classifier，`getModels()` 默认只取 chat，
`getAllModels()` 才覆盖混合类型。classifier 支持 TypeSafe/Jev 与 gateway，
typed choice、score、bool 的公共接口与线协议 noul 有对应关系。
不能把这些分类结果自动解释成行动批准。

system prompt 与 tool 增减可以记为 transcript 更新；支持 mid-conversation 更新的 provider
使用相应映射，不支持的 provider 会折叠为当前 system/tools 请求，可能改变缓存前缀。
统一 API 不保证各 provider 的 cache、thinking 或跨模型语义完全一致。
依据：[pi-ai API 与 provider 合同](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/ai/README.md)。

轻量 `Agent` 新合同也值得关注：`prepareRequest` 在每次会话请求前处理 canonical context，
不负责偷取队列；`finishTurn` 在 normal/error/abort 的结果收尾后、turn_end 前执行。
error/abort 保留强停止语义，不能由 finishTurn 的继续决策覆盖。
可移植的是边界时序及测试，不能把 hook 当成另一套任意请求控制通道。
依据：[Agent 生命周期](https://github.com/earendil-works/pi/blob/a13d35a742c6ef8462812a28fbe1d8c8b7431c32/packages/agent/README.md)。

## 8. mini-loop 的采用顺序应调整

对照当前 [架构与默认状态](../README.md)、[Go parity](../GO_PARITY_MATRIX.md)、
[typed decisions](DECISIONS.md)，mini-loop 已有 Provider SPI、Jev decision provider、
Python 执行权限/批准边界及可选持久化。Go 已有 typed loop、immutable catalog 与 tool gate，
但默认只注册 Bash；HTTP、真实 provider、durable approvals/journal 等仍待实现。
因此不能继续沿用八月报告中“先从零建设 Provider SPI/decision”的优先级。

| 优先级 | 吸收目标 | 必须保留的 mini-loop 合同 | 验收起点 |
|---|---|---|---|
| P0 | durable 工具 intent/effect/outcome 与 safe/unsafe 恢复测试 | owner、lease/epoch、批准 receipt、unknown-effect；同 ID 不同 payload 拒绝 | 效果已发生但 outcome 未提交的 crash-window fixture |
| P0 | generation/queue/child ownership 状态机 | stop_reason、取消收尾、输入顺序、背景任务边界 | prepare/request/tools/retry 的重开矩阵 |
| P0 | nested tool 调用合同 | 所有子调用继续经过单一 gate，固定 catalogue，单独审计 | before rewrite、denial、取消、parent ID 与结果配对 |
| P1 | deferred/codemode 工具发现与 transcript 记载 | visibility 不扩权；搜索结果仍受 owner/role 策略限制 | resume/fork 后 schema 与可调用权限一致 |
| P1 | provider canonical context 与明确生命周期 hook | 不覆盖 error/abort；计量与缓存差异可见 | provider fixture 和请求前缀实测 |
| P2 | Chord 类 replicated read model 与服务门面 | 认证先于 attach；慢消费者有界；重连 hydrate | 丢连接、慢客户端、stale attachment 合同 |
| 暂不采用 | 整体替换为 pi-durable/codemode | Python/Go 两条语言路线与现有租约、存储、权限语义 | 先证明增量收益，不因 v1 名称放宽实验门槛 |

以上是研究建议，不是已实施的计划。尤其不建议在 Go parity 尚未完成时，
同时引入另一套 TypeScript durable runtime、跨语言 worker 和新的权限兼容层。
若只做一项下一步工作，优先写 crash-window/恢复合同测试，收益和迁移风险最清楚。

## 9. 工程验证与限制

### 9.1 本地执行结果

固定检出在临时目录，未将上游依赖安装进 mini-loop。使用 Node 22.19.0、npm 10.9.0。
执行 `npm ci`，下载发布的 source archive 并核对官方 SHA256SUMS；仅提取构建所需
`packages/ai/src/providers/data`。archive SHA256 为
`89089c82d41759b800124a77e212adaa867caaa9d1269d8012ef0df9bc86b92e`。
随后 `npm run build:offline` 成功，避免把即时模型数据刷新误当成 release 内容。

| 测试范围 | 文件通过数 | 测试通过数 | 跳过数 |
|---|---:|---:|---:|
| pi-durable | 42 | 853 | 1 |
| pi-agent-core | 4 | 90 | 0 |
| pi-chord | 21 | 345 | 0 |
| pi-codemode | 3 | 62 | 0 |
| pi-mcp | 5 | 39 | 0 |
| pi-protocol | 3 | 133 | 0 |
| pi-client | 3 | 27 | 0 |
| pi-server | 6 | 44 | 0 |
| coding-agent 选取的集成测试 | 12 | 163 | 0 |
| 合计 | 99 | 1,756 | 1 |

coding-agent 选取范围：experimental remote runtime、worker manager/lifecycle、
MCP extension/OAuth store/refresh、nested tool calls、codemode worker/renderer，
以及 AgentSession codemode、MCP、MCP OAuth 三组 suite。

首次直接测试时因未构建的 workspace 导出和模型数据缺失而加载失败；
完成 release 数据准备与离线构建后，以上测试重新执行全部通过。
这是环境前置条件，不将前次加载失败算作上游功能回归，也不隐去它。
SQLite 运行仍提示 Node experimental warning。Gondolin 示例声明 Node >=23.6，
本次未执行该示例；evals 的 pnpm 要求也未验证。

### 9.2 依赖与安全边界

本次 `npm audit --omit=dev --json` 返回 3 个 high 级受影响 package 条目，
并非 3 个独立漏洞：CLI 的 minimatch 10.2.6 引入 brace-expansion 5.0.9，
对应多项 DoS 告警；Gondolin 扩展示例链引入 node-forge 1.4.0 的 RSA 签名校验告警，
并导致 Gondolin 自身列为继承受影响。`npm explain` 已确认依赖路径。
这些是调研时 advisory 数据；尚未证明 Pi 的具体输入路径可利用，也未自动执行 audit fix。
参考：[brace-expansion advisory](https://github.com/advisories/GHSA-qhr7-859c-m2p7)、
[node-forge advisory](https://github.com/advisories/GHSA-86w9-cpqp-85rv)。

Pi 的根 README 仍明确不内建 permissions/sandbox：宿主进程能做的事工具也能做。
Chord 插件与 MCP stdio command 也是执行代码，不是安全数据。
QuickJS 对脚本的访问限制，不能弥补宿主工具默认拥有本机权限这一信任假设。
mini-loop 同样不能夸大默认 NullSandbox，但不应为了兼容 Pi 放弃已实现的单一权限 gate。

本次未执行：全 monorepo 测试、真实付费模型请求、真实 OAuth、交互终端人工验收、
断电/跨机器故障实验、生产并发/吞吐 benchmark。通过的 durable recovery tests 多为
确定性 checkpoint/关闭重开测试，不等同于完整现场 SIGKILL/断电证明。

本仓库交付只改研究文档与自动生成索引；Research Atlas 的 content 检查、构建与
5 个 rendered HTML 测试通过，固定 SHA 的 32 个源码链接均已验证路径存在。
未运行 mini-loop Python/Go 全套测试，因为没有运行时代码改动；此项不计为运行时验证。

## 10. 最终判断与复核命令

Pi 的工程价值明显上升：新 durable 内核已足以作为恢复设计与测试的直接参考，
MCP/codemode 能展示大工具集的实际产品化方法，Chord 展示了状态与服务组合的清晰分层。
但 v1.0.0 是整个发布版本，不代表每个实验包都稳定；pi-durable 明确保留 API 可随时变化的声明。
建议由“等待新 harness 实现”改为“审阅并移植关键合同”，整体替换仍不建议。

以下命令在已固定 v1.0.0、安装依赖并准备好 release provider data 的上游检出中执行：

```sh
git checkout --detach a13d35a742c6ef8462812a28fbe1d8c8b7431c32
npm ci
npm run build:offline
npm run test --workspace=@earendil-works/pi-durable --workspace=@earendil-works/pi-agent-core
npm run test --workspace=@earendil-works/pi-chord --workspace=@earendil-works/pi-codemode --workspace=@earendil-works/pi-mcp
npm run test --workspace=@earendil-works/pi-protocol --workspace=@earendil-works/pi-client --workspace=@earendil-works/pi-server
npm audit --omit=dev --json
```

coding-agent 集成子集需在 `packages/coding-agent` 执行：

```sh
../../node_modules/.bin/vitest run \
  test/experimental-remote-runtime.test.ts \
  test/experimental-session-worker-manager.test.ts \
  test/experimental-session-worker-lifecycle.test.ts \
  test/mcp-extension.test.ts test/mcp-oauth-store.test.ts test/mcp-oauth-refresh.test.ts \
  test/nested-tool-calls.test.ts test/codemode-worker-config.test.ts test/codemode-renderer.test.ts \
  test/suite/agent-session-codemode.test.ts \
  test/suite/agent-session-mcp.test.ts \
  test/suite/agent-session-mcp-oauth.test.ts
```
