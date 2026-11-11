# Homing relay v2 — 接口与实现说明

> 本文件描述 `relay/` 在 Homing v2 中的结构、接口与兼容性约定。
> 安卓端 v2 由 Codex 按 `docs/homing-v2/SPEC.md` 重做，relay 侧不假设其内部结构，只保证下列接口稳定。
> 端口仍是 **18787**；8787 属于 ResetWatch，与本项目无关，relay 从不占用。

---

## 1. 架构：scanner 注册表

```
relay/
├── server.mjs                  # 路由 + 注册表 + 事件/注入/附件等运行时能力
├── session-state.mjs           # 会话治理状态（read/pinned/muted/archived）
├── notify.mjs                  # 通知分级（P0/P1/P2）
└── scanners/
    ├── registry.mjs            # Map<id, scanner> + 单色图标
    ├── codex.mjs  claude.mjs  workbuddy.mjs  opencode.mjs  zcode.mjs
    └── lib/
        ├── util.mjs            # 从 v1 server.mjs 原样搬迁的公共工具
        └── opencode-db.mjs     # OpenCode/ZCode 的只读 SQLite 读取
```

**新增一家 agent**：写 `relay/scanners/<id>.mjs` → 在 `registry.mjs` 的 `MODULES` 加一行 → 完事。UI 不需要改。

### scanner 接口

```js
export function init(ctx)                     // 注入 { transcriptPaths, log }（可选）
export const pushSignal                        // "hook" | "http_events" | "poll"
export const capabilities                      // { reply, sessions, ... }
export async function detect()                 // → { installed, version, detail? }
export async function listSessions({ limit })  // → [{ session_id, source, title, title_source, last_ts, native_state, cwd?, model? }]
export function meta(sessionId, cwdHint)       // → { cwd, nativeTitle, titleSource, firstPrompt, branch, model, ... }
export function readItems(sessionId, { limit })// → [{ id, role, kind, seq, at, text, error }]（可选）
export function isEmptySession(sessionId)      // → boolean（可选；没有则回落到文件条目判定）
export async function reply(sessionId, text, opts) // 可选；不导出即视为不支持
export default { id, name, icon, ... }         // 注册用
```

`source` 取值即 scanner 的 `id`，会原样出现在 `/api/sessions` 的 `source` 字段。

---

## 2. 接口

### `GET /api/agents`

在 v1 字段（`source` / `display_name` / `available` / `capabilities` / `models` / `default_model`）之上新增：

| 字段 | 说明 |
|---|---|
| `icon` | 单色 SVG 字符串（16×16，`currentColor`），跟随文字色，不用品牌色 |
| `installed` | 是否检测到该 agent（目录/数据库存在） |
| `version` | 版本号，取不到为 `null` |
| `push_signal` | `hook`（有钩子）/ `http_events`（本地服务推）/ `poll`（轮询） |
| `status` | `connected` / `pending` / `missing` |
| `last_event_at` | 该 source 最近一条事件时间 |

**兼容性**：v1 字段一个不改、不少；0.6.2 的 App 读到的仍是原结构。

### `GET /api/sessions`

| 参数 | 默认 | 说明 |
|---|---|---|
| `limit` | 100（上限 500） | 返回条数 |
| `source` | all | 按 agent 过滤 |
| `status` | any | 按状态过滤 |
| `include_empty` | 0 | **默认不返回空会话**；`1` 才返回 |

每条会话在 v1 字段之上新增：

| 字段 | 类型 | 说明 |
|---|---|---|
| `read` / `pinned` / `muted` / `archived` | bool | 治理状态（存 `relay/data/session-state.json`） |
| `is_temp` | bool | cwd 落在 `/tmp`、`/private/tmp`、`/private/var/folders`、`/var/folders` 下 |
| `is_empty` | bool | 没有任何 user / assistant 条目（只有启动记录或系统消息） |

排序：`pinned` 置顶，其余按 `last_ts` 倒序。

### `PATCH /api/sessions/<session_id>`

