# Go provider 范围审计

## 摘要

代码基线：`477a209`；Source usage 捕获提交：`ab1833a`。审计时间：2026-10-10，Anthropic SDK 0.107.1。
Launcher 自动发现已经实现，不能继续列为待实现项。当前确认的运行时缺口是
**SDK usage 元数据和事件形状**：Python 会保留 SDK 完整 usage，Go 只建模其中五个字段。
本报告与七个真实 SDK 离线捕获确定下一项实现任务，不代表 G4/G7 已验收。

## 实际请求生成路径

对 `python/mini_loop/**/*.py` 的 AST 调用节点检查发现三个 SDK 调用位置：

| 位置 | 传递参数 | 对应 Go 边界 |
| --- | --- | --- |
| `transport.py:66`，DirectTransport.send | `**kwargs` | `provider.Client.Complete` |
| `transport.py:140`，StreamingTransport.send | `**kwargs` | `provider.StreamingClient.CompleteStream` |
| `memory.py:653`，extract_memories 的无 Agent fallback | `model/max_tokens/messages` | memory 模型接口与具名 ModelRequest |

`Agent._create` 构造 `model/messages/max_tokens`，按需加入 `system/tools`。
恢复策略修改 `model/max_tokens/messages`。缓存标注写入系统、工具和消息的副本。
Go `protocol.ModelRequest` 对应这五个具名请求字段；`Purpose` 是本地来源，
`Cache` 在 wire 投影中展开，不作为额外顶层请求字段。
SDK 流式入口另外加入 `stream: true`，对应 Go streaming wire 选择。

当前仓库生成路径没有 `output_config/output_format/temperature/top_p/top_k/tool_choice`
等 SDK 选项。这项静态检查不是所有自定义 provider/transport 的行为证明；
扩展可以改变调用方式，扩展接缝仍需单独审计。不能仅因 SDK 支持某选项就把它
当成默认 Python 运行路径的缺失功能。

## 已确认的 usage 缺口

Python `agent._usage_payload` 调用 SDK Usage.model_dump；`Agent._create` 将结果写入
`model_end.usage`。因此下列数据是仓库的可观察输出，不是被忽略的 SDK 私有元数据。

| SDK 具名字段 | 当前 Go | 本轮 Source 证据 |
| --- | --- | --- |
| input_tokens / output_tokens | 已建模整数 | minimal / nested 的 direct 和 stream |
| cache_creation_input_tokens / cache_read_input_tokens | 已建模可选整数 | nested；缺省 null 事件形状仍不同 |
| service_tier | 已建模可选值 | nested；缺省 null 事件形状仍不同 |
| cache_creation | 丢弃 | 1h / 5m 输入 token 分桶 |
| inference_geo | 丢弃 | `us` |
| output_tokens_details | 丢弃 | thinking_tokens |
| server_tool_use | 丢弃 | web_fetch_requests / web_search_requests |
| 未知扩展字段 | 丢弃 | future_counter.generation |

SDK 最小回复仍输出全部九个声明字段，七个可选字段为 null；Go 的 omitempty
目前不保留这种形状。direct 和 SDK 流式累计均保留 message_start 中的嵌套 usage。
字符串场景还有一个明确观察：SDK 保留必填 input/output 的字符串，却将可选
cache_creation_input_tokens 转成整数，序列化产生 Pydantic 警告。不得假设所有计数
具有统一强制转换规则；现有 native 严格整数适配需在后续实现中明确界定。

捕获文件：[python-provider-usage.json](go/testdata/python-provider-usage.json)。
工厂：[provider_usage_contracts.py](python/tools/provider_usage_contracts.py)。
测试：[test_provider_usage_contracts.py](python/tests/test_provider_usage_contracts.py)。
捕获包含 SDK/Pydantic/CPython 版本及 Usage、SDK stream accumulator、仓库
_usage_payload 的源码哈希。HTTP 使用 httpx.MockTransport，没有真实网络请求。

## 后续实现与验收顺序

1. 为四个遗漏字段建立具名嵌套结构；保留 absent/null/value 与 SDK 缺省事件形状。
   未来扩展使用既有闭合 JSON 值边界，不能增加开放 domain payload。
2. 更新 direct/SSE 解码、克隆、事件投影、记录与持久化读写，对照七个 Source 场景。
   不能只修正 HTTP 解码而继续在事件或 restart 路径丢字段。
