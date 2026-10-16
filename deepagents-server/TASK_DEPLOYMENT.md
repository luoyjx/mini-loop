# 独立 Task API / Worker 容器部署

此部署入口对应 `task_server`，由 `08b4333` 交付。一个应用镜像通过
`launch api` 或 `launch worker` 启动实际 CLI；API 和 Worker 是独立进程与容器，
共用外部 Temporal 的 namespace、队列和保留历史。它不启动标准 LangGraph Agent
Server，不共享原 Python/Go 业务，不创建 Temporal、PostgreSQL、Redis 或入口代理。

当前源码已按 [项目结构](docs/PROJECT_STRUCTURE.md) 重构；构建实现位于
`tooling/build_task_bundle.py`，原 `scripts/build_task_bundle.py` 保留兼容入口。
下文 `08b4333` 镜像与冷重启结果是历史基线，当前源码需重新构建并核对自己的 manifest。
本轮实际验证以 [交付记录](DELIVERY.md) 为准。本文中的配置
不代表生产 HA、容量、真实模型调用或不兼容版本滚动升级已通过验收。

## 构建可审查的镜像

新增依赖只有用户批准的 Docker 官方基础镜像：
`python:3.12.15-slim-bookworm@sha256:34386ef0cb081344d7ec1c103ba398e6e9f64e9ab3a1509accc92a4e24a07258`。
此摘要固定多平台索引；已有平台验收仅针对 `linux/arm64`。Python 包继续使用已有
`uv.lock` 的默认依赖与 `temporal` extra，没有增加包或使用浮动安装版本。镜像仍
包含当前模块的既有依赖闭包，独立入口不等于已经裁减为只含 Temporal 的发行包。

以下在仓库根目录执行，`TASK_BUILD_DIR` 指向尚未存在的、任务专用绝对目录。
构建期间不得并发修改模块源码、README、锁文件和 Dockerfile。

```sh
mkdir -p "${TASK_BUILD_DIR:?set a private build directory}"
nice -n 10 uv build --project deepagents-server --wheel --offline \
  --out-dir "$TASK_BUILD_DIR/wheel"
nice -n 10 deepagents-server/.venv/bin/python \
  deepagents-server/scripts/build_task_bundle.py \
  --wheel "$TASK_BUILD_DIR/wheel/mini_loop_deepagents-0.1.0-py3-none-any.whl" \
  --output "$TASK_BUILD_DIR/context" \
  --forbiddenfruit-wheel "${TASK_FORBIDDENFRUIT_WHEEL:?set the existing audited wheel path}" \
  --forbiddenfruit-sdist "${TASK_FORBIDDENFRUIT_SDIST:?set the locked sdist path}"
```

`build_task_bundle.py` 先逐文件核对 wheel 中的包源码、元数据、README 和 RECORD，再
按锁文件离线导出依赖。输出是专用 allowlist 构建上下文：Dockerfile、带 hash 的
requirements、已验证 wheel、manifest 和 `.dockerignore`。环境文件、部署凭据、
测试、`tooling/`、脚本及运行状态不进入上下文；不能将仓库目录直接传给 `docker build`。
生成上下文要求目标不存在，避免复用混有其他文件的旧目录。

锁中既有的 `forbiddenfruit==0.1.4` 只有 sdist，纯 binary-only 下载无法完成。脚本
为此要求显式提供既有 `forbiddenfruit-0.1.4-py3-none-any.whl` 与锁中的 sdist，不会
自动下载、执行或编译 sdist。先核对 sdist 的锁 hash/大小，再核对 wheel 唯一运行
模块和两份许可证逐字节相同、严格七文件 allowlist、包名/版本/依赖/extra 元数据、
README、pure-Python tag 和全部 RECORD。只给此依赖追加审计后的 wheel hash，
通过 `--find-links` 提供安装候选，其他依赖继续按锁定 wheel hash 安装。
`manifest.audited_dependency_wheels` 保存源归档与 wheel 摘要及匹配源码清单；
`generator_claim` 只是 wheel 声明，不是可信构建来源或完整可复现构建证明。
未提供上述两个输入时构建明确失败，不开启源码构建或静默删除既有默认依赖。

