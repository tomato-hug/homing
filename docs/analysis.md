# 本机 WorkBuddy / Codex 桌面 App 消息通道逆向分析

- 分析时间：2026-11-08（本机时区 UTC+8）
- 机器：Mac mini（arm64），macOS 26.3.1 (25D771280a)
- 分析方式：只读静态逆向（asar / 打包 mjs / 二进制 strings / SQLite 只读打开）+ 有限的运行时观察。
  唯一的一处写操作是第 1.2 节的临时 Stop hook 测试（已备份、已恢复、前后 sha256 一致）。
- 本报告不包含任何 token / 密钥 / 账号标识 / 完整对话内容；所有敏感值以 `<redacted>` 代替，或只给出长度、前缀。

被测对象：

| 目标 | 路径 | bundle id | 版本 |
|---|---|---|---|
| WorkBuddy | `/Applications/WorkBuddy.app` | `com.tencent.workbuddy.mac` | 5.9.2（Electron 37.10.3 / Chromium 138.0.7204.251） |
| Codex 桌面 App | `/Applications/ChatGPT.app` | `com.openai.codex` | 26.1103.41825（内嵌 codex-cli `0.167.0-alpha.3.2`） |

> 注意：本机 Codex 桌面端的 App bundle 名字叫 **ChatGPT.app**，bundle id 才是 `com.openai.codex`；`/Applications` 下没有 `Codex.app`。
> （`mdfind "kMDItemCFBundleIdentifier == 'com.openai.codex'"` → `/Applications/ChatGPT.app`）

---

## 一、WorkBuddy

### 1.1 桌面端会话记录存在哪里、什么格式

**结论：两层存储。**

1. **会话索引在 SQLite**：`~/.workbuddy/workbuddy.db`（约 8.7 MB，drizzle 迁移）。
2. **真正的对话正文在 JSONL 转录文件**（一行一条记录，不是 LevelDB）：

```
~/.workbuddy/projects/<cwd-slug>/<sessionId>.jsonl
~/.workbuddy/projects/<cwd-slug>/<sessionId>.file-rollback.ndjson
~/.workbuddy/projects/<cwd-slug>/<sessionId>/tool-results/*.txt
```

`<cwd-slug>` = 会话工作目录把 `/` 换成 `-`，例如 `Users-fanqiege-WorkBuddy-2026-11-01-10-59-08`。

证据：

```bash
$ sqlite3 "file:/tmp/wb-ro.db?mode=ro" ".tables"
__workbuddy_drizzle_migrations  migration_meta
automation_delivery_outbox      session_usage
automation_runs                 sessions
automation_runtime_state        workspace_trust_decisions
automations                     workspaces
buddy_snapshots

$ ls ~/.workbuddy/projects/Users-fanqiege-WorkBuddy-2026-11-01-10-59-08
9712bb98-....file-rollback.ndjson
9712bb98-....jsonl            # 811 KB
9712bb98-..../tool-results/   # function_call 的大输出外置
```

`sessions` 表（节选，共 41 列）：`id, cwd, user_id, title, custom_title, status, created_at,
updated_at, last_activity_at, deleted_at, source_mode, mode, model, expert_id, permission_mode,
project_id, session_settings, transport('local'|'cloud'), unread, ...`

> `transport` / `unread` 两列对"检测新回复"有参考价值，但实测本机会话都是 `local`，`unread` 全为 0（桌面端在线时基本不靠它）。

**一条助手消息的字段结构（脱敏）**，取自 270 行的真实转录：

```jsonc
{
  "id": "<uuid-v7>",
  "parentId": "<uuid-v7>",
  "timestamp": 1793525042437,            // 毫秒时间戳
  "type": "message",
  "role": "assistant",
  "status": "completed",
  "sessionId": "<sessionId>",
  "cwd": "/Users/<user>/WorkBuddy/<ts>",
  "content": [
    {
      "type": "output_text",
      "text": "<助手回复正文，前 200 字略>",
      "providerData": { "annotations": [] }
    }
  ],
  "providerData": {
    "conversationRequestId": "<id>",
    "messageId": "<id>",
    "stepSeq": 66,
    "model": "deepseek-v4.1-flash",
    "requestModelId": "deepseek-v4.1-flash",
    "requestModelName": "DeepSeek-V4.1-Flash",
    "traceId": "<id>",
    "agent": "cli",
    "rawUsage": { "prompt_tokens": 109300, "completion_tokens": 1200,
                  "completion_tokens_details": { "reasoning_tokens": 544, ... },
                  "prompt_tokens_details": { "cached_tokens": 109056, ... },
                  "credit": 0.1 },
    "usage": { "requests": 1, "inputTokens": 109300, "outputTokens": 1200 }
  },
  "message": { "usage": { "input_tokens": 109300, "output_tokens": 1200 } },
  "_meta": { "traceparent": "00-<trace>-<span>-01", "baggage": "codebuddy.session_id=<id>;..." }
}
```

同一文件的记录类型分布（`type` 字段）：`message`(49，含 user/assistant)、`reasoning`(50)、
`function_call`(74)、`function_call_result`(73)、`file-history-snapshot`(18)、`session-meta`(5)、`ai-title`(1)。
用户消息的 `content[0].type` 是 `input_text`，且开头常带 `<system-reminder data-role="user-context">` 注入块。

**其它相关目录（都不是正文存储）**

