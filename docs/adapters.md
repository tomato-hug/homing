# 自定义 agent 接入（适配器）

三层，互不依赖；仓库里的可复制模板在 [`integrations/`](../integrations/README.md)：

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
- 签发和吊销令牌需要控制台 cookie 或本机 Bearer token；`/api/ingest` 本身只认它签发的 `hmg_…` 令牌。
- 给了 `reply_url` 的 agent，手机端回复会由 Go 服务 POST 到该 URL：`{ agent, session_id, text }`，默认超时 10s。
  回调返回 2xx 记为 `injected`，失败会把具体状态写回回复记录；没登记 URL 的 agent 仍是只读上报。

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

### 通用钩子：`POST /hook/generic`

能执行命令、但不想管令牌的 agent 可以直接调用本机通用钩子。只接受回环来源，`source` 不再局限于内置三家：

```sh
curl -sX POST http://127.0.0.1:18788/hook/generic \
  -H 'Content-Type: application/json' \
  -d '{"source":"my-agent","session_id":"s-1","kind":"done",
       "title":"构建完成","text":"测试通过","cwd":"/tmp/demo"}'
```

`source` 只允许 1–64 位小写字母、数字、点、下划线、连字符；`kind` 可用
`reply`、`done`、`needs_reply`、`needs_input`、`permission`、`approval`、`error`、`message`。
服务端会统一把 `needs_reply` 记为 `needs_input`、把 `approval` 记为 `permission`，
这样通知分级和会话状态不会因为两套叫法而漏掉。

`integrations/hooks/homing-hook.sh` 和 `.mjs` 会把 stdin 的 JSON 转给这个接口：

- `HOMING_AGENT`（或第一个参数）补 `source`，`HOMING_KIND` 补 `kind`。
- `.mjs` 版还会识别常见的 `hook_event_name`、`thread-id`、`sessionId`、
  `last_assistant_message` 等字段。
- 服务端直接接受 `X-Homing-Agent` / `X-Homing-Kind` 请求头，以及
  `hook_event_name` / `thread-id` / `last-assistant-message` 等别名；未知事件
  默认按 `message` 入箱，不会误判成完成或授权。

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
  format: claude-code         # claude-code | codex | gemini-cli | dsh | openclaw | generic-json | generic-jsonl | custom
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
| `dsh` | zstd 压缩的 JSONL；`user/message` / `assistant/message` 的正文在 `data.message.content` | `session/title` → 首条用户消息；跳过 `runtime-context` / `skill-catalog` |
| `openclaw` | JSONL，首行 session 头，正文在 `message.role` / `message.content` | 首条用户消息；跳过 `thinking` 块 |
| `generic-json` | 单个 JSON 对象、顶层消息数组，或 `{sessionId, messages:[…]}`；字段可用 `format_options` 覆盖 | `title` → 首条用户消息 |
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
| `GET` | `/api/adapters` | 控制台形状：`builtin` / `installed` / `custom` / `community` / `formats` / `root` 等；同时保留 `items` / `dirs` / `counts` 兼容字段 |
| `POST` | `/api/adapters` | 保存清单：`{yaml}` 或控制台物件 `{adapter:{name,source,push,reply}}`，先校验、备份后写入 |
| `POST` | `/api/adapters/test` | 试读：`{yaml}` 走清单加载，`{path,format}` 只检查路径与文件形状 |
| `DELETE` | `/api/adapters/<id>` | 改名留痕地删除清单或脚本 |
| `GET` | `/api/agent-catalog` | Agent 页的统一目录：57 家 agent 的 readiness、本机检测、已接入状态、能力与路径 |
| `POST` | `/api/agent-catalog/<id>/install` | 安装已核实的内嵌清单到 `~/.homing/agents/<id>.yaml`，并打开 Homing 自己的接入开关 |
| `POST` | `/api/agent-catalog/<id>/kit` | 在 `~/.homing/integrations/<id>/` 生成 hook、包装器、README 与未自动加载的 draft 骨架；幂等且不覆盖已有文件 |
| `POST` | `/api/agent-catalog/<id>/quick` | 一键完成该 Agent 可用的最强接入：按需安装清单、生成套件、打开接收入口；不执行第三方安装命令 |
| `DELETE` | `/api/agent-catalog/<id>/kit` | 把接入套件目录改名成 `.deleted-<纳秒时间戳>` 留痕，不递归删除用户文件 |
| `DELETE` | `/api/agent-catalog/<id>` | 把已安装清单改名成 `.deleted-<时间戳>`，并断开 Homing 的接入设置 |
| `POST` | `/api/ingest` | 事件上报（Bearer 令牌） |
| `POST` | `/hook/generic` | 通用钩子（回环来源，任意小写 `source`） |
| `POST` | `/api/ingest/tokens` | 发令牌（body `{agent, reply_url?}`，需本机鉴权） |
| `DELETE` | `/api/ingest/tokens` | 吊销（body `{agent}` 或 `{token}`） |