Dockerfile 的两阶段构建都使用上述固定基础镜像。安装先核对上下文摘要，使用
`--require-hashes --only-binary=:all:` 安装锁定依赖，再以 `--no-deps` 安装应用 wheel，
最后执行 `pip check`。导出是离线操作，首次镜像拉取和依赖 wheel 下载仍需要网络。

在本轮 Docker daemon 支持 legacy builder 的环境，可用下述受限命令构建；
`TASK_BUILD_CGROUP` 必须是本次运行独有名称。CPU/内存参数约束 build container，
还需观察 daemon/VM 和宿主内存压力；这些参数不是整个 Docker VM 的硬上限。
重型构建应串行执行，压力上升时停止本次所属构建并保存日志。

```sh
DOCKER_BUILDKIT=0 docker build --platform linux/arm64 \
  --memory 1920m --memory-swap 1920m \
  --cpu-period 100000 --cpu-quota 190000 \
  --cgroup-parent "${TASK_BUILD_CGROUP:?set a unique build cgroup}" \
  --no-cache --tag mini-loop-task:local "$TASK_BUILD_DIR/context"
docker image inspect mini-loop-task:local --format '{{.Id}}'
```

本地 `.Id` 是当前 daemon 上不可变的镜像内容标识，不是可跨主机拉取的 registry
manifest digest。生产应将经过验收的镜像发布到自己的 registry，再把
`MINILOOP_TASK_IMAGE` 固定为 registry 的 `name@sha256:...`；Compose 不会代为构建
或发布镜像。不同 CPU 架构需单独构建并验收相应依赖 wheel，不能由索引摘要推定通过。

## 准备外部服务与凭据

部署方应已有 Temporal 服务、可读的 namespace、可信 CA/服务名及各角色的 RPC
权限，并提供接入 Compose 网络的负载均衡入口。默认示例采用客户端 mTLS：API 与
Worker 分别挂载自己的证书/私钥，共用 CA。服务端授权策略、namespace 配额、
History retention、备份和数据库 HA 仍由外部 Temporal 部署负责。

```sh
cd deepagents-server
cp .env.task.example .env.task
```

编辑被忽略的 `.env.task`：填写镜像引用、外部服务地址、namespace、队列和部署宿主
上的绝对 Secret 文件路径，只保存路径与非敏感配置，不写入凭据内容。

| 容器角色 | 独有的 Secret 挂载 | 共同的连接配置 |
| --- | --- | --- |
| API | token→owner JSON、cursor 签名 secret、API 客户端证书/私钥 | Temporal CA、服务名、地址、namespace、队列 |
| Worker | OpenAI API key、Worker 客户端证书/私钥 | 同上；不需要 HTTP Bearer 或 cursor secret |

所有 API 副本必须使用相同 token→owner 映射和 cursor secret。已有 token 不应改绑
owner。cursor secret 轮换会使旧事件游标失效。模型配置和密钥只进入 Worker；API
不能从请求选择 provider 或测试 fixture。

`launch.py` 只为当前角色加载 allowlist 的 `*_FILE`：API 的
`MINILOOP_DEEPAGENTS_TOKENS_FILE`（64 KiB）和
`MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE`（4 KiB），Worker 的
`OPENAI_API_KEY_FILE`（16 KiB）。同名 inline 环境值与文件同时存在会拒绝启动；
文件必须可读、非空、普通文件或指向普通文件的 Secret symlink。标量允许一个末尾
LF/CRLF，禁止空白、多行及非 ASCII 可见字符；token JSON 仍由实际 API 解析和校验。
加载整体校验通过后才更新进程环境，随后在同进程调用实际 CLI，不用 shell/子进程。

Temporal 的 `MINILOOP_TASK_TEMPORAL_*_FILE` 继续由共享连接层读取，不由 launcher
转存。API-key-only Temporal 部署需要显式 Compose override：移除客户端证书对及
其挂载，设置 `MINILOOP_TASK_TEMPORAL_API_KEY_FILE` 的独立 Secret，保持 TLS=true、
正确服务名和可信 CA。本文 mTLS profile 不验证 Cloud API-key ACL。