| 路径 | 内容 |
|---|---|
| `~/WorkBuddy/<YYYY-MM-DD-HH-MM-SS>/` | 工作区目录（会话的 cwd），不是会话记录 |
| `~/.workbuddy/app/` | Electron userData（CodeCache / session / window-state.json） |
| `~/.workbuddy/{blobs,artifact-index,changes-index,changes-detail,file-history,traces,audit-log}` | 附件、产物索引、文件改动快照、trace、审计日志 |
| `~/Library/Application Support/WorkBuddy/` | 只有 `pending-telemetry/` |
| `~/Library/Application Support/com.tencent.workbuddy.mac/` | 只有腾讯统计桩文件 |
| `~/.codebuddy/` | **独立 codebuddy CLI** 的配置目录（同格式 `projects/*/*.jsonl`、`settings.json`、`web-ui-port.json`），与桌面端不是同一个 config dir |

不确定点：`~/.codebuddy/projects` 下确有少量转录，说明本机也单独跑过 codebuddy CLI；桌面端运行时用的是
`WORKBUDDY_CONFIG_DIR`（默认 `~/.workbuddy`），两者互不覆盖。

---

### 1.2 桌面端是否内部启动 codebuddy CLI / 是否走 ACP / 是否执行 hooks

#### (a) 会启动 CLI，且是"预热池 + 就地激活"模式

桌面主进程会起一个后台 daemon：

```bash
$ ps -Ao pid,ppid,command | grep app.asar
53905  ...  /Applications/WorkBuddy.app/Contents/MacOS/Electron
53951 53905 /Applications/WorkBuddy.app/Contents/MacOS/Electron --disable-warning=ExperimentalWarning \
            /Applications/WorkBuddy.app/Contents/Resources/app.asar/main/daemon-app-server-entry.js --stdio
```

daemon 再按池大小拉起 CLI 子进程（**argv 完全一致，只有 `--prewarm-id` 不同**）：

```bash
$ ps -o command= -p 54185 | tr ' ' '\n'
/Applications/WorkBuddy.app/Contents/MacOS/Electron
--optimize-for-size
--max-semi-space-size=2
/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy
--prewarm
--prewarm-id
wb-pool-1794136837792-80690b
```

来自 `app.asar/main/cli-prewarm-pool.js` 的原始构造：

```js
let u = [`--optimize-for-size`, `--max-semi-space-size=2`, this.deps.cliPath, `--prewarm`, `--prewarm-id`, o];
let h = spawn(this.deps.execPath, u, { detached:true, stdio: m?['ignore','ignore','pipe','pipe']:[...] });
```

- `deps.execPath` = `/Applications/WorkBuddy.app/Contents/MacOS/Electron`（配 `ELECTRON_RUN_AS_NODE=1`，
  即"拿 Electron 当 node 跑"）。
- `stdio[3]`（fd 3）是预热 IPC socket：实测为 `/tmp/codebuddy-prewarm-wb-pool-<id>.sock`。
- CLI 进程环境由 `app.asar/main/cli-process-env.js` 固定注入：
  `ELECTRON_RUN_AS_NODE=1`、`CODEBUDDY_DISABLE_IDE=1`、`SERVER__PORT=0`、`SERVER__HOST=127.0.0.1`、
  `DISABLE_TELEMETRY=0`，并剔除 `CODEBUDDY_*`/`ELECTRON_*`/代理类变量后重新拼装。
- **激活时不改 argv**：daemon 通过预热 socket 下发 `activate`，对齐
  `buildPrewarmActivateArgs(...)` → `[..., '--port', String(port)]`，进程就地开始 `LISTEN`。

实测（就是上面那个 prewarm 进程，被激活后）：

```bash
$ lsof -nP -p 55742 | grep -E 'LISTEN|TCP 127.0.0.1'
TCP 127.0.0.1:58347 (LISTEN)                     # ← 该会话的 ACP HTTP 端口
TCP 127.0.0.1:58347->127.0.0.1:58348 (ESTABLISHED)  # daemon 已连上
unix /var/folders/.../T/cbb-vwT7zi/broker.sock
```

会话结束后该进程被回收/复位，端口随之消失——**端口是会话级的、临时的**。

#### (b) 走 ACP（HTTP + SSE）

`app.asar/main/acp.js` 里是完整的 ACP 协议表（`initialize`、`session/new`、`session/load`、
`session/prompt`、`session/cancel`、`session/update`…）。CLI 侧实现是

```js
let R = new URL("/api/v1/acp", s).toString();   // s = http://127.0.0.1:<port>
await this.prewarmServer?.markActive({ cwd: process.cwd(), endpoint: R });
```

第三方也能直接握手成功（无鉴权，见 1.3）。

#### (c) hooks：**会执行**，实测通过

CLI 打包体（`.../cli/dist/codebuddy-lite-wb.mjs`，9.9 MB）支持的事件名：

```
PreToolUse  PostToolUse  PostToolUseFailure  PermissionRequest  UserPromptSubmit
SessionStart  SessionEnd  Stop  SubagentStart  SubagentStop  Notification
PreCompact  WorktreeRemove ...
```

支持的 hook 类型（`executeLocalHook`）：

```js
"prompt"===t.type || "agent"===t.type ? executePromptHook(...)
: "http"===t.type ? executeHttpHook(...)
: executeCommandHook(...)
```

即 **`command` / `prompt` / `agent` / `http` 四种**（`http` 类型对中继服务最省事）。

hook 配置读取路径：

```js
async getHooks(t){ ... let s = await this.settingsManager.get("hooks"); if(s){ let i = s[t]; i && n.push(...i) } ... }
```

