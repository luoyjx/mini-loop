# Deep Agents Server delivery — 2026-10-16

当前迭代以 `a6d61c6` 为基线，整理独立 Python 模块的职责与依赖方向，分离运行代码、
仓库工具和测试。已有公开入口与持久协议保留，不新增 Python 依赖。下文的历史
验收保留各自源码、镜像、run_id 和资源范围，不由本轮目录迁移追认。需求对应关系见
[TASK_ACCEPTANCE](docs/TASK_ACCEPTANCE.md)，部署入口见 [TASK_DEPLOYMENT](TASK_DEPLOYMENT.md)。

## Python 项目结构重构

源码导航见 [PROJECT_STRUCTURE](docs/PROJECT_STRUCTURE.md)。这次拆分有三个明确边界：

- Task API 由 `api.py` 负责启动和兼容导出，`http.py` 处理 ASGI、认证、请求限制及响应；
  `protocol.py` 定义纯错误、身份摘要和 `TaskOperations`，`service.py` 编排任务操作；
  `backend/` 分别封装 owned Run、History 分页/导航、签名 cursor 和 readiness。
- `temporal_support/determinism.py` 保存共用的确定性 StateBackend 和工具消息 ID 适配，
  生产 Task Workflow 不再导入历史 `temporal_prototype` 的实现。Workflow 名称、Activity
  名称、memo、payload、checkpoint envelope 与 cursor 格式仍是原协议。
- `tooling/acceptance/{scenarios,support,fixtures}` 分别管理故障流程、公共设施及离线
  模型/Worker；`tooling/build_task_bundle.py` 管理包审计与构建上下文。原有 `scripts/`
  保留命令兼容层，`tests/` 只承载测试，工具和 fixture 不发布到生产 wheel。

生产 API、Worker 和角色 launcher 的模块入口保持。目录分层发生在进程内部，没有新增
服务或存储系统。冷重启验收现在显式要求本次 image ID 与 manifest，核对当前源码及
镜像内实际文件后才启动故障流程，不再内置历史制品。

| 本轮 Gate | 已知结果 | 边界 |
| --- | --- | --- |
| 模块 pytest | **601 passed in 5.41s** | wrapper 8.14 秒，采样峰值 211.25 MiB，最低 memory-free 43%，4 个采样点，无资源阻断 |
| Archify | **9/9**，零错误/警告 | JSON 与生成 HTML 同步；双主题 Task 区域和完整浅色图静态检查通过；回退字体，浏览器交互未验 |
| 原生 Temporal replay | **31 个 Run 通过** | 30 个历史 V2 Run、1 个原始 V1 step-limit Run；实际 Workflow sandbox，单执行线程，零模型调用 |
| 重构后的镜像构建 | **通过** | 81.5 秒、74 个采样点，所属 cgroup 峰值 509.29 MiB，宿主最低空闲 44%、VM 最低可用 90.51%；observer 已移除 |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，93.05 秒 | wrapper 93.90 秒，采样峰值 323.56 MiB，最低 memory-free 49% |
| 原 Python invariants | **77 modules checked** | 只检查原 Python 包的不变量声明，不是独立模块的专用 checker |
| 标准 Agent Server HTTP smoke | **10 组通过** | wrapper 16.32 秒，采样峰值 250.88 MiB，最低 memory-free 43%；服务进程树峰值 171.9 MiB |
| 重构后的容器恢复 | **3 组通过，六任务全部完成** | 102.61 秒、153 个采样点，所属 RSS+cgroup 峰值 899.1 MiB，最低宿主空闲 43%、VM 可用 85.89%；所属资源已清理 |

原生 replay 运行 `7e8e356142ec441b937a7ea804993909` 从两份历史 SQLite 的私有副本导出
并核对 History 摘要，原数据库保持不变。30 个 V2 Run 来自既有六任务冷重启实验，
另一个是原始 v1 的 step-limit Run。耗时 8.671 秒、17 个资源采样点、所属进程树
峰值 308.2 MiB，最低宿主空闲 49%；2 GiB/180 秒/free 15% 预算内完成，所属进程
全部退出。原回执 SHA-256 为
`aea3bfb4278b10c12d20a73835e089fa1196a2b3d26149fb9f6255d8310985d3`。

首次回放 `714d00b03a42452eb97a629f67cc93b2` 失败记录保留。SDK 1.34 的 Replayer 为
`workflow.info().task_queue` 提供合成队列，无法复现原不可变请求摘要。通过的验收
Runner 仅把此字段还原为已核对摘要的原生 `WorkflowExecutionStarted` 队列；没有修改
生产 runtime、History、payload、memo 或命令。此适配范围随回执公开，不能把本次结果
扩大为任意历史/版本迁移兼容，或新 Worker 的实际故障接管。

新的 wheel/构建上下文包含 33 个包源码文件，排除 `tests`、`scripts` 和 `tooling`。
最终 manifest SHA-256 为
`2cad74f68d5e6cd2a7d7f3366d44d1b7040535cc6cce04c56eddb7105c34fd66`，wheel SHA-256 为
`6aa8f4922ece6fcbebe60d52a504191de33c8fbb0caa9a94a41c7bd479a37450`。
重建的 `linux/arm64` 本地应用 image ID 为
`sha256:520e0e67f9a1cf9e8ee1509c86803ee9c05cc77dd7f47c25ee8b1255a7465bf9`，
441,168,129 bytes，未发布 registry。builder 为 1920 MiB/1.9 CPU，observer 为
64 MiB/0.1 CPU，swap 0；实测 cgroup 约束符合配置。

新镜像冷重启运行 `a91bdf50d2c94169858a9988f69561c0` 的 **3 组通过**。起始镜像探针
确认内部 manifest 与本次构建相同，安装的 33 个包文件逐一匹配。两 API、两单
Activity 槽 Worker 接受 6 Task/3 owner；两项任务在父工具步骤 24、3 次 CAN 后已
运行 45.120/45.010 秒，另四项为原生 SCHEDULED。Temporal 被强杀时 readyz=503、
healthz=200。全部旧 API/Worker/Temporal/observer 停止后，观测到 **3.131 秒**完整
零服务窗口；全新进程与连接恢复，四项排队任务在新 Worker 启动前仍可查询且未执行。

六项最终各完成 **37 个父工具步骤、4 次 CAN/5 Run、2 个 GP 子图及 2 次原生摘要**。
30 份 History 没有 WorkflowTaskFailed，24 对相邻 CAN 输入摘要相等。276 个模型
Activity、278 次尝试仅有两次故障重试；中断 Activity 的原生身份、请求摘要和完整
native 状态保留。四项排队任务的首次执行来自新 Worker，同 key 复用、旧 cursor 与
Task/首 Run 身份保持。观测到接受至终态为 57.031–72.228 秒，包含模拟 pacing、
排队与故障门控，不用作真实 provider 延迟或生产容量指标。