容器使用数值 UID/GID `10001:10001`。Compose 的本地 file-backed Secret 保留宿主
文件权限；部署方必须使这个身份能读取需要的文件及父目录，同时限制其他身份访问。
不要用 `chmod 777` 修补权限，也不要假定 Compose secret 的 `mode/uid/gid` 能覆盖
本地 bind mount 权限。密钥不会进入镜像，但加载后的应用凭据存在进程内存/环境中；
这不是对宿主 root 或容器管理员的保密边界。

## 启动、探测与替换

先静态展开配置，再按已批准的资源预算启动。以下是两 API、两 Worker 的部署命令，
不是这台开发机的容量建议；每个 Worker 默认一 Activity 槽、两 Workflow task 槽。

```sh
docker compose --env-file .env.task -f compose.task.yaml config --quiet
docker compose --env-file .env.task -f compose.task.yaml up -d \
  --scale task-api=2 --scale task-worker=2
docker compose --env-file .env.task -f compose.task.yaml ps
```

API 只向容器网络 expose 8123，没有固定 host port、`container_name` 或内置负载
均衡器。已有 ingress 必须加入项目网络并把请求路由到健康 API 副本；宿主浏览器
不能因此直接访问 localhost:8123。HTTPS、边缘限流和公开 probe 访问策略由 ingress
配置承担。任务 HTTP 协议与 owner 隔离见 [Task Server](TASK_SERVER.md)。

| 端点或机制 | 当前语义 | 不能由它推导 |
| --- | --- | --- |
| `GET /healthz` | 无认证的进程存活响应，不访问 Temporal | Temporal、Worker 或 provider 可用 |
| `GET /readyz` | 无认证的只读 `DescribeNamespace`；要求名称匹配且 state 为 REGISTERED；只返回 ready/not_ready，HTTP 200/503 | Workflow Start 权限、队列存在可用 Worker、模型额度或端到端完成 |
| readiness 预算 | 单次 RPC 1 秒；进程内 singleflight；成功/失败均缓存 1 秒；请求取消不取消共享 RPC | 集群全局探测预算或完全即时的故障切流 |
| Compose API healthcheck | 每 10 秒调用 readyz，单次 timeout 3 秒，3 次失败，start period 20 秒 | unhealthy 会自动重启或负载均衡会自动摘除 |
| `restart: unless-stopped` | daemon 的进程退出重启策略 | 把 readiness 不健康转换成容器重启 |

Worker 没有网络依赖 liveness probe；短暂断链交由 SDK 重连，避免通过反复重启放大
网络故障。launcher 为 Worker 默认生成 `hostname:pid` identity，显式 `--identity`
仍由实际 parser 接收。日志 READY 只证明 SDK Worker 已启动，不保证任务成功。

两个角色均启用 init、只读 root、64 MiB `/tmp` tmpfs、drop ALL capabilities、
no-new-privileges 和 128 PID 限额。API 默认 0.5 CPU/512 MiB，Worker 1 CPU/1 GiB。
这些是小规模配置，不是已验收容量，也不等于运行不受信任代码的 sandbox；容器仍
需要到 Temporal 和 provider 的网络。应用状态由外部 Temporal 保存，不在 `/tmp`
或容器 writable layer 保存恢复权威。

Compose 用 SIGTERM 和 20 秒 stop grace，保留 Worker 默认 2 秒 drain 与 15 秒
硬退出的时间；不能改成 Docker 默认 10 秒却仍宣称完整执行了 15 秒硬期限。
Worker 停机不发送 Task cancel，未完成 Activity 的接管仍消耗既有重试预算。
`08b4333` 实际 Linux 容器验收发现：仅在 Python signal handler 中 `Event.set()` 不能可靠
唤醒空闲 selector。修复在保留立即置位的同时用 `loop.call_soon_threadsafe` 唤醒
事件循环，启动信号竞态和原硬期限语义保持不变。

同版本替换可在核对镜像、Secret 和配置后执行：

```sh
docker compose --env-file .env.task -f compose.task.yaml up -d \
  --force-recreate --scale task-api=2 --scale task-worker=2
```

Compose recreate 不承诺逐个零停机滚动或 surge/PDB。API 短时全部不可用时客户端
应保留 TaskID，恢复后查询同一任务；任务恢复依赖外部 Temporal 可用与兼容 Worker。
更改队列会改变请求摘要，同 key 可能返回 409。新镜像摘要只是制品 pinning，不是
Temporal Worker Versioning；不兼容 Workflow/graph 升级须先实现版本路由或排空。
证书文件在 Client 创建时读取，挂载内容更新后仍需重建 Client/替换进程。

