# Web UI 计划：目标与覆盖矩阵

目标（/goal）：参考 DeepSeek Harness 的 UI 功能，为 mini-loop 构建一套
覆盖全部功能面的 Web UI。

**摘要（2026-08-28）**：R1–R7 保留已完成记录；根据实际使用反馈，新增
[R8 Activity](#r8-activity)：阶段摘要标题与工具语义展示，**尚未实现**。
现有工具折叠、状态徽章和 commentary 事件是基础，不代表已具备该能力。

## 设计原则（继承自 console 与 dsh 挖掘成果）

1. **自包含交付**：源文件分离（`mini_loop/webui/` 下 html/css/js），
   import 时组装为单页内联交付——CSP（仅 `unsafe-inline` 脚本）不动，
   无静态目录穿越面，无构建步骤。
2. **textContent-only**：没有 innerHTML/outerHTML/document.write/
   insertAdjacentHTML；安全测试逐字扫描源文件（对齐
   test_console_safety.py）。
3. **dsh 账本，不是事件汤**：span 配对成行（model_start/end、
   tool_use/result 各归并为一行带时长与状态）、#N 请求编号、子代理按
   depth 缩进、参考行（catalog/system/capability）折叠。
4. **只消费既有 HTTP API**：UI 不新增服务端能力；发现缺口时先在
   server 层补路由（各自带测试），UI 再接。

## 覆盖矩阵（loop 队列；完成一项勾一项）

### R1（本轮）：核心会话面
- [x] 会话列表：GET /sessions 轮询，activity 徽章
      （idle/running/awaiting_approval/stuck/error）
- [x] 新建会话：mode（interactive/auto/readonly）+ 可选 system
- [x] 实时账本：SSE 事件流 span 配对、深度缩进、流式增量文本、
      steer/compact 参考行
- [x] 作曲器：发消息（忙时自动转 steer）、cancel、fork、mode 切换
- [x] 审批面板：pending 列表 + allow/deny（含 approval_request 事件驱动刷新）
- [x] 健康栏：/healthz 的 model/authenticated/sessions
- [x] Token 存取（localStorage，与 console 同约定）

### R2：轨迹与历史（本轮完成）
- [x] 会话轨迹列表 + 打开 dsh 账本视图（复用 /trajectories/{id}/view）
- [x] 导出 json（jsonl 经同一 export 路由，UI 暂只给 json 按钮）
- [x] transcript 视图（含 epoch 选择——superseded 历史可读）
- [x] 会话删除（确认语明说：workspace 移除、录制保留归属主可读）

### R3：调度与自动化（本轮完成）
- [x] cron 列表/新建/取消/arm（新增会话作用域 HTTP 路由；HTTP 调度即
      人授权边、直接 armed；DISARMED 徽章 + Arm 按钮）
- [x] self_audit 报告视图（新增 GET /self-audit：认证部署下 owner 作用域、
      manager 级账本仅开放部署包含——观测端点不做跨租户侧信道）

### R4：技能与记忆（完成）
- [x] 技能目录视图（GET /sessions/{id}/skills，模型看到什么就显示什么）
- [x] 记忆列表（GET /sessions/{id}/memory：名称/类型/描述）
- [x] personal-skills preview/commit 流（Capture draft → 审看 → Commit，
      digest 原样回传——所见即所发布）
- [x] 记忆正文查看（GET /sessions/{id}/memory/{name}：正文写入时已 mask、
      且 runtime_facts 本就回喂给属主的模型——读者看到的就是模型看到的，
      不构成新披露）

### R5：自进化面板（本轮完成）
- [x] propose_improvement 触发（POST /sessions/{id}/propose-improvement，
      400 透传 git/验收拒绝）+ 提案 branch/diff/receipt 摘要展示
- [x] improvement_proposed 事件在账本可见（未知类型通渲染兜底）
- [x] paired benchmark 展示（Benchmark 页签 + POST /benchmark：**仅 fake**
      ——UI 演练仪器不花钱；真跑留在终端，花预算必须是显式的终端动作）

### R6：打磨（完成）
- [x] 真浏览器验收 ×2（Playwright：R1-R5 全流程 + R6 新面板逐一核验，
      控制台零错误）
- [x] favicon（内联 data: SVG，CSP img-src data: 本就允许）
- [x] 认证下 view/export：fetch 带 Authorization → Blob → window.open
      同进程 blob 文档——服务端看到认证请求，地址栏永远看不到 token
- [x] focus-visible 全交互件、prefers-reduced-motion、移动端纵排断点
- [x] console 去留：保留 `/` 作单会话开发台，页头与 /ui 互链

### R7：协作与状态面（完成——完结复查揪出的真实缺口）
- [x] 任务图页签（GET /sessions/{id}/tasks：文件后端只读视图，
      状态徽章/owner/blockedBy/worktree）
- [x] goal 徽章（GET /sessions/{id}/goal：objective/phase/轮次预算,
      blocked 显红；turn done 时刷新）+ plan-mode 徽章
- [x] teams 收件箱视图：先给总线加了**非消费性 `peek`**（read 的排空
      即投递契约，视图绝不能替 agent 收信），manager 走
      `peek_team_inbox`，Team 页签只读展示；突变守卫钉死"peek 不清空"。
      每个会话天生是一人团队（team_id = 自身 id，身份 lead），面板
      如实显示该身份。

### R8 Activity

**阶段摘要标题与工具语义展示：待实现；本次只记录计划，不修改运行时。**

#### 现状与证据

**[事实]** 复核日期为 2026-08-28，已提交基线为 `a3a0e22`，并检查了
当日工作区的相关实现。当前缺少“阶段标题 → 工具记录分组”的完整链路：

| 能力 | 当前实现 | 本轮确认的缺口 |
|---|---|---|
| 会话 activity | [app.js](../mini_loop/webui/app.js) 的 `activityLabel()` 格式化状态枚举 | 不是自然语言阶段标题 |
| 工具折叠行 | `onEvent()` 用 `tool_use.name` 作标题，详情显示已脱敏的 input 与 output | 未生成 Read / Search / List / Ran 等语义标签 |
| 进度与正文 | [agent.py](../mini_loop/agent.py) 已区分 commentary / final_answer；[transport.py](../mini_loop/transport.py) 已有 stream_id | 没有独立的 activity 标题事件与摘要来源标记 |
| 流式 thinking | `StreamingTransport.send()` 将 text / thinking 合并为临时 assistant_delta | 不能将这些混合增量直接当作公开 reasoning summary |
| 事件关联与存储 | `_send()` 带 message_id，工具带 span_id / parent_span_id；[session.py](../mini_loop/session.py) 统一脱敏、编号和分发 | 可复用现有通道，但还没有阶段分组与标题回放约定 |

**[事实]** Codex 固定提交 `758ef40f50c1a458425c7cfbf1eb12cbc07af0b0`
中，[TUI 的 on_agent_reasoning_delta](https://github.com/openai/codex/blob/758ef40f50c1a458425c7cfbf1eb12cbc07af0b0/codex-rs/tui/src/chatwidget/streaming.rs#L229-L271)
从 reasoning 摘要中提取加粗标题；[get_command](https://github.com/openai/codex/blob/758ef40f50c1a458425c7cfbf1eb12cbc07af0b0/codex-rs/core/src/tools/handlers/unified_exec.rs#L97-L142)
仍读取原始 `args.cmd`；[parse_command](https://github.com/openai/codex/blob/758ef40f50c1a458425c7cfbf1eb12cbc07af0b0/codex-rs/shell-command/src/parse_command.rs#L44-L84)
只提取用于展示的命令元数据。这里采用的是三者分离的设计，不声称已核对
iOS 客户端的具体分组实现，也不引入 Codex 私有协议依赖。

#### 目标与边界

**[建议]** 用户看到“检查水位持久化”等阶段标题，展开后看到
“Read watermark.go”“Ran go test …”；详情仍可查看脱敏后的原始参数、
结果、错误与耗时。阶段可以包含多个工具调用，不强求一条标题对应一个工具。

- 执行权威仍是 `ToolCall.name/input`、现有 hooks / guards / approvals / sandbox；
  标题和语义标签只供显示，不能修改命令、判定权限或充当执行成功的证据。
- 不把工具 schema 的静态 description 改成每次调用的标题，不向 tool input
  塞 UI 字段，不增加每个工具一次的 LLM 摘要请求。
- 第一版从**已公开的完整 commentary** 提取短标题，并提供确定性工具标签
  兜底；只有 provider 明确提供公开 summary 时才接入该来源。原始 thinking、
  signature 和不可公开内容不用于生成标题，不新增其持久化或披露路径。
- 标题不覆盖正文，不改变 commentary / final_answer 语义；审批等待、失败、
  取消等真实状态优先展示，不能被“正在检查”等文案遮住。

#### 分步执行队列

以下事件名与字段是**待实现的协议草案**，不是当前 API。

- [ ] **R8-1 展示契约与来源分流**：沿用 `session` / `message_id` /
      `agent` / `depth`、`stream_id`、`span_id` / `parent_span_id` 的关联，
      增加 `activity_id` 和独立的 `activity_update` 展示事件；约定
      `title`、`source`（public_summary / commentary / tool_fallback）及
      `provisional`。完整 commentary 的首句或首行作保守短标题；无可用文本
      时退回工具标签。若接入公开 summary，等待完整标题边界再更新；不依赖
      当前混合的 assistant_delta 猜来源。标题单行、限定长度、统一脱敏，
      格式异常不阻断工具执行。先补 agent / transport 的事件与测试，再接 UI。
- [ ] **R8-2 工具语义投影**：先映射 `read_file`、`glob`、`write_file`、
      `edit_file` 的已知参数；再对 `bash` 的简单 cat / sed 只读形式、
      rg / grep 搜索、文件列举作保守分类。命令替换、重定向、混合管道、
      解析失败或未知工具一律退回工具名或原命令预览，不运行解析出的内容。
      元数据由独立展示 helper 产生，不能复用为风险或免批判定。
      Requested / Running / Read / Ran / Edited 等时态随真实生命周期变化：
      `tool_use` 仅代表请求，拒绝或失败不得写成成功；没有真实 diff 证据时
      不显示增删行数。没有执行开始事件时保持 Requested，不能推测 Running。
      保留原参数详情，不以预览替换执行参数。
- [ ] **R8-3 阶段分组与交互**：在 `onEvent()` / `ledgerRow()` 上增加
      阶段折叠头和工具子行；按显式关联归组，不使用“最后一个全局标题”
      猜测并行调用的归属。新阶段只接纳对应调用，不重命名历史分组；同阶段
      多工具各自保留 span、状态和耗时。覆盖并行工具、子代理、会话切换、
      重试和取消；保持审批入口可见。旧事件或缺失元数据继续显示现有工具名。
- [ ] **R8-4 回放与验收**：标题增量保持 ephemeral，只有完整、脱敏、
      有界的展示快照随现有事件通道记录；不写入模型 transcript 或改变工具
      参数。按 seq / activity_id 幂等恢复分组；新 stream 替换旧临时标题，
      done / error / cancel 清理当前活动态，历史快照仍可读。历史事件缺少
      标题时确定性降级，不能在重连时另调模型补写过去的意图。持久性仍取决于
      实际配置的 StateStore / TrajectoryStore，不将 Null 存储描述为可恢复。

#### 验收门槛

| 验收项 | 必须证明 | 优先扩展的已有测试 |
|---|---|---|
| 执行不变 | 标题变化不影响 dispatch 收到的 name/input、命令、审批、结果；恶意标题不执行 | `tests/test_tool_pipeline.py`、`tests/test_tool_payload.py` |
| 来源与降级 | 无 summary 的 provider 正常工作；公开 commentary 与 thinking 不混用；坏格式和长标题安全降级 | `tests/test_streaming.py`、`tests/test_provider_fidelity.py` |
| 生命周期 | 多工具分组、并发、子代理、重试、切换会话不串标题；取消/拒绝/失败不显示成功 | `tests/test_streaming_failures.py`、`tests/webui_dom.test.cjs` |
| 安全与回放 | 秘密和 HTML 片段安全处理；临时增量不落盘；完整快照重连不重复，旧事件可读 | `tests/test_streaming.py`、`tests/test_event_stream.py`、`tests/test_webui.py` |
| 实际 UI | 折叠/展开、详情、审批、键盘焦点、窄屏可用；375/768/1024/1440 宽度复核 | 真浏览器验收，不能以静态扫描代替 |

每步先跑相关测试；实现完成后运行 `.venv/bin/python -m pytest -q` 和
`git diff --check`。包模块变化运行 `tools/verify_invariants.py`；扫描目标或
guard 行为/锚点变化分别运行 `tools/verify_scans.py` / `tools/verify_guards.py`
（均使用 `.venv/bin/python`）。按仓库要求同提交复核 README 架构基线；若
新增事件流影响图中数据流，再同步 Mermaid、边界说明与交互架构规格。
只有实际实现并通过对应验收后才能勾选，不能以本次计划入库代替完成。

## 完成范围与后续演进

R1–R7 的完成记录仅覆盖当时的功能面，不包含新增的 R8。后续需求继续由使用
反馈驱动，纳入上述队列后逐项验收。

原矩阵对 workflows 的排除是历史范围说明，不代表持续有效的默认状态；实际
启用与暴露边界以 [README 的 Runtime posture](../README.md#runtime-posture)
为准。本次 Activity 计划不改变任何功能默认值。