```jsonc
// request
{ "read": true, "pinned": true, "muted": false, "archived": false }   // 只写传入的键
// 200
{ "session_id": "...", "source": "codex", "state": { "read": true, "pinned": true, "muted": false, "archived": false }, "session": { ... } }
```

- 只改 relay 的 `data/session-state.json`，**不碰 agent 自己的文件**。
- 原文件不存在时自动创建；写入用 tmp + rename，坏文件自动重建。

### `GET /api/inbox`

```jsonc
{
  "waiting":     [{ "kind": "approval|needs_reply|error", "session": {...}, "event_id": "...", "title": "...", "summary": "...", "at": "...", "pinned": false, "read": false }],
  "running":     [{ ... }],      // 最近 5 分钟还有事件的会话
  "recent_done": [{ ... }],      // 最近 24 小时完成的会话
  "counts":      { "waiting": 0, "running": 0 },
  "generated_at": "2026-11-09T..."
}
```

- `muted` 与 `archived` 的会话**不进 waiting**。
- `kind` 映射：`permission → approval`，`needs_input → needs_reply`，`error → error`。
- 每个会话只按它**最新一条事件**归类，不会重复出现。

### 既有接口（未改动）

`/health`、`/apk`、`/f/:token/*`、`/hook/{claude,workbuddy,codex,generic}`、`/api/events`、`/api/stream`、`/api/read`、`/api/reply`、`/api/replies`、`/api/away`、`/api/upload`、`/api/projects`、`/api/client-log`、`/api/sessions/new`、`/p/:port/*`。

---

## 3. 通知分级（SSE `event: notify`）

原有 `event: message`（原始事件流）**完全不变**，v2 只新增一个事件通道：

| 级别 | 触发 kind | 策略 |
|---|---|---|
| **P0** | `permission` / `error` | 立即单条推送 |
| **P1** | `needs_input` | 同一会话 **120 秒**内合并成一条（取最新内容，`merged`/`count` 标注） |
| **P2** | `reply` / `done` / `stop` | 不单推；**60 秒**窗口内合并成「N 个会话已完成」摘要 |

- `muted` 会话一律不推（包括 P0）。
- 事件体形如 `{ type: "notify", level: "P0", source, session_id, event_id, title, summary, at, merged, count }`。
- 旧 App 用 `EventSource.onmessage`，只处理 `message` 事件，收到 `notify` 会忽略——因此**不会因为新通道而报错**。

---

## 4. 数据来源（只读）

| agent | 位置 | 方式 |
|---|---|---|
| Codex | `~/.codex/session_index.jsonl` + `~/.codex/sessions/**/rollout-*.jsonl` | 读文件（原 v1 逻辑） |
| Claude Code | `~/.claude/projects/*/*.jsonl` | 读文件（原 v1 逻辑） |
| WorkBuddy | `~/.workbuddy/<uid>/sidebar-list-snapshot.json` + 转录 | 读文件（原 v1 逻辑） |
| OpenCode | `~/.local/share/opencode/opencode.db`（`$OPENCODE_DB` 可覆盖） | SQLite **只读**（`readOnly: true`），不写、不 VACUUM、不锁 |
| ZCode | `~/.zcode/cli/db/db.sqlite`（与 OpenCode 同表结构） | 同上 |

OpenCode / ZCode 读取的表：`session_v2`（id/title/directory/model/time_updated/time_archived）、`session_message`（id/type/seq/time_created/data）、`session_v2.parent_id`（子会话）。

**约束**：所有库连接都是只读，关闭即释放；带 WAL 的库在写入过程中可能读到稍旧快照，对「会话列表」够用，但**不能**据此做写回。

---

## 5. 自测记录（2026-11-09）