3. 核对 usage 计量消费者与 scalar-string 适配，确认事件值与计量转换的区别。
4. 单独完成剩余响应 block 与自定义 provider/transport 扩展范围审计。SDK 当前有
   十二个回复 content discriminator；Go 的客户端工具 union 不等同于全部 SDK
   服务端工具类型，不能据此直接声称完整 G4 关闭。
5. 默认 full-feature CLI 的 Python 语法解析后端仍待依赖批准。SQLite 驱动批准
   不覆盖该后端。G0 外部类型闭包、完整公共入口、跨功能与 G7 性能/发布门禁仍开放。

SDK Message 的 container/stop_details 与完整 Usage 是不同问题：当前 Agent 事件
明确投影 stop_reason、served_model、usage、content；未看到它读取前两者。
其自定义调用者语义需要后续审计，不在本轮擅自宣布已覆盖或必需新增。

## 源码固定

| Python 文件 | SHA256 |
| --- | --- |
| providers.py | 19b2aaade7606e8ddea10dd2ce9b1ea274326937184d908c356353915a18d597 |
| agent.py | c8e88035edc59d81812bc8b0d83e56519eb93427898d332d3cef4d2be66263b6 |
| transport.py | ce7fc744213272848cc5ba01e06b851d49ca106da0560746d5ae7b6929503873 |
| recovery.py | bf388d43dcebb900aee263d060f9be9db756a7f30783fffd423f5ae94de99aca |
| memory.py | 39f463b9ed848a5ac21195c9ac0505cf3f4ade9c0ba659b079fe8aefa71eb38f |

本轮只新增 Source 契约、测试与审计文档；Go 运行时 usage 尚未修正。
历史覆盖率不是这项未实现能力的证明。完整迁移目标继续保持开放。

## 本轮验证

- Python provider usage/auth 测试：7 passed，1 个字符串计数场景的预期 Pydantic 警告。
- Source 导出：203 文件一致，退出 0。
- Scanner 验证：19 项通过，退出 0。
- README 架构地图与 Markdown outline 已复核，拓扑、默认能力、权限和持久化不变。
- diff 与 staged diff 检查通过。未重跑 Go/race/vet、全量 Python、mutation、覆盖率或性能；
  Go 运行时未变，Source fixture 与审计文档没有修复 native usage。

验证日志位于 `/tmp/mini-loop-provider-usage-{export,scans}.log`；没有 merge 或 push。

## 具名协议与事件基础更新

`14e1ca1` 后的实现增加四个 metadata 字段、三个具名嵌套结构、null 状态和闭合扩展。
协议可往返六个结构有效的 Source usage 输出；scalar-string 明确保留 native 拒绝。
克隆、事件脱敏、存储事件 JSON 往返已有测试；没有把 JSON 往返当成真实 SQL restart。
当前 HTTP/SSE 解码仍丢弃 metadata，后续必须完成 provider 接入与持久化组合证明。

## HTTP/SSE usage 接入更新

基线 `9495de6` 后，direct HTTP 与 SSE 接入具名 usage 解码、SDK null 默认值与闭合扩展。
新增部分嵌套计数与 server-tool delta 两个 SDK 捕获，共九例；八个结构有效场景
通过真实 Go Client 对照。嵌套计数使用具名可选整数，缺省在 SDK 投影时补 null，
原生 sparse codec 保留缺省/null/零区别。SSE 服务端工具计数按 SDK accumulator 更新。
scalar-string 场景仍明确拒绝，属于原生整数适配。真实 SQLite/recording restart
组合证明与完整 G4/G7 审计仍未完成。上方初始审计及基础提交状态保留为历史基线。

## SQLite 与记录文件组合证明

`caa135c` 后的 direct/SSE 集成测试组合 SDK nested usage 与服务端工具 delta，
增加注册密钥 canary 和嵌套扩展。真实 HTTP 经 Agent、原生 SQLite、Manager 停止/
显式恢复与恢复后第三轮，逐字段比较 SQL 原始事件 payload 和 typed archival reader。
记录文件检查实际 JSONL 与重新实例化的 reader 输出，保留全部元数据与 null/扩展形状，
且注册密钥的值与字段名均被脱敏。引用数据转发、owner 隔离和 lease 释放的原测试
仍保留。此证明覆盖实际关闭重开配方，不代表进程崩溃/断电持久性或完整 G4/G7 验收。

