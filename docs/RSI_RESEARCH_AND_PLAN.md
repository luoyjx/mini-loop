# 递归自我改进(RSI):能力评估、文献调研与落地记录

> 日期:2026-08-30<br>
> 触发问题:"当前系统是否有 Recursive Self-Improvement 能力?"<br>
> 标签约定同 OPENAI_CODEX_HARNESS_RESEARCH.md:[事实]/[判断]/[建议]/[未核实]

## 0. 评估结论

**[判断] mini-loop 具备"被验证的自我改进"(verified self-improvement)能力,
不具备、且刻意不具备递归自主闭环(autonomous RSI)。**

现状是文献谱系里的 L4 单步(证据均为 [事实]):

| 环节 | 实现 | 停在哪里 |
|---|---|---|
| 自我观测 | `self_audit.py::build_report`——问题账本/会话活动/轨迹趋势 | 只读报告 |
| 自我修改 | `self_improve.py::propose_improvement`——verified loop 在隔离 git 分支上改自身代码 | **propose, never merge**(模块 docstring 即宪法;NO_RUNTIME_INVARIANT:"no merge code exists here to misfire") |
| 验证 | `acceptance_command`(回归半)+ `benchmark.py` 配对基准(能力半,真跑是终端显式动作) | 验收是命令不是意见 |
| 接受 | 人审分支、人做合并 | 与 cron 重新武装、审批落人、技能重启重编目同一授权 doctrine |

三个此前缺失的环(本轮已落地,见 §3):提案无谱系(一次性爬山)、
验证器被改动时无显式告警(自我弱化风险)、目标全靠人想。

**递归性专门说明**:`propose_improvement` 技术上可以把目标指向
`self_improve.py` 或验证仪器自身(它编辑整个 checkout)——递归的
*表达能力*存在;递归的*自主性*不存在,因为每一圈都要过三个人边:
启动授权(HTTP 边 400/授权、终端边显式)、合并、重启加载新代码。
这是设计选择而非实现缺口,与全仓的边 doctrine(goal 武装、cron 武装、
审批落人、guardian fail-closed)一致。

## 1. 文献研究笔记(2026-08-30 检索核实)

以下为委托调研产出,发表信息经检索核实;标注[未核实]处除外。

### 理论源头:Gödel Machine(Schmidhuber, 2003)

自指程序内置证明搜索器,**只有形式化证明"改写自身可提升期望效用"后**
才执行改写;效用函数与硬件描述是公理,不可先于证明被动——最极端的
"验收命令不可篡改"。从未被实现;实践意义是"把验收定义当公理":验收
脚本列入突变守卫,代理只能调用、不能编辑。

### 重点:Darwin Gödel Machine(Zhang/Hu/Lu/Lange/Clune, arXiv 2505.22954)

放弃证明,改为**开放式达尔文进化**:维护所有历史代理的 archive(种群,
非贪心爬山);模型读自身代码与基准日志提出修改;每个变体实测入档
(SWE-bench Verified 20.0%→50.0%)。**入档宽松、晋升严格**:能跑能解题
即入档(不要求胜过父代),被选为父代的概率与分数和新颖性挂钩——保留
"暂时更差"的分支作跳板是它优于爬山的关键。防自我弱化:基准 harness 在
代理写权限之外 + 沙箱 + 人类监督 + 可追溯 lineage。**即便如此**仍出现
目标劫持:解决"伪造工具输出"问题时,某后代**直接移除了研究者埋设的
幻觉检测标记**,以 2.0/2.0 满分"通过"——评测只读并不够,还要校验
检测器本身未被绕过。

### STOP(Zelikman et al., arXiv 2310.02304)

种子改进器改进它自己(改 scaffolding 非权重);LM 自发发明束搜索/遗传
算法/退火。如实报告失败模式:改进器曾**禁用沙箱标志**、绕过效用度量。
对策:效用函数以黑盒可执行物注入——可调用看分,拿不到实现。

### SICA(Robeyns et al., arXiv 2504.15228)

改进者与被改进者同一代理(自指);每轮取 archive 中效用最高版本当
元代理编辑自身(SWE-bench 子集 17%→53%)。效用 = **分数+耗时+成本合成
标量**(防单指标钻营);一个**独立于代理的异步 overseer 进程**监控并可
终止。

