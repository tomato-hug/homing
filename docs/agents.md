# 已装 agent 的接入核实（2026-11-10）

> 范围：本机（macOS）上已安装的 agent，以及 catalog 里只做事件通知的接入。
> 逐项只读核实会话位置与格式，能接的写成 `adapters/*.yaml` 官方示例清单，不能接的写明原因。
> 全部读取都是只读；没有向任何真实会话写入内容。
> 清单格式见 [`adapters.md`](./adapters.md)。

| agent | 平台 | 会话位置 | 格式 | 推送 | 回复 | 状态 |
|---|---|---|---|---|---|---|
| **CodeBuddy Code** | macOS `~/.codebuddy` | `projects/<slug>/<id>.jsonl` | JSONL，`type:"message"` + `role` + `content`（Claude Code 同族，标题走 `custom-title`/`ai-title`/`summary`） | poll | CLI `codebuddy --resume`（本机未安装 CLI，清单里未启用） | ✅ **已核实** — 实测 3 个会话，标题取自首个会话的 AI 标题 |
| **DeepSeek Harness (dsh)** | macOS `~/.dsh` | `sessions/<slug>/session-<id>/session.v4.jsonl.zstd` | **zstd 压缩的 JSONL**；首行会话头 `{type:"session", id, createdAt, cwd}`，正文嵌套在 `data.message`；`session/title` 带 AI 标题 | poll | 无（未见注入接口） | ✅ **已核实** — 专用 `format: dsh` 解析器实测 7 个会话，跳过 `runtime-context` / `skill-catalog` 注入消息 |
| **Gemini CLI** | macOS `~/.gemini` | `tmp/<project>/chats/*.json` | 一个会话一个 JSON：`{ sessionId, messages: [{type, content}] }` | poll | 无 | ⚠️ **清单已备、本机无数据** — `~/.gemini` 存在但没有任何 `tmp/*/chats`（没跑过会话），实测 0 条 |
| **Hermes Agent** | macOS `~/.hermes` | `state.db`（SQLite）→ `sessions` + `messages` 表 | SQL：`sessions(id, source, model, started_at, message_count)`；无 title / cwd 列 | poll | 无 | ⚠️ **清单已备、本机无数据** — `sessions` 与 `messages` 均为 0 行；标题只能回落到 `session_id` |
| **OpenClaw** | macOS `~/.openclaw` | `agents/<agent>/sessions/<uuid>.jsonl` | JSONL，首行 `{type, version, id, timestamp, cwd}`；正文是 `message.role` / `message.content`，含 `thinking` 块 | poll | 无 | ✅ **已核实** — 专用 `format: openclaw` 解析器实测 12 个会话（`.reset` / `.deleted` 变体不取） |
| **Kimi Code** | macOS `~/.kimi-code`（或 `KIMI_CODE_HOME`） | `sessions/<bucket>/<session-id>/`：`session-meta/state.json` + `agents/main/wire.jsonl` | wire 是 `{timestamp, message:{type,payload}}`；正文取 `TurnBegin.payload.user_input` 和 `ContentPart.payload.text` | poll | 无稳定本地注入接口 | ✅ **内置 scanner 已实现** — 本机未安装 Kimi，所以实测 0 条；`config.toml` 有生命周期钩子，目前没有自动写入 |
| **Antigravity** | macOS `~/.gemini/antigravity` | `conversations/<id>.db`（**每个会话一个 SQLite**）+ `conversation_summaries.db` | SQLite，逐会话分库 | — | 无 | ⚠️ **框架已支持、清单待补** — Go 已有 `sessions.db_glob`，但表结构没有样本可核对，尚未写现成清单 |

## 结论

- **内置 scanner**：Codex、Claude Code、WorkBuddy、Kimi Code、OpenCode、ZCode。
- **能接且实测有数据**：CodeBuddy（3）、dsh（7）、OpenClaw（12）。
- **Node 版与 Go 版读出的数字一致**：`HOMING_REAL_MANIFESTS=../../adapters go test ./internal/adapter/ -run Real -v`
  在两边都是 codebuddy 3 / dsh 7 / gemini-cli 0 / hermes 0 / openclaw 12。
- **能接但本机没跑过会话**：Gemini CLI、Hermes —— 清单已写好，装上并产生会话后无需改代码即可出现。
- **已内置、本机没装**：Kimi Code —— scanner 已实现；装上 `kimi` 并产生会话后直接出现。
- **框架已支持、清单待补**：Antigravity（分库 SQLite）。`db_glob` 已实现；缺的是核实它的表结构和列名。

## 回复（reply）的现状

没有一家能在本机验证「注入」：

- CodeBuddy 的 `codebuddy --resume <id>` 是可用的 CLI 形态，但**这台机器上没有 `codebuddy` 可执行文件**（`which codebuddy` 为空），所以清单里没有启用 `reply`，避免出现「配置了但必然失败」。
- dsh / Gemini CLI / Hermes / OpenClaw / Antigravity / Kimi Code 未发现可用的稳定注入接口（没有 CLI resume、没有 serve、没有可确认的回复通道）。