## Container-upload 回复与 usage 消费者核对

基线 `b3a4bd9` 后增加 `container_upload` 的具名 FileID、闭合 Block 分支、
direct/SSE 解码、克隆、脱敏/存储 codec 及 SQLite 文本校验。六个 SDK 0.107.1
捕获覆盖普通、空串、Unicode ID。SDK 流式投影还包含 text/citations/parsed_output
三个 null 默认字段，原生实现显式保留这一形状。它不进入 client tool dispatch；
decision_llm 的 Source allowlist 明确不包含该块，Go 保持相同拒绝。
SDK ContentBlock 十二类中的另外七类 server-tool/result 回复仍需审计和实现，
本轮没有把一种新变体等同于完整 provider 验收。

usage 消费者核对：Python metering._field 仅对 int/float（包括 bool）转 int，
忽略字符串；decision_llm.evaluate 则尝试 int(value)。因此字符串 usage 的事件
原值、prompt 计量与 decision 计数不是一个统一转换规则。Go 严格整数 ingress
目前会提前拒绝该场景，不能宣称已实现这一 Source 适配；后续需要独立核对
有限/不受限整数、float/bool 和各消费者语义。

## Server web-search result 回复更新

基线 `ada45b2` 后支持 `web_search_tool_result`：ToolUseID/Caller 与成功列表或
错误的闭合 union。结果具名保留 Title/URL/EncryptedContent/PageAge；错误保留
具名 code，未知 future_code 按实际 SDK 捕获保留。root/result/error 扩展进入
不可变闭合 JSON 对象，不替代具名字段。14 个 SDK direct/SSE 配方核对空列表、
成功、错误/caller/扩展；transport 显式补 caller/page_age 的 SDK null 默认值，
历史 codec 保留缺省且允许超网络边界的大记录。此块是服务端观察，不进入 client
tool dispatch/pairing；decision content allowlist 继续与 Source 一致地拒绝它。
SDK ContentBlock 尚缺其余六种：server_tool_use、web_fetch_tool_result、
tool_search_tool_result、code_execution_tool_result、bash_code_execution_tool_result、
text_editor_code_execution_tool_result。完整 provider/default/G7 验收仍未完成。

## Server tool-search result 回复更新

基线 `1c68fd0` 后支持 `tool_search_tool_result` 的具名 ToolUseID 和引用/错误
闭合 union。工具引用只保留 ToolName；错误保留 Code/可空 Message。root/result/
reference/error 的扩展进入不可变闭合 JSON，已知字段仍进入具名类型。12 个 SDK
direct/SSE 配方验证空引用、已知/未知/Unicode 名称、错误/future code、消息和扩展。
SDK 流式 ParsedTextBlock 的 text/citations/parsed_output null 默认值及错误消息
默认 null 均显式保留；原生 sparse codec 区分缺省，历史 codec 支持大记录。
引用不注册或执行客户端工具，decision allowlist 保持 Source 拒绝。尚缺五种
SDK 回复：server_tool_use、web_fetch_tool_result、code_execution_tool_result、
bash_code_execution_tool_result、text_editor_code_execution_tool_result。完整验收未完成。

## Provider-owned tool-use 回复更新

基线 `0781cd7` 后支持 `server_tool_use` 的具名 ID/ServerToolName/ServerToolInput/
ToolCaller。SDK 的 input 是无具体字段定义的对象，Go 以具名封装的不可变闭合 JSON
保存；它不进入客户端 ToolInput。7 个已知名称有常量，future 名称保留字符串。
32 个 SDK direct/SSE 配方验证输入对象、caller null/三个具名变体、扩展及流式输入
替换/空 delta 保留。完整 JSON/object 是原生停止块的准入边界；SDK partial 或畸形
输入及 caller 扩展仍需审计。转发、脱敏、克隆、大历史记录和实际 SQLite 重开保留
元数据；无效 UTF-8 具名字段使批次回滚。不会执行客户端工具或要求客户端结果配对。
剩余四类 SDK 回复：web_fetch_tool_result、code_execution_tool_result、
bash_code_execution_tool_result、text_editor_code_execution_tool_result。完整验收未完成。

## Web-fetch document 回复更新