## 验收证据与尚未覆盖项

要求与证据对应关系见 [Task 验收矩阵](docs/TASK_ACCEPTANCE.md)。以下构建、Linux 修复
与两任务容器故障保留为 `08b4333` 历史基线；后半的六任务组合验收由 `a6d61c6`
记录，复用了该镜像。它们不自动覆盖本轮重构源码。

`08b4333` 的 `linux/arm64` 应用镜像内容标识为
`sha256:3ccfbdd7c2a184b8e655f012a81da5d42310f733e07b0108a1c742763411e30f`，
构建 manifest SHA-256 为
`118e0aa4e2288259745cc46ed9732d3b550c032e458c862ab9dab7281b2f5b50`。
这是本机 daemon 的 image ID，尚未发布到 registry。重建耗时 142.31 秒，采样的
所属 build cgroup 峰值 532.24 MiB，最低宿主可用内存 38%、VM 可用内存 90.47%；
构建容器限额 1920 MiB/1.9 CPU，额外 observer 64 MiB/0.1 CPU，daemon/VM 增长
仅作软预算观察。镜像中 24 项包源码与输入匹配，99 个 distribution 按锁/基础镜像
版本核对；这些数量不是运行容量。

该提交的历史故障运行 `0c189c0de2614b5c8d4db07c4b4268be` 的 **4 组通过**：

| 实际场景 | 观察结果 |
| --- | --- |
| 默认生产 `launch worker`，空队列 | READY 后 SIGTERM 进入 DRAINING/STOPPED，0.914 秒退出 0；没有提交任务或调用 provider |
| 两 API、两受信任模型 fixture Worker 容器分别 SIGKILL/重建 | 同 key 复用已接受 Task，两个门控 Activity 从 attempt 1→2，Run/Activity ID、request SHA 与完整 native 状态保持一致 |
| 两项真实 Deep Agents V2 Task 完成 | 各 10 个 native 工具步骤、1 次 CAN；TaskID/首 Run 保持，4 份 History 合计 0 WorkflowTaskFailed |
| 已完成任务后 Temporal 断开/同 SQLite 重启 | 原 API 容器不退出，readyz 为 200→503→200，healthz 仍 200，结果保持一致 |

实测 UID/GID 10001、root 写入 EROFS、`/tmp` 64 MiB 与 cgroup 资源限制。实验
使用比示例 Compose 更低的容器资源限额；并未运行整套 mTLS Compose 部署或已有
LB。耗时 39.57 秒，61 个采样点，宿主所属 RSS 加容器 cgroup 观测峰值 596.2 MiB，
最低宿主可用内存 40%、VM 可用内存 87.44%。10 个所属容器、2 个宿主 Temporal
进程和驱动均已清理；末尾四个业务容器由清理流程 SIGKILL、退出 137，不能宣称
全部优雅 drain。六个测试凭据片段扫描 4 份 History/12 份日志，匹配为零；此结论
只覆盖这些测试值。

首轮失败 `74a3ec4f9711414a8d41b1ad7d463d4c` 保留为故障证据：旧镜像空闲 Worker
收到 SIGTERM 后未唤醒事件循环，15 秒 watchdog 退出 70，主任务故障阶段未执行。
修复后的最小 Linux selector 验证无周期 timer，实际 SIGTERM 后 0.318611 秒退出 0；
随后上表默认生产 Worker 和完整容器故障场景重新通过。旧镜像与失败运行不能用于
替代最终镜像验收。

在 `deepagents-server/` 下可重跑以下有界机制验收；镜像与 Temporal CLI 必须已经
存在，脚本不会拉取镜像或下载服务。脚本限定当前 arm64 验收环境，最多 180 秒、
2 GiB 所属 RSS/cgroup 观测预算及 15% 宿主可用内存底线，并清理自身创建的资源。

```sh
.venv/bin/python scripts/task_container_fault_smoke.py \
  --image "${TASK_IMAGE_ID:?set the rebuilt local image ID sha256:<64hex>}" \
  --temporal-binary "${TEMPORAL_BIN:?set the verified Temporal CLI path}" \
  --binary-sha256 33c298d41f6eefebf01a28a3511f444480e939e21d3518cb0c12a4abad467c4b
```