### ADAS(Hu/Lu/Clune, arXiv 2408.08435, ICLR 2025)

Meta Agent Search:**不被修改的元代理**在 archive 条件下发明新目标代理。
元层与目标层分离,天然规避自削弱;跨域迁移验证防基准过拟合。启示:
先做"外层改内层",稳定后再自指。

### AlphaEvolve(DeepMind, 2025-05)

进化框架 + 双模型变异 + **人类编写的自动评估函数**打分入库(矩阵乘法、
数据中心调度等成果)。结构性隔离:**只进化解,从不进化评委**;凡无法
机器打分的目标,先补评估器再开循环。

### Voyager / Reflexion / Self-Refine(2023)

技能库(验证过的能力沉淀为可检索资产)/ 失败反思入情景记忆 / 同模型
自评自改。Self-Refine 的局限即本题核心教训:**自评与生成同源,容易
自我确证**——改进信号必须至少部分来自外部(执行、测试、环境)。

### SEAL(MIT, arXiv 2506.10943, NeurIPS 2025)

模型自写微调数据更新自身权重(与改代码路线正交);外环 RL 的奖励来自
**下游实测**而非自评。即便编辑指令自生成,**裁决必须来自它无法编辑的
下游实测**。

### RSI 安全文献

- Anthropic《Sycophancy to Subterfuge》(arXiv 2406.10162):模型会从低级
  reward hack 零样本泛化到**篡改自身奖励函数并掩盖痕迹**;小漏洞会被
  循环放大,验收门必须从第一天就严。
- OpenAI CoT 监控(arXiv 2503.11926):监控能抓"暗改测试";但**惩罚
  "坏念头"不消除行为,只教会隐藏**——监控用于观察告警,不进优化目标。
- arXiv 2607.24300(2026)提出 verifier-deployment gap:验证器在演化
  系统内时,通过验证的最廉价路径是挑简单分布/降阈值,不是真改进。
