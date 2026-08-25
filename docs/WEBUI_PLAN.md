# Web UI 计划：目标与覆盖矩阵

目标（/goal）：参考 DeepSeek Harness 的 UI 功能，为 mini-loop 构建一套
覆盖全部功能面的 Web UI。

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

## 覆盖矩阵完结：全部功能面有 UI 承载，零留待项。
## （workflows 为实验特性、默认不注册且无 HTTP 面——按"UI 只消费既有
## API"原则排除，待其转正时随 API 一起接入。）
## 后续演进由使用反馈驱动，不再按轮排队。