| 项 | 结果 |
|---|---|
| `node --check`（server.mjs + 7 个 scanner/lib 文件） | 通过 |
| 用 `PORT=18799` 起测试实例 | 启动正常，5 个 agent 探测完成 |
| `/health` | `{"ok":true}` |
| `/api/agents` | codex/claude/workbuddy/opencode = connected，zcode = missing |
| `/api/sessions` | 65 条（claude 27 / codex 17 / workbuddy 10 / opencode 11），治理字段齐全 |
| `/api/inbox` | waiting 6 / running 0 / recent_done 15 |
| `PATCH /api/sessions/<测试 id>` | 写入 `session-state.json` 成功，读回一致；测试条目已删除 |
| `launchctl kickstart -k dev.agentpush.relay` | 新 PID 监听 18787，`/health` 正常 |
| 8787 (ResetWatch) | 未触碰，进程仍在 |
| scanner 单元验证（v2.1） | `opencode`: installed / signal=poll / reply=false / models=4；`zcode`: missing，`detail` 写明查过 12 个路径 |
| `pushSignal` / `capabilities` 是 getter | 无 serve 时报 `poll` 与 `reply=false`；起 serve 后自动变 `http_events` 与 `reply=true`（registry 用 `defineProperty` 保留 getter，不能用对象展开） |
| 重启后 `/api/agents` | codex/claude/workbuddy/opencode = connected，zcode = missing |

自测只用了 `/tmp` 下的临时会话与一条显式命名的测试 session id（`test-homing-v2-probe`），没有向任何真实 Claude / Codex / WorkBuddy / OpenCode 会话写入内容。

---

## 6. 尚未完成

1. **条目解析与回复注入仍部分留在 server.mjs**：`extractItems` / `readRecentItems` / `sessionFilePath` / `injectClaudeReply` / `injectCodexReply` 未搬进 scanner。scanner 已接管 `meta` / `listSessions` / `readItems`（opencode、zcode 有 `readItems`）与 `detect`，注入链路仍在 server。
2. **OpenCode 的 reply 只在 `opencode serve` 运行时可用**（见 §7）。没有 serve 时 `capabilities.reply=false`，注入返回明确的错误而不是静默失败。
3. **opencode / zcode 的 models 还没接到 `/api/agents`**：scanner 已实现 `models()`（见 §9 的接入指引），但 `listAgents()` 里对注册表新增的 agent 仍写死 `models: []`。这一处需要改 `relay/server.mjs` 一行，本次按约定没有动它。
4. **inbox 的 waiting 未跟踪「已处理」**：只按「最新一条事件 + read/muted/archived」判定，历史遗留的 permission 事件会一直算 waiting，直到用 PATCH 置 `read`。
5. **ZCode 无法端到端验证**：这台 Mac 上没有安装（证据见 §8），`listSessions` / `models` 只按同族格式实现，没有真实样本可核对。

---

## 7. OpenCode 回复注入与事件流（逆向结论）

依据：官方文档 <https://docs.opencode.ai/docs/server/> 与 <https://docs.opencode.ai/docs/sdk/>（2026-11 版），以及本机 `~/.config/opencode/service.json` 的存在（说明这台机器上 `opencode serve` 曾经跑过）。

### 7.1 服务形态

| 项 | 值 |
|---|---|
| 启动 | `opencode serve [--port <n>] [--hostname <h>] [--cors <origin>]` |
| 默认监听 | `127.0.0.1:4096` |
| 鉴权 | `OPENCODE_SERVER_PASSWORD` → **HTTP Basic**，用户名默认 `opencode`（`OPENCODE_SERVER_USERNAME` 可改） |
| 规格 | `GET /doc` 提供 OpenAPI 3.1 |
| TUI 自带服务 | 跑 `opencode` 时 TUI 会起一个**随机端口**的服务；只有 `opencode serve` 才是固定 4096 |

本机的 `~/.config/opencode/service.json` 里有一个 `password` 字段——relay 的 scanner 读它作为 Basic 密码，因此不需要用户再配一遍。

### 7.2 往指定会话发消息

| 方式 | 端点 | 特点 |
|---|---|---|
| **异步（采用）** | `POST /session/:id/prompt_async` | body 同下，返回 `204`，不等待模型 |
| 同步 | `POST /session/:id/message` | 会一直等到模型回复完成，可能几分钟 |
| 无回复注入 | 同上 + `body.noReply: true` | 只加上下文，不触发 AI 回复 |

请求体（SDK 示例确认）：

