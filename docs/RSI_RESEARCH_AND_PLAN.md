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
| 5 | 假定 hacking | ✓ **本轮落地**:verifier_touches 具名告警(DGM 满分劫持的直接对策);哈希校验为后续项 |
| 6 | 人审合并门 | ✓ 宪法级(propose, never merge) |
| 7 | archive 优于爬山 | ✓ **本轮落地**:宽松入档(unverified 也入),晋升仍是人 |
| 8 | 多维效用 | ◐ benchmark 报 not_worse;成本/时延维度为后续项 |
| 9 | 独立监督 | ◐ guardian(fail-closed 审批复核)是近亲;不随提案分支演化 |
| 10 | held-out 迁移 | ✗ 后续项(晋升前跨任务复测) |
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

- [建议] 验收仪器哈希校验:proposal 记录 acceptance_command 所涉文件的
  运行前哈希,复核时比对(实践 #5 的强化)。
- [建议] 多维效用:paired benchmark 报表加成本与时延列(实践 #8)。
- [建议] held-out 复测:晋升(合并)前在提案未见过的任务上复测(#10)。
- **明确不做**(现阶段):自动合并(违宪)、自动重启加载提案代码、
  无人边的连续循环(DGM/SICA 的自主循环以沙箱+监督为前提,mini-loop
  的部署姿态是单机开发工具,人边即监督);评委完全出仓(把验收仪器
  移出代理可写范围会同时废掉"改进仪器自身"的合法目标,当前用具名+
  人审换取这份表达力,是有意的权衡)。