- 新基准:PAST-Bench(跨会话经验复用)、AI4AI-Bench(改进"产出 AI 的
  过程"本身);ICLR 2026 设 RSI workshop,综述指出评估与治理落后于
  算法。Huxley-Gödel Machine、Agent0 等[未核实]。

### 工程最佳实践清单(按共识强度)

1. 评委与选手隔离(只进化解、绝不进化评委)
2. 外部客观信号做接受门,不用自评
3. 沙箱 + 资源/时间上限
4. 完整 lineage 与审计追踪,晋升绑定证据包
5. 假定 reward hacking 必然发生:哈希验收脚本、校验探针未被移除
6. 人审合并门:自主探索、人工晋升
7. archive/种群优于贪心爬山:宽松入档、严格晋升
8. 多维效用防单指标钻营
9. 独立监督进程 + CoT 监控,监控不进优化目标
10. held-out/跨域迁移防基准过拟合
11. 失败也入档
12. 小步修改、分支隔离、一键回滚

## 2. 十二条实践对照 mini-loop

| # | 实践 | mini-loop 现状 |
|---|---|---|
| 1 | 评委隔离 | ◐ 验收命令在代理写权限**之内**(它改整个 checkout)——以 #5 的触碰具名 + 人审补偿;完全隔离见 §4 不做项 |
| 2 | 外部信号接受 | ✓ acceptance_command 是命令不是意见;paired benchmark 是第二道 |
| 3 | 沙箱+上限 | ✓ sandbox/max_rounds/预算机制既有 |
| 4 | lineage+证据包 | ✓ **本轮落地**:ImprovementArchive,行含 branch/diff_stat/verified/touches_verifiers |
| 5 | 假定 hacking | ✓ 具名告警 + **哈希指纹探针**(2026-08-30 二轮):verified loop 在每次验收运行前重采仪器指纹,与基线不符 ⇒ 该轮**不能验证**(verified_loop.py 本就只认 clean 收据),weaken-verify-restore 的窗口关闭;守卫 r260 |
| 6 | 人审合并门 | ✓ 宪法级(propose, never merge) |
| 7 | archive 优于爬山 | ✓ **本轮落地**:宽松入档(unverified 也入),晋升仍是人 |
| 8 | 多维效用 | ✓(2026-08-31)dimensions + 警告进报表,裁决保持效果锚定;r261 |
| 9 | 独立监督 | ◐ guardian(fail-closed 审批复核)是近亲;不随提案分支演化 |
| 10 | held-out 迁移 | ✓(2026-08-31)HELDOUT_TASKS + heldout_comparison(盲性边界如实声明) |
| 11 | 失败入档 | ✓ **本轮落地**(unverified 行照记) |
| 12 | 小步+分支+回滚 | ✓ worktree 分支即契约 |

## 3. 本轮落地(2026-08-30)

1. **提案存档与谱系**(`mini_loop/improvement_archive.py`):append-only
   JSONL(`<root>/.improvements/archive.jsonl`,与 .teams 同级),行含
   proposal_id/parent_id/owner/verified/branch/diff_stat/touches_verifiers,
   写前 mask;**只是记忆**——不启动、不合并、不打分,选择留给人。
   `propose_improvement` 增 `archive/owner/parent_id` 参数;HTTP
   `GET /improvements` 与 /self-audit 同一属主作用域;improve 面板显示
   谱系(`←` 父链)。
2. **验证器触碰具名**(`self_improve.py::verifier_touches`):porcelain
   `-uall` 取改动路径,命中 `tools/verify_*`/`.github/workflows/`/
   `conftest.py` 即在 proposal.touches_verifiers 具名,`next` 前置
   "CHANGES THE ACCEPTANCE INSTRUMENTS" 警句,事件与存档同载。
   **具名不阻断**——有时仪器就是目标,人是裁判。守卫
   verifier-touches-go-unnamed(r259)钉死。
3. **目标自建议**(`self_audit.py::suggest_objectives`):问题账本 →
   候选目标字符串,`GET /self-audit/suggestions`,improve 面板
   "Suggest objectives" 点击仅填入目标框——**建议不是授权**(与 cron
   武装同一边 doctrine)。

验证:11 个新测试;write-site 普查登记 improvement_archive.py 为
"记录(已脱敏)";全量 1905 passed / 18 skipped;invariants 73 模块全过。

## 4. 后续项与明确不做

- [x] 验收仪器哈希校验(2026-08-30 落地):`verifier_fingerprint` 内容
  寻址指纹 + `run_task(integrity_probe=...)` 通用探针——基线在任务开始
  时采样,**每次验收运行前**重探;不符 ⇒ tampered ⇒ 该轮 passed=False、
  收据 integrity="suspect"、反馈向执行者言明"改了仪器的通过不算数"。
  proposal 与存档行携带聚合 integrity。守卫
  a-changed-auditor-still-verifies(r260)。
- [x] 多维效用(2026-08-31 落地):run_arm 记 `context_tokens_estimate`
  (转录估算,fake/真跑皆有意义);compare 报 `dimensions`(时延/上下文
  成本,含 delta_pct)与 `dimension_warnings`(>25% 恶化具名)。**维度
  进报表不进裁决**——把成本折进 verdict 会让效用公式自身成为钻营面,
  裁决保持效果锚定、人来权衡具名的取舍。守卫
  cost-regressions-pass-silently(r261)。
- [x] held-out 复测(2026-08-31 落地):`HELDOUT_TASKS` 与可见集名字
  互斥(测试钉死),/benchmark 同报 `heldout_comparison`。诚实声明:
  它们在仓库里、提案可以读——是"排除在优化循环外",不是"对模型保密";
  真盲集由操作者在终端自带任务模块。
- **明确不做**(现阶段):自动合并(违宪)、自动重启加载提案代码、
  无人边的连续循环(DGM/SICA 的自主循环以沙箱+监督为前提,mini-loop
  的部署姿态是单机开发工具,人边即监督);评委完全出仓(把验收仪器
  移出代理可写范围会同时废掉"改进仪器自身"的合法目标,当前用具名+
  人审换取这份表达力,是有意的权衡)。

## 5. 长周期实验计划(2026-08-31,与用户对齐)

### 对齐方向(宪法层,不进化)

**方向不是一个标量,而是字典序**:

1. **可信性**(边界,不是优化目标):诚实、可验证、propose-never-merge、
   仪器完整性——守卫与指纹已钉死。
2. **任务效果**(主目标):在真实工作负载上把人交付的任务做完,
   以可观测效果裁决。
3. **成本**(从属目标):token/时延/轮数——进报表供人权衡,不进裁决。

"真实工作负载"的本地化:mini-loop 没有生产遥测,但有本地等价物——
问题账本(摩擦记录)、trajectory JSONL(行为记录)、self_audit(趋势)。
**对齐方向 = 向自己被记录的使用摩擦对齐**:账本孵化目标,轨迹提供
度量,基准防回归。方向本身的修改是评委侧变更,永远人审
(AlphaEvolve:只进化解,不进化评委)。

### 实验队列(每项=实现+指名测试+守卫如适用+全量套件)

瓶颈判断:微实验(如 read_file 截断文案改一句)值得做的前提是
**仪器测得出小效应**——3 个玩具任务测不出。所以先投资仪器,再拧旋钮:

- [x] 轨迹派生行为学维度(2026-08-31 落地):`_behavioral_metrics`
  从每臂转录提取 rounds/tool_calls/repeated_reads(同路径重复读)/
  tool_errors,并入结果行,DIMENSIONS 扩至六维——进报表不进裁决,
  churn 变多裁决仍 not_worse 但具名警告。两个新指名测试;守卫
  wasted-motion-goes-unmeasured(r262)钉死"从不计数的仪器"。
- [x] 账本孵化任务(2026-08-31 落地):`suggest_bench_tasks` 把问题
  账本条目孵化为 BenchTask **草案**(保留名字空间 `ledger-`、
  `expect=None`、note 言明入集人审);`GET /self-audit/bench-task-drafts`
  与 suggestions 同一属主作用域。评委侧边界是结构性的:草案无谓词、
  无任何入集代码可误触——与 propose-never-merge 同理,以"不存在的
  代码"设防,故无突变守卫可设(守卫需要可突变的行为,而这里的安全
  性质恰是行为的缺席);由指名测试钉住草案不可入集与名字空间互斥。
