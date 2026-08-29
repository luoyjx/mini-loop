# Codex 追齐计划:可补齐点队列与进度

目标(/loop,每小时一轮):按 [OPENAI_CODEX_CORE_DEEP_DIVE.md](OPENAI_CODEX_CORE_DEEP_DIVE.md)
§8 的建议清单,先补齐可补齐的点,再参考其架构重构对应模块,直到能力与架构追齐。

规则:每项落地 = 实现 + 指名测试 + 突变守卫(如适用);每轮结束跑全量套件;
提交需用户显式授权。§8 编号在此展开为可执行队列。

## 队列(完成一项勾一项)

### R1(本轮完成)
- [x] 打断标记补全(§8.2):`_record_interruption` 在有存活后台任务时追加
      "N 个后台任务仍在运行、可能已改文件、用 check_background 查看"——
      仅在为真时说(无后台工作的会话保持裸标记,精确相等测试钉死)。
      守卫 interruption-marker-hides-surviving-background-work(r253)。
- [x] 压缩摘要交接框架(§8.5):COMPACTION_PROMPT 四要素(进度与决策/约束与
      偏好/下一步/必要数据)+ SUMMARY_PREFIX 把摘要框定为"上一个实例的交接、
      别重复已完成的工作"。守卫 compaction-summary-loses-its-handoff-framing(r253)。
- [x] 文档修正:§8.4(head/tail 截断)核对后 mini-loop 已具备
      (tools.py `capped(keep_tail=True)`),改标"无行动项"。

### R2(本轮完成)
- [x] 姿态变更的模型可见语义(§8.1):`change_permission_mode` 队一条
      old -> new 附含义注释的通告,`posture_injector` 下一轮以
      `<posture_update>` 注入(刻意不穿 `<user_interjection>`——规则变更是
      harness 事实,不是用户话语);HTTP /mode 边走通告路径,创建期与
      直接赋值保持静默(所有权规则)。事件 posture_update 进账本。
      守卫 posture-changes-happen-behind-the-models-back(r254)。
      plan_mode 在 main 上只有模型自己的 enter/exit 工具(模型天然知情),
      无人为切换边,无需覆盖。
- [x] flush 顺序注释化(§8.8):cancel 路径(事件是"转录已定,重读"的信号,
      flush 必须先行)与 restore 崩溃标记(标记为下一次崩溃而存在,推迟到
      首轮 flush 就什么都保护不了)两处补上协议注释;其余三处原有注释已足。

### R3(本轮完成)
- [x] 审批即学习(§8.3):`resolve(remember=True)` 把 pending 的
      grant_candidate 记入会话级授权表——shell 恰取前两 token
      (`git reset --hard X` 至多泛化成"允许 `git reset`"),其余工具取
      工具名;后续同候选调用在 ask 前命中直接放行(事件
      approval_grant_used)。硬边界:黑名单头(rm/sudo/解释器/curl…)
      **允许本次、拒记规则**并发 approval_grant_refused;deny 上的
      remember 无效;授权表 runtime-only(重启即重新问,与
      permission_mode 同一 doctrine);会话删除连带清表;候选在审批
      snapshot 里前置展示(知情的 yes)。HTTP ApprovalReq 加 `remember`。
      守卫 grants-never-actually-skip-the-ask、
      banned-heads-get-remembered-anyway(r255)。
      **与 Codex 的差异(有意)**:Codex 由模型经 `prefix_rule` 参数提议、
      黑名单挡过宽提议;本轮把泛化的话语权给人(resolve 侧),更安全也
      免动工具 schema。模型提议参数留作 R4 评估项。