基线 `d271228` 后支持 `web_fetch_tool_result` 具名 ToolUseID/Caller 和文档/错误
闭合 union。成功结果保留 URL、可空 RetrievedAt 和具名 FetchedDocument；文档
保留可空 Title/Citations 配置、具名 Enabled，以及 text/PDF 闭合 source 的 Data/
MediaType。42 个 SDK direct/SSE 配方覆盖文本/PDF、空文本、nullable 字段、引用
配置 on/off、caller、9 个错误码、future code 和嵌套扩展。各层未知扩展是不可变
闭合 JSON；SDK direct 的具名模型和 stream ParsedTextBlock null 默认值分别保留。
脱敏、克隆、历史大记录与实际 SQLite 重开保留元数据，具名 UTF-8 错误使批次回滚。
这是观察数据，不打开文档、取 URL 或执行客户端工具。尚缺三个执行结果类型：
code_execution_tool_result、bash_code_execution_tool_result、
text_editor_code_execution_tool_result。caller 扩展、partial 输入及完整验收仍待审计。

## Code-execution result 回复更新

基线 `897cad8` 后支持 `code_execution_tool_result` 的具名 ToolUseID 和普通/
加密/错误闭合 union。OutputFiles、Stdout/EncryptedStdout、Stderr、FileID 均具名；
ExecutionReturnCode 保留 Python 整数，不截断机器位宽。28 个 SDK direct/SSE 配方
覆盖普通/加密、空/非空输出文件、大有符号整数、4 种错误、future code 和扩展。
root/result/output/error 扩展进入不可变闭合 JSON；stream 的三个 null 默认字段
显式保留。转发、脱敏、克隆、大历史记录和实际 SQLite 重开保留数据；无效 UTF-8
具名字段使批次回滚。数据不触发代码执行、文件打开或解密，不进入客户端配对。
剩余两类：bash_code_execution_tool_result、text_editor_code_execution_tool_result。
caller/partial 输入、usage scalar、默认诊断与完整 provider/G7 验收仍待完成。

## Bash execution result 回复更新

基线 `37d4451` 后支持 `bash_code_execution_tool_result` 的具名 ToolUseID 和
结果/错误闭合 union。OutputFiles、Stdout、Stderr、FileID 具名，ReturnCode 复用
保留 Python 整数的 ExecutionReturnCode。Bash 结果与文件标签独立；普通 code
execution 标签不会在此类型中混用。26 个 SDK direct/SSE 配方覆盖输出、空文本/
文件、大有符号整数、5 种错误（含 output_file_too_large）、future code 与扩展。
未知扩展为不可变闭合 JSON；SDK stream null 默认字段、转发、脱敏、克隆和历史
大记录保真。实际 SQLite 重开保留配方，无效 UTF-8 字段使批次回滚。此观察数据
不调用客户端 Bash、不执行命令或打开文件，也不要求客户端结果配对。尚缺
text_editor_code_execution_tool_result；caller/partial 输入、usage scalar、默认
诊断与完整 G4/G7 验收仍未完成。

## Text-editor result 回复更新

基线 `2d8ba81` 后支持 `text_editor_code_execution_tool_result` 的具名 ToolUseID
和 view/create/replace/error 闭合 union。Content/FileType/IsFileUpdate/Lines 具名；
可空行号/行数用 EditorInteger 保留大有符号整数，错误保留 Code/可空 Message。
48 个 SDK direct/SSE 配方覆盖三个 view 文件类型、create/update、replace、缺省/
null/空数组/值、大整数、5 种错误、future code 与扩展。SSE 曾提前将不属于该
SDK 类定义的 caller 扩展当客户端 caller 验证；新配方复现失败后修复为按 block
类型选择解码，客户端 caller 拒绝规则仍受回归检查。扩展保持不可变闭合 JSON。
转发、脱敏、克隆、历史大记录与实际 SQLite 重开保真，无效 UTF-8 使批次回滚。

已核对 SDK `types/content_block.py` 的 12 类：text、thinking、redacted_thinking、
tool_use、server_tool_use、web_search_tool_result、web_fetch_tool_result、
code_execution_tool_result、bash_code_execution_tool_result、
text_editor_code_execution_tool_result、tool_search_tool_result、container_upload。
当前均有具名 Go 变体。这证明种类覆盖，不能代替完整字段与消费者验收。已声明
caller 的扩展、partial/畸形输入、usage scalar 消费者、默认诊断和最终 G4/G7
验收仍未完成；元数据不触发文件读取、创建、编辑或客户端工具权限。