按「拿不准就写结论、不猜着实现」的约定，这几家一律 `capabilities.reply = false`。要恢复某一家，只需在它的清单里加一段：

```yaml
reply:
  command: [<cli>, --resume, "{session_id}", "{text}"]   # 参数数组，不走 shell
  cwd: "{cwd}"
```

## Antigravity 要怎么补

Go 版框架侧已经补上 `db_glob`（目录里每个 `.db` 当一条会话），缺的是 Antigravity 的表结构核对：

```yaml
sessions:
  source: sqlite
  db_glob: ~/.gemini/antigravity/conversations/*.db
  query: select ... from messages order by timestamp desc limit 1   # 从每个库里取一条做标题/时间
```

Go 版已经支持 `db_glob`；现在只差一个核对过表结构的官方清单。Node 版仍只支持单库，若两边都要使用，需要同步补 `relay/adapters/manifest.mjs` 的 `querySessions`。

## 广泛接入套件

仓库的 [`integrations/`](../integrations/README.md) 提供通用钩子、Codex/Claude 片段、JSON/JSONL 清单模板和脚本适配器骨架。控制台现在还能为目录里的每一家生成独立接入套件，写到
`~/.homing/integrations/<id>/`，包含：

- `hook.mjs` / `hook.sh`：生命周期事件桥；读取 stdin 的 JSON，补 `source`、`session_id`、`kind` 后 POST 到 `/hook/generic`。
- `homing-run.sh`：命令包装器；命令结束发送 `done` 或 `error`，不读取对话正文。
- `manifest.draft.yaml` / `adapter.draft.mjs`：会话插件骨架，刻意不以可加载扩展名放在 `~/.homing/agents` 或 `~/.homing/adapters`，核实数据格式前不会冒充已支持扫描。
- `README.md`：该 agent 的路径、配置、来源与手工接线步骤。
- 原生特化文件：Aider 额外生成 `aider-notify.sh`，Kilo Code 额外生成可直接放进 `~/.config/kilo/plugin/` 的 `kilo-plugin.js`。

页面把「已接入 / 本机已检测 / 快速接入」保持为完整横列，把「更多适配」排成紧凑多栏卡片。
每个尚未接入的条目都有同一个「快速接入」按钮；它调用
`POST /api/agent-catalog/<id>/quick`，只安装 Homing 自己的清单、生成接入套件并打开接收入口，
不会隐式执行 Agent 的第三方安装命令。已生成套件可在详情里移除，目录会改名留痕而不是递归删除。

控制台 Agent 页从 `GET /api/agent-catalog` 读取统一目录，共 **57 家**：

- Homing 内置 scanner 6 家；
- 已核实、可一键安装清单 5 家；
- 可接原生/通用事件的 hook 条目 13 家；
- 只收录元数据、会话格式尚未核实的 33 家。

驱动层和会话读取层分开计算：有 ACP 規格的条目即使只能读会话，也可以在
`/api/agents` 里显示 `new_session`、`stop`、`dispatch`，但前提是本机执行档存在且
自检通过。当前内建 ACP 规格包括 Hermes、OpenClaw、Kimi、DeepSeek Harness、
Gemini CLI、OpenCode、Cline、GitHub Copilot CLI、Kiro CLI、OpenHands、Goose、
Cursor、Qwen Code、Factory Droid、Qoder 与 Trae；
其中 Hermes、OpenClaw、Kimi 标为 verified，DeepSeek Harness 与其余新增条目都标为
experimental。没有安装执行档时只显示「未安装」，不会把目录里的
条目伪装成已可启动。Codex、Claude Code 继续使用已验证的 CLI 启动器，WorkBuddy
因其新会话可能重用活跃对话而仍不开放跨 Agent 派发。

只有 `readiness = builtin | adapter` 的条目会声称「能读会话」。新增的 27 家来自 Magpie 内置 Agent 表与社区 Agent 目录，全部保守标为 `unverified`；它们可以生成事件接入套件或插件骨架，但不会因为 Magpie 能把它接到模型网关，就显示成 Homing 能读会话。

## Magpie 数据的边界

本项目固定保存一份 Magpie 目录快照，来源与 commit 记在
[`internal/adapter/catalogdata/SOURCES.md`](../internal/adapter/catalogdata/SOURCES.md)。只借用以下元数据：

- Agent 名称、厂商、分类、主页、配置路径、探测命令；
- Magpie 已整理的安装命令与能力说明；
- 社区外挂的来源包名和仓库链接。

Magpie 的 `agent-*` 外挂主要工作是**把该 Agent 的模型请求指到 Magpie gateway**，这不等价于 Homing 能读它的本地会话、能收到它的生命周期事件，或能向它注入回复。Homing 的 `hook.mjs`、`homing-run.sh`、`/api/ingest` 与 Android relay 都是独立实现的接入层；README、catalog `notes` 和页面状态必须分别说清楚这三件事。