`settingsManager` 的用户层文件就是 `getWorkbuddySettingsPath()` = `$WORKBUDDY_CONFIG_DIR/settings.json`
（默认 `~/.workbuddy/settings.json`），另加 `settings.local.json`、项目层、插件层。

**工作区信任门禁（重要）**：桌面端把 `settingSources` 传给 CLI，若工作区被判为受限则只保留 `user`：

```js
resolveWorkspaceTrustConfiguration(a, {deniedTools:i, settingSources:['user','project','local']})
// restricted → settingSources.filter(e => e === 'user')，且 deniedTools 追加 'Bash'
```

`~/.workbuddy/workbuddy.db` 的 `workspace_trust_decisions` 目前只有 2 条 `trusted` 记录
（`.../WorkBuddy/2026-10-02-21-06-06`、`.../WorkBuddy/2026-10-03-18-21-44`）。
结论：**用户级 `~/.workbuddy/settings.json` 的 hooks 始终生效；项目级 `.codebuddy/settings.json`
里的 hooks 只有工作区被信任（或该目录没有 `.codebuddy` 触发目录）时才会加载。**

#### (d) 我做的实测（可复现，已还原）

1. 备份：

```bash
cp -p ~/.workbuddy/settings.json /tmp/wb-hook-backup-<ts>/settings.json
shasum -a 256 ~/.workbuddy/settings.json
# bb5cd1975514f6182bcf50c5c0b809314293ad09835b2260b38dc3d320b764cb
```

2. 备份时 App **没有在运行**（`pgrep -fl WorkBuddy` 为空）；因为 hook 要读用户级 settings，
   先退出 App，再写入一个 `hooks.Stop`：

```jsonc
"hooks": { "Stop": [ { "hooks": [ {
  "type": "command",
  "command": "echo \"$(date -u +%Y-%m-%dT%H:%M:%SZ) wb-stop-hook session=$CODEBUDDY_SESSION_ID\" >> /tmp/wb-hook-test.log",
  "timeout": 10
} ] } ] }
```

3. 启动 App 并发一条消息。因为本机给 `osascript` 的辅助功能（Accessibility）未授权、
   `screencapture` 也被拒，GUI 输入改用 Electron 的 CDP 通道完成（不改 App 任何文件）：

```bash
env -u ELECTRON_RUN_AS_NODE /Applications/WorkBuddy.app/Contents/MacOS/Electron --remote-debugging-port=9333
curl -s http://127.0.0.1:9333/json/list          # 只有一个 page target
# 用 CDP Runtime.evaluate 聚焦 composer（div[contenteditable]），
# Input.insertText 写入文本，Input.dispatchKeyEvent 发 Enter
```

4. 结果（`/tmp/wb-hook-test.log`，保留作为证据）：

```
2026-11-08T11:23:12Z wb-stop-hook session=08fdb08a-4537-4e23-9481-a64e74c72cf5
2026-11-08T11:24:19Z wb-stop-hook session=08fdb08a-4537-4e23-9481-a64e74c72cf5
2026-11-08T11:26:06Z wb-stop-hook session=01a1bae8-68a5-71e4-b9d6-4410baf3addb
```

第 1、2 行 = 桌面 GUI 发的两条消息的回合结束；第 3 行 = 我通过 ACP 新建的会话（见 1.3）跑完一轮。
**结论：桌面端运行的会话确实会触发 Stop hook。**

5. 恢复：

```bash
cp -p /tmp/wb-hook-backup-<ts>/settings.json ~/.workbuddy/settings.json
shasum -a 256 ~/.workbuddy/settings.json
# bb5cd1975514f6182bcf50c5c0b809314293ad09835b2260b38dc3d320b764cb   ← 与备份前完全一致
# 校验 "hooks" in settings == False
```

Stop hook 的 stdin 字段（来自打包体 `executeStopHook` 的构造代码，未抓样本）：

```js
{ session_id, transcript_path: this.getHookTranscriptPath(t), cwd: this.hookHost.getWorkDir(),
  hook_event_name: "Stop", stop_hook_active: ..., is_truncation_recovery: ...,
  ...buildAgentTrigger(t), ...last_assistant_message ? { last_assistant_message } : {} }
```

→ **`transcript_path` + `last_assistant_message` 都在里面**，中继服务拿这两个字段就够推送了，不必再解析文件。

---

### 1.3 运行时端口 / Unix socket / 可用的订阅与注入接口

实测一次完整运行（App + daemon）时的监听面：

```bash
$ lsof -nP -iTCP -sTCP:LISTEN | grep Electron
Electron 53905  127.0.0.1:18488 (LISTEN)   # 主进程本地 HTTP（404 {"ok":false,"error":"Not Found"}）
Electron 53951  127.0.0.1:57454 (LISTEN)   # daemon：空 404
Electron 53951  127.0.0.1:57455 (LISTEN)   # daemon：{"error":"not_found"}
node     54003  127.0.0.1:57502 (LISTEN)   # daemon 里的 node 侧：Not Found
Electron 55742  127.0.0.1:58347 (LISTEN)   # ★ 会话级 ACP HTTP（SSE）
```

（除 ACP 那个端口外，其余都是每次启动随机分配的，不能写死。）

Unix socket：

| socket | 用途 | 第三方可用性 |
|---|---|---|
| `/tmp/codebuddy-prewarm-<prewarmId>.sock` | daemon ↔ 预热 CLI 的控制通道（activate / kill / 健康探测） | 私有协议，需 prewarmId，未逆向出完整帧格式 |
| `$TMPDIR/cbb-XXXX/broker.sock` | 会话运行时的内部 broker | 私有 |
| `/tmp/workbuddy-sandbox-center-<hash>-<pid>.sock` | 沙箱中心（sandbox-center 5.9.4） | 私有 |
| `~/.workbuddy/wbipc/endpoint.json` → `$TMPDIR/wb-2c9a62/<hash>/wbipc/b-<hex>.sock` | wbipc 管道 broker | **需要 ticket**（见下） |