该实验在 2 GiB/180 秒/free 15% 预算内完成；10 个所属容器和 4 个宿主服务/观察
进程已移除或回收，无容器 OOM。四个最终业务容器按清理协议 SIGKILL/137，不新
宣称优雅排空通过。10 个测试凭据片段在 30 份 History、14 份日志中零命中，结论仅
覆盖这些测试值。原回执 SHA-256 为
`42d595f68047c3e7941201e0d597dcc7564d02679527baf218d3b7e4bc15102d`。
这是当前重构代码的容器恢复证据；仍是单机、明文 CLI/SQLite 和受信任离线模型，
不代表跨宿主故障域、原生 TLS/Cloud ACL、生产 HA 或真实模型吞吐已通过。

迁移后的标准 Agent Server smoke 通过原 `scripts/smoke.py` 入口运行真实开发服务与
Deep Agents graph，覆盖认证/owner、文件、跨 Thread memory、Store 授权、原生子代理、
SSE、客户端、同 Thread 并发拒绝/取消、禁用 shell 与优雅开发重启。fixture 来自新
`tooling/acceptance/fixtures/`，不需要真实模型 key；此项仍不证明带许可证的
PostgreSQL/Redis 崩溃恢复。原始观测与资源回执在本轮 evidence 中记录摘要。

重型门禁串行执行，使用既有资源守门器；本轮没有资源超限中止。Go tests/vet、
`verify_scans` 与 `verify_guards` 未重跑，因为没有 Go、扫描器或 guarded mutation
anchor 变化。未新增、改名或删除顶层 `docs/*.md` 研究报告，因此不触发 Research Atlas
内容生成。静态目录/导入合同、wheel 排除规则与旧入口回归由模块测试覆盖。

Archify 规范 SHA-256 为
`481662041122915eb98cc4bd71128a2ad13f31d147e9693bba5915ed9eaa6ccc`，生成 HTML SHA-256 为
`abe362ec53bf8e3430354258a49da0490170bc8b6143cf643e791520b52828c8`。
生成和静态目检回执在忽略目录 `var/deepagents-delivery/task-structure-archify*.receipt.json`。
[本轮结构图](../docs/audits/task-server-structure-2026-10-16.png) 单独标注这次职责划分；
由 built-in imagegen 基于完整 base 图生成并经一次文字修正，精确 prompt 与 repair
prompt 随图保存。成图 SHA-256 为
`b02657810fddf5973e690552b4f025a5a6fe91d0be04215db71ed5c7a5d25499`。
本轮没有增加或删除部署组件，base 图保持；规范拓扑仍以根 README Mermaid 和对应
架构 JSON 为准。[本轮证据摘要](docs/task-structure-evidence.json) 保存源码、门禁、
制品及提示词摘要，不发布私有 History 内容。

## 六任务全栈冷停与冷启动历史验收（a6d61c6）

该轮以已推送的 `08b4333` 为基线，补齐运行任务与排队任务共同经历全栈冷停的组合
验收。复用已验证的固定镜像，生产源码、依赖及部署配置没有变化；新增独立的故障
驱动、观察进程、模拟模型 fixture 和验收测试。以下内容记录该轮原始结果。

运行 `c0a4afaaf20c48e5b65fc51b7e9a7580` 的 **3 组通过**。两 API、两单 Activity 槽
Worker 接受 6 Task/3 owner。前两项在第 24 个父工具步骤、3 次 CAN 后停在模型门控点，
年龄分别为 45.200/45.097 秒；另四项首 Activity 被原生 Describe 确认为 SCHEDULED，
没有模型执行。随后强杀 Temporal，实测 readyz=503、healthz=200，再终止全部旧
API/Worker 和提交/观察进程。

四旧业务容器与旧 Temporal/observer **全部不存活 3.189 秒**。保留原 SQLite 目录，
创建全新的 Temporal、API、observer；新 Worker 启动前，六项任务仍可查询、同 key
全部复用，四项仍为 SCHEDULED。新 Worker 随后恢复执行：原生 History 证明四排队项
的首次 ActivityStarted 属于新 Worker、attempt 1；两项中断调用以原 Run/Activity
及相同请求 SHA 重试为 attempt 2，完整 native snapshot 相等。

六项最终各完成 **37 个父工具步骤、4 次 CAN/5 Run、2 个真实 GP 子图与2次原生摘要**。
30 份 History 为零 WorkflowTaskFailed，24 对相邻 Run 的完整 continuation 输入
摘要相等。276 个模型 Activity、278 次尝试，只有上述两次重试；TaskID、首 Run、
owner、旧事件游标和最终文件/计划状态保持。任务接受至终态观测为 63.764–68.624 秒，
包含模拟模型 pacing、排队、门控和停机；没有把它当作真实 provider 延迟或小时级容量。

完整实跑 93.63 秒、142 个采样点，所属 RSS+cgroup 峰值 912.6 MiB，宿主最低空闲
42%、VM 最低可用 86.14%，无 OOM；VM memory PSI 不可用。九个所属容器、新旧两
Temporal 和两 observer 及驱动全部已退出/移除。最终四业务容器使用 SIGKILL/137
清理，正常 drain 的证据仍是上一轮独立默认入口测试。十个测试凭据片段在30份
History及13份日志中无命中，只覆盖这些实际测试值。

公开原始观测摘要：[task-cold-restart-evidence.json](docs/task-cold-restart-evidence.json)。
私有原回执 SHA-256 为 `70ffca86c8b8b259e1b03d00c61332caf2776a0ed5d6b6dbe3c799e9d497e38f`，
源码、快照、History 与资源证据均保存摘要。观察进程通过有限的私有 mailbox 与驱动
通信；冷启重新创建 SDK/HTTP Client，没有保留旧观察连接作为恢复条件。

| 最终 Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest | **587 passed in 5.27s** | wrapper 8.20 秒；采样峰值 186.1 MiB；最低 memory-free 51% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，106.00 秒 | wrapper 108.08 秒；采样峰值 319.6 MiB；最低 memory-free 42% |
| 生产基线与源码冻结 | **31 个生产文件**逐字节等于 `08b4333`；38 个回归冻结文件一致 | 含24个包源码文件、锁/配置；新4个验收源码及最终README/报告有单独摘要 |
| Archify | **9/9**，零错误/警告 | 65组件、87连线、8边界不变；只更新证据说明；明暗静态目检通过 |
| Research Atlas | **21 篇，5/5** | wrapper 10.20 秒；采样峰值 467.8 MiB；最低 memory-free 47%；仅目标报告 record 改变 |
| Markdown / 制品摘要 | **238 个本地引用**、fence、README outline、`git diff --check` 通过 | 38个回归冻结文件、41个故障证据源码和图片/提示词摘要一致 |

