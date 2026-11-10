# Agent Server 生产化执行计划

## 结论先行

mini-loop 的独立 Agent Server 已具备真实 Deep Agents 执行、异步任务、身份隔离和有故障实测的 Temporal 恢复机制。下一阶段应先把一类受控长任务运行到可测量、可恢复、可运营的生产水平，再按业务需要扩展代码执行、人工审批和托管平台功能。当前不能将本地恢复实验解释为生产高可用或容量承诺。

P0 已确定优先推进 Temporal Task 路径的生产化，保留标准 LangGraph Agent Server 作为独立的会话型服务。每个任务只由一个执行后端负责调度和恢复，不建立重复调度同一 graph 的第二套队列。

计划日期：2026-11-09。状态：**执行中：P1.5 受控注入与 P1.6 协议边界已通过；P1.4/P1.5 真实 provider 执行仍待授权**。2026-11-10 已将首个受控任务类型、Temporal 唯一主路径、当前负责人和本地离线运行环境写入 [P0.1 试点范围决策](../deepagents-server/docs/PRODUCTION_PILOT_SCOPE.md)，将负载、时长、状态、RPO/RTO、保留、费用和资源门槛写入 [P0.2 验收配置](../deepagents-server/docs/PRODUCTION_ACCEPTANCE.md)，并以 [P0.3 当前协议](../deepagents-server/docs/CURRENT_TASK_PROTOCOL.md) 固定唯一 Workflow、wire identity 与状态预算。[P1.1 关联合同](../deepagents-server/docs/TASK_OBSERVABILITY.md)贯通 request、Task、首/当前 Run 与模型 Activity attempt；[P1.2 指标合同](../deepagents-server/docs/TASK_METRICS.md)以 API/Worker 进程内 Prometheus exporter 覆盖接受/拒绝、健康、排队、attempt、终态观测、CAN 和状态大小；[P1.3 trace 合同](../deepagents-server/docs/TASK_TRACING.md)通过 Temporal SDK 的 LangSmith plugin 连接父代理、子代理、摘要与 Activity retry，并在 SDK enqueue 前删除内容型字段、保留经校验的 usage，缺失值明确为 unknown。[P1.4 验收合同](../deepagents-server/docs/PRODUCTION_PROVIDER_ACCEPTANCE.md)固定真实规划、文件、子代理、摘要、CAN、History、trace 和 cost 检查，且要求仓库外短期审批与 file-backed 凭据。[P1.5/P1.6 验收](../deepagents-server/docs/TASK_PROTOCOL_FAILURE_ACCEPTANCE.md)已用本地真实 Temporal、两个 API 和生产 Workflow 记录有界 429/超时/断连、响应丢失核对及取消竞争。当前没有 provider/trace/Task 目标或消费授权，远端 project 连通、实际 30 天 TTL 和真实任务 waterfall 尚无凭据，P1.4 及 P1.5 的真实 provider 部分不能勾选；具体目标供应商/地域、独立复核人和运营责任仍须在进入试点前定版，不承诺未经估算的日历工期。

## 当前基线与计划边界

| 项目 | 已有事实 | 后续工作不能据此假定 |
| --- | --- | --- |
| 源码基线 | P1.5/P1.6 验收审阅仓库 `36cb193`；独立模块当前 runtime 来源提交 `c65073a` | 其他 Python/Go 功能已经进入独立服务 |
| 固定依赖 | Deep Agents 0.7.25、LangGraph API 0.16.0、Temporal SDK 1.34.0 | 浮动官方文档中的能力全部适用于这些版本 |
| 标准入口 | Threads/Runs、后台执行、SSE、取消、Checkpoint、owner Store Memory；API/Worker 分离配置 | PostgreSQL/Redis 生产恢复和许可证条件已验收 |
| Temporal 入口 | Start 确认后返回 202；owner/key 去重；查询、取消、History；分段续跑及 Worker 接管 | 永久幂等、全局配额、通用审批、跨任务 Memory 或工具 exactly-once |
| 当前回归 | 2026-11-09 独立模块 `pytest -q -p no:cacheprovider`：599 passed，6.05 秒 | 本次已重新执行容器、故障注入或真实模型测试 |
| 历史故障证据 | 6 Task/3 owner/2 API/2 Worker 全栈冷停后完成；每 Task 4 次 CAN，30 份 History 无 WorkflowTaskFailed | 单机 SQLite、模拟模型的实验已证明数据库 HA、小时级任务或多机容量 |
| 执行与状态限制 | 无宿主 shell；Task 无跨任务 Memory；24 小时总期限、512 个父模型边界；压缩 checkpoint 384 KiB、CAN 信封 512 KiB | 24 小时已实测；父模型边界包含全部子代理调用；大文件可无限进入 History |