wbipc 发现文件（脱敏）：

```json
{"endpoint":"/var/folders/.../wbipc/b-<hex>.sock","ticket":"<redacted, 43 chars>"}
```

打包体里 wbipc 的错误码表说明它是有鉴权、有信任校验的：

```
E_TICKET_INVALID  E_ENDPOINT_UNTRUSTED  E_PROTOCOL_MISMATCH  E_PIPE_UNKNOWN
E_METHOD_UNKNOWN  E_CONSENT_REQUIRED   E_POLICY_DENIED ...
方法：broker/GetPipe、broker/ListPipes
端点文件权限被强制 0600，目录强制 0700，且有 uid 校验
```

→ wbipc **不适合**当第三方入口。

#### ACP 端口的鉴权（关键发现）

`codebuddy-lite-wb.mjs` 的 `AcpSecurity` 中间件：

```js
checkAuth(t, i){ if(!this.auth?.isEnabled() || isLoopbackAddress(i)) return true; ... }
checkSessionToken(t, i){ if(isLoopbackAddress(i)) return true; ... }
```

**回环地址直接放行**——任何本机进程都能无 token 访问 `/api/v1/acp`。

实测（会话 ACP 端口 58347）：

```bash
$ curl -s -X POST http://127.0.0.1:58347/api/v1/acp/connect -H 'content-type: application/json' \
       -d '{"clientInfo":{"name":"probe","version":"0.1"}}'
{"connectionId":"01a1bae6-...","sessionToken":"<redacted>"}

# initialize（SSE）
$ curl -s -X POST http://127.0.0.1:58347/api/v1/acp \
    -H 'content-type: application/json' -H 'accept: application/json, text/event-stream' \
    -H "acp-connection-id: 01a1bae6-..." \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"probe","version":"0.1"}}}'
:ok
event: message
data: {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{
  "promptCapabilities":{"image":true,"embeddedContext":true},
  "mcpCapabilities":{"http":true,"sse":true},"loadSession":true,...},
  "authMethods":[{"id":"iOA"},{"id":"external"},{"id":"internal"},{"id":"selfhosted"}]}}

# 想接管"桌面端正在用的那个会话"
$ ... -d '{"jsonrpc":"2.0","id":11,"method":"session/load","params":{"sessionId":"08fdb08a-...","cwd":"...","mcpServers":[]}}'
{"jsonrpc":"2.0","id":11,"error":{"code":-32000,
 "message":"Persistent Session already has an external writer","data":{"reason":"writer_occupied"}}}

# 新建会话 —— 成功，并持续收到 session/update 事件流
$ ... -d '{"jsonrpc":"2.0","id":12,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}'
event: message
data: {"jsonrpc":"2.0","method":"session/update","params":{
  "sessionId":"01a1bae8-...","update":{"sessionUpdate":"config_option_update",...}}}

# 对新会话发用户消息 —— 成功，跑完一轮，Stop hook 触发
$ ... -d '{"jsonrpc":"2.0","id":21,"method":"session/prompt","params":{"sessionId":"01a1bae8-...",
        "prompt":[{"type":"text","text":"只回复两个字母：ok"}]}}'
→ 一路 session/update（session_info_update / agent 流式消息）→ 结束
→ /tmp/wb-hook-test.log 出现 11:26:06Z 那一行
```

**失败的一步**：对桌面端自己持有的会话直接 `session/prompt`，服务端返回 HTTP 200 + `:ok` 后
**没有任何事件**（悬挂）——与 `writer_occupied` 一致：一个持久会话只允许一个 writer。

---

### 1.4 结论：检测"新助手回复"的最佳方式 / 外部发消息是否可行

**检测新回复（推荐优先级）**

1. **Stop hook（首选，已实测通过）**
   `~/.workbuddy/settings.json` + `hooks.Stop`，`type` 用 `http` 直接把 stdin 的 JSON POST 到中继
   `http://127.0.0.1:8787/hook/generic`（CLI 原生支持 `http` 类型），或 `type=command` 调脚本。
   拿 `session_id`、`transcript_path`、`cwd`、`last_assistant_message` 即可推送。
   注意：`~/.workbuddy/settings.json` 属于产品保护文件（CLI 里有 `wb-policy:config-protected-file`
   规则），改动要在 App 退出时进行，且 App 若重写该文件需要重新加。
2. **tail JSONL 转录（无侵入兜底）**
   监视 `~/.workbuddy/projects/*/*.jsonl`（append-only），过滤 `type=="message" && role=="assistant"`
   且 `content[0].type=="output_text"`；用 `id` 去重、`timestamp` 排序。缺点：文件大（单会话可到 1.7 MB+），
   且流式过程中会分多步写入（`reasoning`/`function_call` 也各占一行）。
3. **SQLite 轮询**：`sessions.last_activity_at` / `status` / `unread`。粒度粗，只适合"有活动"提醒。
4. **ACP 订阅**只对你**自己创建**的会话有效（见下）。

**从外部往已有会话发一条用户消息：走 ACP 不行，走 UI 可以**

