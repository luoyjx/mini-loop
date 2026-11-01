# mini-loop 演进为主动个人助理：Go 优先实施路线

> 状态：路线草案 + 可运行 demo D0、D1（2026-11-01）
>
> 基线：mini-loop `c65073a`，D1 基于 `b1a0a72`；demo 代码在 `go/assistant`、`go/cmd/miniloop-assistant`
>
> 已拍板（2026-11-01）：先部署在用户自己的 Mac 上；真实信道暂不做，继续用模拟网页；集成走 MCP，先用模拟 MCP server
>
> 场景：个人助理（邮件、日程、例行事务），不是研发 Super-agent

## 结论先行

主动个人助理需要六个部件：常驻运行的「自己的电脑」、应用集成、记忆、routine/触发器、审批闸、审计账本。mini-loop 的 Go 运行时已经具备其中最难补的治理部分：带风险分级的 `ToolGate`、权限模式、action journal、SQLite 状态、owner 记忆和 typed extension tool。缺的是「常驻 + 事件驱动 + 对外信道」这一层。

demo 已经用 Go 做了两轮。D0 是一个只监听回环地址的守护进程，加一个模拟信道网页。D1 按拍板结果做了三件事：

- 邮件和日历挪进一个独立子进程里的模拟 MCP server；
- 新邮件改为轮询发现，批准后的发送经 MCP 执行并带幂等键；
- 守护进程可以用 launchd 常驻在本机 Mac 上。

现在的闭环是：

> 外部收件箱（模拟 MCP）→ `mail_poll` 轮询 → SQLite 触发器 → 规则快路径 → Thread 订阅的 worker → 带闸门的工具（读经 MCP，只能起草）→ 待批准提案 → 人工批准 → 发件箱经 MCP 执行（同一幂等键重试）+ 账本

守护进程或 MCP 子进程被 `kill -9` 后，状态都能恢复，已批准的发送也不会重复。后续路线的顺序：

1. 硬化本机常驻底座（Keychain 凭据、备份、日志轮转）；
2. 把审批和账本做成可上线的形态；
3. 用真实的邮件和日历 MCP server 替换模拟 server，先读后写；
4. 增强主动性（LLM Router、投机执行、Watchdog）；
5. 做长期记忆；
6. 真实信道放到最后，等需要离开电脑使用时再做。

`deepagents-server` 不作为这条路线的底座，只作为持久任务语义的参考实现，原因见第 4 节。

## 1. 范围与证据边界

- **参考材料**（均为 2026-10-31 发布）：
  - 图片《Personal AI agents — 6 must know terms》，即本文的六个部件；
  - DeepSeek Harness 团队《Super-agent 设计探索：迈向更自主的 Agent 系统》（<https://mp.weixin.qq.com/s/zoS7GlReWPhfCjENGxXCdg>）；
  - 赛博禅心《动态图解：DeepSeek 的 Super-agent 架构》（<https://mp.weixin.qq.com/s/lcIkBMOKHSvkRYELgkduMg>）。

  文章只按其表述引用；文中的实验数据没有经过独立复现。
- **仓库事实**来自对 `go/`、`deepagents-server/` 的代码阅读，以及本轮 demo 的测试与手工运行。
- **判断与建议**都会明确标注为「建议」。demo 能证明的只是机制可行，不代表生产可用。

## 2. 参考架构摘要

文章把持续在线、事件驱动的 Agent 称为 Super-agent，并归纳了四个特征：连续、高效、主动、持久。支撑这四个特征的是五个模块：