三项最终执行 gate 串行、`nice -n 10`，1.5 GiB/300 秒/free 15%预算；实跑故障使用
2 GiB/180 秒预算，无超限中止。因生产包、依赖、Docker/Compose 均未更改，本轮没有
重复 wheel/lock/invariants 或重建镜像；也未改 Go/scanner/旧 mutation anchors，相关
gate未重跑。当前README晚于镜像构建元数据，不声称已进入旧镜像。门禁收据见
[task-cold-restart-validation.json](docs/task-cold-restart-validation.json)。

本轮验证真实 Deep Agents/Temporal 控制流与持久恢复，模型输出使用确定性 fixture。
本机 plaintext SQLite dev 服务、有限规模与相同版本重启的证据，不扩展为生产 HA、
真实模型调用、任意版本迁移或持续吞吐 SLO。生产环境与负载目标未给定，这些边界保留。

## 独立 Task 容器与 Linux 停机历史验证（08b4333）

API 与 Worker 共用一份镜像，通过 `task_server.launch api|worker` 同进程启动，分别
读取各自允许的 Secret 文件。Compose 设置非 root UID 10001、只读根、有限 `/tmp`、
CPU/内存/PID 限额和 20 秒停止预算；外部 Temporal 的部署、持久性与 HA 由运营方提供。
`/healthz` 只表示进程存活；`/readyz` 合并并发 DescribeNamespace RPC、限定一秒并缓存
一秒，只证明 namespace 当时可用，不证明提交权限或 Worker 容量。

固定基础镜像为 `python:3.12.15-slim-bookworm@sha256:34386ef0cb081344d7ec1c103ba398e6e9f64e9ab3a1509accc92a4e24a07258`。
最终本地应用 image ID 为
`sha256:3ccfbdd7c2a184b8e655f012a81da5d42310f733e07b0108a1c742763411e30f`，
平台 `linux/arm64`，441,144,845 bytes；没有推送镜像 registry，因此不将该 image ID
冒充 registry digest。镜像含 99 个 distribution，除基础镜像的 pip 外全部版本匹配锁；
24 个项目包文件逐字节匹配。Manifest SHA-256 为
`118e0aa4e2288259745cc46ed9732d3b550c032e458c862ab9dab7281b2f5b50`。

构建上下文由白名单生成，排除 checkout 环境文件、凭据、测试 fixture 和运行状态。
首次 binary-only 构建遇到已锁定的 `forbiddenfruit==0.1.4` 只有 sdist；随后严格核对
现有缓存 wheel 与锁内 sdist 的运行时代码、许可证、metadata 和完整 RECORD，并仅为
该审计 wheel 添加 hash。未执行 sdist、未增加依赖或编译器；构建命令要求显式提供
两份审计输入。最终构建 142.31 秒，观察到的自有 cgroup 峰值 532.24 MiB；builder
配置 1920 MiB / 1.9 CPU，observer 64 MiB / 0.1 CPU，swap 0。宿主最低空闲 38%，
VM 最低可用 90.47%；守门器没有把 Docker CLI 的 RSS 当成容器内存。

首次完整容器 run `74a3ec4f9711414a8d41b1ad7d463d4c` **失败**：默认 Worker
记录收到 SIGTERM 后，空闲 selector 未唤醒，最终 deadline exit 70；主恢复测试尚未进入。
修复保留立即设置 stop event 的启动期合同，并用 `call_soon_threadsafe` 唤醒事件循环。
重建镜像后，无定时器 Linux selector 探针在信号后 0.318611 秒退出 0；真实默认入口
Worker 在独立空队列收到 docker stop 后 0.914 秒退出 0，出现完整排空日志、无 OOM。
失败回执保留，未用后续成功覆盖。

完整容器 run `0c189c0de2614b5c8d4db07c4b4268be` 在 39.57 秒内通过：两 API 和两
Worker 各自 SIGKILL 后重建，两个并行 Task 各完成 10 个工具步骤及一次 CAN；重试前后
完整状态与请求 SHA 相等。22 个原生模型 Activity、24 次尝试，两个 Activity 从 1 重试
至 2，最大尝试 2；四份原生 History 没有 WorkflowTaskFailed。停掉本机 Temporal 后
`readyz` 由 200 变 503，`healthz` 保持 200；同 SQLite 重启后 `readyz` 回到 200，
已完成结果不变。六个合成秘密匹配项在四份 History 与十二份日志中均无命中。

验收共 61 个资源样本，宿主自有 RSS 加容器 cgroup 峰值 596.2 MiB，宿主最低空闲
40%，VM 最低可用 87.44%，无 OOM。VM 未暴露 memory PSI，未将缺测计为零。
十个自有容器和全部自有宿主进程已清理；最终四个 fixture 业务容器用 SIGKILL 收尾，
不能称为全部正常排空。正常停机证据来自上述独立默认入口 Worker。

发布证据：[task-container-evidence.json](docs/task-container-evidence.json)。故障恢复
使用只读挂载的离线模型 fixture，镜像中没有该 fixture 或公共 fixture selector；真实
默认 Worker 停机验收没有提交任务或调用模型。本轮故障连接使用本机明文 Temporal dev
服务，未验 Compose 编排或其生产 mTLS 全链路；上一轮 mTLS 测试证据独立保留。

| 最终 Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **576 passed in 3.79s** | wrapper 6.12 秒；采样峰值 184.5 MiB；最低 memory-free 50% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，97.68 秒 | wrapper 99.81 秒；采样峰值 411.3 MiB；最低 memory-free 47% |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅原 Python package，不替代独立模块验收 |
| Lock / offline wheel | **105 packages**；24 个包文件、23 个 Python 模块、其中 11 个 Task 模块 | 源码与 README metadata 逐字节匹配；package / lock 不变；最终 wheel SHA 与容器构建一致 |
| Archify | **9/9**，0 errors / 0 warnings | 从 JSON 生成 HTML；明暗静态目检通过，浏览器交互未验 |
| Research Atlas | content **21 篇**，build 与 HTML **5/5** 通过 | wrapper 8.16 秒；采样峰值 532.7 MiB；最低 memory-free 54%；仅目标报告 record 变化 |
| Markdown / source freeze | **213 个本地引用**、fence、README outline、`git diff --check` 通过 | 52 个回归冻结文件、44 个容器证据文件及 PNG/prompt 摘要一致 |

全部 gate 串行、`nice -n 10`，常规预算 1.5 GiB / 300 秒 / memory-free 15%；
容器故障预算 2 GiB / 180 秒，未资源超限。短命检查只有启动采样，不把它当作可靠峰值。
最终回归前后 52 个冻结文件摘要一致；其中已发布容器证据冻结 44 个源码/部署/测试文件。
v1/v2 Workflow、checkpoint 和 summarization 保持基线字节一致；lifecycle 的本轮修复
单独列明。未改 Go、source scanner 或旧 guarded mutation anchors，未运行相应 gate。
日志索引为 `var/deepagents-delivery/task-container-final-validation.receipt.json`；
公开门禁摘要见 [task-container-validation.json](docs/task-container-validation.json)。

