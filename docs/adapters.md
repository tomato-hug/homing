# 自定义 agent 接入（适配器）

三层，互不依赖：

| 层 | 你要做的 | 适合 |
|---|---|---|
| **A 事件上报** | 让你那个 agent 在跑完/要输入时 POST 一次 `/api/ingest` | 能改代码或能挂 hook 的 agent |
| **B 清单** | 写一个 YAML 描述「会话在哪、什么格式、怎么回复」 | 会话是 JSONL / JSON / SQLite 的 agent（多数情况） |
| **C 脚本** | 写一个 `.mjs`，导出与内置 scanner 相同的接口 | 格式特别、需要自己解析的 agent |

---

## A. 事件上报：`POST /api/ingest`

### 发令牌

```sh
curl -sX POST http://127.0.0.1:18787/api/ingest/tokens \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"agent":"my-agent","reply_url":"http://127.0.0.1:9001/homing-reply"}'
# → {"ok":true,"agent":"my-agent","token":"hmg_…"}
```

- 令牌存 `relay/data/ingest-tokens.json`（0600），可吊销：`DELETE /api/ingest/tokens` body `{"agent":"my-agent"}`。
- 给了 `reply_url` 的 agent，手机端回复会 POST 到该 URL：`{ agent, session_id, text }`。

### 上报

```sh
curl -sX POST http://127.0.0.1:18787/api/ingest \
  -H "Authorization: Bearer hmg_…" -H 'Content-Type: application/json' \
  -d '{"agent":"my-agent","session_id":"s-1","title":"部署检查","cwd":"/tmp/demo",
       "kind":"needs_reply","text":"要现在发布吗？"}'
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `agent` | 是 | 必须与令牌登记的一致 |
| `session_id` | 是 | 该 agent 自己的会话 id |
| `kind` | 否 | `needs_reply` \| `approval` \| `done` \| `error` \| `message`，默认 `message` |
| `title` / `cwd` / `text` | 否 | 标题、工作目录、正文（会在收件箱里显示摘要） |

**只监听回环**：请求必须来自 `127.0.0.1`（以及 `::1`），其他来源直接 403。

`kind` 与通知分级的对应：`approval`/`error` → P0 立即；`needs_reply` → P1 120s 合并；`done` → P2 摘要。

---

## B. 清单：`~/.homing/agents/*.yaml`

Windows 读 `%APPDATA%\Homing\agents\*.yaml`。清单在加载时被转换成与内置 agent **完全相同**的 Scanner，
所以会话列表、详情、收件箱都不需要为它写特例。

```yaml
id: codebuddy                 # 必填，[a-z0-9._-]，≤64
name: CodeBuddy Code
icon: codebuddy               # 单色 SVG 名（内置图标名或 generic）

detect:
  bin: [codebuddy, cbc]       # 探测可执行文件
  paths: [~/.codebuddy]       # 探测目录/文件

sessions:
  source: jsonl               # jsonl | json | sqlite | http
  glob: ~/.codebuddy/projects/*/*.jsonl
  format: claude-code         # claude-code | codex | gemini-cli | generic-jsonl | custom
  # sqlite 时用：db + query（列名取 session_id / title / cwd / model / last_ts）
  # http 时用：url

push: poll                    # watch | poll | hook

reply:                        # 不给这一段 = 不支持回复
  command: [codebuddy, --resume, "{session_id}", "{text}"]
  cwd: "{cwd}"
  timeout_ms: 30000

models:
  file: ~/.codebuddy/settings.json