历史 `task_cold_restart_smoke.py` 运行 `c0a4afaaf20c48e5b65fc51b7e9a7580`
**3 组通过**：6 Task/3 owner，2 API/2 单 Activity 槽 Worker。两个运行中任务在
父步骤 24/3 CAN、年龄 45.200/45.097 秒时与四项原生 SCHEDULED 任务一起经历
全栈冷停。四旧业务容器与 Temporal/observer 全部停止，零服务窗口实测 3.189 秒；
全新的 observer/服务/业务容器冷启后，在新 Worker 启动前即可查回全部已接受任务，
四项仍排队，首 Activity 的原生 Started 事件归属于新 Worker 且晚于其启动。

六项最终各 37 个父工具步骤、4 CAN/5 Run、2 GP 子图、2 次跨 Run 原生摘要；
30 份 History 为 0 WorkflowTaskFailed，276 Activity/278 attempts 仅有两个故障重试。
两项门控任务重放后的完整 native 状态与请求 SHA 相同，同 Task/首 Run、旧 cursor
及 owner 边界保持。接受至观察终态 63.764–68.624 秒包含人工门控/排队/停机，不能
用来承诺真实 provider 速度或生产容量。

运行耗时 93.63 秒、142 样本、所属 RSS+cgroup 观测峰值 912.6 MiB，宿主最低空闲
42%、VM 最低可用内存 86.14%，无 OOM；VM PSI 不可用。9 个容器、2 个 Temporal/
2 个 observer 和驱动全部清理；末尾四业务容器为 cleanup SIGKILL/137。10 个测试
凭据片段在 30 History/13 日志中零命中。实验为单机 OrbStack/明文 CLI SQLite 与
确定性模拟模型，没有替代目标部署 HA、mTLS/Cloud ACL 或真实模型调用验收。

当前冷重启入口要求显式提供 `--image sha256:<64hex>` 与该镜像的 `--manifest`，不再
内置历史镜像或构建目录。先完成上面的当前源码构建，再运行下列命令。脚本核对
manifest 中的包源码全集、`pyproject.toml`、`uv.lock` 和 `Dockerfile.task`，并用预算内
额外 64 MiB 的短命容器检查镜像内 build-manifest 摘要及实际安装源码。
`image_source_boundary` 与 `image_manifest_probe` 保存本次核对结果；任何不一致都拒绝
执行恢复实验。脚本不会自动构建或拉取镜像，不能用历史 image ID 代表当前源码。

```sh
.venv/bin/python scripts/task_cold_restart_smoke.py \
  --image "${TASK_IMAGE_ID:?set the rebuilt local image ID sha256:<64hex>}" \
  --manifest "${TASK_BUILD_DIR:?set the matching build directory}/context/manifest.json" \
  --temporal-binary "${TEMPORAL_BIN:?set the verified Temporal CLI path}" \
  --binary-sha256 33c298d41f6eefebf01a28a3511f444480e939e21d3518cb0c12a4abad467c4b
```

新原始回执在忽略的 `var/task-cold-prototype/<run_id>/receipt.json`；已有脱敏
[冷重启证据](docs/task-cold-restart-evidence.json) 与 [验收矩阵](docs/TASK_ACCEPTANCE.md)
保留历史运行的对应范围，新运行不覆写旧证据。容器场景原始回执位于
`var/task-container-prototype/<run_id>/receipt.json`。
最终门禁与推送记录见 [DELIVERY.md](DELIVERY.md)；同一实验环境的持久 SQLite 文件
保留，不证明磁盘丢失恢复、数据库主备、跨故障域可用性或 RPO/RTO SLO。

[历史冷重启图](../docs/audits/task-server-cold-restart-2026-10-16.png) 标记该组合验收的范围
与恢复路径；[本轮结构图](../docs/audits/task-server-structure-2026-10-16.png) 标明源码分工。
验收场景、公共设施及离线 Worker 分别位于 `tooling/acceptance/scenarios/`、`support/`、
`fixtures/`，通过原有脚本入口运行，且不打入生产 wheel。完整 Task/CAN 协议见
[Task Server](TASK_SERVER.md)，规范拓扑以 [根 README](../README.md#architecture) 为准。
