# Task trace 与模型用量合同

P1.3 使用 Temporal Python SDK 1.34.0 自带的实验性 `LangSmithPlugin`，把 API
发起的 Workflow、Workflow/Activity 调度和 Deep Agents 已有 LangChain trace 上下文
串联起来。Temporal History 继续是 Task、Run、重试、状态和结果的唯一权威；LangSmith
是可丢失的诊断副本，不参与调度、去重、恢复或业务判断。

## Trace 结构与用量

生产 Task profile 在 API 与 Worker 的 Temporal Client 上安装同一个显式配置的插件，
并开启 Temporal run 节点。典型任务树包含：

```text
RunWorkflow:TaskWorkflowV2
  parent agent / model
    general-purpose subagent / model
    summarization middleware / model
  StartActivity:deepagents.invoke_model
  RunActivity:deepagents.invoke_model  # 每个 Temporal attempt 独立出现
    provider model run
```

实际层次由 Deep Agents/LangGraph 的固定节点与 SDK 上下文传播生成，不另建执行器。
Workflow replay 使用 SDK 的确定性 run ID，重放不应产生新的 trace；Activity retry 是
新的实际调用，因此保留独立 attempt 和耗时。父代理、general-purpose 子代理和摘要调用
通过既有父子层次、固定 run/node 名和 `langgraph_node` 区分。

LangChain 在 provider 返回标准 `usage_metadata` 时，把 `input_tokens`、
`output_tokens`、`total_tokens` 及安全的 token detail/cost 数值放进对应模型 run metadata。
Worker 的 `task.model_activity` 完成/失败记录也写同一 attempt 的三项 token 用量；字段
缺失、非整数或调用失败时写字符串 `unknown`，部分可用时写 `usage_status=partial`，绝不
用 0 代替未知。0 只表示 provider 明确报告了整数 0。跨 attempt 或跨父/子/摘要的求和
必须保留 unknown/partial 状态，不能把它们当作免费调用。

## 上传前脱敏

`task_server.tracing` 创建专用 LangSmith Client，不依赖 SDK 的环境默认 Client：

- 所有 run inputs 和 outputs 在进程内清空；模型 message、tool argument/result、文件内容
  和摘要正文不会进入 trace payload。
- exception/traceback 正文统一替换为固定的 `redacted`；不上传 Python/OS/runtime 信息。
- Client 在 SDK enqueue 前再对整个 run operation 做 fail-closed 清洗：删除 `serialized`、
  event 与 attachment，只保留协议 ID/时间/父子关系、受控 name/tag 和 allowlist metadata；
  因而 LangChain serializer/runtime 不能绕过 input/output 开关携带正文或主机信息。
- metadata 使用 allowlist，只保留 Task/Run/Activity 关联 ID、固定图节点/模型标签、镜像与
  wire identity，以及经类型、非负和有限值校验的 usage/cost 数值。owner 被删除。
- API key 只从两个角色各自的只读 Secret 文件加载；inline 与 file 同时存在会拒绝启动。
  key、文件名和底层 SDK 异常都不进入启动错误。

Run name 和 tag 只来自固定 Workflow/Activity/graph/tool 名与部署代码；当前受控 pilot
不得引入由 Task message、文件内容或 tool payload 动态构造的 run name/tag。新增工具或
动态命名在进入 profile 前必须补脱敏测试。

## 启用、保留与故障边界

Library 默认 `TaskTracingSettings()` 关闭且不读取环境。生产 `compose.task.yaml` 显式要求：

- `LANGSMITH_TRACING=true`；
- 专用 `LANGSMITH_PROJECT` 与 HTTPS/443 endpoint；
- 两个角色都可读的 `LANGSMITH_API_KEY_FILE`；
- `MINILOOP_TASK_TRACE_RETENTION_DAYS=30`。

30 天是 pilot 的最大 trace 保留策略。启动校验只证明部署声明与合同一致，不能设置或
验证远端项目的删除策略；上线门禁必须由 LangSmith 管理员把专用 project/workspace 的
TTL 配为 30 天并保存截图或 API/审计证据，过期后抽查删除。不得通过 feedback、dataset
复制或导出延长包含 pilot trace 的保留期。API key 应使用可用的最小 workspace 权限，
并独立于模型和 Temporal 凭据轮换。

Trace 写入和 flush 是诊断路径；远端不可用不改变 Temporal 任务结果。由于当前没有
LangSmith 凭据或目标 workspace 的仓库内证据，单元测试只验证插件装配、上传前脱敏、
attempt 用量和 unknown 语义；真实 project 的连通、TTL 与一项真实任务 waterfall 必须
在 P1.4 provider 验收时一并保存，不能由本合同推导。

## 运维检查

用 TaskID 在专用 project 中定位 `RunWorkflow:TaskWorkflowV2`，再核对：

1. parent、general-purpose 和 summarization 分支均能从父子树识别；
2. 每个 `RunActivity` attempt 与 Temporal History 的 Activity ID/attempt 对应；
3. 模型 run 有 provider 报告的 usage，或本地 JSONL 明确为 unknown/partial；
4. inputs、outputs、error、owner、测试 token、prompt 和 tool payload 均不可见；
5. waterfall 的模型/工具/排队耗时只用于解释，不覆盖 Temporal 状态与指标告警。