- ❌ ACP `session/load` + `session/prompt` → `writer_occupied`（桌面端 daemon 就是那个 writer）。
- ✅ **驱动渲染层**（我用的办法）：`--remote-debugging-port` + CDP `Input.insertText` / `Input.dispatchKeyEvent`，
  或（若给了辅助功能权限）AppleScript `System Events` 键盘输入。实测两条消息都正常发出、正常触发 Stop hook。
- ✅ **另开新会话**：对任一活跃运行时的 ACP 端口 `session/new` + `session/prompt`，完全可用
  （新会话会出现在 App 的任务列表里，标题由首条 prompt 生成）。这不是"注入已有会话"，但能实现
  "从手机让 WorkBuddy 干活"。
- ⚠️ 桌面端自己的 HTTP（18488 / 57454 / 57455 / 57502）是渲染层↔daemon 的私有 API，未见公开文档，
  且主进程里有一整套 `server-side-access-token.js` 的令牌逻辑，本次未逆出可复用的调用方式。

不确定之处：

- daemon 那几个端口的实际路由表没有逐条枚举（`server.js` 3.7 MB 混淆严重，本地路由是拼接出来的）。
- 预热 socket 的帧格式没逆完；如果要做"稳定拿到端口"，更实际的是轮询 `lsof` 或读 CLI 自己的日志。
- 会话结束后运行时是否一定被回收：观察到 30 秒以上的长任务结束后 `--serve` 进程消失，但没有覆盖所有空闲 TTL 分支。

---

## 二、Codex 桌面 App（只读，未做任何修改）

### 2.1 `~/.codex/config.toml` 现状（只列键与命令，值里含密钥的一律脱敏）

```toml
notify = ["/Users/<user>/.codex/computer-use/Codex Computer Use.app/Contents/SharedSupport/SkyComputerUseClient.app/Contents/MacOS/SkyComputerUseClient", "turn-ended"]
sandbox_mode = "danger-full-access"
model = "workbuddy/deepseek-v4.1-flash"
model_reasoning_effort = "high"
model_verbosity = "medium"
service_tier = "default"
openai_base_url = "http://127.0.0.1:3425/backend-api/codex"      # magpie 本地网关
model_catalog_json = "/Users/<user>/.codex/magpie-desktop-models.json"
allow_symlinked_codex_home = true

[features]
js_repl = false
fast_mode = true
# 没有 codex_hooks 这个键

[hooks.state]
[hooks.state."/Users/<user>/.codex/hooks.json:permission_request:0:0"]
trusted_hash = "sha256:ae256e16…9dd359e"
enabled = true
[hooks.state."/Users/<user>/.codex/hooks.json:stop:0:0"]
trusted_hash = "sha256:7545e206…30b05e0faf91"
[hooks.state."codex-voice-alert@personal:hooks/hooks.json:pre_tool_use:0:0"]   trusted_hash = "sha256:3d6d00f9…"
[hooks.state."codex-voice-alert@personal:hooks/hooks.json:permission_request:0:0"] trusted_hash = "sha256:dae95b4c…"
[hooks.state."codex-voice-alert@personal:hooks/hooks.json:stop:0:0"]           trusted_hash = "sha256:84a06db7…"

[model_providers.codex_local_access]  base_url = "http://127.0.0.1:3425/v1"  experimental_bearer_token = "<redacted>"
[model_providers.deepseek]            base_url = "https://api.deepseek.com/v1" experimental_bearer_token = "<redacted>"
[model_providers.magpie]              base_url = "http://127.0.0.1:3425/v1"  experimental_bearer_token = "<redacted>"
```

**`hooks.json`（`~/.codex/hooks.json`，721 B）**

```
/hooks/PermissionRequest[0]/hooks[0] = {type: command,
   command: "/opt/homebrew/opt/python@3.14/bin/python3.14 /Users/<user>/.agentbell/bin/hook.py codex",
   timeout: 3600, statusMessage: "AgentBell"}
/hooks/Stop[0]/hooks[0]              = 同上（AgentBell）
/description = "Lifecycle hooks (AgentBell → WeChat)"
```

**features 下没有 `codex_hooks`**。这个版本的开关已改名为 `hooks`，且默认开：

```bash
$ codex features list | grep -i hook
hooks                stable      true
plugin_hooks         removed     false
```

（`codex features list` 共 90+ 项，其余与本任务无关；`--enable/--disable <FEATURE>` 等价于
`-c features.<name>=true/false`。）

其它 hook 来源（插件）：`~/.codex/plugins/cache/personal/codex-voice-alert/0.1.0+codex.*/hooks/hooks.json`
→ `Stop` / `PermissionRequest` / `PreToolUse(matcher: request_user_input|…send_user_message_async)`
全部执行 `/usr/bin/python3 ${PLUGIN_ROOT}/hooks/voice_alert.py`（语音提示）。

> ⚠️ `[hooks.state...]` 是**信任指纹**机制：改了 `hooks.json` 里的命令，对应的 `trusted_hash` 就对不上，
> 需要 Codex 重新信任（否则钩子会被跳过）。`~/.codex/.hook-trust/2cce684c…`（71 B）里还存了
> 一个 `sha256:fd48c912…` 的文本指纹。

### 2.2 桌面 App 跑的会话会不会触发 notify 和 Stop 钩子

**用的是 app-server，且是 App 的子进程（stdio，无监听端口）**：

```bash
$ ps -Ao pid,ppid,command | grep codex | grep -v grep
68648   1  /Applications/ChatGPT.app/Contents/MacOS/ChatGPT
68708 68648 /Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex \
            -c features.code_mode_host=true app-server --analytics-default-enabled \
            -c plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true

$ /Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex --version
codex-cli 0.167.0-alpha.3.2
```