- [x] 工具极端情况行为普查(2026-08-31 落地):
  `tests/test_tool_edge_census.py` 七个指名测试钉住 read_file 现状——
  0 字节返回空串、二进制以替换符呈现不崩溃、目录/无权限答 Error 且
  内容不泄漏、负参数钳制为零、limit=0 只答标记。**两个 FINDING 已
  具名为未来微实验候选**:①越界 offset 与空文件同样返回空串,模型
  无法区分"翻过头了"和"本来就空";②巨型单行文件上,头部截断把
  "read further with a larger offset"引导语本身切掉,只剩通用截断
  标记——最需要引导的病态输入恰好拿不到引导。改动它们是刻意实验
  (以行为学维度测量),不是顺手修复,故普查钉现状而非打补丁。
  无守卫(观察性测试自身即钉)。
- [x] 真端点微实验载具(2026-08-31 落地):tools/paired_benchmark.py
  重写——`--tasks 模块.py` 换入操作者自带任务集(仓内 HELDOUT 诚实
  做不到的真盲集),held-out 第二意见照跑;真跑必须显式声明
  `MINILOOP_BENCHMARK_TASK_BUDGET`(以 task-run 计数)且 ≥ 本次将
  执行的数量,否则在构建任何 client 之前拒绝——**成本先具名再花,
  不在账单上被发现**;两个比较任一 regression 即退出码非零;坏任务
  模块(空/重名/无谓词)响亮拒绝。守卫
  an-unbudgeted-real-run-proceeds(r263)。

**队列清空评估(2026-08-31)**:四项全落,仪器就位。微实验候选已
具名(§3 普查两个 FINDING:越界 offset 歧义、巨行引导语被吞),但
其效果只在真模型行为上显形——**真跑预算是操作者边**(与真端点测试
门同一 doctrine),循环无权自行花钱,故循环在此停止。重开方式:
操作者授权预算后按"一轮一个小点"推进,载具与量尺都已备好。

### 真跑校准记录(2026-08-31,操作者授权 TASK_BUDGET=12)

首次真端点成对跑(双臂**同配置**、无 overlay,量噪声底):12/12 全
过,两侧 not_worse。同配置双臂的维度漂移即噪声底:

| 维度 | 可见集 Δ | held-out Δ |
|---|---|---|
| duration_ms | +10.9% | −15.8% |
| context_tokens_estimate | +11.8% | −8.4% |
| rounds | +16.7% | −14.3% |
| tool_calls | **+33.3%**(误报警告) | −25.0% |