## Temporal TLS / mTLS 历史验证（06a93d4）

真实固定 SDK 连接 stdlib mTLS 测试隧道，后端仅转发到 loopback Temporal dev server。
两个正式 Task API 进程、两个调用正式 Worker 进程入口的离线模型 fixture，以及 observer
使用五张不同客户端证书。四个错误配置各自有成功的前后新连接 RPC 对照，故障不是由
服务整体不可用冒充的证书拒绝。测试隧道不进入生产组件图。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **491 passed in 3.65s** | wrapper 6.11 秒；采样峰值 184.2 MiB；最低 memory-free 48% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，120.60 秒 | wrapper 122.46 秒；采样峰值 329.2 MiB；最低 memory-free 43% |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅原 Python package，不替代独立模块验收 |
| 真实 TLS 故障验收 | **3 组通过**；2 Task 各 10 次原生工具操作、1 次 CAN | 13.80 秒；822.0 MiB；最低 memory-free 43%；26 次采样；23 个进程全部退出 |
| 证书负例与重连 | 错 CA、错服务名、缺客户端证书、不受信客户端全部拒绝 | 每例前后 fresh RPC 成功；全部成功连接 TLSv1.3 / h2；代理 SIGKILL 后 503，6.133 秒内原 2 API / 2 Worker / observer 重连 |
| 状态与重试 | 4 个 Run 无 WorkflowTaskFailed；完整门控快照相等 | 22 个原生模型 Activity、24 次尝试；两项 attempt 1→2，原 Activity / Run / request SHA 一致；最大尝试 2，预算仍为 3 |
| 凭据检查 | 147 个合成凭据匹配项，4 个原生 History 与 23 份最终日志均无命中 | 只验证这些实际记录；不宣称任意第三方日志均不泄露 |
| Lock / offline wheel | 105 packages；离线锁检查、构建和内容审计通过 | 23 个包文件逐字节匹配：22 个 Python 模块，含 10 个 Task 模块，另含 instructions.md；README metadata 一致；依赖及锁文件不变 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | JSON 生成 HTML；一次连线标签修正；明暗静态目检通过；浏览器交互未验 |
| Research Atlas | content **21 篇**，build 与 HTML **5/5** 通过 | 最终措辞校正后 wrapper 8.17 秒；采样峰值 529.0 MiB；最低 memory-free 46%；仅目标报告内容变化 |
| Markdown / source freeze | 186 个本地引用、fence、README outline、`git diff --check` 通过 | 27 项源码/依赖摘要一致；v1/v2 runtime、checkpoint、summarization、lifecycle 与 401e4d5 逐字节一致 |

发布证据：[task-tls-evidence.json](docs/task-tls-evidence.json)，run_id
`cfdf11707eb142be9c8af50e11cb5dae`。原收据保留在忽略的
`var/task-tls-prototype/cfdf11707eb142be9c8af50e11cb5dae/receipt.json`；实跑后补充了纯离线
重试汇总 helper 和单测，用原审计文件生成 `activity-postcheck.json`，未改原收据或重跑
服务。最终 491 项模块测试覆盖该 helper。发布证据标明这个时间边界，不把最终源码摘要
冒充全部代码在实跑前已冻结。

HTTP Bearer 的 owner 检查与 Temporal 原生认证 / ACL 是两个边界。合成 API key 只通过
SDK 连接配置传入；测试隧道不解析或授权它。**原生 Temporal 服务 TLS、Cloud API key
ACL、生产 HTTPS 入口、证书轮转、跨主机 HA、持续高并发容量和真实模型推理均未验收**。
断链造成心跳超时和真实重试，外部副作用仍不保证 exactly-once。最后 harness 清理沿用
1.5 秒辅助期限，可能强杀空闲 Worker；该收尾不计作正常 drain 验证。

全部重验证串行、`nice -n 10`；常规预算 1.5 GiB / 300 秒 / memory-free 15%，故障脚本
2 GiB / 180 秒，无资源超限中止。短检查只得到启动采样，不能当作真实峰值。门禁日志索引：
`var/deepagents-delivery/task-tls-final-validation.receipt.json`。未改 Go、source scanner
或原有 guarded mutation anchors，未运行相应 gate。

## Worker 滚动停机历史验证（401e4d5）

该轮以 `98de8d2` 为基线，增加 Worker CLI 的滚动停机与受控交接。以下数字属于该轮。

`worker.run_process` 是生产 CLI 与受信任验收 fixture 共用的进程入口；嵌入式
`serve` 不安装宿主信号处理器或强制结束宿主进程。第一次信号停止新轮询并排空已经
领取的工作，允许处理 SDK 已缓冲的 Activity；不能把信号后观察到的每次 Start
都解释成新领取。宽限到期由 SDK 取消尚未结束的 Activity，由 Temporal 按原有预算
决定重试；这不是业务 Task 取消。第二次信号或进程期限到达时非零强退，恢复仍依赖
Temporal History、心跳和可重试的任务语义。

本轮专用验收同时检查 API 容量拒绝、已接受任务排队、多个 Worker 的执行槽、
三类停机交接和任务完整终态。故障模型使用离线 fixture；不能据此宣称生产 provider
吞吐、跨主机调度、数据库 HA 或任意版本升级已经完成。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **404 passed in 4.40s** | wrapper 6.13 秒；采样峰值 178.0 MiB；最低 memory-free 48% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，103.81 秒 | wrapper 106.00 秒；采样峰值 328.5 MiB；最低 memory-free 47% |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅原 Python package，不替代独立模块验收 |
| 多任务滚动验收 | **4 组通过**；12 个 Task 全部成功，各 1 次 CAN | 20.95 秒；峰值 1059.5 MiB；最低 memory-free 47%；40 次采样；45 个进程退出 |
| 三种真实 SIGTERM 退出 | 正常完成 0、合作取消交接 0、吞取消后硬期限退出 70 | 验收 grace=1s / deadline=5s；硬退观测 5.087 秒；两个重试均保留 Activity/Run/request SHA，attempt 1→2 |
| 并发 / 排队 / 拒绝 | 3 个模型执行、9 个原生 Activity 排队；最大模型并发 3 | 134 个模型协程区间：132 完成、1 合作取消、1 随进程退出；真实 API 429 后原生 NOT_FOUND，同 key 重试被接受 |
| Lifecycle / harness 定向 | 36 项生命周期与原 runtime 检查、6 项新 harness 检查通过 | 含启动竞态、第二次信号、满 stderr 管道仍硬退；完整模块 gate 已覆盖全部定向测试 |
| Lock / offline wheel | 105 packages；离线 lock、构建与内容审计通过 | 22 个包文件逐字节匹配（21 个 Python 模块含 9 个 Task 模块，以及 instructions.md）；README metadata 一致；package/lock 不变 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | JSON 生成 HTML；一次停机连线标签修正；明暗主题静态目检；浏览器交互未验 |
| Research Atlas | content **21 篇**，build 与 HTML **5/5** 通过 | wrapper 8.17 秒；峰值 526.9 MiB；最低 memory-free 51%；生成数据只变更目标报告 |
| Markdown / whitespace | 175 个本地引用、fence、README outline 与 `git diff --check` 通过 | 21 个源码冻结摘要一致；v1/v2 runtime、checkpoint 与 summarization 和 98de8d2 逐字节一致 |