**Stop 钩子：确实触发（有实证）**

`~/.agentbell/logs/hook.log` 里 codex 事件共 **247** 条（claude 1197 条），尾部：

```
[2026-11-07 23:23:51] invoked codex Stop sync 01a186f9
[2026-11-08 17:03:58] invoked codex Stop sync 01a0e395
[2026-11-08 17:03:58] codex Stop  away=True
```

而 `01a0e395` 对应
`~/.codex/sessions/2026/09/27/rollout-2026-09-27T23-57-30-01a0e395-….jsonl`，其 `session_meta` 是：

```json
{"session_id":"01a0e395-…","cwd":"/Volumes/ESD-USB/机场安检模拟经营类游戏",
 "originator":"Codex Desktop","cli_version":"0.158.0-alpha.2.1","source":"vscode",
 "thread_source":"user","model_provider":"openai"}
```

`originator: "Codex Desktop"` → 该 thread 由**桌面 App** 创建，而它的 Stop 钩子被调用了。
（同目录 11-08 的最新 rollout 也带 `originator":"Codex Desktop"`、`cli_version":"0.167.0-alpha.3.2"`。）

**notify：配置存在、语义明确，但未能实证**

- `notify` 配置给 `SkyComputerUseClient turn-ended`；该二进制的 strings 里有
  `Handles a Codex turn-ended notification` / `ComputerUseIPCCodexTurnEndedRequest` /
  `CodexTurnEndedNotification` / `turnEnded` / `_previousNotify` —— 它是 Codex 官方的
  "回合结束通知 → 释放电脑使用状态"的 notify 接收端。
- Codex 二进制里对应的实现标记是 `hooks/src/legacy_notify.rs`（core 层 legacy notify），
  与前端（TUI / app-server）无关，理论上桌面端也会触发。
- 但 CU 客户端不写日志，`~/Library/Logs/com.openai.codex` 只有空目录，**本次没有拿到可判读的运行痕迹**，
  且只读约束下不能把 notify 改成测试脚本，因此标为"高可能、未实测"。

不确定之处：Codex 从某个版本起，app-server 侧是否会屏蔽 legacy notify 没有在二进制里找到明确分支；
中继服务不要依赖 notify，**优先依赖 hooks.json 的 Stop 钩子**。

### 2.3 会话存储位置与最新助手消息字段

**两层：rollout JSONL（逐条事件）+ thread history SQLite（分页投影）**

```
~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<ISO>-<threadId>.jsonl
~/.codex/thread_history_1.sqlite     # 6.2 GB！表：thread_turns / thread_items / thread_realtime_items
~/.codex/state_5.sqlite              # 6.2 MB，全局状态
~/.codex/queue_1.sqlite              # 待发送队列（对应 thread/queue/* 协议）
~/.codex/logs_2.sqlite
```

rollout 文件里 464 行样本的记录类型：

```
132 event_msg/item_completed      60 response_item/reasoning      60 token_usage_record
60 event_msg/token_count          51 response_item/function_call  51 response_item/function_call_output
27 response_item/message           9 response_item/custom_tool_call(+output)
 1 session_meta  1 turn_context  1 world_state
 1 event_msg/task_started  1 event_msg/thread_settings_applied
```

每行外层统一是 `{timestamp, ordinal, type, payload, metadata}`（`response_item` 分支还带 `metadata`）。

**最新助手消息（脱敏字段结构）**

```jsonc
{
  "timestamp": "2026-11-08T11:17:23.704Z",
  "ordinal": 474,
  "type": "response_item",
  "payload": {
    "type": "message",
    "status": "completed",
    "id": "msg_18e60dc4dc99a810000007b3",
    "role": "assistant",
    "phase": "final_answer",
    "content": [ { "type": "output_text", "text": "**交付信息**\n- APK：`/Users/<user>/…/app-debug.apk`\n- Token：`<redacted>`\n…" } ],
    "internal_chat_message_metadata_passthrough": {
      "turn_id": "01a1bad4-129c-7b12-82f5-bc16e19c27db",
      "content_item_kinds": ["unknown"]
    }
  },
  "metadata": { "retained_source": { "id": { "message_id": "msg_…", "turn_id": "01a1bad4-…", "role": "assistant" }, "revision": "retained_01a1bae0-…" } }
}
```

`thread_items`（SQLite 投影）等价信息：`thread_id, turn_id, item_id, rollout_ordinal, created_at_ms,
item_json, item_type('userMessage'…), updated_at_ordinal`；`thread_turns`: `status, started_at,
completed_at, duration_ms, final_agent_item_id, rollout_byte_offset…`。
→ 想"按 thread 增量取新助手消息"，读 `thread_items`（`WHERE item_type!='userMessage'`，按
`rollout_ordinal` 递增）比解析 1 GB 级 rollout 更省。

### 2.4 App 运行时的 app-server 是否有 Unix socket，第三方能否连接/订阅/发 turn

- **App 自己的 app-server 没有监听 socket**，只有 stdio 管道给父进程（`lsof -p 68708` 只有
  匿名 unix pipe 对 + 到 `127.0.0.1:3425`(magpie) / `7890`(代理) 的 TCP）。
- `codex app-server` 本身**支持** `--listen`：

