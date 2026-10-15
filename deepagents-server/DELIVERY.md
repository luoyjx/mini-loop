# Deep Agents Server delivery — 2026-10-16

Outcome: 新增独立、可启动的 Deep Agents Agent Server 模块，基于 `3df5f4e`
及 MDA 参考设计的标准 Agent Server 路径。已有 Python/Go runtime 不接入此模块。

## 实现与验收依据

| 要求 | 实现与直接证据 |
| --- | --- |
| 新模块及独立依赖 | `pyproject.toml`、`uv.lock`、`.python-version`；`uv sync --locked`、wheel 构建成功 |
| 真实 Deep Agents harness | `graph.py` 调用 `create_deep_agent`；真实文件和 general-purpose 子代理工具在 HTTP smoke 中执行 |
| Agent Server API | `langgraph.json` 注册 graph factory；真实 Agent Server 0.16.0 接受 Threads/Runs/SSE 请求 |
| 身份与资源隔离 | `auth.py` Bearer→owner；跨用户 read/update/delete/run 拒绝；Store API 跨 namespace 拒绝；context/config 身份伪造不能改变 memory owner |
| 状态与恢复 | StateBackend 线程文件、StoreBackend owner memory；优雅重启开发服务后再次读回文件及 memory，并保持 owner 限制 |
| 调度与控制 | 实际后台 Run 运行期间，同线程 reject 返回 409；取消后状态为 interrupted |
| 可使用入口 | `scripts/chat.py` 对真实服务完成 SSE；模块 README 包含安装、配置、启动、恢复和 HTTP 命令 |
| 部署配方 | CLI 生成 Dockerfile，固定基础镜像/PG/Redis digest；Compose 结构校验通过；持久化部署验证独立列为未执行 |
| 架构同步 | README Mermaid、边界、review baseline、Archify JSON/generated HTML、完整 base PNG 与本次标注 PNG 同步 |

版本固定为 Deep Agents 0.7.25、Agent Server 0.16.0、CLI 0.4.35、SDK 0.4.7、
OpenAI adapter 1.7.2；本地执行 Python 3.12.10。传递依赖及分发包 hashes 在 `uv.lock`。

## 验证记录

- 新模块单元/graph 检查：**82 passed**，1.61 秒；包含 production factory 的真实
  ChatOpenAI 构造/schema 检查，不发送 provider 网络请求。
- `scripts/smoke.py`：10 组真实 HTTP 检查通过；两次启动均为实际 `langgraph dev`，
  使用仅在 `tests/` 中的 stateless 测试模型，未调用外部模型。
- HTTP smoke 服务进程树采样峰值 **175.2 MiB**，RSS 预算 1 GiB，macOS memory-free
  下限 15%；运行后服务进程组已退出。
- 根目录 `nice -n 10 .venv/bin/python -m pytest -q`：**2256 passed, 28 skipped,
  104 warnings**，138.98 秒；采样峰值 **279 MiB**，最低 memory-free 36%。
  预算 1.5 GiB / 300 秒，未触发资源阻断。该 gate 针对原 `python/tests`，不包含新模块。
- `.venv/bin/python python/tools/verify_invariants.py`：通过，**77 modules checked**；
  它检查既有 Python package，不包含新模块。
- `langgraph validate`、`uv lock --check`、`uv sync --locked`、`uv build --wheel` 通过；
  wheel 包含 `instructions.md`，不含 `.env` 或测试 graph。
- Compose：使用临时合成凭据执行 `docker compose --env-file <temporary> config
  --no-env-resolution --quiet`，退出 0；未输出真实凭据。
- Archify `validate` + `deliver`：**9/9 showcase**，0 errors / 0 warnings；完整图及
  新泳道明暗主题已静态 SVG 目检。浏览器交互检查因环境及 file URL 策略未完成。
- `git diff --check`：通过；最终 staged diff 同样执行 whitespace 检查。

原始测试日志、资源采样、HTTP 结果在忽略目录 `var/deepagents-delivery/`。
Go 实现、source scanner 和 guarded mutation anchors 没有改变，未运行 Go 或扫描/变异 gate。

## 架构产物

- 规范：[README Mermaid](../README.md#architecture)。
- Archify source：[architecture JSON](../docs/mini-loop-system.architecture.json)；
  generated [interactive HTML](../docs/mini-loop-system.architecture.html)。
  Spec SHA-256 `f9cb50de6bc7e2b9f9048e13de83ab9afd2e51243658c15578a9de3eb81aa47a`；
  HTML SHA-256 `8b57773fb9611783db20e198fdcba1a93820f3af9e517844a5f37a1543040290`。
- 完整 base：[PNG](../docs/mini-loop-layered-architecture.png) 与
  [exact prompts](../docs/mini-loop-layered-architecture.prompt.txt)。
- 本轮图：[highlighted PNG](../docs/audits/deepagents-server-2026-10-16.png) 与
  [exact prompt](../docs/audits/deepagents-server-2026-10-16.prompt.txt)。
  Built-in imagegen 编辑完整 base，橙色仅标注新增独立模块，L1–L5 保留。

## 未执行和能力边界

没有提供真实模型/平台凭据，因此**未执行真实 OpenAI 推理、Docker image 构建、
持久 PostgreSQL/Redis 服务启动、崩溃/断电恢复或云部署验收**。已完成本地可运行模块，
这些是实际部署环境的额外 gate，不将开发快照测试描述成生产持久化通过。

不实现 MDA Compiler、Context Hub、托管 Sandbox、OAuth、Channels；Cron 拒绝。
没有宿主 shell/local filesystem backend；`execute` schema 名称可能存在但执行被 backend 拒绝。
memory 同 owner 跨线程共享，不具备并发编辑 CAS；不要存 secrets/业务唯一事实来源。
不保证外部工具 exactly-once，不把 checkpoint 视作逐 token SSE replay log。
