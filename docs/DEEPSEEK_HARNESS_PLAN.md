# 对照 DeepSeek Harness 改进 mini-loop：计划

参考对象：[deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（`dsh`，MIT，developer preview）。
约 38 万行 TypeScript / 49 个 package，基于 Cordis 的「一切皆插件」架构。
本文只整理计划，不含实现。每条按本仓库惯例给出**原因 / 背景 / 改进点**，外加落地位置与验证方式。

---

## 0. 先说结论：哪些地方我们已经对齐

先划掉不必做的，避免把已有能力当成缺口重做一遍。

| dsh 能力 | mini-loop 现状 |
|---|---|
| 能力接缝（seam）＝定义 / 实现 / 消费者三角 | `harness.py` 的 `Harness` 值对象已经把 14 个接缝收成一个值，`derive()` 保证新接缝自动到达每个构造点 |
| 子进程环境隔离（丢弃 `*KEY*`/`*SECRET*`/`*TOKEN*`） | 已有且更严：`tools.py` / `background.py` / `mcp.py` 走 `secrets.scrub_env`，`ast_context.py` 直接用白名单 |
| 工具输出上限 | `OUTPUT_CAP` / `READ_CHAR_CAP` / `MAX_BASH_CAPTURE`，且 140–169 轮已逐条堵过「有界输出 ≠ 有界工作量」 |
| 沙箱 / 审批 / 权限规则 | `sandbox.py` + `approvals.py` + `permissions.py` |
| 并行工具调用 | 已实现 parallel-safe 并发调度 |
| MCP / skills / cron / 工作区 / 压缩 | `mcp.py` `skills.py` `cron.py` `worktrees.py` `compaction.py` 都在 |
| 机械化校验器 | `verify_guards.py`（247 变异）+ `verify_scans.py`（19 扫描）+ `test_timing_safety.py` |

所以计划的重点不是"补插件数量"，而是补**几条我们确实没有的语义保证**。

---

## 一期（P0）：四条语义保证，各一轮

### P0-1 溢出存储（spill）：截断不再等于销毁

**原因**：140/151/164–169 这几轮把每条输出路径都加了上限，但上限的实现方式是**丢弃**——`tools.py` 取 head/tail 拼一条 `[... N characters capped at 50,000]` 的提示。模型被告知"内容被截断了"，却没有任何办法拿回被截掉的部分。上限保住了上下文预算，代价是数据不可恢复。

**背景**：dsh 把这件事拆成一个独立接缝（`docs/subsystems/spill.md`）：`ctx.spillStore.saveText()` **原样**持久化全文，返回一个不透明 locator、精确字节数、以及后端自己给出的 `retrievalHint`；消费者是 `tools/post-execute` 上的策略插件，超过 `maxInlineBytes` 时把结果换成"预览 + spill 引用"。三个细节值得照抄：
- 落盘用私有目录（0700）+ 随机名 + `open(path,'wx',0o600)` 独占创建，避免被预埋的符号链接改写目标；
- locator 对消费者**不透明**——本地后端渲染成路径，远程后端可以是 URI，所以提示词里不写死"用 read 打开"，而是渲染后端给的 `retrievalHint`；
- 策略层是 **best-effort**：保存失败就保留原来的内联结果，绝不把一次成功的工具调用变成 `isError`。

**改进点**：新增 `mini_loop/spill.py`（`SpillStore` 协议 + `LocalSpillStore`），接入 `Harness`；`tools.py` 的截断点改为"预览 + locator + 取回提示"。截断语义从「丢弃」变成「转存」。

- 落地：`mini_loop/spill.py`、`mini_loop/tools.py`、`mini_loop/harness.py`
- 验证：新增 `tests/test_spill.py`；变异「spill 保存失败时把工具调用变成错误」「locator 用可预测文件名」各须被命名测试抓到

### P0-2 工具执行管线分层：deny 不可被后续 hook 覆盖

**原因**：现在的扩展点只有 `Hook.before_tool` / `after_tool` 两段。`before_tool` 返回字符串即拒绝，但拒绝结果随后仍然流经 `after_tool`，而 `after_tool` 返回字符串就会**替换输出**——也就是说一个排在后面的 hook 可以把一次拒绝改写成一次成功。策略的单调性没有被结构保证，只靠约定。

**背景**：dsh 的管线（`docs/tool-execution-pipeline.md`）分了四层，职责互不重叠：
1. `tools/pre-execute` waterfall：hook、权限、沙箱，可 allow / deny / **ask**；
2. **monotonic guards**：只能 deny 或弃权，不能放行，身份受保护——这一层专门放"绝不能被重排的所有者策略"；
3. `tools/execute` 环绕层：超时、重试、指标；
4. `tools/post-execute`：accept / block / replace / **附加上下文**。
另外两个我们没有的概念：`additionalContexts`（工具结果记录之后，按 FIFO 注入一条 user message，用来在不破坏 call/result 相邻性的前提下补充上下文）和 `tools/result`（**只读**通知，拿到冻结后的权威结果，不能再改）。

**改进点**：`registry.py` 把 hook 链拆成 `pre` / `guard` / `around` / `post` 四段 + 一个只读的 `on_result`；guard 层的返回值域收窄成 `deny | None`，从类型上就不可能放行；deny 之后 `post` 只能观察不能替换。

- 落地：`mini_loop/registry.py`、`mini_loop/permissions.py`
- 验证：变异「让 post 段能改写一条 deny 结果」「让 guard 段能放行」须被抓到

### P0-3 「模型可见即已入账」变成运行时断言

**原因**：`agent.py:1116` 的注入器直接 `self.messages.extend(...)`，压缩、steering、修复工具调用也都在内存列表上动手。日志和模型实际看到的内容之间没有任何断言把它们绑在一起，只有 `fake_llm.validate_transcript` 这个**测试替身**在检查形状。任何一条新的注入路径都可能悄悄地让"模型看到了但日志里没有"，而且不会有任何东西报警——这正是我们 144–160 那批「新路径继承约束」缺陷的同一族。

**背景**：dsh 把这条写成架构级规则："**Model-visible means logged.** 任何进入模型请求的内容都必须能从日志重建，并有一条运行时不变量断言它。"（`docs/architecture.md`）推论也写死了：新增一种模型可见输入 ⇒ 必须新增一种 session event，从日志渲染，而不是塞进内存。

**改进点**：在请求组装点加一条不变量——把即将发给模型的 message 序列与从日志投影出来的序列比对，不一致就报"哪一条不可重建"。默认开启，可配置关闭（性能兜底）。

- 落地：`mini_loop/agent.py`、`mini_loop/storage.py`
- 验证：变异「注入器绕过日志直接 extend」「压缩后不落盘」须被抓到

### P0-4 自动续跑权限不进快照

**原因**：我们有 `cron.py` 定时唤醒、`workflows/` 自动推进、`background.py` 后台任务。一个会话被 resume 或 fork 之后，这些自动机制会**直接接着跑**——恢复一个旧快照就等于重新授权了一次无人值守的自动执行。

**背景**：dsh 的 goal 域把这件事拆成两个正交状态（`docs/subsystems/goal.md`）：durable 的 `phase`（active/paused/blocked/complete）回答"目标发生了什么"，而 process-local 的 `activation`（armed/disarmed）回答"续跑消费者现在可不可以再开一轮"。关键在于 **activation 被刻意排除在持久化重放之外**，所以 resume 和 fork 之后必须先有一次人工授权的 `resume` 才能自动干活。这是把"曾经被授权"和"现在被授权"分开——正是我们 157/158/161 轮反复处理的 held vs once-held 缺陷类。

**改进点**：给自动续跑通道加一个不落盘的 armed/disarmed 位；`restore()` 一律恢复成 disarmed，需要显式命令重新 arm。

- 落地：`mini_loop/session.py`、`mini_loop/cron.py`、`mini_loop/workflows/service.py`
- 验证：变异「restore 后仍是 armed」「activation 写进快照」须被抓到

---

## 二期（P1）：让上下文管理从启发式变成可测量

### P1-1 token 计量以 provider usage 为锚点

**原因**：`compaction.estimate_tokens` 是纯启发式，压缩触发点因此是"猜的"。猜高了浪费上下文，猜低了在真正溢出时才发现。

**背景**：dsh 的 `ctx.tokenMeter`（`docs/subsystems/token-meter.md`）：当最近一次成功请求的**规范化请求信封相同**且其 total 不低于该次的全量启发式锚点时，直接复用 provider 返回的 usage 作为 baseline，否则退回启发式；再用带符号的 `surfaceDeltaTokens` 表示相对锚点的增减（增和减都要能表示）。每个 surface 节点单独定价，`logRevision` 记录这次测量消费了多少条持久事件——测量结果是**不可变快照**，不会随底层重放推进而变。

**改进点**：`metering.py` 记录每次成功请求的 usage 与请求信封指纹；`compaction` 优先用锚点重定价，锚点不可用时才用启发式，并把用了哪种写进事件。

- 落地：`mini_loop/metering.py`、`mini_loop/compaction.py`

### P1-2 先剪枝、再测量、能不摘要就不摘要

**原因**：我们已有 `microcompact` / `tool_result_budget`，但"剪枝"和"摘要"之间没有**顺序与再测量**：一旦触发压缩就走到摘要，即使只剪掉几条巨型工具结果就已经降到阈值以下。摘要是有损且要花一次模型调用的，能不做就不该做。

**背景**：dsh 的顺序是：压力在 `agent/pre-step` 触发 → 先调可选的 `ctx.toolResultPruner`（确定性的 head/middle/tail 剪枝）→ 经 `ctx.tokenMeter` **重新测量** → 只有还超才做区间选择与摘要。而且剪枝的记法很讲究：**追加一条新的 pruned tool-result 事件去遮蔽旧事件**，`PrunedEntry` 同时记 `originalSeq` 和 `replacementSeq`——全保真原件永远留在日志里。区间边界保证 tool-call/result 配对，但**不保证整轮对齐**，所以一个超大 turn 里较早的已关闭 step 也可以被压。

**改进点**：把压缩改成两级；剪枝以追加遮蔽事件实现而非原地改写；剪枝后重新测量，达标即返回、不进摘要。

- 落地：`mini_loop/compaction.py`、`mini_loop/storage.py`

### P1-3 只有上下文真的变小了才重试

**原因**：`recovery.py` 在请求失败后重试。如果失败原因是上下文溢出，而这一轮压缩什么也没压掉，重试就是同一个请求再发一次——一个必然失败的循环。

**背景**：dsh 的规则很干脆：`agent/request-error` 只有在 **surface replacement generation 前进了**（剪枝或摘要确实换掉了内容）时才返回 retry action；否则原始错误保持权威。取消永远优先。恢复发生在"失败 step 已关闭、失败 turn 尚未关闭"之间，重试开的是一个全新 turn。

**改进点**：给 surface 加一个单调 generation 计数；`recovery` 的重试判据从"发生了压缩尝试"改成"generation 前进了"。

- 落地：`mini_loop/recovery.py`、`mini_loop/compaction.py`
- 验证：变异「压缩没生效也重试」须被抓到

### P1-4 goal 域：同会话目标 + 轮次上限 + CAS

**原因**：`tasks.py` 管的是待办清单，`stuck.py` 管的是卡住检测，但"这个会话正在追求的那一个目标"没有第一类表示。于是自动续跑没有可审计的预算，也没有 blocked 的机器可路由原因。

**背景**：dsh 的 goal 是**状态，不是调度器**：日志是唯一真相，durable 的 `goal/change` 事件带完整快照或 clear 墓碑；`GoalRef{id, revision}` 做 compare-and-set，每次成功变更 revision +1；`maxGoalRounds` 封顶已准入的续跑轮数，**只有目标来源的 user message 才消耗轮次预算**（同会话里的人类插话不算）；`blocked` 带一个稳定的 kebab-case `code`（给路由）加一段自由文本（给人和模型）。重放会拒绝非正轮次、跳号、过期 revision、已停止相位和超额。

**改进点**：新增 `mini_loop/goals.py`，与 P0-4 的 activation 合并成一套；`/goal` 类人类命令 + 模型可见工具。

- 落地：`mini_loop/goals.py`、`mini_loop/builtins.py`

### P1-5 plan mode：软引导，且不动工具目录

**原因**：现在没有"先出方案、人确认后再动手"的一等状态。

**背景**：dsh 的做法（`docs/subsystems/plan.md`）有三点值得抄：
- `plan/mode` 是 **log-only、整值替换**的 session event，从不进模型 transcript；当前状态永远是日志的纯 fold，所以 resume / fork / 压缩后都能无镜像恢复；
- **软引导**：plan mode 只加一段 `plan:policy` 提示词，真正的限制由 sandbox 和审批策略**各自独立**执行，两边都不读 plan 状态；
- `exit_plan_mode` **在 plan mode 关闭时依然注册**——进出 plan mode 只改提示词、不改请求里的工具目录。对我们有直接的实际价值：工具目录一变，`caching.py` 的前缀缓存就会失效。

**改进点**：新增 `mini_loop/plan_mode.py`；退出通过 `approvals.py` 走人工确认，"继续修改"实现为一次带反馈的失败调用而不是静默退出。

- 落地：`mini_loop/plan_mode.py`、`mini_loop/prompts.py`、`mini_loop/approvals.py`

### P1-6 工具并发：exclusive 屏障 + 有界滚动池 + 启动前重分类

**原因**：我们已经并发执行 parallel-safe 工具，但缺三样：显式的 `exclusive` 屏障、并发上限（有界滚动池），以及**启动前重新分类**——一个调用在排队期间，世界可能已经被前面的调用改变了。

**背景**：dsh 的分类是 `ToolExecutionMode = {kind:'parallel'} | {kind:'exclusive'}`，调度是"屏障 + 有界滚动池，start 前重新分类"；结果按**模型顺序**回收（`next model-order result ready` → ordered post），执行乱序、记录有序。

**改进点**：`registry.py` 增加 `execution_mode(call)`；调度器实现屏障 + 上限；`post` 段严格按模型顺序执行。

- 落地：`mini_loop/registry.py`、`mini_loop/agent.py`

---

## 三期（P2）：扩展面

### P2-1 每模块自带不变量伴生 + 第三个校验器

**原因**：我们的三件仪器都是**静态 / 测试期**的（变异、扫描、锚点新鲜度）。运行时没有对应物。

**背景**：dsh 的 `ctx.invariants`（`docs/subsystems/invariants.md`）要求**每个 package 发布一个 `./invariant` 伴生插件**，按自己的 npm 名注册检查；失败抛出带 `packageName` 的 `InvariantError`。约定写得极其克制——检查只能断言**权威事件流或可变数据**，绝不断言"某个服务/方法存在"。真正对我们有启发的是 `verify-package-invariants`：它机械地拒绝生成的占位、**没有解释的空实现**（空实现必须以 `No runtime invariant:` 开头并说明本包为什么没有可检查的东西）、忽略 reporter 的非空实现、注册名写错、导出/依赖装配不全。

**改进点**：新增 `mini_loop/invariants.py` + 每个模块一个 `_invariant()`；新增 `tools/verify_invariants.py` 作为第三件仪器，机械拒绝无解释的空实现。P0-3 那条断言是它的第一个住户。

### P2-2 子智能体提供者接缝

`teams.py` 目前只有进程内一种。dsh 的 `ctx.subagents` 背后可以是 spawn-in-process、fork-in-process、外部 SDK，甚至**另一个产品**（`subagent-claude-code`、`subagent-codex`、`subagent-acp`）。同时把 lineage 明确成**数据**（`parentSession`、`delegationDepth`、`subagentDepth`），而不是可见性结构——作用域只有两级且平坦，子智能体不继承父作用域。

### P2-3 持久终端（PTY）/ LSP / Code Mode

三个我们完全没有的模型可见能力。优先级排序：LSP（`ast_context.py` 已经打好底子，接 LSP 收益最直接）> 持久终端 > Code Mode。Code Mode 的核心思路是把 `run_code` 当作一种**传输**：模型写一段程序，程序里的每次子调用仍然完整走一遍工具管线（带父 token、记 `tool/code-dispatch`、拒绝以绑定异常返回）——不是绕过策略的后门。

### P2-4 配置分层：profile / bundle / patch + `--dump-config`

dsh 的启动树由有序层叠加而成：bundle 按顺序 → profile 的 `cordis.patch.yml` → home 级 → `--patch` 覆盖；每一行都有 id，上层可以整行替换。`dsh --profile web --dump-config` 打印机器实际启动的树。对我们最有价值的是**"打印你实际跑的是什么"**这件事本身。

### P2-5 会话查询与跨会话引用

`ctx.sessionQuery` / `ctx.sessionReferenceResolver`：读取、trace、过滤、搜索，以及跨会话快照准备。我们已有 SQLite 存储，缺的是查询面和模型可见的检索工具。

---

## 四、防御性模式：一轮专门的审计

dsh 的 `docs/defensive-patterns.md` 是"已经踩过的坑"清单。逐条对照我们：

| 规则 | 我们的状态 | 动作 |
|---|---|---|
| 正交结果各自独立上报（`timedOut` / `signal` / `exitCode` 可同时成立） | `CommandResult` 重构后需核对是否把 timeout 嵌在某个分支里 | **审计一轮** |
| 公共契约两侧都要守（适配器可能 throw 也可能发 error finish，运行时须归一） | `transport.py` 需核对是否归一 | **审计一轮** |
| dispose 要跑到静止，不只是发出请求（kill → await done；**先关监听器再 kill**） | 169 轮已加有界 join，但"先关监听器"这一半没做 | 值得一轮 |
| 分发器要吞掉回调异常（一个坏订阅者不能拖垮生命周期） | `Hooks` 链与事件订阅需核对 | 审计 |
| 不给不可信输出环境变量与可预测路径 | 已做（scrub_env / 白名单） | ✅ |
| 链状路径用 `lstat` + `unlink`，不对可能是符号链接的路径递归删 | 工作区回收路径需核对 | **审计一轮** |

---

## 五、明确不做

- **不移植 Cordis**。它的收益（插件树、可逆 effect、HMR）建立在 TS 生态和 38 万行体量上；mini-loop 一万九千行 Python 用 `Harness` 值对象已经拿到了接缝的主要好处。
- **不改 monorepo / 不做 49 个包的拆分**。包边界的价值在多人并行开发；我们的瓶颈不在这里。
- **不重写 Web 前端**（dsh 的 client 有 7.3 万行）。
- **不引入双语文档生成体系**（i18n pairing、生成式目录）。但 dsh 的 `gen-doc-graphs.ts`（从源码生成 Mermaid 生命周期图与接缝图，并有 `verify` 门禁保证不过期）这一件值得单独考虑——它和我们的锚点新鲜度检查是同一种思路。

---

## 六、执行顺序

```
一期  P0-1 spill  →  P0-2 管线分层  →  P0-3 入账不变量  →  P0-4 activation
二期  P1-3 重试判据 → P1-1 token 锚点 → P1-2 两级压缩 → P1-6 并发 → P1-5 plan → P1-4 goal
审计  正交结果 → 契约归一 → 链状路径
三期  P2-1 不变量注册表 → P2-2 子智能体接缝 → P2-3 LSP → P2-4 配置分层 → P2-5 会话查询
```

一轮一条，沿用现有交付形态：行为探针 → 修复 → 测试 → 在 `verify_guards.py` 加变异并逐条确认被抓到 → `HARDENING_NOTES.md` 记一条 → 全量测试 + `verify_scans` + 全量变异扫描 → 原因/背景/改进点报告。