完整事实以[模块说明](../deepagents-server/README.md)、[当前交付记录](../deepagents-server/DELIVERY.md)、[需求验收矩阵](../deepagents-server/docs/TASK_ACCEPTANCE.md)和源码为准。历史证据保留其原始镜像、版本和实验范围。

[多任务与故障恢复设计](AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)继续提供详细协议、故障模型和明确标注的历史运行证据；其当前口径已与源码及 [P0.3 当前协议](../deepagents-server/docs/CURRENT_TASK_PROTOCOL.md) 对齐。当前 Worker 只注册唯一 `runtime.py:TaskWorkflow`，保留 `TaskWorkflowV2`/`task-v2` 持久标识，不支持已删除的 V1。[Agent Platform Roadmap](AGENT_PLATFORM_ROADMAP.md)面向原 runtime 的广义平台演进，本计划只覆盖独立 `deepagents-server/`。

## 对标对象与取舍

OpenAI Agents API 提供托管 Codex harness、持久会话、编排、上下文压缩、恢复和可选执行环境。应用仍承担业务接入和自定义 function handler；选择自托管环境时，还承担其生命周期。[OpenAI Agents API](https://developers.openai.com/api/docs/guides/agents-api/overview)、[Architecture](https://developers.openai.com/api/docs/guides/agents-api/architecture)

Managed Deep Agents 把 Deep Agents 与部署、Memory、sandbox、渠道、评测和 tracing 组织成托管产品；当前 Quickstart 标明 public beta，支持 LangSmith Cloud 与符合版本要求的 BYOC。不能把早期公告的部署范围当成当前限制。[MDA Quickstart](https://docs.langchain.com/langsmith/python/managed-deep-agents-quickstart)、[产品能力说明](https://www.langchain.com/blog/managed-deep-agents-is-now-in-public-beta)

对标重点是服务合同、恢复、执行隔离和运营闭环。第一轮不要求复制 MDA Compiler、Context Hub、渠道市场或完整控制台。标准 Agent Server 已提供的协议与调度应继续复用；自建能力必须对应明确业务缺口。上述官方资料核对日期为 2026-11-09，实施前再次核对所用版本、许可及接口。

## 目标等级与阶段依赖

| 里程碑 | 使用范围 | 必须完成 | 对外表述边界 |
| --- | --- | --- | --- |
| M1 内部受控试点 | 已知用户、固定模型与工具、低并发、可人工介入 | P0、P1，加 P2 中 Worker 恢复、备份恢复和兼容发布检查 | 只报告已测任务类型与负载；不宣称 HA |
| M2 生产长任务服务 | 在确定的部署与配额下持续承接任务 | P2、P3；完成故障、容量、真实模型和运行手册验收 | 发布实测容量、RPO/RTO、保留期及已知限制 |
| M3 业务执行型 Agent | 执行代码或修改外部业务系统 | M2，加所需 P4 分支 | 工具授权、审批、隔离和副作用逐项验收 |
| M4 托管产品 | 多项目、多版本、自助接入、渠道与运营 | M3 或对应受限产品基线，加选定 P5 项 | 不用一个能力矩阵宣称与商业平台全面等价 |

执行顺序为 **P0 → P1 → P2 → P3**。P4 设计可在 P1 后开展，但带副作用的能力上线依赖 P2/P3 的可靠性与容量合同。P5 按真实产品需求选择。每个阶段开始时指定实现负责人、验收负责人和运行环境；角色可以由同一维护者承担，但验收结果必须能由另一人按记录复现。

## P0 固定服务范围与验收合同

优先级 P0。依赖：无。交付位置以 `deepagents-server/docs/` 为主，不修改原 Python/Go 的默认运行行为。

1. 选定首个业务用例和主路径。后台独立长任务优先 Temporal；依赖现有 Thread/SSE/Store 的会话业务评估标准 Agent Server。切换选择时同步调整后续验收矩阵，不强行把两条调度链合并。
2. 记录目标部署、数据驻留、持久存储、可用许可证、认证方式、模型和调用预算；明确由谁维护 Temporal 或 PostgreSQL/Redis、入口 TLS 和备份。
3. 冻结提交、去重、未知结果、取消、超时、失败、结果与事件保留合同。区分 Task、Run、Activity attempt；定义保留期过后同 key 的行为。
4. 明确工具集合和数据范围。第一阶段保持虚拟文件；Memory、shell、外部写操作、审批和渠道分别作为后续范围。
5. 对齐过时设计描述，记录唯一 Workflow 实现、当前协议和状态大小限制；保留历史证据，不改写历史通过项。

以下是**验收配置草案**，用于形成可运行的测试集，数值不代表当前能力或用户已批准的 SLA。P0 结束前填入环境配置并定版。

| 验收项 | 初始建议或必须确定的内容 |
| --- | --- |
| 功能负载 | 从 3 owner、2 API、2 Worker 开始，分别验证空闲、满槽、排队和突发 |
| 真实长任务 | 覆盖约 5 分钟、30 分钟、2 小时的任务；记录真实步骤与等待，不能仅靠 sleep 冒充长推理任务 |
| 状态规模 | 覆盖消息、摘要、子图、文件增长，以及接近和超过 checkpoint/CAN 限额 |
| 服务延迟 | 定义提交确认、状态查询和取消确认的 p95 目标；分别统计排队时间、执行时间和完成时间 |
| 恢复 | 按 Worker 丢失、后端不可用、数据库故障分别填写 RTO；区分服务恢复和任务恢复 |
| 数据损失 | 分别填写已接受任务、checkpoint、结果、产物的 RPO；基于复制与备份方案确定 |
| 持续运行 | 先完成小规模 24 小时服务观察，再按目标并发压测；服务观察不能替代单任务时长验证 |
| 费用与资源 | 填写每次真实模型测试的费用上限、允许模型、停止条件和资源预算；未确定前只做离线验收 |

完成条件：主路径、范围、负责人、环境、指标、预算与保留期都有明确记录；每项必需能力能映射到后续任务和可执行验收。无法确定的指标保留为未满足条件，不用默认值伪装完成。

## P1 打通真实模型与可观测性

优先级 P0。依赖：P0。主要代码位置为 `task_server/http.py`、`service.py`、`worker.py`、`runtime.py` 及 `tooling/acceptance/`；标准入口使用其原生 Run 和 tracing 接口。

| 任务 | 实施内容 | 验收条件 |
| --- | --- | --- |
| P1.1 任务关联 | 贯通 request、Task、首 Run、当前 Run、Activity attempt、owner、镜像/协议版本；身份来自受信任服务上下文 | API 日志可定位模型活动和结果；跨 CAN、重启仍能关联 |
| P1.2 基础指标 | 记录接受/拒绝、排队、运行、终态、重试、超时、取消、CAN 次数和状态大小；区分 API 与 Worker 健康 | 面板或机器可读查询可以定位“服务活着但任务不前进” |
| P1.3 Trace 与用量 | 接入既有可用 tracing；记录父代理、子代理、摘要及重试用量，缺失值标记 unknown；定义脱敏和保留策略 | 一项任务可以解释主要模型/工具耗时；未知用量不记为零；日志与 trace 不泄露测试凭据 |
| P1.4 真实 provider | 建立固定用例及结果校验，覆盖规划、文件、子代理、摘要和跨段恢复；由预算控制执行 | 真实模型用例有输入、预期状态、实际产物、费用和失败分类；无需人工猜测是否成功 |
| P1.5 失败处理 | 分开验证可控 429/超时/断流注入和真实 provider 集成；记录 SDK、Activity 的重试叠加 | 重试次数、总等待、重复计费风险和终态均可解释；无无限重试 |
| P1.6 协议边界 | 实测 Start 成功但响应丢失、并发同 key、冲突内容、取消/完成竞争与越权查询 | 同一业务提交不会因盲目重试产生两个任务；不确定结果可核对；越权拒绝 |

模型质量验收使用业务结果或状态检查，例如文件内容、任务完成条件和允许的工具行为；不以固定自然语言输出逐字匹配。重放测试使用保存的 History，要求零模型调用。故障注入与真实模型结论分别记录。

完成条件：选定主路径形成“提交 → 执行 → 结果 → 诊断”的完整证据；真实模型集成和离线失败测试各自通过，成本与异常都能归因。

## P2 生产持久性与安全发布

优先级 P0。依赖：P1。目标是在选定环境证明恢复能力，并给出可执行的升级与回退方法。

1. **生产数据底座**：Temporal 路径连接目标托管或自管集群，验证原生 TLS/认证/namespace 权限、History 保留和后端持久性。标准入口则完成带适用许可的 PostgreSQL/Redis 部署验收。仅对选定路径设必过门禁。
2. **故障域与入口**：在目标要求涉及多机时将副本分散到相应故障域，配置负载均衡、readiness 摘流和停止窗口；确认探针分别代表什么，不能把 DescribeNamespace 成功解释为 Worker 或模型可用。
3. **版本合同**：保存应用镜像、Workflow/graph、状态格式、工具协议和模型配置引用。先建立兼容 History replay 与旧池排空；业务确需不兼容升级时再选择并验证版本路由或显式迁移。
4. **停止与接管**：分别测试优雅排空、超期硬退、强杀、耗尽原重试预算，以及 API 与 Worker 分批替换。记录任务是否恢复、在哪个版本恢复和用了多少 attempt。
5. **备份与恢复**：在隔离环境从备份恢复，核对已接受任务、终态、产物与权限映射；测量 RPO/RTO。声明可恢复范围，记录密钥和数据丢失的处理方式。
6. **数据生命周期**：明确删除、保留、归档与恢复窗口。删除权限覆盖结果、事件和产物；避免已删除任务通过其他入口重新暴露。

固定 Deep Agents 摘要接点和 checkpoint 序列化是升级风险点。依赖升级必须重新执行子图、摘要、CAN、冷重放及旧任务完成检查；不能只看 import 或单元测试通过。

完成条件：选定部署上的故障矩阵通过，RPO/RTO 有原始时间线，升级与回退演练可复现；旧任务始终有明确的处理方式。单纯重启原容器或复用未损坏的本地 SQLite 不计为数据库灾备验收。

## P3 全局配额与容量

优先级 P1。依赖：P2；可观测基础来自 P1。避免以增加 Worker 数量替代上游模型和数据库容量治理。

| 任务 | 实施内容 | 验收条件 |
| --- | --- | --- |
| P3.1 全局准入 | 定义集群与 owner 的并发、排队和速率上限；持久接受后的任务只排队、取消或显式失败，不静默丢弃 | 多 API 副本同时提交仍遵守上限；拒绝/排队语义与 P0 合同一致 |
| P3.2 配额权威 | 先评估现有后端原语；需要独立配额存储时定义 reservation、释放、过期和崩溃核对 | 配额与 Workflow Start 之间的任意失败都不泄漏名额或重复执行；不新增 graph 调度权威 |
| P3.3 成本预算 | 按任务/owner 约束模型请求、token 或费用预算，计入子代理、摘要和重试；定义估算与最终统计差异 | 预算耗尽进入可解释状态；并发调用与未知用量不会被当成免费执行 |
| P3.4 公平与背压 | 控制突发 owner、昂贵子代理及 provider 配额；实现有界等待和退避 | 一个 owner 不能长期挤占所有槽位；扩容不会持续放大 429 |
| P3.5 容量曲线 | 按低、中、目标、过载阶梯测量到达率、并发、队列年龄、p95/p99、内存、DB连接、重试和费用 | 输出“负载与资源 → 达标/不达标”的表格；瓶颈与停止扩容条件明确 |

扩缩容至少分开看 API 请求、待执行任务和 Worker 资源。用确定性负载测基础设施吞吐，用受预算约束的真实模型样本校准端到端表现，两种结果不混报。

完成条件：P0 的容量和延迟目标在持续负载下达成，过载会有界拒绝或排队，任务和数据不丢失；输出可运营的容量建议，而不是仅列 Worker 数量。

## P4 按业务接入执行环境与人工控制

优先级 P1，按用例选择。依赖：上线前完成 P2/P3。第一阶段的纯虚拟文件服务可以不启用这些能力；一旦业务需要，则相关安全与恢复验收必须先于上线。

### 代码执行与文件产物

- 选择适用 sandbox backend，定义每任务或每会话隔离、CPU/内存/磁盘/进程预算、网络出口、超时和清理。
- 将执行环境标识和生命周期纳入恢复合同：Worker 消失后重连、环境过期后恢复文件、取消后停止子进程。
- 将大文件产物放入适合的存储，以引用、校验和、owner、保留期和签名下载权限关联任务，避免持续把二进制文件塞进 History。
- 验收跨 owner 读写、宿主访问、网络限制、资源耗尽、僵尸进程、重启和垃圾回收；应用容器的非 root/只读根不能代替租户 sandbox。

### 持久审批与外部副作用

- 定义 waiting_for_approval/input、决策版本、授权主体、期限、重复决策、取消和恢复语义；等待释放模型/执行槽位。
- 审批绑定具体操作及参数摘要；参数改变使旧审批失效。审批可以批准操作，不能扩大部署权限。
- 为有外部写入的工具定义稳定业务操作 ID、回执查询和重试策略；不支持幂等的系统需要人工核对或补偿。
- 验收“外部动作已成功、回执尚未保存时 Worker 崩溃”、重复批准、跨 owner 批准及批准与取消竞争。不能宣称通用 exactly-once。

### 进度与会话控制

- 按产品需要增加生命周期事件订阅、Webhook 或流式进度，定义游标、去重、重连、慢消费者和事件保留。
- 明确持久事件、临时 token 输出与最终结果的区别；Temporal 的 History 分页不直接包装成 token 重放承诺。
- 若需在执行中补充输入，先定义排队、打断和状态修改的协议；继续会话与重试失败步骤必须可区分。

完成条件：每个启用能力都通过权限、重启恢复、取消和资源回收验收，并纳入既有 trace 与成本记录。

## P5 托管产品能力

优先级 P2。依赖：已经稳定运行的服务基线和实际接入需求。按用户使用频率与维护成本排序，不把全部能力设为 M2 的前置条件。

| 能力 | 建设重点 | 上线前验收 |
| --- | --- | --- |
| 项目与版本管理 | 声明式配置、构建产物、版本发布、配置校验与回退 | 可从固定源码重建；任务绑定版本；未通过校验不能发布 |
| 身份与凭据 | OIDC/OAuth、凭据引用、轮换、撤销、最小权限和访问审计 | owner 来自已验证身份；撤销和轮换不依赖修改 prompt；秘密不进入 History |
| Memory 与上下文 | 明确 owner/agent/thread 作用域、并发写、版本、删除和备份；配置与运行时知识分离 | 并发更新不无声覆盖；部署不擦除业务要求保留的记忆；Memory 不能授予权限 |
| 调度与渠道 | Cron、Webhook、消息平台的签名校验、去重、身份映射和发送回执 | 重复投递和重启不重复触发副作用；渠道取消与任务状态一致 |
| 评测与运营台 | 真实失败转回归样本；版本质量、成本、任务成功率和人工介入统计 | 模型、工具或配置升级可比较；隐私与保留策略覆盖评测样本 |

MDA Compiler、Context Hub 或完整自助门户应在上述能力确有重复接入需求时再建设；也可以采用现有托管产品。新依赖、外部服务或许可证按仓库约束另行确认，不因写入本计划自动获得安装或采购授权。

## 故障验收矩阵

每项记录版本、输入摘要、故障点、Task/Run/attempt、预期状态、实际状态、时间线和资源峰值。历史通过只能作为回归样本，目标环境仍需新证据。

| 场景 | 必须观察到的结果 | 阶段 |
| --- | --- | --- |
| 并发同 key 与不同内容 | 同内容复用、不同内容冲突、owner 不串用 | P1 |
| Start 已成功但响应丢失 | 核对原任务；重试不创建第二次业务执行 | P1 |
| 模型 429/超时/断流 | 有界重试、可解释费用和失败；与 SDK 重试区分 | P1/P3 |
| Worker 在模型 Activity 中消失 | 接管保持业务身份，允许的重试可观察 | P2 |
| CAN 前后崩溃 | 消息、文件、todos、子图与摘要按协议恢复 | P2 |
| 后端断连及数据库故障切换 | 接受合同、RPO/RTO 和重新连接符合目标 | P2 |
| 兼容及不兼容版本发布 | 旧任务继续、排空或显式迁移；无孤儿任务 | P2 |
| 备份恢复与保留期结束 | 可恢复范围可核对；过期键、事件与删除行为明确 | P2 |
| 多 owner 突发与配额协调进程崩溃 | 不超卖、不泄漏配额、公平性和等待有界 | P3 |
| checkpoint/文件/任务预算超限 | 可观察的失败或拒绝，无无限重放和失控增长 | P1/P3 |
| 外部动作完成后丢失回执 | 稳定操作 ID 核对或进入人工处理，不盲目重放 | P4 |
| 审批等待跨重启与重复决策 | 权限/参数版本保留，重复决策只生效一次 | P4 |
| sandbox 丢失、资源超限与取消 | 文件恢复语义明确，子进程停止，资源回收 | P4 |
| 流中断、慢消费者与重复通知 | 执行不依赖客户端，按声明范围补读或去重 | P4/P5 |

## 首轮可直接执行的任务

以下任务按顺序形成首轮交付；完成计划编写不勾选实现任务。

- [x] P0.1 固定首个任务类型、主路径、负责人及运行环境。证据：[P0.1 试点范围决策](../deepagents-server/docs/PRODUCTION_PILOT_SCOPE.md)（2026-11-10；P0 已在 P0.3 对齐后完成）。
- [x] P0.2 落盘验收配置：负载、时长、状态规模、RPO/RTO、费用和资源预算。证据：[人类可读合同](../deepagents-server/docs/PRODUCTION_ACCEPTANCE.md) · [机器配置](../deepagents-server/docs/PRODUCTION_ACCEPTANCE_PROFILE.json) · [校验器](../deepagents-server/tooling/acceptance/production_profile.py)（2026-11-10；门槛已固定，目标尚未证明）。
- [x] P0.3 对齐当前协议说明，清理设计文档中的旧 V1 支持表述。证据：[当前 Task 协议](../deepagents-server/docs/CURRENT_TASK_PROTOCOL.md) · [校正后的设计依据](AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)（2026-11-10；历史运行事实保留，但不属于当前支持矩阵）。
- [x] P1.1 贯通任务与模型 Activity 关联。证据：[字段、脱敏与兼容合同](../deepagents-server/docs/TASK_OBSERVABILITY.md) · `tests/test_task_observability.py` · `tests/test_task_api.py` · `tests/test_task_runtime.py`（2026-11-10；process-local JSONL，不代表 P1.2 指标后端或 P1.3 trace 已完成）。
- [x] P1.2 接受/排队/运行/终态、重试、超时、取消、CAN 与状态规模基础指标。证据：[指标、隐私与查询合同](../deepagents-server/docs/TASK_METRICS.md) · `tests/test_task_metrics.py` · `tests/test_task_runtime.py` · `tests/test_task_deployment.py`（2026-11-10；process-local Prometheus exporter，不代表持久指标平台、dashboard 或 P1.3 trace 已完成）。
- [x] P1.3 贯通父代理、子代理、摘要和 Activity retry trace，用量缺失为 unknown，并固定上传前脱敏与 30 天保留策略。证据：[trace、用量、脱敏与保留合同](../deepagents-server/docs/TASK_TRACING.md) · `task_server/tracing.py` · `tests/test_task_tracing.py` · `tests/test_task_observability.py`（2026-11-10；远端 project/TTL 与真实 provider waterfall 留给 P1.4，不能由单元测试推导）。
- [ ] P1.4 建立小规模真实 provider 用例及状态校验，执行一次受预算约束的验收。验收器、固定用例和三重授权门禁见 [P1.4 真实 provider 验收](../deepagents-server/docs/PRODUCTION_PROVIDER_ACCEPTANCE.md) · `tooling/acceptance/live_provider.py` · `tests/test_live_provider_acceptance.py`（2026-11-10；当前没有凭据、目标服务或费用批准，尚无真实调用/receipt，不能勾选）。
- [ ] P1.5 分开验证受控 429/超时/断流与真实 provider 集成。受控部分已通过：[合同与边界](../deepagents-server/docs/TASK_PROTOCOL_FAILURE_ACCEPTANCE.md) · [脱敏证据](../deepagents-server/docs/task-protocol-failure-evidence.json) · `tooling/acceptance/scenarios/task_protocol_failures.py`（2026-11-10；生产 SDK 内层重试关闭，Temporal attempt 有界；真实 provider 部分仍缺授权，故本项不勾选）。
- [x] P1.6 实测 Start 成功但响应丢失、同 key 重试/冲突、取消/完成竞争与越权查询。证据同上（2026-11-10；真实本地 Temporal、两个 API 和生产 Workflow；不代表永久幂等、生产 HA 或外部副作用 exactly-once）。
- [ ] P2 为选定后端建立持久环境与恢复手册，完成 Worker 丢失、备份恢复和兼容发布演练。
- [ ] 按 M1 门槛评审内部试点；未达成项保留，继续执行相应阶段，不以单元测试替代。

## 实施纪律与交付证据

每项实现保持一个清晰的状态权威。永久业务台账、quota store、outbox 或结果索引只在明确需要时引入，并写清其与 Temporal/Agent Server 的一致性与恢复关系。索引不可成为第二个执行器。

每次交付遵循仓库门禁：

- 先用 `ast-outline` 定位；变更运行结构前读 README，变更注入点读 `EXTENDING.md`，变更保护边界读 `docs/HARDENING_NOTES.md`。
- 独立模块变更运行其相关测试和完整模块 pytest；合并或推送实现前运行根 `.venv/bin/python -m pytest -q`。Python 模块、扫描器、guard 和 Go 变更分别运行其专门门禁，不用其他测试替代。
- 重型验证串行、`nice -n 10`；先按当前宿主容量确定进程树 RSS 预算，监测内存压力，超限停止本次拥有的进程树并保留日志。Go 验证使用 `GOMAXPROCS=2`、保留其他 flags 的 `GOFLAGS=-p=1`、`go test -parallel=2` 和保守 `GOMEMLIMIT`。
- 实现迭代更新 README 架构审查基线；拓扑或边界变更同步 canonical Mermaid、说明与架构 JSON，从 JSON 生成 HTML；按仓库要求保留 imagegen 标注图、精确 prompt 与说明。
- 新增顶层研究 Markdown 后重新生成 Research Atlas 索引并运行 `npm test`；每次变更执行 `git diff --check`，只提交任务所属文件。

每阶段交付应包含：代码提交与不可变镜像、测试命令与退出码、环境和配置摘要、故障时间线、脱敏 History/trace、原始测量、费用、资源峰值、运行手册，以及未执行/阻断项。对外容量或可用性结论只覆盖对应部署和负载。

本次计划的[范围标注图](audits/agent-server-production-plan-2026-11-09.png)、[精确提示词](audits/agent-server-production-plan-2026-11-09.prompt.txt)与[交付检查](audits/agent-server-production-plan-2026-11-09.md)单独保存。图中路线是计划，不改变 README Mermaid 所描述的当前架构。