| 模块 | 要点 | 在个人助理里的含义 |
|---|---|---|
| 双层架构 | 常驻 Router（轻量模型、不带 bash）只负责交互和分派；Worker 上下文隔离，配最强模型和全套工具 | 用户面对的始终是同一个助理，后台每件事各由一个 worker 办 |
| 事件驱动 | Trigger 先过程序规则（快路径），再交给模型判断（慢路径）；同一话题的事件归入 Thread，订阅后直接送达 worker；上下文按 Inline & Spill 和链式召回组织；可投机执行 | 新邮件、邀约、日程提醒都是 Trigger；收到邀约时提前查冲突并起草回复 |
| 面向人的交互 | 异步 start/stop，不阻塞对话；IM 回复不超过 100 词 | 只在需要决定或验收时打扰用户 |
| 计算、状态、环境分离 | 计算无状态，状态进数据库，执行环境留在原地，用 daemon 接入 | 笔记本合上后照常运行；状态可以恢复 |
| Watchdog | 外部的控制回路，既盯基础设施，也盯任务有没有实质进展 | 发现卡住或跑偏的 worker |

## 3. 六个部件 × 现状 × 本轮 demo

| 部件 | Go 运行时原有能力 | 本轮 demo 补上的部分 | 仍然缺少 |
|---|---|---|---|
| **Own computer** | `sqlitestate`（会话、消息、事件、actions、approvals、leases）；`RestoreScheduledSession`。但 `cmd/miniloop` 不打开 SQLite | D0：常驻守护进程；`assistant.db` 和 `agent.db` 双库；重启时把中断的触发器重新排队；Thread 订阅的 worker 按需恢复。D1：默认数据目录在 `~/Library/Application Support/mini-loop/assistant`；`launchd-plist` 生成按用户的 LaunchAgent（崩溃自动拉起）；routine 在睡眠唤醒后补跑 | 真实模型的凭据（launchd 不读 shell 环境，计划用 Keychain）；备份和日志轮转；多进程领取 |
| **Integrations** | typed extension tool；MCP 只有 stdio 和进程内两种传输，注册的 MCP 工具风险一律固定为 `external` | D0：模拟连接器，3 个只读工具 + 2 个 `propose_*` 起草工具。D1：邮件和日历只存在于 `mock-mcp` 子进程自己的 `world.db` 里，守护进程只经 stdio JSON-RPC 访问。守护进程启动时校验 7 个必需工具；读工具由运维包装成 `read` 风险的 typed 工具；`mail_poll` 和两个效果工具只给守护进程用，模型看不到；子进程拿不到凭据形状的环境变量 | 真实 MCP server、MCP HTTP/OAuth 传输、凭据引用、按工具的读写清单 |
| **Memory** | `memory.Store` + `remember`/`recall`（词面检索，按 owner 隔离） | 首次启动写入经理和偏好；worker 先 recall 再判断 | 链式召回、夜间整理、结构化画像、语义检索 |
| **Routines** | cron：JSON 文件、进程内 ticker、重启后不自动启用、没有补跑 | 每日 routine：时段领取和入队在同一事务里完成，保证只触发一次，当天错过的补跑一次；规则快路径；Thread 订阅。D1：新邮件由轮询游标发现，入队和游标推进同事务、按邮件 ID 去重；首次连接从收件箱 head 开始，不回放旧邮件 | LLM Router、批处理、webhook 推送、多个 routine 的编辑 UI |
| **Approval gates** | `ApprovalBroker`（进程内，300s 超时，重启后作废） | 用持久的提案发件箱代替「挂起的回合」：worker 只能起草，重复批准返回 409。D1：批准只把提案移到 `approved`，由唯一的发件箱经 MCP 执行，每次重试都用 `proposal-<id>` 幂等键；server 拒绝立即失败，传输失败退避重试，重启后续发 | 审批推送到手机；按收件人的自动批准策略；提案过期 |
| **Audit log** | action journal、事件、trajectory | 只追加的账本：trigger（来源：轮询、信道或 scheduler）→ 规则 → 路由 → 工具调用（含 action_id）→ 提案 → 决定（谁批准）→ 效果（MCP 工具、回执、幂等键，重放会标出）或失败尝试 | 保留和导出策略、成本统计、「你不在时发生了什么」摘要 |

D0 的验证（`go/assistant`、`go/cmd/miniloop-assistant`）：