```jsonc
{ "parts": [{ "type": "text", "text": "要发的话" }],
  "model": { "providerID": "deepseek", "modelID": "deepseek-chat" },   // 可选
  "noReply": false }
```

`parts` 的形状由 SDK 文档的 `session.prompt({ body: { parts: [{ type: "text", text: "Hello!" }] } })` 直接给出，不是推测。

**relay 的选择**：走 `prompt_async`。理由是同一条消息如果用 `/message`，HTTP 连接会挂到模型跑完；手机端的投递语义是「放进会话」，不需要等回复。

### 7.3 事件流

| 端点 | 说明 |
|---|---|
| `GET /event` | SSE，首事件 `server.connected`，之后是总线事件 |
| `GET /global/event` | 全局事件流（含跨项目） |
| `GET /session/status` | 所有会话的状态快照（可轮询） |

scanner 的做法（`relay/scanners/opencode.mjs`）：

1. 每 30 秒探测一次 `GET /global/health`（800ms 超时，带 Basic）。
2. 探测到就在后台订阅 `GET /event`，把事件里带 `sessionID` 的、类型命中 `permission` / `idle|completed|finish` / `error` 的，转成 relay 的通用钩子事件 `POST /hook/generic`（该端点只接受回环来源，不需要 token）。
3. 探测不到就断开订阅，`pushSignal` 退回 `poll`。

**未经真实 serve 验证的部分**：事件类型名（`message.part.updated` / `session.idle` / `permission.updated` 之类）来自 opencode 事件总线的命名习惯，本机没有 serve 在跑，无法实测。因此 scanner 用的是**模式匹配**（`/permission/i`、`/idle|completed|finish|done/i`），而不是写死某个类型名——匹配不到就丢弃，不会误推。这一点在真正跑起 serve 后应当复核一次。

### 7.4 为什么不写数据库

`session_pending` 与 `session_inbox` 两张表看起来像「待投递队列」（`type` / `payload` / `delivery` / `enqueued_seq`），但：

- 本机实测两张表都是 **0 行**，没有样本可以确定 `delivery` 的取值语义；
- 写库会破坏「只读」约定，而且 opencode 自己正在用这个库（WAL 模式下外部写入有风险）。

所以 reply 只走 HTTP，不碰库。

---

## 8. ZCode 检测结论：这台 Mac 上没装

### 8.1 证据（2026-11-09 逐个查过）

| 检查项 | 结果 |
|---|---|
| `~/.zcode` / `~/.zcode/cli/db/db.sqlite` | 不存在 |
| `~/Library/Application Support/ZCode`、`~/Library/Application Support/zcode` | 不存在 |
| `~/Library/Preferences/*zcode*` | 无 |
| `~/.vscode/extensions` 里含 zcode / z-ai / zhipu | 无 |
| `brew list --cask` 里含 zcode / z-ai | 无 |
| `mdfind -name ZCode` | 只命中本仓库的 `relay/scanners/zcode.mjs` 与一个无关的测试文件 |
| `find ~ -maxdepth 4 -type d -name ".zcode"` | 无 |
| PATH 里 `zcode` / `z-ai` | 无 |

**结论**：`missing` 是正确状态，不是检测逻辑的 bug。

### 8.2 检测逻辑仍然改强了

原来的实现只认 `~/.zcode/cli/db/db.sqlite` 一个路径。现在 `candidates()` 覆盖 12 个候选：

```
$ZCODE_HOME/{cli/db,db}/db.sqlite · $ZCODE_HOME/db.sqlite · $ZCODE_HOME/cli/db/zcode.db
~/.zcode/…（同上四种）
~/Library/Application Support/ZCode/…（同上四种）
~/Library/Application Support/zcode/…（同上四种）
```

`detect()` 现在会返回 `{ installed, version, detail, db, checked }`，`detail` 直接写明「查过 N 个路径」，这样界面上再显示 missing 时，能一眼看出是「没装」还是「装了但路径没对上」。