### R4(本轮完成)
- [x] 模型提议免批前缀(§8.3 后半):bash/background_run schema 增加可选
      `approval_prefix`(描述即提示词:说明"须是命令自己的前导词、至少两个、
      解释器/删除器头永不记住");`proposed_candidate` 只采信**诚实前缀**
      (必须逐词等于命令开头,2-6 个 token,头不在黑名单)——说谎/过宽/
      单 token 提议一律回退默认候选,pending 以 `grant_proposed` 标注
      泛化出处;`granted()` 改为变长前缀匹配(模型提议可比默认两 token 长,
      更窄)。Web UI 审批面板加 "Allow + remember: <前缀>(model-proposed)"
      控件,DOM 桩测试补 remember 用例。守卫 a-lying-prefix-is-taken-at-its-word
      (r256)+ r255/r100 锚点随代码同步复验。

### R5(本轮完成:WEBUI_PLAN R8-1 + R8-2 的 agent/helper 侧)
- [x] Activity 事件契约(R8-1):`activity_update` 事件(activity_id/title/
      source/provisional)+ `tool_use` 显式携带 `activity_id`(分组是记录
      的关联,不是"最新标题"猜测);标题取自完整公开 commentary 首行首句,
      单行 80 封顶、mask、不可用不发不阻断。
- [x] 工具语义投影(R8-2):新模块 `mini_loop/activity.py`,`tool_label`
      产出 {verb, object},bash 只认单一用途简单形态、任一元字符即退回
      run 预览,时态留给消费方;`tool_use` 事件新增 `display` 字段。
      守卫 labels-classify-past-a-pipe(r257);r21 锚点同步。
      详情与验收记录以 [WEBUI_PLAN.md R8](WEBUI_PLAN.md) 为唯一维护处。

### R6(本轮完成:WEBUI_PLAN R8-3 + R8-4,R8 全部收口)
- [x] Activity UI 分组与回放:折叠组按显式 activity_id 归组(不猜最新
      标题)、动词只在真实成功时转过去式(denied 不得写成 Ran)、无
      display 的旧事件降级为工具名、重放按 id 幂等、会话切换清态;
      3 个 DOM 桩用例 + py 接线扫描;真浏览器端到端验收(375/1440,
      控制台零错误)。落地细节记录在 WEBUI_PLAN R8-3/R8-4 附注。

### R7(本轮完成:两项评估,均以代码证据收口)
- [x] Stop hook 泛化评估(§8.7)——**结论:已泛化,无需重构**。核对:
      `Hook.on_stop` 本就是基类上的通用挂点(registry.py:444/470,
      "return a continuation prompt to keep the loop running"),
      `Hooks.stop`(registry.py:565-570)按注册顺序组合、首个非 None
      续跑;GoalContinuation(goals.py:101)只是唯一的内置注册者,不是
      特例实现。与 Codex 的 Stop hooks(turn.rs:363-407,should_block +
      prompt 注入)功能等价:continuation 文本即注入的续跑 prompt
      (agent.py 追加为 user 消息)。第二个消费者出现时直接注册即可,
      不存在需要"抽成通用点"的重构。
- [x] 确定性 id 评估(§8.6)——**结论:对当前 wire 形态不适用,以
      "合成文本字节稳定"承接同一关切**。Codex 需要 UUIDv5 是因为
      Responses 形态的 input item 自带 id、随机 id 会破坏前缀缓存;
      Anthropic Messages 形态的请求消息**没有 item id**,prompt cache
      按内容前缀命中。mini-loop 对应的纪律是合成消息文本必须确定:
      打断标记、UNKNOWN_RESULT、posture 通告均为稳定常量(无时间戳/
      随机量)✓;compaction 替换消息含时间戳路径,但它本就替换整个
      transcript(缓存必然重置),不构成额外破坏。**仅当**未来接入
      Responses 形态 provider 时重启此项。

### 明确不做(§8.9)
- remote compaction(依赖服务端能力)、MITM 网络代理(重型,sandbox+
  deny-list 够用)、服务端下发 prompt(单 provider 收益有限)。

## 进度日志
- R1(2026-08-28):两项落地 + 文档修正;全量 1863 passed / 18 skipped;
  新守卫 2 条均 load-bearing。未提交(待授权)。
- R2(2026-08-28):姿态通告 + flush 协议注释;5 个新测试;全量 1868 passed /
  18 skipped;守卫 r254 load-bearing。未提交(待授权)。
- R3(2026-08-28):审批即学习(会话级 remember 授权 + 黑名单);8 个新测试;
  全量 1876 passed / 18 skipped;守卫 r255 ×2 load-bearing,r100 锚点随
  代码同步更新并复验。未提交(待授权)。
- R4(2026-08-29):模型提议免批前缀(诚实性校验 + 变长匹配 + UI remember
  控件);py 3 个新测试 + DOM 桩 1 个,兼容并行会话的 UI 重设计与其
  deepEqual 断言;全量 1880 passed / 18 skipped;守卫 r256 load-bearing。
  未提交(待授权)。
- R5(2026-08-29):Activity R8-1/R8-2 agent+helper 侧(activity_update
  事件契约、显式 activity_id 关联、保守工具投影);9 个新测试;
  invariants 72 模块全过、scans 19 全锚定、diff --check 干净;全量
  1889 passed / 18 skipped;守卫 r257 load-bearing,r21 锚点同步复验。
  未提交(待授权)。
- R6(2026-08-29):Activity R8-3/R8-4 UI 侧收口(分组/时态/降级/幂等);
  DOM 桩 34 全过(+3),py webui +1;真浏览器验收通过;全量 1890 passed /
  18 skipped。R8 四步全部完成。未提交(待授权)。
- R7(2026-08-29):§8.7/§8.6 两项评估以代码证据收口(前者已泛化、
  后者对 Anthropic wire 形态不适用),零代码变更。**队列清空:调研文档
  §8 全部行动项(1-5)+ 使用反馈项(R8 四步)+ 两项评估(6-7)全部
  完成或有据关闭;§8.9 为明确不做。追齐目标在既定范围内达成,循环停止。**