- **单元和集成测试**（含 `-race`）。覆盖的场景：
  - 冲突邀约起草婉拒、经理来信起草回复；
  - newsletter 被规则忽略；
  - 可疑邮件不产生提案；
  - 批准只发出一次；
  - 账本把效果、action_id 和批准人串在一起；
  - 安静的 worker 不发信道；
  - routine 只触发一次、错过后补跑；
  - 重启后追问交给恢复出来的同一个 worker；
  - 中断的触发器重新排队、提案去重；
  - HTTP 的 Host 和 Content-Type 防护。
- **手工运行**：`kill -9` 后重启，提案、信道和账本都保留；追问路由到从 `agent.db` 恢复的 worker；SIGTERM 时进程正常退出。

D1 新增的验证：

- **测试**：
  - 测试二进制作为真实 stdio 子进程跑 mock MCP，覆盖工具契约、轮询、幂等发送和拒绝；
  - JSON-RPC 协议错误码；
  - 首次连接从 head 开始，同一邮件只入队一次，轮询故障能显示也能恢复；
  - 发送的响应丢失后重试只产生一次效果；
  - 已批准的提案跨重启续发；
  - server 拒绝不重试；
  - 睡眠唤醒补跑；
  - 守护进程拉起子进程并校验工具；
  - LaunchAgent plist 通过 `plutil -lint`，并拒绝 `go run` 临时二进制、凭据型模型和非回环地址。
- **手工运行**：
  - 真实二进制拉起 `mock-mcp` 子进程，投递、轮询、批准后经 MCP 发出；
  - `kill -9` 子进程后，下一次调用自动重启它；
  - `kill -9` 守护进程后，子进程随 stdin 关闭而退出，重启后追问路由到恢复的 worker；
  - 第一版 demo 的数据目录能原地升级。

## 4. 运行时选择：为什么 Go 优先，`deepagents-server` 是什么角色

**`deepagents-server` 的角色**（事实）：它是仓库里独立、需显式启用的参考模块，有自己的环境、依赖锁和部署配置，并且明确「不调用已有 `mini_loop` Python/Go runtime」。它承担两件事：

1. 用 LangChain Deep Agents 和标准 LangGraph Agent Server 复现 Managed Agent Server 的分层，参考设计见 [MDA 设计](MANAGED_DEEP_AGENTS_AGENT_SERVER_DESIGN.md)；
2. 用 Temporal Task HTTP 服务验证长任务的持久语义：幂等提交、202 只在远端确认 Start 后返回、取消、冷重启恢复、Continue-As-New、优雅排空、容器加固。

它有意不包含 mini-loop 的 ToolGate、审批、hooks 和 SQLite schema，Task 运行时里也没有真实工具和 owner 记忆。

**上一版建议的修正**：上一版曾建议以 Temporal task server 为底座，理由是当时只有它被验证过能扛住进程死亡。现在有了新的前提：Go 优先、单用户个人助理，而且 demo 已经验证「SQLite + 守护进程」能扛住 `kill -9`。在这个前提下，改为如下建议。

**建议：以 Go 运行时为底座**，理由如下：

- **治理已经在 Go 里**：风险分级、权限、action journal、owner 记忆、typed extension、SQLite 状态都有。如果放到 `deepagents-server`，等于在第三套代码里重新移植这些治理能力。
- **运维简单**：单个二进制、goroutine 并发、`mattn/go-sqlite3` 是唯一的依赖。一个用户一台常开主机就够，不需要额外运行 Temporal 集群和 PostgreSQL。
- **不新增依赖**：Temporal Go SDK 属于新增依赖，需要单独审批。只有出现多主机高可用需求时，再把触发器队列后端替换为 Temporal；路线里会保留这个替换的接口位置。

**从 `deepagents-server` 借鉴的部分**（建议）：

- 「owner + 幂等键 → 任务 ID」；
- 「持久接纳后才返回 202」；
- 冷重启验收场景；
- SIGTERM 排空；
- 非 root、只读根文件系统的容器配置；
- `task_contracts.py` 里的 admission、fencing、`dispatch_unknown` 状态，作为触发器队列扩展到多进程时的设计参考。