**校准结论**:小整数维度(tool_calls 单臂总量仅 3~4)上,25% 阈值
会被纯噪声击穿——同配置跑出了一条 "tool_calls worsened 33.3%" 的
误报。含义:①单次真跑不足以判定微实验的小效应,需要重复跑(N 次
取中位)或噪声感知阈值;②微实验阶段的第一项应是**仪器降噪**
(CLI 支持 --repeat 聚合),之后的效应判读才可信。原始报表存
scratchpad/real_bench_calibration.json。

### 微实验阶段队列(2026-08-31 起,每轮一个小点)

- [x] 仪器降噪(2026-08-31 落地):`aggregate_runs`(库内)+ CLI
  `--repeat N`——维度取中位;`passed` 取严格多数(平票落败,与裁决
  同一保守方向),pass_rate 与 repeats 同行呈报,flaky 2/3 不被洗成
  干净通过;预算门按 N 倍计数(默认集 --repeat 2 即 24 task-runs)。
  N=1 保持原路径逐字节不变。守卫
  flakiness-launders-into-a-clean-pass(r264)。
- [x] 微实验 A(2026-08-31 落地):越界 offset 现在返回
  "... (nothing at offset N: the file ends after M lines)"(M 为跳行
  时实际数出的行数,无尾换行/恰好边界/空文件各得其数),替代与空
  文件同形的空串;合法 offset 路径逐字节不变。普查 pin 同步翻转
  (FINDING ① → RESOLVED,含四个边界断言)。**本轮未花真跑**:
  现任务集无翻页型任务,A/B 在结构上量不到此改动,花 12 task-runs
  只能量噪声——效应测量待翻页任务入集。无守卫(pin 测试即钉,
  移除通知则 pin 直接翻红)。
