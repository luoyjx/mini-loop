# Provider usage 的实际 Source 消费者契约

## 固定与范围

- 原生基线：`bf4c866`；Anthropic SDK 0.107.1，Pydantic 2.13.4。
- 工厂：`python/tools/provider_usage_consumer_contracts.py`。复用实际 SDK HTTP/SSE
  MockTransport 捕获，不发真实网络请求，也不从手写预期构造 SDK 对象。
- 34 场景：14 种 input 值分别 direct/stream，加 4 个混合缓存场景和
  2 个仅 SSE 的 message_delta 覆写场景。捕获原始 response/SSE、SDK 事件值、
  message_start/message_delta 累计快照、实际 prompt_tokens/TokenMeter 结果。
- `_usage_payload`、SDK accumulate_event、Usage 与 `_field`、prompt_tokens、
  TokenMeter.observe 源码哈希固定在 fixture 中。

## 已验证行为

| 输入 / 路径 | 最终事件 input_tokens | prompt 计量 |
| --- | --- | --- |
| direct 数字字符串 `"13"` | 字符串 | 不计入 |
| SSE 数字字符串 `"13"` | 整数 13，从 message_start 起归一化 | 13 |
| direct bool true | bool true | 1 |
| SSE bool true | 整数 1 | 1 |
| direct 浮点 13.0 | 浮点 13.0 | 13 |
| SSE 浮点 13.0 | 整数 13 | 13 |
| direct / SSE 分数 13.75 | 浮点 13.75 | 截断为 13 |
| direct / SSE 负分数 -3.75 | 浮点 -3.75 | anchor -3，used 截为 0 |
| 数组、对象、null、不可计量文本 | 保留 Source 事件值 | 不计入 |
| required 字符串 + cache_creation 字符串 17，direct | required 字符串，cache 整数 17 | 17 |
| 同一配置，SSE | input 整数 13，cache 整数 17 | 30 |
| SSE delta 将 input/output 改成整数样式字符串 | 归一化为整数 | 使用归一化值 |

计量的 `_field` 只接受 Python int/float（bool 属于 int），并用 int 截断；
字符串即使看似数字也不会在该消费者中转换。零总和返回 None，不产生计量观察。
SDK 事件与计量投影必须分别建模。以上是本次捕获范围，不能外推为任意 SDK
错误数据、整包 Pydantic 归一化或最终 provider 验收。

## 当前原生差异

[原生报告](provider-usage-consumer-2026-10-10.json) 调用现有 direct decodeReply、
SSE readStream 与实际 agent.PromptTokens。这里核对解码/计量边界，不声称
进行了新的真实 HTTP 服务或持久化组合验证。

34 个 Source 成功场景中，Go **拒绝 30 个**；剩余 **4 个**的事件和 prompt
结果一致。诊断另外比较计数字段的 kind，避免 Python 数字相等
掩盖 bool/float/int 的事件差异。所有拒绝保持列出，不通过 skip 或重写 Source
把它们变成通过。报告生成测试成功只证明诊断执行完成，不是 parity gate。

## 实现顺序

1. 建立具名 usage 计数变体和独立计量投影；保留 int、float、bool、text、null
   的区别。结构化异常输入使用显式、无执行权限的观察变体。已知字段不得
   退化为 any、开放 map 或 RawMessage；未知扩展继续使用现有不可变边界。
2. 分开重放 SDK direct 构造与 SSE start/delta 归一化，按实际累计阶段比较。
   不把全部文本转整数，也不把 Source 允许的负数统一拒绝。
3. 对齐 Agent model_end、TokenMeter、compaction summary 和 decision 消费者。
   其中严格 decision 计数验证不得接受一个由无效原值投影出的假整数。
4. 验证 clone/masking、事件/记录、SQLite 重开和 Manager restore 后写入；
   用真实消费者测试替换这份诊断中已经交付的差异，然后跑所需全量门禁。

## 本轮验证

- Source 两个 helper 测试通过：2 passed，12 个预期 SDK serializer warnings。
- 全 provider 包测试通过；当前 9 个 usage metadata 旧契约保持通过。
- 诊断重放通过并生成 34 行报告；拒绝 30 行的事实保持显式。
- 扫描守卫：19 项锚定。运行时未修改，未刷新覆盖率或全仓测试。
- Source export、显式诊断 race 和 diff 检查见本轮计划/矩阵的最终记录。

## 原生具名计数实现更新

最新[原生观察报告](provider-usage-count-native-2026-10-10.json) 使用
`a5bde56 plus named usage count adoption` 基线：34 场景现均成功，事件值/根字段
类型和 prompt 计量均匹配，拒绝从 30 降至 0。旧报告保留为实现前证据。
独立 parity 测试比较整个 JSON 树的值和 kind，以及全部 36 个 SSE 累计快照，
不依赖报告生成测试的成功。34 个实际 TokenMeter 状态和 SQLite 事件重开
场景通过；摘要事件保持观察值，decision 只接受非负整数变体。

具名 UsageCount 及所有已知根/嵌套计数字段使用闭合具体类型；文本和结构化
观察不会取得计量或 decision 整数权限。文本/对象键值脱敏保留现场原始观察值。
有限原生 int/float 计量范围仍为适配；更多 Pydantic coercion、其他错误字段
形状及完整 G4/G7 验收仍开放。Go IntegerUsageCount 是本地/fake 的显式构造器。