## 5. 分阶段路线（Go）

顺序原则沿用 [Agent Platform Roadmap](AGENT_PLATFORM_ROADMAP.md)：先持久和闸门，再接外部世界，最后才主动。每个阶段都要求测试、`-race`、架构图和账本证据。

### D0 — 可运行 demo（已完成）

见第 3 节。边界：模拟信道、模拟数据、无认证、只监听回环地址、离线脚本模型（可选真实模型）。

### D1 — 本机常驻 + 模拟 MCP（已完成）

- **集成**：`miniloop-assistant mock-mcp` 是同一个二进制的子命令，在子进程里用 stdio JSON-RPC 提供 9 个工具：
  - 读：`mail_list`、`mail_read`、`calendar_list`、`calendar_get`；
  - 轮询：`mail_poll`；
  - 效果：`mail_send`、`calendar_respond`，都按 `idempotency_key` 幂等；
  - 模拟外部世界：`sim_deliver`、`sim_deliver_custom`。

  守护进程经 `go/mcp` 的 stdio 客户端调用它，子进程死后会在下一次调用时被重启。
- **触发**：轮询器每 2 秒拉一次 `mail_poll` 游标；网页投递后立即唤醒轮询器，不再直接写触发器。
- **发件箱**：批准后的提案进入 `approved` 状态，由发件箱经 MCP 执行；记下回执，或者记下失败并退避。
- **本机部署**：默认数据目录放在 Application Support；`launchd-plist` 子命令输出 LaunchAgent，安装和卸载命令见 `go/assistant/README.md`。
- **边界**：
  - 模拟 server 和模拟信道仍然没有认证，只监听回环地址；
  - launchd 下只支持离线脚本模型；
  - 一个数据目录只能跑一个守护进程。

### P1 — 本机常驻底座硬化（Own computer）

- **凭据**：从 macOS Keychain 读取模型和连接器凭据（launchd 不继承 shell 环境），接上 `secrets.Registry` 遮蔽；修掉 launcher 不设置 `ManagerServices.Secrets` 的缺口。
- **运维**：SQLite 定时备份、账本保留策略、launchd 日志轮转；`doctor` 子命令检查 LaunchAgent 状态、端口、数据目录和 MCP 子进程。
- **租约**：触发器队列和发件箱加通用租约（沿用 `sqlitestate/lease.go` 的条件 UPDATE 模式），防止同一数据目录被两个守护进程同时领取。
- **推迟**：Linux 容器镜像和 systemd 单元，等需要换到常开主机时再做。
- **验收**：在 launchd 下用真实模型跑通一轮；备份能恢复；第二个守护进程会被拒绝。

### P2 — 审批闸与账本可上线（Approval gates + Audit log）

- 把提案扩展成 typed 动作集合（发邮件、回复邀约、创建日程……），加上过期和撤回；D1 的 `proposal-<id>` 幂等键沿用到每种动作。
- 给信道加认证（复用 `httpapi.TokenAuth`）。
- 策略即代码：用户批准的「自动批准规则」，例如「回复经理的确认类邮件」。Guardian 只能作为第二审批人，不能扩大权限。
- 账本按天汇总「你不在时发生了什么」，并支持导出。
- 验收：审批挂起期间重启，事后仍能批准；所有效果都能追到批准人或命中的策略。

### P3 — 真实 MCP 集成，先读后写（Integrations；原 P4，提前）

- 用真实的邮件和日历 MCP server 替换 `mock-mcp`。守护进程只依赖 D1 定下的工具契约（7 个必需工具和它们的参数、结果形状）。不符合契约的 server 先套一层薄适配器，不改守护进程。
- 第一步只开读和轮询，`mail_send` 和 `calendar_respond` 指向仍然模拟的发件端。读通以后再开真实发送，并要求真实 server 支持幂等键，或者由适配器按 `proposal-<id>` 去重。
- **传输**：本机可以优先用 stdio server；需要远端 server 时，再给 `go/mcp` 加 HTTP 传输和 OAuth。凭据只按引用使用，绝不进入模型上下文。
- **风险分类**：每个工具的读写风险由运维写的清单决定，不信 server 自报的 readOnlyHint；保留「未分类按 external 处理」的规则。
- **入站**：先保持轮询；需要实时性时再接推送（验证签名）。
- **验收**：读收件箱不弹审批；任何发送都必须经过提案；trajectory 和账本里扫描不到凭据；断网或 server 重启后，轮询和发件箱能自己恢复。

