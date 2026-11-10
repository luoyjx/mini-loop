# P1.2 Task 基础指标合同

Task API 与 Worker 使用 Temporal Python SDK 1.34.0 自带的 Prometheus exporter，分别在
各自进程内提供机器可读 `/metrics`。生产 Compose 将两个角色的 `9464` 仅 expose 到容器
网络；端点没有应用层认证，不得直接发布到公网。未设置
`MINILOOP_TASK_METRICS_BIND` 时 exporter 关闭，library 调用不会读取环境或自行监听端口。

指标是 process-local、重启归零的运行观测，不是任务台账、结果索引或第二个调度器。
Temporal retained History 仍是 Task/Run 状态和结果的唯一权威。Prometheus 抓取、保留、
聚合和告警由部署环境负责；本轮没有新增持久指标服务或 dashboard。

## 固定标签与隐私边界

每条 exporter 指标有 `mini_loop_service_role=api|worker`、协议版本、snapshot schema 和
不可变镜像 digest。Task 指标只使用有限枚举标签，例如 operation、result、outcome、
status、retry 和 checkpoint kind。owner、TaskID、RunID、request ID、Activity ID、Worker
identity、message、Idempotency-Key、模型/工具内容、异常文本和凭据均不得成为指标名称、
标签或值。单任务关联继续使用 [P1.1 内容无关日志](TASK_OBSERVABILITY.md)。

## 自定义指标

启用 exporter 时 counter 带 `_total`，带单位的 histogram 使用 `_ms` 或 `_byte` 后缀。

| 指标 | 角色与语义 |
| --- | --- |
| `mini_loop_task_process_up` | API/Worker 进程已完成指标初始化；抓取失败仍须看 Prometheus 自带的 `up` |
| `mini_loop_task_api_temporal_ready` | API 最近一次 `/readyz` 的 namespace 只读结果；初始为 0 |
| `mini_loop_task_worker_polling` / `mini_loop_task_worker_draining` | Worker 已通过 SDK 启动校验并轮询，或正在停止轮询与排空 |
| `mini_loop_task_api_requests_total` | 非 probe 请求的 operation/result 观测；区分 success、rejected、timeout、error |
| `mini_loop_task_submissions_total` | submit 结果 accepted、reused、rejected 或 unknown；unknown 不代表任务不存在 |
| `mini_loop_task_status_observations_total` | API 返回的 running、succeeded、failed、cancelled、timed_out、terminated、continued_as_new；是读取次数，不是唯一任务转换次数 |
| `mini_loop_task_cancel_requests_total` | accepted、noop、rejected 或 error；accepted 仅表示取消请求已提交 |
| `mini_loop_task_workflow_runs_total` | Workflow Run started/completed 及已分类的 step/checkpoint 终止原因 |
| `mini_loop_task_model_boundaries_total` | 已完成的安全父模型边界，用于判断运行是否继续前进 |
| `mini_loop_task_continue_as_new_total` | 已发起的 CAN 次数 |
| `mini_loop_task_checkpoint_size_byte` | CAN 时 raw、compressed、envelope 三种状态大小分布 |
| `mini_loop_task_model_activity_attempts_total` | 模型 Activity attempt 的 started/completed/failed，`retry=true` 表示 attempt > 1 |
| `mini_loop_task_model_activity_queue_latency_ms` | 当前 attempt 从 scheduled 到 started 的排队时延 |
| `mini_loop_task_model_activity_inflight` | 当前 Worker 进程正在调用 provider 的模型 Activity 数 |

SDK 原生 Worker/Workflow/Activity 指标与上述指标出现在同一 scrape；其名称属于固定
Temporal SDK 版本的观测面，不替代本仓库稳定的 `mini_loop_task_*` 合同。意外 Workflow
失败、Temporal task 重试和 poller/slot 细节应同时查看 SDK 原生指标与 History。

## 最小排障查询

下面是可改写为告警的 PromQL 形状；实际 job/instance 由抓取配置决定。

```promql
# API 可抓取且 namespace ready，但没有可轮询 Worker
max(mini_loop_task_api_temporal_ready{mini_loop_service_role="api"}) == 1
and max(mini_loop_task_worker_polling{mini_loop_service_role="worker"}) == 0

# 最近 5 分钟有接受/复用，但没有新 Workflow Run
sum(rate(mini_loop_task_submissions_total{outcome=~"accepted|reused"}[5m])) > 0
and sum(rate(mini_loop_task_workflow_runs_total{outcome="started"}[5m])) == 0

# 有模型调用在途，但父模型边界和 CAN 都没有推进
sum(mini_loop_task_model_activity_inflight) > 0
and sum(rate(mini_loop_task_model_boundaries_total[10m])) == 0
and sum(rate(mini_loop_task_continue_as_new_total[10m])) == 0
```

第一条还应结合 scraper 的 `up`：目标完全死亡时无法自行报告 `process_up=0`。第二条是
跨副本的速率信号，不是一一配对；第三条可能代表正常的长模型调用，需结合 Activity
queue latency、attempt、provider 告警和 P1.1 关联日志确认。没有单个 gauge 能证明端到端
任务健康。