```
--listen <URL>  Supported values: `stdio://`(default), `unix://`, `unix://PATH`, `ws://IP:PORT`, `off`
```

- 存在 `~/.codex/ipc/ipc.sock`（`srw-------`），但持有者是 **ChatGPT.app 主进程**（68648 上有 8 个 fd
  指向它），不是 app-server；托管 daemon 已死：

```
$ cat ~/.codex/app-server-daemon/daemon.pid
{"pid":64519,"processStartTime":"日 10月/11 23:30:22 2026", …}   # 该 pid 已不存在
$ cat ~/.codex/app-server-daemon/loaded-threads.json
[]
```

  实测 `codex app-server proxy --sock ~/.codex/ipc/ipc.sock` 发送 `initialize` → 无任何 JSON-RPC 回复
  （连上后被关），`python3 socket.connect()` 也立刻 EOF。
  **结论：目前第三方连不上 App 的 app-server，也没有可订阅的 App 端 socket。**

- 但**协议能力齐备**，用导出的 JSON Schema 可确认（`codex app-server generate-json-schema --out /tmp/...`）：

  * 客户端 → 服务端（v2）：`thread/start`、`thread/resume`、`thread/list`、`thread/read`、
    `thread/turns/list`、`thread/items/list`、`thread/queue/add|update|delete|list|start`、
    `thread/inject_items`、`thread/queue/…`、`turn/start`、`turn/interrupt`、`turn/settings/update`、
    `review/start`、`model/list` …
  * 服务端 → 客户端通知：`thread/started`、`thread/status/changed`、`turn/started`、`turn/completed`、
    `AgentMessageDelta`、`CommandExecutionOutputDelta`、`hook/started`、`hook/completed`、
    `thread/tokenUsage/updated`、`thread/queue/changed` …
  * `TurnStartParams` 必填仅 `threadId` + `input`（`UserInput[]`），另有 `cwd`、`approvalPolicy`、
    `sandboxPolicy`、`model`、`effort`、`summary`、`clientUserMessageId` 等。

- 另外 CLI 有现成命令（不必自己写 JSON-RPC）：

```bash
codex queue --thread <thread-uuid|名字> --message "文本" [--resume]
codex app-server --listen unix:///tmp/codex.sock     # 自建可订阅的 app-server
```

**可行性判断**

| 需求 | 可行性 | 做法 |
|---|---|---|
| 订阅桌面 App 正在跑的 thread 事件 | ❌ 现在不行 | App 的 app-server 是 stdio；无 socket。除非让 App 以 `--listen` 方式启动（未找到该开关） |
| 向已有 thread 发一条用户消息 | ✅ 可行（推荐 `codex queue`） | `codex queue --thread <id> --message ...`；或自建 `codex app-server --listen unix://…` 后 `turn/start` / `thread/queue/add` |
| 检测"有新的助手回复" | ✅ Stop hook（已实证）/ 轮询 rollout 或 `thread_items` | 见 2.2 / 2.3 |

不确定之处：

- 自建 `codex app-server --listen` 并用 `thread/resume` 接管一个**桌面端正在运行**的 thread，
  很可能遇到"同一 thread 被另一进程占用"的竞争（未实测，避免污染正在运行的 state 库）。
- `~/.codex/ipc/ipc.sock` 的帧格式未逆出；它更像 App 内部 IPC 汇聚点，不建议作入口。

---

## 三、本机已有的通知类工具（接线清单，避免冲突）

### 3.1 AgentBell（微信 ClawBot / PushPlus 那条线）

- 目录：`~/.agentbell/`（`bin/agentbell.py`、`bin/hook.py`、`config.json`、`state.json`、`logs/`）
- 进程：**正在运行** `python3.14 ~/.agentbell/bin/agentbell.py run`（pid 742），
  `TCP 127.0.0.1:18765 (LISTEN)`
- 配置键（值脱敏）：`port=18765`、`secret=<redacted>`、`ilink_base=https://ilinkai.weixin.qq.com`、
  `quiet_start_hour/quiet_end_hour`、`away_idle_seconds=300`、`stop_wait_seconds=3500`、
  `question_answer_mode=answers`、`doorbell=""`、`doorbell_key=""`、`doorbell_url=""`
  → **`ilinkai.weixin.qq.com` 就是"微信 ClawBot"通道**；`state.json` 里只有 get_updates 游标与会话 token（<redacted>）。
- PushPlus：只在 `agentbell.py` 里以"备用门铃"实现（`https://www.pushplus.plus/send`，
  `agentbell doorbell pushplus <token>`）；当前 `doorbell` 为空 = **未启用**。
- **已接在哪些 agent 上：**

| Agent | 配置文件 | 事件 |
|---|---|---|
| Codex | `~/.codex/hooks.json` | `Stop`、`PermissionRequest` → `python3.14 ~/.agentbell/bin/hook.py codex` |
| Claude Code | `~/.claude/settings.json` | `PreToolUse`、`PermissionRequest`、`Stop`、`UserPromptSubmit` → `…/hook.py claude` |

- 日志：`~/.agentbell/logs/hook.log`（`invoked codex Stop sync <thread前缀>` / `invoked claude …`）、
  `~/.agentbell/logs/daemon.log`（近期大量 `getupdates error: nodename nor servname` —— 微信侧网络/代理问题）。

### 3.2 OpenClaw

- `~/.openclaw/openclaw.json`：`gateway = {port:18789, mode:local, bind:loopback, auth:{mode:token,token:<redacted>}}`
- `channels`: `feishu`(enabled, 含 appId/appSecret/verificationToken `<redacted>`)、`openclaw-weixin`
- `hooks.internal`: 只有 `session-memory`(disabled)、`command-logger`(enabled)
- 运行中：`node …/openclaw/dist/index.js gateway --port 18789`（pid 745）
- **与 Codex / WorkBuddy 的消息通知没有接线**，是独立的飞书/微信 bot 网关。

