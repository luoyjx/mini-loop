# P1.5/P1.6 受控失败与协议边界验收

2026-11-10 的本地真实进程场景已完成 P1.5 的**受控注入部分**和 P1.6 的协议边界。
它启动固定校验和的本地 Temporal、两个真实 Task API 进程和运行生产
`TaskWorkflow` 的测试 Worker；模型 fixture 只存在于仓库工具，不可由 HTTP 输入或
生产 CLI 选择。脱敏结果见
[机器证据](task-protocol-failure-evidence.json)。

## 结论

| 边界 | 实测结果 | 可作出的结论 |
| --- | --- | --- |
| 429 | 同一 Activity attempt 1/2 失败，3 成功；首末调用相隔 2009.1 ms | Temporal 最多尝试 3 次，最终成功，没有无限重试 |
| timeout | 同一 Activity attempt 1/2/3 均失败；首末调用相隔 3027.4 ms | 到第 3 次后稳定失败，不把失败伪装成成功 |
| disconnect | 同一 Activity attempt 1 失败、2 成功；首末调用相隔 1016.7 ms | 可恢复断连只发生一次重试 |
| Start 成功、响应丢失 | 代理完整接收上游 202 后丢弃客户端响应；按确定性 Task ID 查询成功，同 key 重试 `reused=true` | 不确定提交结果可核对；重试只对应 1 个 Temporal Workflow |
| 同 key 不同内容 | HTTP 409 | 内容冲突不会复用旧任务 |
| 越权查询与取消 | GET/result/events/cancel 均为 404 | owner 边界不会泄露任务存在性 |
| 取消/完成竞争 | cancel 接受时仍为 running，最终两个 API 都观察 cancelled；Workflow 数量为 1 | 确认只表示请求已接受；终态可核对且不会派生第二次执行 |

生产 Worker 的 `ChatOpenAI(max_retries=0)` 关闭 SDK 内层重试；fixture 每个记录的
Activity attempt 恰好调用一次模型边界。因此这里可解释的请求上界就是 Activity
attempt 数，而不是 SDK 重试与 Temporal 重试的乘积。受控 429、timeout、disconnect
都是在首次模型边界、任何工具副作用之前注入；证据不能外推真实供应商的网络栈、计费
或限流实现。

## 响应丢失方法

一次性 TCP 代理接收客户端 POST，转发到 API，完整读取并哈希 API 的 HTTP 202 响应，
随后在不向客户端发送任何响应字节的情况下断开连接。客户端实际得到
`RemoteProtocolError`。场景不读取响应正文来恢复 Task ID，而是用公开协议中的
`owner + Idempotency-Key` 确定性派生 ID，再从第二个 API 副本查询并重试。代理只把
status、字节数和哈希写入私有收据，提交的 token 与 message 不进入提交证据。

## 范围与后续

本次通过不等于 P1.4 或 P1.5 的真实 provider 集成通过。真实 provider 仍缺少短期
审批、目标 HTTPS Task API、file-backed API/trace 凭据和费用授权，因此没有执行任何
外部调用，也没有真实 token/cost/trace waterfall。P1.4 与 P1.5 的真实供应商部分必须
继续保持未完成。

复现命令（只使用已选定的本地二进制）：

```bash
cd deepagents-server
nice -n 10 .venv/bin/python -m tooling.acceptance.scenarios.task_protocol_failures \
  --temporal-binary ../var/deepagents-delivery/temporal-cli-1.9.1/temporal \
  --binary-sha256 33c298d41f6eefebf01a28a3511f444480e939e21d3518cb0c12a4abad467c4b
```

场景使用 180 秒总期限、2 GiB owned process-tree RSS 上限和 15% 系统空闲内存下限；
本次峰值为 634.9 MiB，最低空闲内存为 93%，20 次资源采样，全部子进程已确认退出。