### P4 — 一个真实信道（Channel；推迟）

本机阶段继续用模拟网页。等需要离开电脑使用时再做：

- 抽出 `Channel` 接口：入站聊天 → Trigger；出站消息和审批卡片（带按钮）→ `DecideProposal`。
- 只接一个信道（Telegram、飞书或企业微信待定），保留模拟网页作为测试替身。
- 遵守 IM 回复不超过 100 字的约束；worker 永远不直接发信道。
- 验收：手机上完成「邀约 → 卡片 → 批准 → 日历更新」。

### P5 — 主动性增强（Routines + 双层架构）

- 加轻量 LLM Router 作为语义慢路径：先过规则，再判断要不要处理；积压的触发器批量注入。
- 扩展投机执行：收到待办先准备材料，收到账单先核对（只走草稿档）。
- 加入异步 start/stop：用 Go 的 background 服务回注结果。
- Watchdog：独立的定时 routine，检查 worker 有没有实质进展（复用 stuck 和 recovery 判定），异常时向信道报告。
- 验收：在合成的 300 条事件流上测 p50/p95、Router 的决定数，以及安静率（不打扰用户的比例）。

### P6 — 长期记忆（Memory）

- 链式召回：给 Trigger、Thread、Worker 加只读的引用工具，每次返回内联下一跳的 ID 和摘要。
- 夜间 routine 做记忆抽取和整理；加结构化用户画像（经理、常用联系人、作息偏好）。
- 词面检索不够用时，再评估语义检索；新增依赖需要审批。

## 6. 横切不变量

- **外部内容一律不可信**：邮件、邀约等触发器发起的回合是 untrusted；只有用户在信道里亲自说的话才是 explicit-human。
- **Worker 没有发送能力**：任何对外效果都只能通过提案加人工决定（或用户批准过的策略）。决定先持久化，再由唯一的发件箱用该提案的幂等键执行；效果工具不进入模型的工具目录。
- **单一工具闸门**：所有工具调用都经过 `ToolGate`，账本经由结果观察者记录，不另开第二条执行路径。
- **Router 是唯一的信道写入者**：worker 的推理过程和工具调用不外泄到信道。
- **账本只追加**：每个效果都能追溯到 trigger、session、action_id、批准人。
- **默认不变**：`cmd/miniloop` 和 Python 运行时的默认行为不受 demo 影响；新能力一律需显式启用并如实标注状态。

## 7. 明确暂不做

- 不同时铺多个信道，也不做浏览器自动化和语音。
- 不做 Router 分片、A2A、端侧 daemon（BYOM）。
- 不为 demo 引入 Temporal、PostgreSQL 或向量库；这些都需要先审批依赖。
- 不给 demo 补 Python 对等实现（Go addition 已在 parity matrix 中标注）。

## 8. 待拍板

已拍板（2026-11-01）：

- 部署位置：先部署在用户自己的 Mac 上；
- 信道：暂不接真实信道；
- 集成：走 MCP，先模拟。

仍待拍板：

1. 真实邮件和日历 MCP server 选哪家：Gmail 加 Google Calendar，还是 IMAP 加 CalDAV？用现成 server 还是自写适配器？
2. 在 launchd 下跑真实模型时，凭据是否统一放进 macOS Keychain？
3. 自动批准策略的范围：只限「回复经理的确认类邮件」，还是允许按收件人或类别配置？
4. 以后接真实信道时选哪个：Telegram、飞书，还是企业微信？