发布证据：[task-rollout-evidence.json](docs/task-rollout-evidence.json)，run_id
`7058dd0bb0f84158bb1d8a76155b59e4`。默认生产参数仍为 2 秒宽限 / 15 秒总期限；
验收主动缩短为 1 / 5 秒，并未将这两个配置混为同一次实测。12 个任务各执行
10 次原生工具操作，替换 Worker 领取了此前排队的 9 项任务。24 个 Run 的起止链
保持原 Task/first Run 身份；不将这些短时离线场景外推为生产长时间吞吐结论。

原始收据和逐模型区间留在忽略的 `var/task-rollout-prototype/`；门禁日志索引为
`var/deepagents-delivery/task-rollout-final-validation.receipt.json`。发布摘要保留原始
文件 SHA、身份、区间和资源记录，不包含任务正文、凭据或本机绝对路径。模型区间
统计不等于 provider 吞吐或 SDK 完整占槽时间；正式三次退出和 harness 最后清理
分别记录，收尾可能强杀空闲替换进程，不能把它们算作正常 drain 成功。

重检查串行并使用 `nice -n 10`；常规预算 1.5 GiB / 300 秒 / memory-free 15%，
故障脚本 2 GiB / 180 秒。没有资源超限中止；短检查的启动采样不能作为真实峰值。
未改 Go、source scanner 或原有 guarded mutation anchors，未运行相应 gate。

## Task V2 Continue-As-New 历史验证（98de8d2）

该轮以 `9af554a` 为基线，为独立 Task HTTP 服务增加 **task-v2 分段续跑**。
新任务在安全的父模型边界每 8 次执行 Continue-As-New，把完整 checkpoint 写入日志
传给下一个 Run；TaskID、owner、初始执行链和原生 24 小时总期限保持不变。
真实 Deep Agents 的文件、计划、子代理及摘要状态一起恢复。Worker 同时注册不可变的
v1 与新 V2，新 API 能复用既有 v1 幂等键。本轮没有新增依赖。
详细合同见 [TASK_SERVER.md](TASK_SERVER.md)，故障摘要见
[task-can-evidence.json](docs/task-can-evidence.json)。

该轮新旧故障验收使用固定 Temporal CLI 1.9.1 / Server 1.32.0、SDK 1.34.0、
Deep Agents 0.7.25。生产 Workflow、原生工具和子代理真实运行；模型 Activity 使用
确定性离线 fixture，因此不将本轮结果当作真实 provider 或生产 HA 验收。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **373 passed in 3.45s** | wrapper 6.16 秒；采样峰值 176.5 MiB；最低 memory-free 51% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，106.74 秒 | wrapper 108.47 秒；采样峰值 331.1 MiB；最低 memory-free 45% |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅覆盖原 Python package，不替代 Task V2 验收 |
| V2 真故障验收 | **4 组通过**，4 次 Continue-As-New / 5 个 Run | 45.86 秒；797.3 MiB 峰值；最低 memory-free 37%；87 次采样；19 个进程退出 |
| 原生工具 / 子代理 / 摘要恢复 | 37 次父工具步骤、2 个真实 general-purpose 子代理、2 次跨 Run 摘要 | 子代理 Activity 中、分段前、分段后及第三次 CAN 后整组强杀；快照与重放模型请求逐字节相符；5 个 Run 的 WorkflowTaskFailed 均为 0 |
| 原 8 组 HTTP 故障与 v1 兼容 | **8 组通过** | 22.18 秒；827.9 MiB 峰值；最低 memory-free 40%；17 个进程退出；默认 V2 冷重放完整状态一致 |
| 已有 v1 合同 | 当前 API 复用 v1；冲突 409 / 跨 owner 404；原 128 步失败终态 | 43 个模型 Activity 完成；task_step_limit non-retryable；WorkflowTaskFailed=0；不修改 v1 Workflow 源码 |
| Lock / offline wheel | `uv lock --check --offline`、离线构建与内容审计通过 | 105 packages；21 个包文件逐字节匹配（20 个 Python 模块含 8 个 Task 模块，以及 instructions.md）；README metadata 一致；package/lock 不变 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | 冻结 JSON 生成 HTML；CAN 回流标签修正后明暗主题静态目检，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build 与 HTML **5/5** 通过 | wrapper 10.23 秒；采样峰值 889.5 MiB；最低 memory-free 50%；只更新目标设计报告的生成数据 |
| Markdown / whitespace | 168 个本地引用、fence、README outline 与 `git diff --check` 通过 | 6 份文档；21 个交付源码摘要匹配；v1 runtime 与 9af554a 逐字节一致 |

主验收 run_id 为 `caffa4578ab348a3bc7198eb430bcbfb` 与
`d252d1bc60c9470b84747c5dcbda28bc`。原始快照、History、日志与进程采样留在忽略目录
`var/task-can-prototype/`、`var/task-server-prototype/`；发布证据保留内容摘要和源码
SHA-256，不发布完整任务内容或凭据。所有重验证串行并降低优先级，普通 gate 预算
1.5 GiB / 300 秒 / memory-free 15%，故障脚本预算 2 GiB / 180 秒。
没有资源超限中止；短 lock/wheel/invariants gate 的启动采样不能当作真实峰值。
完整门禁日志索引在 `var/deepagents-delivery/task-can-final-validation.receipt.json`。
未改 Go、source scanner 或原有 guarded mutation anchors，未运行相应 gate。
证据中的源码摘要是两次验收后的冻结清单；旧 HTTP harness 在两次运行之间调整过
v1 兼容场景，不能倒推每个辅助脚本在两个运行时都与当前版本相同。生产源码保持一致。

## Task HTTP 历史验证（9af554a）