### 3.3 `~/.claw` / `~/.agents`

- `~/.claw/plugins/installed/`：`example-bundled-bundled`、`sample-hooks-bundled` + `installed.json` —— 只是插件样本，**无接线**。
- `~/.agents/`：`skills/`（含 skill-lock.json）、`plugins/` —— Skill 安装目录，**无通知接线**。

### 3.4 WorkBuddy 自带的推送通道（容易被忽略的"重复源"）

- `~/.workbuddy/settings.json`：`claw.channels.wechatmp.enabled = true`（`connectionMode: webhook`），
  `legacyOwnerUid` 为某 user id。
- `~/.workbuddy/workbuddy.db` 里还有专门的投递表与字段：
  `automation_delivery_outbox(channel 默认 'wechatmp', status, attempt_count, payload_json …)`、
  `automations.push_to_wechat` / `push_to_wecom_bot` / `wecom_bot_chat_id`。
  即 **定时任务的完成推送是 WorkBuddy 内建的**，与"钩子推送"会形成两条并行通知。

### 3.5 冲突与注意事项（给中继服务的硬约束）

1. **Codex hooks 有 `trusted_hash` 指纹**：改 `~/.codex/hooks.json` 的 command 后，必须让 Codex 重新信任；
   正确做法是**追加**一个 hook 条目而不是替换 AgentBell 那条。
2. **不要覆盖 `notify`**：仓库里已有的 `hooks/codex-notify-wrapper.sh` 思路正确（把原 notify 放进
   `CHAIN_NOTIFY` 再转发）；注意 notify 的 JSON 是**追加在最后一个 argv**，不是 stdin。
3. **WorkBuddy 的用户级 settings.json 是产品保护文件**（CLI 内部 `wb-policy:config-protected-file`），
   改动要在 App 退出时做；建议改用 `~/.workbuddy/settings.local.json`（当前不存在）先验证是否被读取，
   再决定是否动主文件。
4. **端口别撞**：AgentBell `18765`、OpenClaw `18789`、现有中继脚本里出现 `18787`。
   任务书里约定的 `127.0.0.1:8787` 目前空闲，可用。
5. **重复推送**：Codex 侧同一回合会有 3 个钩子（AgentBell Stop、voice_alert Stop、未来你的 Stop），
   外加 WorkBuddy 内建公众号推送；中继要做去重（`session_id/turn_id + 时间窗`）。

---

## 四、本次对系统做的改动与恢复（透明记录）

| 改动 | 位置 | 恢复情况 |
|---|---|---|
| 备份 `settings.json` | `/tmp/wb-hook-backup-<ts>/settings.json`（+ `SHA256.before`） | 保留（证据） |
| 加临时 `hooks.Stop` | `~/.workbuddy/settings.json` | **已还原**，sha256 与备份前一致：`bb5cd197…20b764cb`；`'hooks' in settings == False` |
| 启动 WorkBuddy（分析前未运行） | GUI + daemon + 预热 CLI | 测试结束已全部退出（`pgrep -f WorkBuddy` 为空） |
| 用 `--remote-debugging-port=9333` 启动 | 仅为注入 UI 输入 | 随 App 退出消失 |
| 生成/保留 `--prewarm` 进程 | `/tmp/codebuddy-prewarm-*.sock` | 随 App 退出清理 |

**测试产生的数据（用户可自行删除）**

- `~/.workbuddy/projects/Users-fanqiege-WorkBuddy-2026-10-16-22-48-45/08fdb08a-4537-4e23-9481-a64e74c72cf5.jsonl`
  （GUI 发的 2 条消息："请只回复两个字：收到"、"请先运行命令 sleep 30，然后只回复一行：完成"）
- 同目录 `01a1bae8-68a5-71e4-b9d6-4410baf3addb.jsonl`（我通过 ACP `session/new` 创建的会话）
- `/tmp/wb-hook-test.log`（钩子触发证据，3 行）
- `/tmp/codex-schema/`（`codex app-server generate-json-schema` 导出的协议 schema，只读产物）

**未改动**：Codex 的任何配置与数据（`config.toml` / `hooks.json` / sessions / sqlite 全部只读），
WorkBuddy 的数据库、`.codebuddy`、`~/WorkBuddy` 工作区、`.agentbell` / `.openclaw` / `.claw` 均未写入。

---

## 五、给中继服务的落地建议（一句话版）

- **WorkBuddy 推送**：在 `~/.workbuddy/settings.json`（或 `settings.local.json`）加
  `hooks.Stop = [{hooks:[{type:"http", url:"http://127.0.0.1:8787/hook/generic", timeout:5}]}]`，
  payload 里直接用 `session_id` + `transcript_path` + `last_assistant_message`；写入前 App 必须退出。
- **Codex 推送**：保留现有 `notify` 包装（chained），并在 `~/.codex/hooks.json` 的 `Stop` 数组里
  **追加**一条转发条目，然后让 Codex 重新信任（`trusted_hash` 会变）。
- **手机 → 桌面**：
  - Codex：`codex queue --thread <id> --message <text> --resume`（最省事），或自建
    `codex app-server --listen unix://…` + `turn/start`。
  - WorkBuddy：ACP 只能新建会话（`session/new` + `session/prompt`）；对已有会话需要用 UI 注入
    （CDP/Accessibility），ACP 会返回 `writer_occupied`。
- **去重**：以 `session_id/turn_id + 首次出现时间` 为键，避免 AgentBell、voice_alert、新钩子、
  WorkBuddy 内建公众号推送四条通道重复。