### Agent catalog（Go）

`internal/adapter/catalog.go` 维护产品侧的 agent 目录，`internal/adapter/catalogdata/*.yaml` 是已核实清单的
**内嵌副本**（测试会逐字节对比 `adapters/*.yaml`，防止两边漂移）。`GET /api/agent-catalog` 把静态目录和
运行时的 scanner / 设置合并，返回每家 agent 的 `readiness`、`detected`、`connected`、`manifest_installed`、
`can_install`、`can_connect`、`can_generate_kit`、`sessions`、`reply`、`models`、`push_signals`、
`hook_support`、`integrations`、`kit_installed`、`catalog_source`、`source_url`、`install_commands` 等字段。

目录共 57 家：内置 6、已核实清单 5、事件/钩子 13、未核实 33。`catalogdata/magpie-agents.json` 是对
Magpie 内置 Agent 表与社区 Agent 目录的固定快照，只含发现元数据；Magpie 的模型路由外挂不会被当作
Homing 的会话扫描器。来源 revision 见 `catalogdata/SOURCES.md`。

`readiness` 只有四档，含义不能混：

| 值 | 含义 | 一键安装 |
|---|---|---|
| `builtin` | 编译进二进制的 scanner | 不需要（开关即可） |
| `adapter` | 已核实格式、内嵌 YAML 的 scanner | 是 |
| `hook` | 没有会话读取，只能通过 `/hook/generic` 或 `/api/ingest` 收事件 | 否 |
| `unverified` | 本机可能检测到，但会话格式/钩子没有核实 | 否 |

安装和移除只写 Homing 自己的目录与设置，**不改任何 agent 的真实配置**：安装写入
`~/.homing/agents/<id>.yaml`（旧文件先备份成 `.bak-<时间戳>`），移除把文件改名成
`<id>.yaml.deleted-<时间戳>`，不会真的删除。控制台 Agent 支援库也从同一份 catalog 生成，避免多个页面
各维护一份名单。Node 中继目前没有这组 catalog 路由；Agent 页的目录读取以 Go 版为准。

### 每家的接入套件

`POST /api/agent-catalog/<id>/kit` 调用 `InstallCatalogKit`，输出目录固定在
`~/.homing/integrations/<id>/`。它生成三类内容：

Agent 页的「快速接入」调用 `POST /api/agent-catalog/<id>/quick`。它只做三件可逆的 Homing
本地操作：安装已核实清单（如果该条目有）、生成每家独立的接入套件、写入 Homing 自己的
`connected` / `push_to_phone` 设置。第三方 Agent 的 `npm` / `curl` 安装命令仍只显示给用户复制，
不会由快速接入隐式执行。

- 事件桥：`hook.sh`、`hook.mjs`；
- 命令包装器：`homing-run.sh`；
- 需要人工核实的插件骨架：`manifest.draft.yaml`、`adapter.draft.mjs`，以及说明文件 `README.md`。

`draft` 文件名与目录都刻意避开自动加载规则：Homing 只加载 `~/.homing/agents/*.yaml` 与
`~/.homing/adapters/*.mjs`，而套件在 `~/.homing/integrations`。因此“生成套件”不会让一个空扫描器
出现在会话页。Aider 额外生成 `aider-notify.sh`，Kilo Code 额外生成 `kilo-plugin.js`；这两份仍是
手工拷贝到对应 Agent 插件/配置位置，生成器不会改第三方配置。

生成器是幂等的：已有文件不覆盖，只补缺失文件。测试会为全部 catalog 条目生成一遍，检查 YAML、JS
语法和模板占位符，并验证空 `detect_paths` 仍能生成合法的空骨架。

Node 版的接线方式（Go 版在 `internal/adapter` 的 `init()` 中已经接好，不需要这段）：

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
**尚未为 antigravity 写现成清单**：它的分库读取能力已经具备，但表结构没有样本可核对，按约定不猜；等装过并跑过一次会话再补。

### 与 relay 的关系

`relay/` 下的 Node 版保持不动，也不再接进 `relay/server.mjs`。Go 版通过 `internal/adapter` 的 `init()`
自己 `agent.RegisterProvider` 与 `api.Register`，不需要改 `internal/api`、`internal/agent` 或 `cmd/homing`。
