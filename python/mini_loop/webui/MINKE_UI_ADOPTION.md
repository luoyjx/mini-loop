# Minke UI 增量与 mini-loop 采用边界

> 源码核验：2026-08-28。只采用「Minke 有、当前 DeepSeek 没有、mini-loop 现有能力可承接」的交集。

## 结论与本轮范围

[判断] 本轮补齐三种操作入口：**全局命令面板、可配置快捷键、会话访问前进/后退**。它们组织已有能力，不增加 Agent 工具、执行权限或后端服务。

[事实] mini-loop 已有新建/切换会话、侧栏搜索、Settings、主题切换，以及 Tasks、Team、Trajectories、Transcript、Cron、Skills、Memory、Improve、Benchmark 等面板。这里的缺口是统一动作检索、键位配置和访问历史导航，不能说这些底层功能都没有 UI。原有边界见 [Web UI 说明](README.md#interaction-and-boundaries)。

## 固定比较版本

| 对象 | 核验版本 | 用途 |
| --- | --- | --- |
| [Minke][m-root] | `c156b73e1e663a46de4e741da03ff0affb0c5476` | 本轮 UI 参考 |
| [Minke 的 DeepSeek 子模块][d-old] | `b150a551b8d465e31e418e1b2eaf5e79bbb7d28e` | 与上一轮设计参考相同；由 `git ls-tree HEAD vendor/deepseek-harness` 核实 |
| [DeepSeek 查询时的当前 HEAD][d-current] | `cd5ef8148158c3a752a658978873241fdf8e2bbc` | 判定「DeepSeek 没有」的实际基准 |

[事实] 比较覆盖两个 DeepSeek 版本的 `packages/` 生产源码，重点检查 client 插件注册、会话 Header、composer 命令、键盘处理、导航、`session-query/session-log-export`。下面的「未见」只针对这些固定版本，不代表永久缺失；不能只搜 `packages/client`，因为导出 UI 位于 `session-query`。

## 可采用的三项

| UI | Minke 源码事实与交互 | DeepSeek 对照 | mini-loop 采用方式 |
| --- | --- | --- | --- |
| 全局命令面板 | [CommandPalette.tsx:21–153][m-palette]：搜索框、分组动作、↑↓ 选择、Enter 执行、Esc 关闭、恢复原焦点；[ActionList.tsx:20–59][m-actions] 显示快捷键和禁用原因。通过 `shell.overlay` 实际注册，见 [install.tsx:110–123][m-install]。 | 两个版本均未见等价全局动作面板。已有 [ui-commands/index.ts:58–76][d-commands] 只挂到 `conversation.input.overlay`；其 [CommandContribution:46–55][d-command-contract] 是会话 slash 命令，不能视为全局面板。 | 搜索和调用现有 UI 动作；无会话等不可用情况展示原因。复用原来的确认、鉴权和 API 流程，不把动作名发给模型代执行。 |
| 可配置快捷键 | [ShortcutSection.tsx:64–250][m-shortcuts]：逐项录制、冲突提示、禁用、恢复默认；[runtime.ts][m-shortcut-runtime] 统一动作注册、分发和配置保存。 | 两个版本均未见等价的动作键位设置页；composer 发送键、局部 Esc 等处理已经存在，不能称为完全没有快捷键。 | 将已有动作纳入一个快捷键登记表；设置在浏览器本地保存。不要移植 Electron IPC，不能把浏览器本地偏好称为服务端持久化。 |
| 会话访问前进/后退 | [session-navigation.ts:5–82][m-history] 记录已选择会话；回退后打开另一个会话会截断前进分支。通过 [install.tsx:124–150、232–274][m-install] 接入会话选择，并在面板和快捷键中开放操作。 | 两个版本均未见等价的会话访问历史栈。已有会话列表、分组、搜索，不等于访问顺序导航。 | 复用现有会话选择；只保存当前页面的访问顺序。失效会话、身份切换、异步选择响应须守住原有边界；不是新增服务端会话历史或 Transcript 存储。 |

## 排除项：避免把既有功能当作增量

| 候选 | 核验结果 | 本轮处理 |
| --- | --- | --- |
| 消息大纲 / 对话导航细轨道 | Minke [ConversationOutline/model.ts:68–89][m-outline] 取已加载的 user / steering 消息，并非完整历史检索。DeepSeek `b150a55` 无对应轨道，但当前版已有 [TurnNavigator.tsx:56–128][d-navigator]，提供轮次标记、悬停/焦点预览和跳转。 | 不属于相对当前 DeepSeek 的缺口，本轮不做。 |
| Session log / 对话导出 | DeepSeek **旧版已含** [HeaderAction.tsx:11–31][d-export-old] 和 [注册代码:32–50][d-export-install-old]，也有 `/export` 命令；当前版仍有。[Minke 原生导出:123–355][m-export] 增加保存位置选择、下载协调和系统文件夹定位。 | 不把通用导出标为 Minke 独有；本轮不迁移原生下载管理。 |
| 独立 Files 工作台 | Minke [FilePreviewPane.tsx:54–182、265–413][m-files] 有源码编辑、未保存标记和 diff。DeepSeek 已有目录选择、文件/产物打开与工具结果展示，但未见同等独立编辑工作台。 | Minke 借助 [host/workspace.ts:171–261][m-host-workspace] 的 Files RPC 或原生 bridge。mini-loop 内部文件工具不能直接当作已获授权的浏览器文件 API，本轮不新增文件后端。 |
| 可交互 PTY 终端 | Minke [TerminalView.tsx:56–312][m-terminal] 将输入和 resize 交给终端 controller；[desktop/main/tabs/terminal.ts:115–202][m-pty] 真正管理 PTY。DeepSeek 的 [TerminalBlock.tsx:146–255][d-terminal] 是命令输出卡片，不是交互终端。 | 不将已有 shell 工具等同 PTY 服务；不新增终端依赖或旁路工具审批的接口。 |
| 内嵌浏览器、Agent Browser、远程接入 | Minke 的 [Tabs 注册:130–686][m-tabs] 按 Files、Terminal、Web、Agent Browser 等 port 的可用性注入；普通浏览器端仍需要 Minke Host，原生 Browser/Agent Browser 与 [remote 设置][m-remote] 依赖其主进程能力。 | 这些是 Minke 新增的后端与宿主能力，不是只换一个 UI 就可承接。本轮不新增 Electron、CDP、远程通道或公网暴露。 |

## Minke 快捷键参考与冲突策略

[事实] 原始默认值来自 [shortcut-contract.ts:9–19][m-defaults]。`Mod` 在 Apple 平台为 Command，其他平台为 Ctrl；这张表描述 Minke，并非 mini-loop 的默认值承诺。

| 动作 | Minke 默认 |
| --- | --- |
| 命令面板 | `Mod+K` |
| Settings | `Mod+,` |
| 新会话 | `Mod+N` |
| 聚焦输入框 | 未分配 |
| 后退 / 前进会话 | `Mod+[` / `Mod+]` |
| 侧栏开关 | `Mod+S` |
| 右侧 / 底部 Tabs | `Mod+P` / `Mod+B` |

- [事实] `setBinding` 和 `resetBinding` 都先检查其他动作的有效键位；冲突则返回 `conflictActionId`，不抢占。若载入的配置已含冲突，分发器仅在命中恰好一个动作时执行。见 [runtime.ts:84–403][m-shortcut-runtime]。
- [事实] 录制时 Esc 取消，无修饰的 Delete / Backspace 禁用；事件归一化忽略 `defaultPrevented`、长按重复、IME、AltGraph 和仅修饰键。见 [ShortcutSection.tsx:64–250][m-shortcuts]、[binding.ts:104–141][m-binding]。
- [事实] Minke 的配置可编辑性依赖原生 shortcut bridge；bridge 缺失时返回不可用，而非假装已保存。见 [desktop/shortcuts.ts:11–44][m-shortcut-store]。
- [建议] mini-loop 在普通浏览器运行，不照搬桌面的新窗口、打印、保存等键位；录制页需提示冲突并提供重置。只借鉴登记/录制/冲突交互，不移植原生菜单。

## mini-loop 已落地的交互

- [事实] 顶栏 Commands 可检索已有动作与已获取的会话；不可用动作解释原因，删除仍经过原确认。界面不会把动作名发送给模型。
- [事实] 默认仅分配 `Ctrl / Command + K` 与 `Ctrl / Command + [` / `]`，后两项明确用于当前页面的会话访问导航，不调用浏览器 URL 历史。其他动作可在 Keyboard shortcuts 中录制，常见浏览器/编辑键及所有带 Ctrl/Command 的 Enter 组合受保护。
- [事实] 快捷键保存在浏览器本地；访问栈只在当前页面内保留，最多 100 条。身份切换或清空当前会话会清栈；新选择让旧导航请求失效，404 与临时失败分别处理。
- [事实] 没有新增依赖、API、执行权限或默认开启的后端能力。操作说明与本轮测试记录见 [Web UI 验收说明](README.md#verification)。

## 验证边界

[事实] 本文是固定版本的源码核验，未运行 Minke 或 DeepSeek、未安装其依赖；截图或 README 宣称没有被当作运行验证。mini-loop 的实现验收由本轮实际测试和浏览器检查另行记录。

[m-root]: https://github.com/lencx/Minke/tree/c156b73e1e663a46de4e741da03ff0affb0c5476
[d-old]: https://github.com/deepseek-ai/deepseek-harness/tree/b150a551b8d465e31e418e1b2eaf5e79bbb7d28e
[d-current]: https://github.com/deepseek-ai/deepseek-harness/tree/cd5ef8148158c3a752a658978873241fdf8e2bbc
[m-palette]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/palette/CommandPalette.tsx#L21-L153
[m-actions]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/palette/ActionList.tsx#L20-L59
[m-install]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/shortcuts/install.tsx#L110-L274
[d-commands]: https://github.com/deepseek-ai/deepseek-harness/blob/cd5ef8148158c3a752a658978873241fdf8e2bbc/packages/client/ui-commands/src/client/index.ts#L58-L76
[d-command-contract]: https://github.com/deepseek-ai/deepseek-harness/blob/cd5ef8148158c3a752a658978873241fdf8e2bbc/packages/client/ui-commands/src/client/contract.ts#L46-L55
[m-shortcuts]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/shortcuts/ShortcutSection.tsx#L64-L250
[m-shortcut-runtime]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/shortcuts/runtime.ts#L84-L403
[m-history]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/shortcuts/session-navigation.ts#L5-L82
[m-outline]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/conversation-outline/model.ts#L68-L89
[d-navigator]: https://github.com/deepseek-ai/deepseek-harness/blob/cd5ef8148158c3a752a658978873241fdf8e2bbc/packages/client/ui-chat/src/client/chat/TurnNavigator.tsx#L56-L128
[d-export-old]: https://github.com/deepseek-ai/deepseek-harness/blob/b150a551b8d465e31e418e1b2eaf5e79bbb7d28e/packages/session-query/session-log-export/src/client/HeaderAction.tsx#L11-L31
[d-export-install-old]: https://github.com/deepseek-ai/deepseek-harness/blob/b150a551b8d465e31e418e1b2eaf5e79bbb7d28e/packages/session-query/session-log-export/src/client/index.ts#L32-L50
[m-export]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/desktop/main/session-export/ipc.ts#L123-L355
[m-files]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/tabs/files/FilePreviewPane.tsx#L54-L413
[m-host-workspace]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/host/workspace.ts#L171-L426
[m-terminal]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/tabs/terminal/TerminalView.tsx#L56-L312
[m-pty]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/desktop/main/tabs/terminal.ts#L115-L202
[d-terminal]: https://github.com/deepseek-ai/deepseek-harness/blob/cd5ef8148158c3a752a658978873241fdf8e2bbc/packages/client/ui-primitives/src/TerminalBlock.tsx#L146-L255
[m-tabs]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/tabs/install.tsx#L130-L686
[m-remote]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/remote/install.tsx#L30-L68
[m-defaults]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/shortcut-contract.ts#L9-L19
[m-binding]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/shortcuts/binding.ts#L104-L141
[m-shortcut-store]: https://github.com/lencx/Minke/blob/c156b73e1e663a46de4e741da03ff0affb0c5476/packages/harness-overlay/src/client/desktop/shortcuts.ts#L11-L44