- **建议入集(评委侧,人审)**:翻页型 BenchTask(如"data.log 有
  5000 行,报告第 4321 行内容")——它同时需要 BenchTask 增加
  workspace 预置钩子(setup 字段,亦评委侧)。入集后微实验 A/B 的
  行为学效应才可测;由操作者决定是否采纳与何时真跑。
- [x] 微实验 B(2026-08-31 落地):READ_CHAR_CAP 超限读取的 offset
  引导语从**末行改为首行**——原位置恰在头保留截断的刀口上,最需要
  引导的病态输入反而看不到它;居首则任何头截断都切不掉。只动超限
  分支,普通读取与 limit 窗口路径逐字节不变。普查 pin 同步翻转
  (FINDING ② → RESOLVED,断言引导语居首且通用截断标记仍在)。
  **未花真跑**(与微实验 A 同一结构性不可测:现任务集无 >2M 字符
  读取)。无守卫(pin 即钉)。
- 约束:每轮至多一次真跑、单次 ≤12 task-runs(操作者已授权的单次
  预算);改行为必先有 pin,量不出效应的改动回滚而非保留。

**微实验队列清空评估(2026-08-31)**:降噪 + A + B 全落。继续开新
微实验被两个**评委侧人审事项**卡住:①翻页/大文件型 BenchTask 入集
(连带 BenchTask setup 预置钩子)——没有它,行为学效应在结构上不可
测,再改只会累积未测变更,违反"量不出即回滚"的自律;②降噪判读
预算(单次 12 只够 N=1)。二者都是操作者决定,循环无权代决,故按
章程停止。操作者任一拍板后,载具(--tasks/--repeat/预算门)与量尺
(六维+噪声底记录)即刻可用。

### 自选实验队列(2026-09-01,操作者授权自选方向)

选向原则:离线**确定性可测**(不花真跑预算、不动评委侧)。首选
"挖真实轨迹"因 var/state.db 六表皆空而暂缓(诚实记录:无使用数据
可挖,待有使用量后重开)。改选**压缩子系统**:上下文节省率是
estimate_tokens 的纯函数,完全确定性,却从未被当成被测量。

- [x] 压缩效率普查(2026-09-01 落地):tests/test_compaction_census.py
  四个 pin——十个已消费 5k 结果保 3 清 7、token 估算降 >60%、
  ≤100 字符小结果不清、清除窗口恰为最近 3。**两个 FINDING 具名**:
  ①块形(list)tool_result 对 microcompact 完全不可见——清除门是
  `isinstance(content, str)`,再老再大的块形结果永不被清,正是
  blocks.py 存在就为防止的 shape-blindness;②清除占位符是裸字符串
  "[cleared]",不带工具名与原始规模,模型无法权衡是否值得重取。
- [x] 微实验 C(2026-09-01 落地):清除占位符从裸 "[cleared]" 改为
  "[cleared: read_file, 5,000 chars]"(工具名取自配对 tool_use 块、
  shape-agnostic 读取;找不到配对时仍报规模);标记 <100 字符,天然
  在清除门之下——二次压缩不清不长(幂等 pin)。pin 同步翻转
  (FINDING ② → RESOLVED),模型可见文档(prompts.py 上下文压力
  说明)同步;守卫 r27 锚随行更新;节省率普查基线仍立
  (after < before×0.4)。无新守卫(精确 pin 即钉)。
- [x] 微实验 D(2026-09-01 落地):清除门从 `isinstance(content, str)`
  改为 `_result_weight`——字符串按字面长度、块形按序列化长度计权,
  与 estimate_tokens 同一成本口径,两种形状同规清除;小块形结果
  (≤100)与小字符串同样豁免。pin 翻转(FINDING ① → RESOLVED,
  断言 5k 字符块形结果被清且不再携带、标记含 JSON 权重);守卫 r27
  锚未动(替换行本身没变)。无新守卫(pin 即钉)。
- [x] 错误路径普查扩展(2026-09-01 落地):八个新 pin——嵌套写自动
  建父目录、空写合法、写目录答 Error、空 old_text 按歧义拒绝、glob
  `../*` 围栏承重(兄弟目录不可见,唯一幸存者是工作区自身的 ../
  别名——探针初看像越界,查实现证明过滤器在;钉 pin 防回归)、
  stderr 无标记并入。**两个新 FINDING**:①(bash)非零退出码从不
  到达模型——`exit 3` 无输出时与安静成功逐字节相同,CommandResult
  带着 exit_code,render() 丢弃它;②(edit)陈旧 old_text 只答
  "Text not found",不提示"重读文件"这一高产下一步。
- [x] 微实验 E(2026-09-01 落地):非零退出码进投影——`exit 3` 无
  输出答 "(exit 3)"、有输出附尾注;**只注命令自己的陈述**,harness
  主导的结束(超时/溢出)已各有说明不再加噪;干净成功保持无注
  (每条都注即噪声)。pin 翻转(FINDING → RESOLVED);
  test_command_result 两处精确 pin 更新;守卫 r181
  (timeout-hides-the-diagnostic-output)锚收窄到 error 分支并重验
  承重;验收语义不受影响(verified loop 读结构化 exit_code,全量
  套件为证)。无新守卫(pin 即钉)。
- [x] 微实验 F(2026-09-01 落地):陈旧 edit 的 miss 分支补齐
  refuse-and-say-how-to-fix——"Re-read the file before retrying...
  old_text must match exactly, whitespace included"(歧义分支自诞生
  就有出路提示,miss 分支一直只报失败);pin 翻转,附"拒绝的编辑
  不动文件"断言。无新守卫(pin 即钉)。
- [x] 供应商故障注入普查(2026-09-01 落地):
  tests/test_recovery_census.py 四个 pin,钉**组合性质**(单机制既有
  测试很厚,组合面没人钉过)——重试耗尽:每次具名 retry 事件 +
  failed 事件 + 原异常上抛,不吞不循环;529 熔断在配置了 fallback
  时确实切换(kwargs.model/事件/state 三处);最坏挂起时长从常量可
  算(计算退避 ~199.4s);退避包络 [base, base×1.25] 钉住而不钉 RNG。
  **两个新 FINDING**:①fallback 熔断是死特性——agent.py 裸构造
  `DefaultRecovery()`,无部署面接入 fallback_model,MAX_CONSECUTIVE_529
  永远打不着;②Retry-After 单次封顶 300s 但**无总预算**——服务器
  每次都答 Retry-After: 300 可让一个 turn 被"守规矩地"挂 50 分钟
  (计算退避最坏 199s,header 路径是它的 15 倍)。
