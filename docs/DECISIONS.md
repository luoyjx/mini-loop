# Typed decisions：TypeSafe Jev 能力与 mini-loop 接入

## 结论先行

mini-loop 增加一个可选的 `decision` 工具：把调用方明确提供的状态交给决策后端，一次返回多个有类型的问题答案，覆盖分类选择、按等级评分和二元判断。Jev 后端调用 TypeSafe System One；LLM 后端明确标注估计来源。两者只返回判断数据，后续执行仍走已有权限检查。默认以及 `MINILOOP_FEATURES=all` 均不开启，避免无意增加模型调用和成本。

## 目标与证据基线

用户目标是“根据 typesafe ai jev 的能力，添加 decision 决策的能力”。对应的交付是类型化判断服务和工具入口，而非决策日志、目标管理或自动批准器。适用场景包括将工单分派到候选队列、根据明确等级判断故障严重程度，以及判断一段状态是否满足条件。

本轮源码基线为 `c3e0ab9`，上游资料核验于 **2026-09-29**。TypeSafe Python SDK 核验版本为 **0.7.2**、提交 `f078f1e208a0d885154dc758344ae4fce77ac168`；请求/响应对照固定版本的 [models.py](https://github.com/typesafe-ai/typesafe-sdk-python/blob/f078f1e208a0d885154dc758344ae4fce77ac168/src/typesafe_sdk/_schemas/models.py)。官方文档为在线滚动版本，模型别名可能变化；实现保留每次响应返回的实际模型，不把请求别名当作实际版本。

## 上游能力与本地契约

TypeSafe 接口接收 `model`、`state`、`questions`，将答案放回相同问题 ID。状态与问题说明可用字符串、对象或数组；批量问题共享这次请求的状态。Jev 是语义判断模型；需要精确计数、计算或多步推导时，仍应调用相应代码和工具。[API reference](https://docs.typesafe.ai/api)、[Introduction](https://docs.typesafe.ai/introduction)、[Advanced usage](https://docs.typesafe.ai/primitives/advanced)。

| 类型 | 输入含义 | 结果含义 | 本地约束 |
|---|---|---|---|
| `choice` | 候选名称映射到可选描述 | `choice` 为概率最大的候选，附完整 `probabilities` 与 `confidence` | 候选必须来自 criteria，概率有限且覆盖候选集合 |
| `score` | 从低到高排列的描述性等级 | `score` 是从 0 开始的等级索引的概率加权均值，可为小数；保留 `legend`、分布和 confidence | 2–10 个等级，分数须与分布相符 |
| `noul` | 是/否问题，可说明 `true` / `false` 的含义 | `noul` 是 yes 的概率，范围 0–1；不是已经阈值化的布尔值 | 不凭空补充 Jev 未返回的 confidence |

Score 的数值是等级位置，不能把它解释成发生率或精确测量。各等级应有独立、具体的描述；数值相同也可能来自不同分布，因此调用方应结合分布阅读。[Score](https://docs.typesafe.ai/primitives/score)。

官方将 Choice/Score confidence 描述为概率分布的统计量，并未公开精确计算公式。它不等于答案正确率，也不是 Noul 的 yes 概率。Jev 原值保留；LLM 后端使用 `probability_source="llm_estimate"`，其本地 confidence 来自归一化熵，仅用于说明估计分布的集中程度，不能宣称等价于 Jev。[Confidence](https://docs.typesafe.ai/confidence)。

核验时 `jev-latest` 指向 `jev-1.13.0`。采用别名便于获得后续服务版本，固定模型名称便于自己的评测复现；都应以响应 `model` 记录实际服务版本。[Models](https://docs.typesafe.ai/models)、[Jev 1.13 notes](https://docs.typesafe.ai/model-jaggedness/jev-1.13)。

## 启用与调用

部署配置有三种明确状态：

| 设置 | 行为 |
|---|---|
| `MINILOOP_DECISIONS=off` | 默认；不安装 decision 工具 |
| `MINILOOP_DECISIONS=llm` | 使用已有模型进行独立的类型化估计 |
| `MINILOOP_DECISIONS=jev` | 使用 TypeSafe API；须在本地环境设置 `TYPESAFE_API_KEY` |

`MINILOOP_DECISION_MODEL` 选择 Jev 模型，默认 `jev-latest`。不要将密钥放入问题、状态、代码示例或聊天。显式安装的 decision 工具优先于 SessionManager 的配置装配；关闭配置不影响调用方主动安装的工具。

Python 装配示例：

```python
import os
from mini_loop import (
    JevDecisionProvider,
    SessionManager,
    default_registry,
    full_registry,
    install_decisions,
)

# 明确选择已有 LLM；full_registry() 单独调用不会启用决策。
llm_registry = full_registry(decisions=True)

# 明确选择 Jev；不会因为错误而自动换成 LLM。
jev = JevDecisionProvider(
    api_key=os.environ["TYPESAFE_API_KEY"],
    model="jev-latest",
)
registry = default_registry()
install_decisions(registry, provider=jev)
manager = SessionManager(settings, client, tool_registry=registry)
```

`DecisionRequest`、`DecisionResult` 和 `DecisionProvider` 通过包入口导出，供 Python 集成替换后端与进行类型检查。自定义 provider 实现 `async evaluate(request) -> DecisionResult`，必须支持不同会话的并发请求。可信 Python 调用方也可直接使用后端；这种库调用不经过 agent 工具审批，调用方负责自己的授权和数据选择：

```python
from mini_loop import DecisionRequest

request = DecisionRequest(
    state="CSV 导出失效，但 JSON 导出可用。",
    questions={
        "has_workaround": {
            "type": "noul",
            "instructions": "是否存在明确可用的替代方案？",
        },
    },
)
result = await jev.evaluate(request)
print(result.answers["has_workaround"]["noul"])
```

工具调用只需提供显式状态及问题，不隐式抓取会话、文件、用户资源或其他租户的资料：

```json
{
  "state": {
    "ticket": "CSV 导出失效，但用户仍可使用 JSON 导出。",
    "affected": "一个工作区"
  },
  "questions": {
    "queue": {
      "type": "choice",
      "instructions": "哪个队列适合处理这个工单？",
      "criteria": {
        "product_support": "已有功能的使用问题或故障",
        "billing": "账单、支付和退款"
      }
    },
    "severity": {
      "type": "score",
      "instructions": "按实际可用性判断故障严重程度。",
      "criteria": [
        "仅外观问题，功能可正常使用",
        "部分功能不可用，但存在可行替代方案",
        "关键功能不可用，且没有替代方案"
      ]
    },
    "has_workaround": {
      "type": "noul",
      "instructions": "状态是否明确给出了可用的替代方案？"
    }
  }
}
```

调用方依据自己的业务规则使用 `answers`，例如综合队列、严重程度和是否有替代方案。工具本身不分派工单、不运行选中的命令，也不修改权限。阈值应通过自己的样本验证；本轮没有提供通用的准确率或决策阈值保证。

## 运行边界

`decision` 的两个内置后端均声明 external risk，沿用现有审批机制：interactive 模式需要已有审批路径，readonly 模式拒绝，auto 模式沿用已有允许及审计逻辑。结果不构成后续工具的批准，子代理也不会仅因为有读取能力而继承这个工具。

本地请求每批最多 32 个问题、JSON 最多 128 KiB；Choice 接受 1–255 个命名候选，Score 接受 2–10 个描述等级。未知字段、空问题说明、非 JSON 值和非有限数值均被拒绝。这些是本地可执行契约；不是对上游所有宽松输入形式的保证。

Jev 使用固定的 HTTPS System One endpoint、Bearer 认证及有界请求/响应处理，默认总超时 20 秒、最多重试 2 次、响应上限 512 KiB。服务异常应成为明确错误，不能把 malformed 结果、缺失答案、非有限概率或错误候选当作有效判断。429/529 的有界退避与认证/输入错误区分处理。[API errors](https://docs.typesafe.ai/api#errors)。

LLM 后端通过已有模型调用路径获取 usage 与实际服务模型，不把判断的独立请求计入主会话上下文占用的校准锚点。独立请求不授予执行工具的能力，也不把内部生成流当作主助手答复。默认总超时 60 秒、输出预算 4096 tokens；直接构造 `LLMDecisionProvider` 可调整这两个预算。超过输出预算的截断回答会失败，不会拼接半份 JSON 充当决策；批次上限不代表每种模型都能在默认输出预算内完成最大批次。返回值经过本地类型和分布一致性校验；估计值始终带来源标注。

状态只来自工具输入。两个后端通过 `model_start` / `model_end` 的 `purpose="decision"` 进入已有 usage 统计；LLM 已有模型 span 不重复计费统计。`decision_completed` 事件记录完成元数据和 usage，不另放原始 state；完整类型化结果沿用现有 tool-result、事件和 trajectory 边界。配置了 StateStore 才有对应事件持久化，未配置时不能宣称 durable。没有新增决策数据库、会话恢复业务状态或 HTTP API；现有 `/sessions/{session_id}/messages` 入口即可让会话使用工具。

决策失败抛出脱敏异常，使工具结果和 action journal 一致记录 `failed`。合法决策结果使用紧凑 JSON，action journal 为 `decision` 保留最多 524,288 字符，避免普通工具的 4,000 字符截断破坏批量答案。内存日志同时限制保留条数和总结果字符数；回收只移除旧结果内容，保留身份与状态，重复动作不会因此重新调用服务。SQLite 日志可在重新打开后重放完整、未回收的决策结果。

## 验证与未验证边界

本轮分别验证了输入/输出契约、两种后端、工具执行路径和部署装配。远程判定的语义准确率、Jev 实际延迟及计费需要带凭据的真实样本实验；mock 通过不能证明这些指标。

| 项目 | 本轮证据 |
|---|---|
| 上游协议 | 2026-09-29 官方文档 + SDK 0.7.2 固定提交核验 |
| 类型、概率、后端、权限、批处理及重放 | `.venv/bin/python -m pytest -q tests/test_decisions.py tests/test_decision_llm.py tests/test_decision_tools.py tests/test_decision_replay.py`：138 passed |
| 文档可执行示例 | `.venv/bin/python -m pytest -q tests/test_documented_examples.py`：15 passed / 12 skipped（示意与省略参数示例） |
| 仓库完整测试 | `.venv/bin/python -m pytest -q`：2160 passed / 18 skipped，24 subtests passed；另有 3 个依赖弃用提示与 1 个异步子进程清理提示；提示所在 `tests/test_owner_map_bound.py` 单独复查 4 passed、无提示 |
| 不变量与源码扫描 | `.venv/bin/python tools/verify_invariants.py`：77 modules；`.venv/bin/python tools/verify_scans.py`：19 scanning guards anchored；`git diff --check` 通过 |
| Mutation guards | 使用 `.venv/bin/python -u <staged-snapshot>/tools/verify_guards.py`，分别运行 `--from 1 --to 126`、`--from 127 --to 252`、`--from 253`：377/377 caught；无 stale / survived / ambiguous。快照来自 `git checkout-index`，运行时、测试及验证工具与工作区逐文件一致 |
| 架构生成 | Archify showcase 9/9、0 errors、0 warnings；离线 SVG 深浅主题视觉检查通过。浏览器 file URL 策略阻止交互检查，未验证工具栏、聚焦、导出等交互 |
| Research Atlas | `cd research-site && npm run content && npm test`：18 篇文档索引、内容一致性检查、生产构建及 5 项渲染测试通过 |
| 真实 Jev 服务 | 尚未进行带凭据的线上调用；不宣称线上准确率、延迟或费用已验证 |

## Worklog

- **2026-09-29 — 协议与范围核验：** 对照 Jev System One 文档和固定 SDK，确定 choice/score/noul、批量 ID 对应关系、结构化 criteria、模型与概率来源边界。
- **2026-09-29 — 接入设计：** 定义可注入 provider 与显式启用；保留原有权限和事件路径，Jev 失败不切换后端，不新增执行权限或持久化表。
- **2026-09-29 — 导航与架构：** README / EXTENDING 增加入口、配置和边界；交互图从 JSON 再生成，新增 default-off decision provider 分支。
- **2026-09-29 — 集成审查：** 修复失败结果被记录为成功、批量 JSON 在 action 重放时被截断两处边界；补充失败状态、SQLite 重启重放及保留内存上限测试。
- **2026-09-29 — 运行验证：** 全量测试 2160 passed / 18 skipped；决策专项 138 passed。默认关闭，启用方式和真实服务未测边界均记录在本文。
- **2026-09-29 — 交付验证：** 377 个 mutation guards 全部通过，含新增的决策结果总字符预算 guard；77 个模块不变量、19 个扫描 guard、架构生成及研究站点构建通过。