该轮以 `1fd010d` 为基线，新增独立可选的 **Task HTTP 服务**：
无状态 API 确认 Temporal Start 后返回 202，独立 Worker 执行真实 Deep Agents，
保留的 Temporal History 是任务状态、恢复和结果的唯一权威。新增路径不调用原
Python/Go 业务，不经过标准 Agent Server API，也不引入第二套业务数据库或 outbox。
命令、HTTP 合同与边界见 [TASK_SERVER.md](TASK_SERVER.md)。本轮没有新增依赖。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **323 passed in 7.13s** | wrapper 10.24 秒；采样峰值 175.8 MiB；最低 memory-free 49% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，196.12 秒 | wrapper 197.91 秒；采样峰值 321.2 MiB；最低 memory-free 44% |
| 原 Python invariants | **77 modules checked**，退出 0 | 只覆盖既有 Python package，不代替新服务验收 |
| Task HTTP 真故障验收 | **8 组通过**，32.45 秒 | 双 API、双 Worker；峰值 780.0 MiB；最低 memory-free 44%；61 次采样；16 个进程均退出 |
| 步数上限失败终态 | 真实循环完成 **43 个模型 Activity**，128 步耗尽返回 `failed` / `ready=true` | 原生 `task_step_limit` non-retryable failure；`WorkflowTaskFailed=0`，没有以 Workflow Task 重试掩盖任务失败 |
| 定向 runtime / harness | **20 passed in 2.39s** | 含导入和监控 19.81 秒；峰值 202.9 MiB；最低 memory-free 49% |
| Lock / offline wheel | `uv lock --check --offline`、离线构建与内容审计通过 | 105 packages；18 个包文件逐字节匹配（17 个 Python 模块含新增 5 个 Task 模块，以及 instructions.md）；不含测试、.env 或运行数据 |
| Archify | **9/9 showcase**，0 errors / 0 warnings | 冻结 JSON 生成 HTML；新增泳道深浅主题静态目检，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build 与 HTML **5/5** 通过 | wrapper 8.14 秒；峰值 697.9 MiB；最低 memory-free 45%；生成 JSON 由命令更新 |
| Markdown / whitespace | **160 个本地引用**、fence、README outline 与 `git diff --check` 通过 | 6 份文档检查；12 个受测源码摘要匹配；Research 仅改变目标报告 |

直接故障证据：[task-server-evidence.json](docs/task-server-evidence.json)，运行
`792fc91351654e1ba8ff5f3b7fa35c32`。它验证跨 API 的幂等竞争与 owner 隔离、生产者退出后
双任务并发、API 全部强杀重启后的身份及事件游标、Temporal 与全部 Worker 强杀后的
冷重放、单任务取消、另一任务完整 messages/files/todos 恢复与终态复用，以及真实
步骤耗尽失败。首轮 `2fdf96d9fce9426da998d8aa1b3a6c30` 在步骤上限修复之前通过七组，
仅保留为历史记录，不作为最终代码的主验收。

所有重验证串行、`nice -n 10` 运行；普通 gate 预算 1.5 GiB / 300 秒 / memory-free
15%，故障脚本 2 GiB / 180 秒。无资源超限中止。完整收据、日志和快照留在忽略的
`var/task-server-prototype/`、`var/deepagents-delivery/task-http-final-*`；发布摘要含来源
SHA-256，不含 token、密钥或完整任务内容。未改 Go、source scanner 或原有 guarded
mutation anchors，未运行相应 gate。真实 provider、生产集群 HA 与容量仍未验收。

## Temporal 原型历史验证（1fd010d）

以下是上一轮可选故障原型的验收，范围见 [TEMPORAL_PROTOTYPE.md](TEMPORAL_PROTOTYPE.md)。
其 Memory 和固定阶段 Continue-As-New 属于原型；当前 Task V2 的完整 checkpoint
分段实现及验收另见本页顶部，不把原型证据替代当前入口的验收。

