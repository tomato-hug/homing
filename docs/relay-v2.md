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

自测只用了 `/tmp` 下的临时会话与一条显式命名的测试 session id（`test-homing-v2-probe`），没有向任何真实 Claude / Codex / WorkBuddy / OpenCode 会话写入内容。

---

## 6. 尚未完成

1. **条目解析与回复注入仍留在 server.mjs**：`extractItems` / `readRecentItems` / `sessionFilePath` / `injectClaudeReply` / `injectCodexReply` 尚未搬进对应 scanner；目前 scanner 只管 `meta` / `listSessions` / `readItems`（仅 opencode、zcode 实现了 `readItems`）。
2. **OpenCode 的 `reply` 未实现**（`capabilities.reply=false`）；ZCode 按约定不做。
3. **OpenCode 的 `opencode serve` HTTP 事件未接**：目前是 60 秒轮询；`push_signal` 已标 `poll`。
4. **opencode / zcode 的 `models` 为空**：`/api/agents` 里这两家的模型列表还没接。
5. **inbox 的 waiting 未跟踪「已处理」**：只按「最新一条事件 + read/muted/archived」判定，历史遗留的 permission 事件会一直算 waiting，直到用 PATCH 置 `read`。
6. **workbuddy 复用 claude 解析器**已接（`claudeScanner.fileMeta`），但仅用于钩子记录路径的场景，主路径仍读 `workbuddySnapshot()`。
