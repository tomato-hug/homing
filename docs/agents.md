# 已装 agent 的接入核实（2026-11-09）

> 范围：本机（macOS）上已安装的六个 agent。逐项只读核实会话位置与格式，能接的写成 `adapters/*.yaml`
> 官方示例清单，不能接的写明原因。全部读取都是只读；没有向任何真实会话写入内容。
> 清单格式见 [`adapters.md`](./adapters.md)。

| agent | 平台 | 会话位置 | 格式 | 推送 | 回复 | 状态 |
|---|---|---|---|---|---|---|
| **CodeBuddy Code** | macOS `~/.codebuddy` | `projects/<slug>/<id>.jsonl` | JSONL，`type:"message"` + `role` + `content`（Claude Code 同族，标题走 `custom-title`/`ai-title`/`summary`） | poll | CLI `codebuddy --resume`（本机未安装 CLI，清单里未启用） | ✅ **已接** — 实测 3 个会话，标题取自首个会话的 AI 标题 |
| **DeepSeek Harness (dsh)** | macOS `~/.dsh` | `sessions/<slug>/session-<id>/session.v4.jsonl.zstd` | **zstd 压缩的 JSONL**；首行是会话头 `{type:"session", id, createdAt, cwd}` | poll | 无（未见注入接口） | ✅ **已接** — 实测 3 个会话，均为空会话（只有 session 头，无正文记录），故标题回落为空 |
| **Gemini CLI** | macOS `~/.gemini` | `tmp/<project>/chats/*.json` | 一个会话一个 JSON：`{ sessionId, messages: [{type, content}] }` | poll | 无 | ⚠️ **清单已备、本机无数据** — `~/.gemini` 存在但没有任何 `tmp/*/chats`（没跑过会话），实测 0 条 |
| **Hermes Agent** | macOS `~/.hermes` | `state.db`（SQLite）→ `sessions` + `messages` 表 | SQL：`sessions(id, source, model, started_at, message_count)`；无 title / cwd 列 | poll | 无 | ⚠️ **清单已备、本机无数据** — `sessions` 与 `messages` 均为 0 行；标题只能回落到 `session_id` |
| **OpenClaw** | macOS `~/.openclaw` | `agents/<agent>/sessions/<uuid>.jsonl` | JSONL，首行 `{type, version, id, timestamp, cwd}`，正文按 generic 映射 | poll | 无 | ✅ **已接** — 实测 3 个会话（目录里 12 个文件，`.reset` / `.deleted` 变体不取） |
| **Antigravity** | macOS `~/.gemini/antigravity` | `conversations/<id>.db`（**每个会话一个 SQLite**）+ `conversation_summaries.db` | SQLite，逐会话分库 | — | 无 | ❌ **暂不能接** — 分库结构，当前清单只支持「单库 + 一条 query」；要接得先给 `sessions.source: sqlite` 增加「目录内每文件一库」的模式 |

## 结论

- **能接且实测有数据**：CodeBuddy（3）、dsh（3，空会话）、OpenClaw（3）。
- **能接但本机没跑过会话**：Gemini CLI、Hermes —— 清单已写好，装上并产生会话后无需改代码即可出现。
- **当前框架接不了**：Antigravity（分库 SQLite）。这属于框架能力缺口，不是它自身无法读取；补齐方式见下。

## 回复（reply）的现状

六个 agent 里没有一个能在本机验证「注入」：

- CodeBuddy 的 `codebuddy --resume <id>` 是可用的 CLI 形态，但**这台机器上没有 `codebuddy` 可执行文件**（`which codebuddy` 为空），所以清单里没有启用 `reply`，避免出现「配置了但必然失败」。
- dsh / Gemini CLI / Hermes / OpenClaw / Antigravity 未发现可用的注入接口（没有 CLI resume、没有 serve、没有 hook）。

按「拿不准就写结论、不猜着实现」的约定，这几家一律 `capabilities.reply = false`。要恢复某一家，只需在它的清单里加一段：

```yaml
reply:
  command: [<cli>, --resume, "{session_id}", "{text}"]   # 参数数组，不走 shell
  cwd: "{cwd}"
```

## Antigravity 要怎么补

框架侧的缺口只有一处：`sessions.source: sqlite` 目前只接受 `db` + `query`（单库）。给它加一个 `db_glob` 模式（目录里每个 `.db` 当一条会话），Antigravity 就能接：

```yaml
sessions:
  source: sqlite
  db_glob: ~/.gemini/antigravity/conversations/*.db
  query: select ... from messages order by timestamp desc limit 1   # 从每个库里取一条做标题/时间
```

这需要动 `relay/adapters/manifest.mjs` 的 `querySessions`，本次没有做（不在本轮要求的六家能接范围内，且需要先确认它的表结构）。
