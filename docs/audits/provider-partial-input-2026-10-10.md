# Provider partial-input Source 对照审计 — 2026-10-10

## 固定来源和范围

- Go 生产代码快照：`689a1d9`；本轮不修改解析器或运行时行为。
- 实际 Python SDK：Anthropic 0.107.1，jiter 0.15.0；使用 AsyncAnthropic +
  httpx.MockTransport 完整 SSE，不只直接调用 JSON 库。
- Source helper 固定 SDK accumulator 和 `_content_payload` 源码 SHA256；
  每个 delta 的已交付快照、完整结果或实际 ValueError 都记录在 fixture。
- 49 种输入 × client/server tool-use 两种块 = 98 个场景。
  expected_json/snapshots_json 使用 JSON 字符串保存观察值，容纳 SDK 原样保留的
  NaN/Infinity；整个 fixture 文件仍是标准 JSON，不把非有限数写成裸 JSON token。

## 当前结果

| 块 | SDK 成功 | SDK 拒绝 | 最终结果一致 | 最终结果不同 |
| --- | ---: | ---: | ---: | ---: |
| tool_use | 41 | 8 | 12 | 37 |
| server_tool_use | 41 | 8 | 17 | 32 |
| 合计 | 82 | 16 | 29 | 69 |

“最终结果一致”统计完整成功 content 相等，或双方都拒绝；后者不证明错误时序或
诊断相同。SDK 的 16 个拒绝均在对应 input delta 的累计解析发生；Go 在块停止
时拒绝。审计器显式记录 native_failure_event/native_delta_count。这个诊断任务
运行成功不表示 98 个场景通过迁移验收。

## 已验证差异与实现顺序

1. SDK 在每个非空累计 buffer 后调用 `from_json(..., partial_mode=True)`；Go
   当前在块停止后严格解析。未闭合对象、已完成成员、不完整字符串/数字/关键字、
   嵌套容器与 duplicate key 的保留规则需要逐 delta 实现和验证。
2. SDK 空 delta 保留初始输入；Go server 已保留，client 会清空 buffer 并在停止
   时失败。需要将同一累计语义应用于两类块。
3. SDK partial 解析还接受捕获中的 trailing garbage，以及某些不匹配结束符后的
   已完成成员。不能把普通 JSON 解析器加一个闭合括号当成等价实现。
4. 客户端具名 ToolInput 的额外字段拒绝与 SDK 中间 input 保留是另一条边界；例如
   complete 的 keep 字段在 server 成功、client 被具名校验拒绝。需对照 Python
   Agent 实际工具参数消费规则，再决定映射，不把它全部归因于 partial parser。
5. SDK 可构造非对象根和非有限数；Go 的 ServerToolInput 要求对象、标准 wire
   JSON 要求有限数。目前差异已留存，尚未对照 Source 执行/转发/持久化消费者
   宣布适配理由或完整验收。

## 证据与重放

- [Source fixture](../../go/testdata/python-provider-partial-input.json)
- [SDK 捕获 helper](../../python/tools/provider_partial_input_contracts.py)
- [完整原生观察](provider-partial-input-2026-10-10.json)
- [Go 诊断任务](../../go/provider/partial_input_audit_test.go)

从 go/ 执行：

```sh
MINILOOP_PROVIDER_AUDIT_OUTPUT=/absolute/path/report.json \
MINILOOP_PROVIDER_AUDIT_BASELINE="explicit production snapshot" \
  go test -p=1 -count=1 ./provider -run TestProviderPartialInputAudit
```

诊断任务默认跳过，仅在指定输出时记录观察；不会把当前已知差异变成绿灯验收。
本报告保留 `689a1d9` 的历史生产代码快照。诊断任务现在要求显式传入
MINILOOP_PROVIDER_AUDIT_BASELINE，避免把新实现误标为旧基线；历史结果不代表之后版本。

Source fixture SHA256: c4098b590fd737169fab87ce5b2a61a40290b08ecc8b29d1c2a3720ccd9d2795。

## 本轮验证

Source helper 单测 1 passed（4 个预期 SDK serializer 警告）；显式原生诊断重放、
普通 Go 全量测试、go vet、19 项扫描与 git diff --check 通过。普通 Go watcher
测试结束后 Source export --check 通过：213 份契约均为当前结果。
本轮未修改生产代码，也未刷新 race、完整 Python、类型/变异门禁或覆盖率；
诊断差异仍然开放，普通测试通过不能证明这些差异已解决。

## 原生逐 delta 实现后的对照

原生实现快照为 `4b1aec7` plus native partial parsing；
[新观察报告](provider-partial-input-native-2026-10-10.json)保留全部 98 个结果。
独立 partial decoder 的 98 个 SDK 逐 delta 快照、对应拒绝阶段及 42 个 jiter
边缘契约已通过断言。34 个 finite server object 对上最终结果、历史和 SQLite 重开。
最终差异现在为 40（client 33、server 7），比原始 69 少 29；16 个语法拒绝均已
提前到对应 input delta。client named 参数额外/缺失字段和 root/nonfinite 消费者
仍待 Source 对照；此更新不证明所有 98 个最终回复已达到迁移一致。

原生实现验证：普通全量 Go 测试通过。首次全量 race 的 CLI reload 进程测试收到
HTTP 500（第二轮环境检查），未报告数据竞争；该用例单独 race 重跑与无并行
验证任务的完整 race 重试均通过。首次 500 根因未确认，未修改 reload 代码或测试。
首次失败和重试日志均保留，不宣称首次 race 全量无中断通过。

最终 Source export --check 通过：214 份契约当前，包含 42 个直接 jiter 边缘场景。
该捕获在所有原生 watcher 测试结束后执行，未重写 watcher helper/golden。
定向套件、vet、类型审计（423/1386/0）、19 项扫描及 3 项 block 变异子集通过。
本轮未刷新完整 Python、浏览器视觉、非 Darwin、覆盖率或完整变异测试。
