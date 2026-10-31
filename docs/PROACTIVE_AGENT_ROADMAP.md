# mini-loop 演进为主动个人助理：Go 优先实施路线

> 状态：路线草案 + 第一个可运行 demo（2026-11-01）
>
> 基线：mini-loop `c65073a`；demo 代码在 `go/assistant`、`go/cmd/miniloop-assistant`
>
> 场景：个人助理（邮件、日程、例行事务），不是研发 Super-agent

## 结论先行

主动个人助理需要六个部件：常驻运行的「自己的电脑」、应用集成、记忆、routine/触发器、审批闸、审计账本。mini-loop 的 Go 运行时已经具备其中最难补的治理部分：带风险分级的 `ToolGate`、权限模式、action journal、SQLite 状态、owner 记忆和 typed extension tool。缺的是「常驻 + 事件驱动 + 对外信道」这一层。

本轮已经用 Go 做出一个 demo：一个只监听回环地址的守护进程，加一个模拟信道网页。它把六个部件串成了一条闭环：

> 事件 / routine → SQLite 触发器 → 规则快路径 → Thread 订阅的 worker → 带闸门的连接器工具 → 待批准提案 → 人工批准 → 原子执行 + 账本

进程被 `kill -9` 后重启，状态可以恢复。后续路线的顺序：

1. 硬化持久底座；
2. 把审批和账本做成可上线的形态；
3. 接一个真实信道；
4. 接真实邮件和日历，先读后写；
5. 增强主动性（LLM Router、投机执行、Watchdog）；
6. 做长期记忆。

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
| **Own computer** | `sqlitestate`（会话、消息、事件、actions、approvals、leases）；`RestoreScheduledSession`。但 `cmd/miniloop` 不打开 SQLite | 常驻守护进程；`assistant.db` 和 `agent.db` 双库；重启时把中断的触发器重新排队；Thread 订阅的 worker 按需恢复 | Linux 沙箱（Seatbelt 只支持 macOS）；Secrets 接线；进程监管和备份；多进程领取 |
| **Integrations** | typed extension tool；MCP 只有 stdio，风险一律固定为 `external` | 模拟的邮件和日历连接器：3 个只读工具 + 2 个 `propose_*` 起草工具；不提供任何「发送」工具 | 真实连接器、MCP HTTP/OAuth、凭据引用、按工具的读写清单 |
| **Memory** | `memory.Store` + `remember`/`recall`（词面检索，按 owner 隔离） | 首次启动写入经理和偏好；worker 先 recall 再判断 | 链式召回、夜间整理、结构化画像、语义检索 |
| **Routines** | cron：JSON 文件、进程内 ticker、重启后不自动启用、没有补跑 | 每日 routine：时段领取和入队在同一事务里完成，保证只触发一次，当天错过的补跑一次；事件触发器；规则快路径；Thread 订阅 | LLM Router、批处理、webhook 签名、多个 routine 的编辑 UI |
| **Approval gates** | `ApprovalBroker`（进程内，300s 超时，重启后作废） | 用持久的提案发件箱代替「挂起的回合」：worker 只能起草，人工决定和效果在同一事务里提交，重复批准返回 409 | 审批推送到手机；按收件人的自动批准策略；提案过期 |
| **Audit log** | action journal、事件、trajectory | 只追加的账本：trigger → 规则 → 路由 → 工具调用（含 action_id）→ 提案 → 决定（谁批准）→ 效果 | 保留和导出策略、成本统计、「你不在时发生了什么」摘要 |

本轮 demo 的验证（`go/assistant`、`go/cmd/miniloop-assistant`）：

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

### D0 — 可运行 demo（本轮已完成）

见第 3 节。边界：模拟信道、模拟数据、无认证、只监听回环地址、离线脚本模型（可选真实模型）。

### P1 — 持久底座硬化（Own computer）

- 给守护进程加进程监管：launchd 和 systemd 单元、Linux 容器镜像（非 root、只读根文件系统、数据卷）。容器同时补上 Linux 没有 Seatbelt 的沙箱缺口。
- 接上 `secrets.Registry`，让信道 token、连接器凭据被遮蔽并只按引用注入。修掉 launcher 不设置 `ManagerServices.Secrets` 的缺口。
- 触发器队列加通用租约（沿用 `sqlitestate/lease.go` 的条件 UPDATE 模式），为将来的多进程领取做准备；同时给 SQLite 加备份和账本保留策略。
- 验收：容器内 `kill -9` 和重启的回归脚本；租约过期后可以接管。

### P2 — 审批闸与账本可上线（Approval gates + Audit log）

- 把提案扩展成 typed 动作集合（发邮件、回复邀约、创建日程……），加上过期、撤回，以及给真实连接器用的外部幂等键。
- 给信道加认证（复用 `httpapi.TokenAuth`）。
- 策略即代码：用户批准的「自动批准规则」，例如「回复经理的确认类邮件」。Guardian 只能作为第二审批人，不能扩大权限。
- 账本按天汇总「你不在时发生了什么」，并支持导出。
- 验收：审批挂起期间重启，事后仍能批准；所有效果都能追到批准人或命中的策略。

### P3 — 一个真实信道（Channel）

- 抽出 `Channel` 接口：入站聊天 → Trigger；出站消息和审批卡片（带按钮）→ `DecideProposal`。
- 只接一个信道（Telegram、飞书或企业微信待定），保留模拟网页作为测试替身。
- 遵守 IM 回复不超过 100 字的约束；worker 永远不直接发信道。
- 验收：手机上完成「邀约 → 卡片 → 批准 → 日历更新」。

### P4 — 真实集成，先读后写（Integrations）

- 给 `go/mcp` 加 HTTP 传输和 OAuth，或者为邮件、日历写原生连接器（二者择一）。凭据只按引用使用，绝不进入模型上下文。
- 每个工具的读写风险由运维写的清单分类，不信 server 自报的 readOnlyHint；保留「未分类按 external 处理」的规则。
- 入站优先用 webhook（验证签名）；不支持时退化为轮询连接器。
- 验收：读收件箱不弹审批；任何发送都必须经过提案；trajectory 和账本里扫描不到凭据。

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
- **Worker 没有发送能力**：任何对外效果都只能通过提案加人工决定（或用户批准过的策略），并且和效果在同一事务里提交。
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

1. 第一个真实信道：Telegram、飞书，还是企业微信？
2. 部署位置：家里常开的机器还是云 VPS？这决定 P1 的容器和网络形态。
3. 邮件和日历走 MCP（HTTP + OAuth），还是原生连接器？
4. 自动批准策略的范围：只限「回复经理的确认类邮件」，还是允许按收件人或类别配置？