## Declared caller 扩展字段更新

- 固定 SDK 0.107.1 的 DirectCaller 只声明 type；ServerToolCaller 与
  ServerToolCaller20260120 声明 type/tool_id。实际 direct/SSE 捕获表明额外字段
  原样保留，含 direct 上的 tool_id，而旧 Go decodeStrict 拒绝它们。
- 新增 48 个实际 SDK 场景，覆盖四个声明 caller 的块：client/server tool-use、
  web-search/web-fetch。三个既有 discriminator 保持具名解析；未知字段使用闭合
  不可变扩展。direct 扩展 tool_id 不进入具名 ToolID，也不增加执行权限。
- 转发、脱敏键和值、克隆、历史大记录与 SQLite 关闭重开保留字段；网络上限不变。
  未知 caller 类型、已知字段 scalar/missing/null 宽松构造与 partial-input 差异
  仍需独立捕获，不能据此宣布完整 provider 迁移完成。

## Partial-input 实际 SDK 契约与原生差异审计

- Source capture `d984cbd` 固定 Anthropic 0.107.1 / jiter 0.15.0，49 种输入 ×
  client/server 两种块共 98 个实际 SSE 场景。包含每个已交付 delta 的快照、最终
  `_content_payload` 或实际 ValueError；并固定 SDK accumulator 源码摘要。
- 在 `689a1d9` 原生快照，69 个最终结果不同：client 37、server 32。
  另外 16 个双方拒绝场景的失败发生阶段不同；“双方拒绝”不证明错误顺序相同。
- SDK partial 模式的未闭合容器、截断标量、duplicate key、trailing input 与
  非对象/nonfinite 接受行为均已捕获。client 空 delta 清空与具名输入额外字段
  拒绝分别列为差异，不能用普通 JSON 补括号或只实现成功完整对象替代。
- 本轮新增 Source fixture、诊断重放和审计，没有修复原生 parser，也不关闭 G4。
  后续按 [完整审计与实现顺序](docs/audits/provider-partial-input-2026-10-10.md)
  实现逐 delta 累计解析，并单独核对实际 Python 工具输入/转发/持久化消费者。

## Native partial-input 逐 delta 解析更新

- 新增独立闭合 Value partial 解码器，严格完整/历史 JSON Decode 保持不变。
  原生 SSE 在每个非空累计 delta 后解析；EOF 未完成标量不进入已完成成员，
  空 delta 保留初始输入，畸形内容在对应 delta 拒绝。原始输入先检查尺寸上限。
- 98 个实际 SDK 场景逐 delta 快照与拒绝阶段全部匹配；另有 42 个固定 jiter
  0.15.0 边缘契约（Unicode、UTF-16 pair、separator 和 200/201 层）通过。
  34 个服务端有限对象的最终内容、历史解码和 SQLite 重开匹配 SDK。
- 最终差异从 69 降至 40：client 33、server 7；先前 16 个延迟拒绝现均发生在
  input delta。剩余 client 额外/缺失字段、非对象和非有限数消费者适配仍需核对。
  不据此宣布完整 provider 或迁移验收。最新原生观察报告另存，旧基线保留。

## Default Bash 实际消费者绑定更新

- Source 通过实际 SDK SSE 和 `Agent._exec_tool` 捕获 48 场景，固定
  before/guard/execute/after/result 顺序、缺失 command 和额外参数错误。
- 原生匹配 41 个有限对象消费者场景；before 可重写为合法输入或拒绝。
  参数绑定错误在权限/日志检查后、执行器之前形成 Failed 结果。
- 68 个 client/server 有限对象的最终内容、历史和 SQLite 重开匹配 SDK。
  严格显式输入入口保持拒绝；具名字段不转换为开放映射。
- 最新 98 场景最终差异从 40 降至 14（client 7、server 7）；非对象与
  nonfinite、已知字段错误类型及完整 provider 验收仍开放。报告另存
  `docs/audits/provider-bash-binding-native-2026-10-10.json`，历史报告保留。

## Usage scalar 实际消费与差异固定