| Gate | 结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest，含 Temporal extra | **238 passed in 1.78s** | wrapper 4.08 秒；采样峰值 171.7 MiB；最低 memory-free 46% |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，101.87 秒 | wrapper 104.12 秒；采样峰值 287.5 MiB；最低 memory-free 39% |
| 原 Python invariants | **77 modules checked**，退出 0 | 只覆盖原 Python package，不把该结果当作 Temporal 验收 |
| Temporal 真故障验收 | 并发、Worker 强杀接管、服务强杀重启、全部旧 Worker 丢弃、完整状态迁移与副作用去重通过 | 唯一 run_id、实际 History 与清理证据见 [原型记录](TEMPORAL_PROTOTYPE.md#验收与资源限制)；`production_ha_acceptance=false` |
| 最新 harness 单元检查 | **9 passed** | wrapper 6.12 秒，峰值 67.2 MiB；最终状态断言单独复核 |
| Lock / offline wheel | `uv lock --check --offline` 与离线构建、内容审计通过 | 105 packages；12 个 Python 模块与源码逐字节匹配，含 6 个 Temporal 模块；保留 instructions.md，不含测试、日志、数据库或 .env |
| Archify | **9/9 showcase**，0 errors / 0 warnings | JSON 生成 HTML；新增泳道已静态渲染并目检深浅主题，未做浏览器交互验收 |
| Research Atlas | content **21 篇**、build、HTML **5 tests passed** | bundled Node 24.19.0；wrapper 10.23 秒；采样峰值 467.8 MiB |
| Markdown / whitespace | 本地链接与 fence 检查、`git diff --check` 通过 | README outline 与新增/修改文档核对，最终暂存另复核 |

验证串行运行，使用 `nice -n 10`；普通 gate 的进程树预算为 1.5 GiB、300 秒、
memory-free 15%，真实故障脚本为 2 GiB、180 秒。无资源超限中止。短 gate 只有启动
采样，不能据此推算实际峰值。日志与逐次采样索引保存在
`var/deepagents-delivery/temporal-final-validation.receipt.json`。
本轮只新增已获批准的可选 Temporal SDK 及其锁定传递依赖，默认依赖与现有包版本保持。
没有改 Go、scanner 或既有 guarded mutation anchors，未运行对应 gate。

## 上一轮交付基线

下列长任务客户端交付记录属于 **`e67e174` 的上一轮基线**；其中测试数字与依赖描述
保留当时范围，不作为新增 Temporal 代码的测试结果。

上一轮在实现 `6328007`、架构说明 `9ba2a49` 的基础上，补充异步长任务客户端、
API/Worker 分离部署、stateful Run 同步 Checkpoint 策略和可测试的持久任务库合同。
原 Python/Go runtime 保持原入口；本模块继续使用标准 Agent Server 调度。
**生产 PostgreSQL/Redis 崩溃与部署恢复尚未验收，不能据此宣布生产恢复保证。**

运行合同见 [LONG_TASKS.md](LONG_TASKS.md)，下一阶段控制面设计见
[持久执行设计](../docs/AGENT_SERVER_DURABLE_EXECUTION_DESIGN.md)。

## 标准入口历史实现与验收依据（e67e174）

| 要求 | 当前实现与直接证据 | 边界 |
| --- | --- | --- |
| 独立 Deep Agents server | `graph.py`、`langgraph.json`、独立 `pyproject.toml` / `uv.lock` | 不调用原 mini-loop runtime |
| 异步提交与任务观察 | `jobs.py` / `scripts/jobs.py`；返回可保存的 JobHandle，支持 status/wait/state/cancel/resume | wait 超时不取消；state 是当前 Thread，不是旧 Run 的固定快照 |
| 多任务并发 | 提交默认独立 Thread；同 Thread `enqueue` 串行；Worker 默认两个槽 | 未做生产容量或多副本负载验收 |
| 创建结果未知 | 写请求不自动重试；`SubmissionUncertain` 区分 create_thread/submit/resume/cancel 的核对步骤 | request metadata 不是原子去重键；不存在应用级 admission/outbox |
| 显式续跑 | 校验终态、最新 Run、未完成任务和 graph interrupt 后，`input=None` 创建新 Run | 单一操作者协调；预检查存在 TOCTOU，不能当作跨进程锁 |
| 同步 Checkpoint | 客户端请求 sync；认证 create_run hook 强制 sync/checkpoint_during | 恢复约定针对保留的 stateful Run；stateless/delete 不承诺保留 |
| 身份隔离 | Bearer→owner；Thread/Run owner filter；Store namespace 首段为 owner；Cron 拒绝 | context/config 不能授予身份；memory 不是凭据仓库 |
| 部署分离 | 同一不可变镜像的 API=0 槽、Worker=2 槽；PG 持久卷、Redis 临时协调；资源和停机窗口显式配置 | 需要适用许可证，实际生产栈未运行 |
| 持久任务抽象 | `task_contracts.py` 的不可变类型、状态迁移、admission/fencing 纯函数、adapter Protocol | 仅库合同，没有实际队列、数据库、outbox、lease 或 Task API |
| 架构同步 | canonical Mermaid、边界与 baseline、Archify JSON/generated HTML、分层图和 prompt | 图不能替代生产恢复实测 |

版本保持 Deep Agents 0.7.25、Agent Server 0.16.0、CLI 0.4.35、SDK 0.4.7、
OpenAI adapter 1.7.2；Python 3.12.10。没有新增依赖，传递依赖与包 hashes 保持在 `uv.lock`。

## 上一轮验证记录（e67e174）

| Gate | 已读取结果 | 范围与资源 |
| --- | --- | --- |
| 模块 pytest | **218 passed in 1.77s** | wrapper 4.09 秒，进程树采样峰值 144.6 MiB，最低 memory-free 48%；含 graph、auth、jobs、部署结构和 task contracts |
| 原 Python pytest | **2256 passed, 28 skipped, 104 warnings**，pytest 98.86 秒 | wrapper 100.03 秒；采样峰值 323.1 MiB；最低 memory-free 38%；不包含新模块 |
| 原 Python invariants | **77 modules checked**，退出 0 | 仅既有 Python package，不验证新模块的持久执行 |
| 长任务 HTTP fault smoke | **6 组通过**，22.86 秒 | run_id `18414d1d-7444-4524-a52c-f587c36d9832`；峰值 177.2 MiB，memory-free 47%→46%；`production_acceptance=false` |
| Archify validate + deliver | **9/9 showcase**，0 errors / 0 warnings | 生成 HTML 的变更泳道已静态渲染并目检深浅主题；未运行浏览器交互验收 |
| Research Atlas | content 索引 21 篇；build 与 5/5 HTML tests 通过 | 恢复现有 lockfile 的 463 个依赖；package/lock hashes 不变；最终测试阶段峰值 530 MiB |
| Wheel / Compose | offline wheel 构建、包内容核对与 Compose config 检查通过 | 包含 jobs/task_contracts/instructions；不含测试 graph/.env；配置检查仅使用临时合成凭据 |
| Markdown / whitespace | README/模块 Markdown outline、`git diff --check` 通过 | 最终暂存内容由主交付流程复核 |

模块与根 pytest 通过 `nice -n 10` 串行运行。重测试配置与采样结果保存在
`var/deepagents-delivery/current-longtasks-*.log`、`*.summary.json` 和
`*.resources.jsonl`；没有资源超预算中止。fault smoke 也监测服务进程树 RSS，结果
以本次 receipt 的 `run_id`、终态和对应日志为准。修复后的脚本在开始前原子替换旧
receipt，running/failed/resource_blocked 均不冒充 passed。

本轮没有修改 Go 实现、source scanner 或 guarded mutation anchors，不运行 Go、
scan 或 mutation gate。认证参数策略由新模块测试和真实 HTTP 故障检查单独覆盖，
原仓库的 invariants 不能替代这些检查。

## 初始模块的历史验证

以下结果属于 `6328007` 的初始交付，保留其原范围，不当作本轮代码的新验收：

- 模块 **82 passed**，1.61 秒；production factory 的 ChatOpenAI 构造/schema 检查不发送 provider 请求。
- `scripts/smoke.py`：10 组实际 `langgraph dev` HTTP 检查，使用 `tests/` 的无网络模型；覆盖文件、general-purpose 子代理、owner 隔离、Store、SSE、取消和优雅开发重启。服务树峰值 175.2 MiB，预算 1 GiB。
- 原根 pytest：2256 passed / 28 skipped / 104 warnings，138.98 秒；原 invariants：77 modules。
- `langgraph validate`、`uv lock --check`、`uv sync --locked`、`uv build --wheel` 通过；初始 wheel 包含 `instructions.md`，不包含 `.env` 或测试 graph。本轮新增 package 模块另经上表 wheel 检查。
- 原 Compose 使用临时合成凭据运行结构配置检查；这不证明当前 API/Worker 拆分后的生产服务启动。

原始日志与资源采样位于忽略目录 `var/deepagents-delivery/`，不随代码发布用户凭据。

## 架构产物

- 规范：[README Mermaid](../README.md#architecture)；模块展开：[分层说明](ARCHITECTURE.md)。
- Archify source：[architecture JSON](../docs/mini-loop-system.architecture.json)；generated [interactive HTML](../docs/mini-loop-system.architecture.html)。
  当前冷停验收迭代 Spec SHA-256 `78a5f120f829378b38417639c04a9ed213ff2675dc96f81083648f5579778052`；
  HTML SHA-256 `08daea75ce0cff46aec47c0c7a25395aefba06aca5b5e2744c9859bf9d58ec41`。
- 完整 base：[PNG](../docs/mini-loop-layered-architecture.png) 与 [exact prompt](../docs/mini-loop-layered-architecture.prompt.txt)。
- 初始模块历史标注（6328007）：[PNG](../docs/audits/deepagents-server-2026-10-16.png) 与 [exact prompt](../docs/audits/deepagents-server-2026-10-16.prompt.txt)。
- 标准模块聚焦图（e67e174）：[PNG](docs/agent-server-layered-architecture.png) 与 [exact prompt](docs/agent-server-layered-architecture.prompt.txt)。
- 上一轮 Temporal 泳道标注：[PNG](../docs/audits/temporal-prototype-2026-10-16.png) 与 [exact prompt](../docs/audits/temporal-prototype-2026-10-16.prompt.txt)。
- 上一轮 Task HTTP 图（9af554a）：[PNG](../docs/audits/task-server-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-2026-10-16.prompt.txt)。
  修正提示词：[接口路径与身份范围](../docs/audits/task-server-2026-10-16.repair.prompt.txt)、[移除歧义回流箭头](../docs/audits/task-server-2026-10-16.arrow-repair.prompt.txt)。
- 上一轮 V2 独立 Agent Server 图（98de8d2）：[PNG](../docs/audits/task-server-can-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-can-2026-10-16.prompt.txt)。
  [修图提示词](../docs/audits/task-server-can-2026-10-16.repair.prompt.txt) 校正初稿的接口示例、无状态 API 图标、父模型计数措辞与 CAN 回流线。
- 上一轮滚动替换图（401e4d5）：[PNG](../docs/audits/task-server-rollout-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-rollout-2026-10-16.prompt.txt)。
  [修图提示词](../docs/audits/task-server-rollout-2026-10-16.repair.prompt.txt) 校正模型 I/O 箭头方向，并将 CAN 状态传递与 Activity 心跳/结果分开表述。

- 上一轮 TLS 独立图（06a93d4）：[PNG](../docs/audits/task-server-tls-2026-10-16.png) 与 [exact prompt](../docs/audits/task-server-tls-2026-10-16.prompt.txt)。
  修图提示词：[路径、无状态图标与颜色](../docs/audits/task-server-tls-2026-10-16.repair.prompt.txt)、
  [凭据与认证边界](../docs/audits/task-server-tls-2026-10-16.boundary-repair.prompt.txt)。
  两个未采用的完整 base 草稿提示词也保留：[初稿](../docs/audits/task-server-tls-2026-10-16.base-initial.prompt.txt)、
  [修复尝试](../docs/audits/task-server-tls-2026-10-16.base-repair.prompt.txt)；它们压缩了 CAN / lifecycle 区域，故改为最小文字编辑。

- 上一轮容器独立图（08b4333）：[PNG](../docs/audits/task-server-container-2026-10-16.png)、[exact prompt](../docs/audits/task-server-container-2026-10-16.prompt.txt)、[修图 prompt](../docs/audits/task-server-container-2026-10-16.repair.prompt.txt)。
- 上一轮完整 base 修改链：[初始 prompt](../docs/audits/task-server-container-2026-10-16.base.prompt.txt)、[期限措辞修复](../docs/audits/task-server-container-2026-10-16.base-repair.prompt.txt)。
  [产物摘要与目检记录](docs/task-container-diagrams.json) 固定全部最终 PNG 和精确提示词。

上一轮 built-in imagegen 在完整 base 中保留旧业务所有分层、CAN 回流与生命周期区域，
增加 Task 容器、角色 Secret、readiness 和 20 秒停止预算；修正初稿将进程期限误写成
硬杀的文字。独立图橙色表示新增容器入口、凭据挂载、探针、Linux 信号唤醒和本机验收；
既有 TLS/CAN 保持蓝色。修图移除 API 副本间不存在的连接，并把 readiness 限定为
namespace 可用。图中明确离线 fixture、默认 Worker 入口和生产未验证项。
最终 PNG 已逐项静态目检，模型 I/O、Temporal 权威与原业务独立边界一致。
Archify HTML 从冻结 JSON 生成，9/9、零错误/警告；两次局部路由修复后明暗整图及
Task 泳道静态目检通过，56 个非 Task 组件字节未变。浏览器交互未验，本地 SVG 使用
字体 fallback；回执见 `var/deepagents-delivery/task-container-archify.receipt.json`。

- 本轮冷停独立图：[PNG](../docs/audits/task-server-cold-restart-2026-10-16.png)、[exact prompt](../docs/audits/task-server-cold-restart-2026-10-16.prompt.txt)、
  [接口与能力措辞修复](../docs/audits/task-server-cold-restart-2026-10-16.repair.prompt.txt)、
  [请求示例与父工具计数修复](../docs/audits/task-server-cold-restart-2026-10-16.final-repair.prompt.txt)。
  [产物与精确请求摘要](docs/task-cold-restart-diagrams.json) 保存本轮PNG、完整base和提示词hash。

本轮从完整base与上一轮独立图生成冷停验收图：已有五层全部蓝色，新增组合验收
路径使用橙色。生产拓扑未变，完整base保持原样；修图移除误加的HTTPS/速率限制，
校正HTTP header、模型I/O箭头及父工具计数。最终PNG已目检。交互图仅更新说明，
从JSON重新生成，明暗静态目检通过、无几何调整；浏览器交互未验，SVG使用fallback
字体。回执为 `var/deepagents-delivery/task-cold-archify.receipt.json`。

## 未执行和能力边界

本轮**未执行真实 OpenAI 推理、镜像 registry 发布、Compose 全链路部署、Temporal 生产集群 HA / 容量、
物理断电、任意兼容代码升级或带许可证的标准 Agent Server PostgreSQL/Redis 恢复验收**。
Task HTTP 的本机容器 SIGKILL 恢复、默认 Worker SIGTERM 退出，以及历史进程替换和 TLS 测试隧道重连已验证；该证据不能扩展为生产数据库故障转移或跨
版本升级保证。保留 v1 原实现及其已有任务合同已单独验证；这不等于任意版本升级可重放。
V2 全链上限为 24 小时、512 次父模型边界；每次父图调用另有 128 步限制。原生子代理
有自己的递归上限，不能把 512 或 128 宣称为包含全部子代理和摘要模型调用的总量。
完整 saver 写入日志有 16 MiB 原始、384 KiB 压缩、512 KiB CAN 信封上限；超出后进入
不可重试失败。日志不做压缩后的语义裁剪，状态较大的任务可能提前触顶，尚无无限长执行保证。

Task HTTP 现在具备直接委托 Temporal 的任务入口及持久生命周期事件接口，但没有
独立业务 SQL TaskControlPlane、事务 admission/outbox、租户/集群全局配额或任意外部
工具的 fenced 写入；`task_contracts.py` 中这些合同仍是未来设计。幂等与事件受 History
保留期约束；外部副作用不保证 exactly-once。事件分页不是持久 token 重放日志。
Task HTTP 路径没有跨任务 Memory；V2 已启用上述受控 Continue-As-New。确定性摘要
适配固定 Deep Agents 版本的内部接点；依赖升级必须重做原生子代理与摘要的冷重放验收。
原生 Temporal RPC
必须留在受信任 API/Worker 边界内，HTTP Bearer 不是 Temporal 原生访问控制。

不实现 MDA Compiler、Context Hub、托管 Sandbox、OAuth 或 Channels；Cron 拒绝。
没有宿主 shell/local filesystem backend；`execute` 名称可能在 schema 中出现，执行
仍由 backend 拒绝。标准入口的 Memory 同 owner 跨 Thread 共享，但没有并发编辑 CAS；不要存
secrets 或把它作为业务唯一事实来源。部署升级须保留 graph/schema/工具兼容性、
owner 映射和持久卷；删除数据后不承诺恢复。