按 magpie 的 `internal/sessions/zcode.go` 记录：ZCode 的库在 `~/.zcode/cli/db/db.sqlite`，表结构与 OpenCode 相同（另有 `title_source` 列）；Windows 上即便 app 数据在别处，库仍在 `USERPROFILE` 下。装好之后本 scanner 不需要改代码。

---

## 9. models：scanner 已就绪，接线还差一行

### 9.1 scanner 侧现状

| agent | 来源 | 本机结果 |
|---|---|---|
| `opencode` | 有 serve → `GET /config/providers`；否则 `~/.config/opencode/opencode.json` 的 `provider` + `opencode.db` 的 `kv['models-dev:catalog']` | **4 个**（deepseek 系列），默认 `deepseek/deepseek-chat` |
| `zcode` | 同族格式：`$ZCODE_HOME` / `~/.zcode` / `~/.config/zcode` 下的 config + 库里的目录缓存 | **0 个**（未安装） |

调用方式：`(await scanner.models())` → `{ models: [{ id, provider, model }], defaultModel, source }`。

### 9.2 接到 `/api/agents` 需要的一行

`relay/server.mjs` 的 `probeAgents()` 里，把 models 一并探测并缓存：

```js
// probeAgents() 内，与 detect() 并列
const m = typeof sc.models === "function" ? await sc.models() : { models: [], defaultModel: null };
agentProbe.set(id, { installed: ..., version: ..., models: m.models, default_model: m.defaultModel });
```

然后在 `listAgents()` 里补注册表 agent 的那段，把 `models: []` 换成：

```js
models: (probe.models || []).map((m) => modelEntry(m.id, m.provider, m.id === probe.default_model, [])),
default_model: probe.default_model || null,
```

本次没有改 `relay/server.mjs`（该文件当时已有其他会话未提交的改动），所以 `/api/agents` 里这两家仍是 `models: []`。

---

## 10. Go 新增接口（2026-11-10，Node 无对应）

以下接口只在新 Go 版存在，Node 18787 不会返回它们；客户端对 404 有回退。

### `GET /api/home`

三区注意力首页：`attention[]`（授权 → 等回复 → 错误 → 协作结果 → 完成 → 进展）、`running[]`、`resume[]`（最多 5 条）、`earlier.count`、`counts`、`away`。支持 `since_away`、`approval`、`collab` 和 `unread` 设置，正文见 `docs/homing-v3/SPEC.md` §5.3。

### `GET /api/search`

```jsonc
{
  "query": "登录",
  "scope": "title",
  "hits": [
    {
      "session": { /* /api/sessions 摘要同形 */ },
      "snippet": { "before": "修正", "match": "登录", "after": "按钮" },
      "item_id": "codex-i3"
    }
  ],
  "searched": 40,
  "truncated": false
}
```

`scope=title` 查标题、项目、路径；`scope=content` 再逐会话读最近 300 条正文（最多 30 条命中）。中文按 rune 切片，不会截断 UTF-8。

### 即时事件

`/api/stream?session_id=<id>` 新增：

```text
event: item  data: {"session_id":"…","item":{…agent.Item…}}
event: turn  data: {"session_id":"…","state":"running|idle","ts":"…"}
```

`internal/live` 以 1 秒轮询 `ItemReader` 尾部，首轮只建立水位；条目按 `seq` 单调发布，客户端仍应按 `item.id` 去重。最多同时跟 20 个会话，超过时淘汰最久未活动的跟踪器。

### 安全

- `GET/PATCH /api/devices` 的 `scopes` 为 `read` / `reply` / `drive`；空数组等于全有（兼容旧设备）。
- 缺少 `drive` 时 `/api/compose`、`/api/sessions/new`、`/api/dispatch`、`forward/reply/cancel` 返回 403 `scope_denied`；缺少 `reply` 时 `/api/reply`、`/api/approvals/{id}` 返回 403。
- `/api/compose`、`/api/sessions/new`、`/api/dispatch` 每设备每分鐘 20 次。
- `audit.jsonl` 只允许白名单字段：时间、设备、动作、会话／dispatch id、目标 Agent、目录、授权决定；不写正文。