```

### 格式预设

参照 magpie「少数格式覆盖大量 agent」的做法：

| 预设 | 期望结构 | 标题来源 |
|---|---|---|
| `claude-code` | JSONL，`type`/`role` + `content`（兼容 `type:"message"` + `role` 的变体） | `custom-title` → `ai-title` → `summary` → 首条用户消息 |
| `codex` | JSONL，首行 `session_meta.payload{id,cwd,git.branch}`，正文 `response_item/message` | 首条用户消息 |
| `gemini-cli` | 一个会话一个 JSON：`{sessionId, messages:[{type,content}]}` | 首条用户消息 |
| `generic-jsonl` / `custom` | 一行一条 JSON，字段名由 `format_options` 指定 | 映射里的 `title`，否则首条用户消息 |

`format_options` 的键：`session_id`、`cwd`、`time`、`title`、`role`、`text`、`model`、
`user_values`（哪些 role 值算 user）、`assistant_values`。

### 回复命令的安全约定

- `command` 是**参数数组**，用 `spawn(cmd, args, { shell: false })` 执行，**永远不经过 shell**。
- 占位符 `{session_id}` / `{text}` / `{cwd}` / `{title}`：写成**整个参数**时替换成单个参数（不会被空格或引号拆开）；
  写在参数中间时只做文本替换，仍然不经过 shell。
- `cwd` 若不存在则回落到 relay 进程的当前目录。

### 校验与试读

`POST /api/adapters` 保存前先校验（id 格式、source、format 名、reply 形状），不通过返回 400 与逐条错误。
`POST /api/adapters/test` 用**同一套加载逻辑**跑一次：返回 detect 结果、找到的会话数、最新几条的标题（截断 80 字）、模型数、是否支持回复。

清单保存前会备份旧文件（`<id>.yaml.bak-<时间戳>`）；删除是改名留痕（`<id>.yaml.deleted-<时间戳>`），不真删。

---

## C. 脚本适配器：`~/.homing/adapters/*.mjs`

导出与内置 scanner 相同的接口：

```js
export const meta = { reads: ["~/.myagent/sessions"] };   // 它要读的目录（用于放行）
export const id = "my-agent";
export const name = "My Agent";
export const icon = "generic";
export const pushSignal = "poll";
export const capabilities = { reply: false, sessions: true };

export async function detect() { return { installed: true, version: "1.2.3" }; }
export async function listSessions({ limit }) { return [{ session_id, title, last_ts, cwd }]; }
export async function readItems(sessionId, { limit }) { return [{ role, text, at }]; }
export async function reply(sessionId, text, opts) { /* 或直接不导出 */ }
export async function models() { return { models: [], defaultModel: null }; }
```

运行方式：每条调用都在**独立子进程**里跑

```
node --permission --allow-fs-read=<适配器文件> relay/adapters/script-runner.mjs <适配器> <payloadJSON>
```

- 权限模型只放行声明过的读路径（`meta.reads` 里的目录 + 适配器自身）。
- **网络**：Node 22 的 `--permission` 目前**不管网络**。本轮用环境变量 `HOMING_ADAPTER_NO_NET=1` 作为约定，
  脚本自己遵守；等 Node 权限模型覆盖网络后再改成强制。
- 超时 15s（reply 60s），输出只取 stdout 的 JSON，stderr 作为错误信息。

---

## 接口一览

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/adapters` | 列出内置 + 清单 + 脚本，含 `status`（`builtin`/`ok`/`error`）与 `error` |
| `POST` | `/api/adapters` | 保存清单（body `{yaml}`），先校验、备份后写入 |
| `POST` | `/api/adapters/test` | 试读：`{yaml}` → detect + 会话数 + 最新标题 + 模型数 |
| `DELETE` | `/api/adapters/<id>` | 改名留痕地删除清单或脚本 |
| `POST` | `/api/ingest` | 事件上报（Bearer 令牌） |
| `POST` | `/api/ingest/tokens` | 发令牌（body `{agent, reply_url?}`） |
| `DELETE` | `/api/ingest/tokens` | 吊销（body `{agent}` 或 `{token}`） |

接线方式（等 `relay/server.mjs` 空出来再做）：

```js
import { registerAdapterRoutes, loadCustomScanners } from "./adapters/index.mjs";

const custom = await loadCustomScanners({ dataDir, emit: (e) => addEvent(e), log });
for (const sc of custom.scanners) scanners.set(sc.id, sc);
registerAdapterRoutes(app, custom.runtime);
```

`registerAdapterRoutes` 兼容三种 `app` 形态：有 `addRoute(method, path, handler)` 的注册器、数组、任意对象（挂到 `__homingRoutes`）。
另有 `dispatchAdapterRoute(runtime, { method, pathname, body, token, fromLoopback })` 可直接在原生 `http.createServer` 的分发链里用。

---

## Go 版（internal/adapter）

同一套清单与目录，Go 实现逐项对齐 Node：

| 项 | Node | Go |
|---|---|---|
| 清单目录 | `~/.homing/agents` | 同 |
| 脚本目录 | `~/.homing/adapters` | 同 |
| 令牌文件 | `data/ingest-tokens.json` | 同（字段一致，可直接搬家） |
| YAML | 自带最小子集解析 | `gopkg.in/yaml.v3` |
| SQLite | `node:sqlite` | `modernc.org/sqlite`（纯 Go，CGO_ENABLED=0 也能编） |
| zstd | `zlib.zstdDecompressSync` | `klauspost/compress/zstd` |
| 脚本运行 | 子进程 + `--permission` | 同（runner.mjs 用 `go:embed` 打进二进制） |

脚本适配器的调用协议改成**标准输入输出一行 JSON**：

```
$ node --permission --allow-fs-read=/path/adapter.mjs /tmp/adapter-runner.mjs /path/adapter.mjs
{"op":"listSessions","args":{"limit":5}}      ← stdin
{"sessions":[...]}                             ← stdout
```

- 运行时先找 `node`，没有就找 `bun`；两个都没有时，该适配器在 `GET /api/adapters` 里报 `status: "error"`，
  原因写「没有找到 node 或 bun」，而不是安静地返回空列表。
- 网络仍然只是约定（`HOMING_ADAPTER_NO_NET=1`）：Node 的 `--permission` 不管网络，Go 侧沿用同一条约定并在文档写明。

### `db_glob`：逐会话分库

`antigravity` 这类 agent 每个会话一个 `.db`。清单可以这样写，Go 版已支持：

```yaml
sessions:
  source: sqlite
  db_glob: ~/.gemini/antigravity/conversations/*.db
  query: select session_id, title, cwd, model, last_ts from ... limit 1
```

每个库跑一次 `query`，会话 id 取 `session_id`/`id` 列，取不到就用文件名（去掉 `.db`）。
**尚未为 antigravity 写现成清单**：它的表结构没有样本可核对，按约定不猜；等装过并跑过一次会话再补。

### 与 relay 的关系

`relay/` 下的 Node 版保持不动，也不再接进 `relay/server.mjs`。Go 版通过 `internal/adapter` 的 `init()`
自己 `agent.RegisterProvider` 与 `api.Register`，不需要改 `internal/api`、`internal/agent` 或 `cmd/homing`。
