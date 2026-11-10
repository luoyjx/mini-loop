# P1.4 真实 provider 验收

状态：**验收器与固定用例已就绪；真实执行未获授权，P1.4 尚未通过**

本合同把一项真实 Task 的输入、状态检查、Temporal Run 链、LangSmith usage/cost、预算停止
条件和证据文件固定下来。它不启动或 provision Temporal、Task API、Worker、模型账号或
LangSmith project；操作者必须先提供已有的预生产部署。提交中的 P0 profile 继续保持
`execution_authorized=false`，代码和示例文件都不能授权消费。

## 固定用例与通过条件

机器权威用例是 [PRODUCTION_PROVIDER_CASES.json](PRODUCTION_PROVIDER_CASES.json)。当前只包含
`planning-files-subagents-summary-can-v1`：使用合成内容和固定模型
`openai:gpt-4.1-mini`，要求真实 Deep Agents 执行规划、两个 general-purpose 子代理、精确
虚拟文件、完成 todos、原生摘要和至少两次 Continue-As-New。

验收器不按最终自然语言猜测成功。只有以下证据全部成立才写 `status=passed`：

- Task API 返回成功的 schema 2 snapshot，TaskID、首 Run、最终 Run 与状态一致；
- 19 个指定文件逐字节匹配且没有未请求的 artifact，五个指定 todo 全部完成；已脱敏 trace
  精确出现固定工具调用且没有其他工具 run
  （2 task、19 write_file、2 read_file、2 write_todos）；
- `can_count>=2`、父模型边界不少于 20，并保留 `_summarization_event`、session ID 和历史文件；
- 从最终 Run 反向读到完整原生 History 链，Run 数等于 `can_count+1`，无导航缺口或
  `WorkflowTaskFailed`；
- 专用 LangSmith project 中可定位 Workflow、Activity 和 ChatOpenAI 父子节点；所有 trace
  input/output/serialized/runtime/event/attachment 为空；
- 每个完成的真实 OpenAI model run 都有非负 token 与 cost。任何缺失均为
  `provider_usage_unknown` / `inconclusive`，绝不按零计费或继续冒充通过。

完整 Task result 只写入 mode 0600 的私有运行目录；可分享 receipt 只含摘要、计数、ID、
SHA-256 和已批准的环境标签，不含 prompt、Bearer token、LangSmith key 或模型输出正文。

## 三重执行授权

运行真实用例必须同时满足三项，缺一即在网络调用前退出：

1. 显式环境确认 `MINILOOP_TASK_ACCEPTANCE_REAL=approved-live-provider-run`；
2. 仓库外、mode 0600 的短期 approval JSON，`approved=true`，有效期不超过 24 小时；
3. approval 精确绑定 clean HEAD、不可变镜像 digest、唯一 case、模型、目标环境及本次/月度预算。

[approval 示例](PRODUCTION_PROVIDER_APPROVAL.example.json)故意是 `approved=false` 且包含无效
占位符，不能直接运行。复制到仓库外后由操作者填写；不要提交批准文件或凭据。验收器还要求
Task Bearer 和 LangSmith read key 分别来自 mode 0600 单行文件。Task API 与 LangSmith 只允许
HTTPS；两个客户端都禁用环境代理和重定向，避免凭据被隐式转发。真实 `run` 只接受仓库内
固定 profile 与 case manifest；外部替换文件不能扩大模型、提示词或预算范围。

approval 中的上限不得超过 P0 profile：600 provider calls、200 万 tokens、单次 10 美元、
自然月 50 美元。建议本固定用例批准更窄的 64 calls / 20 万 tokens / 10 美元。每次轮询会
核对已入库的 calls、tokens 和 cost；超过预算、发现 trace 内容或 unknown usage 会请求取消。
LangSmith 入库存在延迟，因此这不是 P3.3 的原子、持久、跨 Worker 全局预算。运行前仍须在
provider 账号/project 设置独立硬消费上限；P1.4 receipt 只陈述本次观察结果。

## 运行与证据

无网络、无消费的合同检查：

```sh
.venv/bin/python -m tooling.acceptance.live_provider check
```

真实执行模板；所有路径和目标必须由获批环境提供：

```sh
export MINILOOP_TASK_ACCEPTANCE_REAL=approved-live-provider-run
.venv/bin/python -m tooling.acceptance.live_provider run \
  --approval-file /private/p1-4-approval.json \
  --api-base-url https://task-preproduction.example \
  --task-token-file /private/task-bearer \
  --langsmith-project mini-loop-task-production \
  --langsmith-api-key-file /private/langsmith-read-key \
  --output-dir /private/evidence/p1-4-run
```

输出目录必须不存在且父目录须由操作者预先 provision，验收器不递归建目录并以 mode 0700 创建。
`private-result.json` 保存合成完整结果，
`receipt.json` 保存通过、失败或 inconclusive 分类。运行超时、API/trace 不可用、结果文件错误、
History 不完整、trace 拓扑缺失、预算超限和凭据/内容泄漏均使用固定错误码；远端响应正文不会
进入错误消息。

交付时还必须附管理面证据：LangSmith 专用 project/workspace、实际 30 天 TTL、provider
账号硬预算、供应商/地域/namespace/许可，以及过期删除计划。验收器只能查询数据面，不能替
操作者设置或证明这些控制。

## 当前阻断

2026-11-10 当前环境没有 OpenAI key、LangSmith key、Temporal/Task 目标或外部 approval，
且 P0 profile 明确禁止消费。因此本轮只运行离线 `check` 和单元/回归测试，没有真实模型调用、
费用、远端 trace、TTL 截图或通过 receipt。P1.4 保持未完成，直到一次获批真实运行生成并复核
上述完整证据。