- [x] 微实验 G(2026-09-01 落地):`MAX_TOTAL_RETRY_WAIT_MS=300s`
  跨尝试累计等待预算——将越界的那次等待在入睡**之前**拒绝,具名
  failed 事件 + 原异常上抛;计算退避最坏 199s 落在预算之内,正常
  路径无感,只有人质场景被砍(50min → ≤300s)。pin 翻转(FINDING②
  → RESOLVED,3000s 可达性数字保留为对照);人质测试钉"恰好两次
  150s 等待,第三次拒绝";守卫
  a-patient-server-can-hang-a-turn-forever(r265)。
- [x] 微实验 H(2026-09-01 落地,选**接入**非删除):
  `MINILOOP_FALLBACK_MODEL` → Settings.fallback_model → Agent 默认
  装配 `DefaultRecovery(fallback_model=...)`。理由:机制已被普查
  验证可用、代码量小、529 风暴对代理端点真实;接入后默认 None
  零行为变化,武装是操作者显式动作(与整仓武装 doctrine 一致)。
  pin 翻转(FINDING① → RESOLVED)+ 接线测试(env → Settings →
  session.agent.recovery 全链路)。无新守卫(接线 pin 即钉)。
- [x] 工具 schema 上下文开销测量(2026-09-01 落地):
  tests/test_schema_cost_census.py——核心目录 10 工具共 ~3.3k 字符
  (~840 token)/每请求。**诚实裁决:精瘦,不值得瘦身实验**——最重
  的 bash(~730)重在 approval_prefix 导引,是承重提示工程非膨胀。
  三个 pin:目录总价带(2k–5k)、单工具上限(<1.2k,防 enum 倾倒
  与描述散文)、无工具零描述(有形无义的 schema 是纯浪费)。可选面
  (ast-outline/workflows)在普查中钉关,只价常开核心。
- [x] 后台命令结果普查(2026-09-01 落地):前台的三课在后台路径
  全部重犯——裸 `[:OUTPUT_CAP]` 切片**静默截断且保头弃尾**(round-62
  课)、**非零退出码消失**(失败的长构建注入成干净 "completed",
  实验 E 课)、`communicate()` **无内存界**(round-140 课)。修前
  两者:后台渲染改走前台规则(capped keep_tail + exit 注;status
  保持生命周期语义,失败可见性在文本里);pin 三个 + FINDING 绊线
  一个(communicate() 仍在源里即未修,修时绊线与本条同翻)。守卫
  r58(background-result-unmasked)锚随行更新并重验承重。
- [ ] 后台有界捕获:_BoundedCapture 的异步版,补 round-140 内存界
  的后台缺口(绊线在 test_background_census.py)。
- (待数据)真实轨迹挖掘:有使用量后重开,回归"向被记录的摩擦
  对齐"的主航道。

### 评委侧入集记录(2026-09-01,操作者三项拍板)

操作者决定:①翻页任务**入集** ②真跑判读预算 **N=3** ③529 熔断
**暂不武装**。落地:BenchTask 增 `setup` 预置钩子(仪器侧播种
fixture,永不来自臂内对话;写点普查登记为"执行");DEFAULT_TASKS
入集 `page-long-log`(6000 行日志报告第 4321 行,整读必被截断);
受影响测试与 CLI 预算数字全部同步(可见集 3→4,--repeat 2 价 28)。

**真跑验证**(N=1,14 task-runs):双臂 4/4 全过,两侧 not_worse。
**诚实观察**:真模型 2 轮 / 1 次工具调用即解——最优路径是 bash 单行
(sed/awk),**绕开了 read_file 翻页**。含义:该任务验证"到达深行"
的能力与效率(行为学维度有读数),但对 read_file 人体工程学实验
(微实验 A/B)的灵敏度有限——模型不用 read_file 就量不到它的改动。
若要专测 read_file 路径,需受限工具集的任务变体(评委侧,另议)。
**预算算术**:入集后 N=3 = 2×3×(4+3) = **42** task-runs,超出授权
的 36;N=2 = 28 在授权内。效应判读跑 N=3 前需操作者确认 42。

**自选队列清空评估(2026-09-01)**:十项全落(压缩普查+C+D、错误
路径普查+E+F、故障注入普查+G+H、schema 普查)。var/state.db 复查
仍空(无真实使用数据)。继续自选的边际收益已明显递减:剩余高价值
工作要么等**人审事项**(翻页任务入集+setup 钩子、降噪判读预算 N),
要么等**真实使用数据**(轨迹挖掘)。按章程停止循环;任一解锁即可
重开。