34 个真实 SDK + Meter 场景成功；Go 拒绝 30 个，4 个的事件和计量匹配。
Direct 与 SSE 对字符串、bool、integral float 的实际归一化不同；分数计量
截断，负 anchor 接受，字符串不计量。已捕获 start/delta 快照，详见
[消费契约与实现顺序](docs/audits/provider-usage-consumer-2026-10-10.md)。
本轮未改变原生 usage 适配，完整字段/消费者/持久化交付仍开放。

## 具名 usage 观察值与实际计量接入

已接入闭合 UsageCount 变体，所有已知根及嵌套计数字段保持具名类型。
34 个 SDK 最终值/类型和 SSE 阶段快照、34 个 Meter 状态及 SQLite 事件
重开场景匹配。计量投影与事件原值分开，摘要保持原值；decision 只接受
非负整数变体。文本/结构化计数中的秘密按投影脱敏，原始值不受修改。
原生整数/有限浮点边界、更多 SDK coercion/错误字段形状及 G4/G7 验收仍开放。

## Finite non-mapping input 实际消费者接入

Source 契约提交 `a59d2d1` / `7748519` 固定了 51 个 Bash 消费者场景
和 12 个实际 SDK direct 解码/转发场景。49 个有限消费者场景匹配执行结果
与 hook 顺序；78 个有限 client/server SSE 场景匹配最终值、历史与 SQLite
重开。Bash 非 mapping 根保留具名失败，before 可重写、拒绝优先，handler
不会执行；server 根仅为不可变观察值。最终诊断差异由 14 降到 4，仍为
NaN/Infinity，不能据此宣称完整 provider 或 Go port 验收。

## Named model Bash optional-field projections — 2026-10-10

Baseline: `9c01921` plus this implementation; Source contract commit `ffc3265`.
`BashBackgroundRequest` and `BashApprovalProposal` preserve finite observed option
values in immutable closed variants. Bash execution accessors return only a boolean
Python-truth request and an optional string-array proposal. Candidate length, exact
command-prefix match, banned heads and actual human approval remain the existing
agent rules. Invalid proposal shapes are ignored, not converted into grants.

Actual Source Agent/default Bash consumers cover 56 profiles: 15 background request
shapes and 13 proposal shapes, each with background tools enabled and disabled.
Text "false", nonzero numbers and nonempty containers request background execution;
empty text/containers, zero, false and null do not. Disabled background support
executes foreground regardless of the request. Twenty-eight wire samples (27 unique inputs)
retain exact original JSON values and kinds through direct/SSE decoding, clone,
masking, historical message reads and SQLite close/reopen. Native tests execute real
foreground shells/background tasks, compare Source candidate selection, and prove
readonly denial starts no background manager. Explicit DecodeToolInput is strict.

This closes the captured finite Bash optional-field adaptation. Nonfinite inputs,
non-text command values, other default tools, broader SDK/caller/coercion scope,
default diagnostics dependency approval and complete G7 acceptance remain open.

## Named non-text Bash command consumers — 2026-10-10

Baseline: `400b6b0` plus this implementation. Source contract commit `d12d58b`
pins actual Agent, default _bash, real Toolset/background consumption, candidate
functions and PermissionHook. Thirty-six consumption profiles and twenty permission
profiles cover finite null/bool/integer/float/array/object commands, foreground and
background activation, extra-keyword priority, before rewrites, before/guard denial,
readonly and immutable deny-list behavior. No invalid command starts a process/task.

BashCommandObservation owns a closed BashCommandKind and immutable finite value.
Permission and approval use Python str(value) as a policy-only projection. Native
handlers return named BashCommandFailure with split/lower stage: foreground split;
background auto selection lower; explicit background initializes the owned manager
then fails split. Original JSON stays in model/event/history records. Explicit
caller admission remains strict, and the existing gate still controls permission,
journal/replay, after hooks and result observation. Rejected command observations
are never shell argv. Before rewrite can replace them with executable text.

Protocol clone/masking/history and native direct/SSE/SQLite reopen cover 44 unique
inputs: 27 optional-field values and 17 non-text command objects. The prior options
inventory had 28 samples including the repeated absent-options object; uniqueness
is now enforced. Earlier statements of 28 distinct inputs are corrected to 28
samples (27 unique). Source authority decisions remain actual captured behavior.
Nonfinite inputs, other default tools, broader SDK/caller/coercion scope, default
syntax diagnostics dependency approval and full G7 acceptance remain open.
