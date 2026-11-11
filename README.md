# Homing

**Every coding agent, one pocket inbox.**

把本机 Claude Code、Codex、WorkBuddy 的回复和待确认事件，通过局域网推送到 Android 手机，并支持在通知里直接回复。Homing 是这套 Android 客户端的正式名称（前身叫「Agent 收件箱」），applicationId 仍保留 `dev.agentpush.poc`，升级不会丢失已有的 ColorOS 自启动和后台权限。

## 目录

```text
agent-push-poc/
├── relay/
│   ├── server.mjs                 # 零依赖 Node 中继
│   ├── config.json                # 首次启动自动生成 token
│   ├── relay.log                  # LaunchAgent 的标准输出/错误日志
│   └── data/                      # events.jsonl / replies.jsonl
├── hooks/
│   ├── codex-notify.mjs           # POST /hook/codex
│   ├── codex-notify-wrapper.sh    # Codex notify 入口，负责链式保留原命令
│   ├── claude-settings-snippet.json
│   └── test-events.sh
├── android/                       # Homing，Compose + Material 3 客户端
└── docs/homing-v2/                # Homing 品牌规格与鸽子动效数据
```

## 端口约定：统一使用 `18787`

本机 `8787` 长期被用户的 `ResetWatch` 服务占用（非本项目，不要停止它）。因此整个原型统一改用 **`18787`**：

| 位置 | 值 |
| --- | --- |
| `relay/server.mjs` 默认端口 | `18787`（`PORT` 环境变量可覆盖） |
| `hooks/codex-notify.mjs` | `http://127.0.0.1:18787/hook/codex` |
| `hooks/claude-settings-snippet.json` | `http://127.0.0.1:18787/hook/claude` |
| `hooks/test-events.sh` | `BASE_URL=http://127.0.0.1:18787` |
| Android 默认服务器地址 | `http://192.168.1.3:18787`（界面上仍可手改） |

当前局域网 IPv4：`192.168.1.3`（另有虚拟接口 `198.18.0.1`，手机不要用这个）。

## 常驻：macOS LaunchAgent

中继由 LaunchAgent `dev.agentpush.relay` 常驻，开机自动启动、退出自动重启。

- plist：`~/Library/LaunchAgents/dev.agentpush.relay.plist`
- `RunAtLoad` / `KeepAlive` 均为 `true`
- 日志：`~/Projects/agent-push-poc/relay/relay.log`
- 启动命令：`/opt/homebrew/bin/node /Users/fanqiege/Projects/agent-push-poc/relay/server.mjs`

查看状态：

```sh
launchctl print gui/$(id -u)/dev.agentpush.relay | grep -E 'state|pid|program'
lsof -nP -iTCP:18787 -sTCP:LISTEN
tail -f ~/Projects/agent-push-poc/relay/relay.log
```

改完 plist 后的重载方式：

```sh
launchctl bootout gui/$(id -u)/dev.agentpush.relay
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.agentpush.relay.plist
```

**卸载（要求 3 步都做）**：

```sh
launchctl bootout gui/$(id -u)/dev.agentpush.relay     # 停止并取消注册
rm ~/Library/LaunchAgents/dev.agentpush.relay.plist    # 删除 plist，避免下次登录再启动
lsof -nP -iTCP:18787 -sTCP:LISTEN                      # 确认端口已无人监听
```

只想临时停一会儿、不卸载：

```sh
launchctl bootout gui/$(id -u)/dev.agentpush.relay
# 恢复
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.agentpush.relay.plist
```

不再使用 `screen` / `nohup` 起中继实例：两个实例会同时写 `relay/data`，事件 id 会互相覆盖。若之前有 `screen` 会话残留：

```sh
screen -ls
screen -S <session> -X quit
lsof -nP -iTCP:18787 -sTCP:LISTEN    # 若 node 仍存活，kill <pid>
```

## 中继自测

```sh
cd ~/Projects/agent-push-poc

curl -s http://127.0.0.1:18787/health
./hooks/test-events.sh                # 默认就打 18787

TOKEN=$(node -e 'console.log(JSON.parse(require("fs").readFileSync("relay/config.json","utf8")).token)')
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:18787/api/events?since=0'
```

`test-events.sh` 会发送 3 条事件：Claude Stop、Codex turn complete、WorkBuddy generic。

## 手机下载 APK（`GET /apk`）

手机浏览器直接打开：

```text
http://192.168.1.3:18787/apk
```

特性：

- 不需要 token；
- 只接受**本机与局域网**来源（`127.0.0.0/8`、`10/8`、`172.16/12`、`192.168/16`、`169.254/16`、IPv6 `::1`/`fe80::/10`/`fc00::/7`），其它来源返回 `403`；
- `Content-Type: application/vnd.android.package-archive`；
- 下载文件名 `agent-inbox.apk`；
- 返回文件：`android/app/build/outputs/apk/debug/app-debug.apk`（可用 `APK_PATH` 环境变量指向别处）。

校验下载完整性：

```sh
curl -s http://127.0.0.1:18787/apk -o /tmp/agent-inbox.apk
stat -f%z /tmp/agent-inbox.apk
stat -f%z ~/Projects/agent-push-poc/android/app/build/outputs/apk/debug/app-debug.apk
shasum -a 256 /tmp/agent-inbox.apk ~/Projects/agent-push-poc/android/app/build/outputs/apk/debug/app-debug.apk
```

## 安装 Android 客户端

```sh
adb install -r ~/Projects/agent-push-poc/android/app/build/outputs/apk/debug/app-debug.apk
```

打开 **Homing** 后：

1. 服务器地址默认已是 `http://192.168.1.3:18787`，换网络时手动改。
2. Token 填 `relay/config.json` 里的 token。
3. 点“保存并重连”，允许通知权限。
4. 点“电池白名单”，允许忽略电池优化。
5. ColorOS 还需要在系统设置中允许“自启动”和“后台运行”，并尽量在最近任务中锁定本应用，防止 ColorOS 清理前台服务。

界面使用 Compose + Material 3，固定深色模式。收件箱以 `GET /api/sessions` 为主数据源，并通过 SSE `event: session` 增量更新；事件只负责详情、未读和通知。默认按 `cwd` 分项目、带工作区与分支组头，也可切到按时间平铺；顶部只保留“全部 / Codex / Claude / WorkBuddy”计数筛选。卡片使用四级标题回退且不显示裸 UUID，标签行依次展示项目、分支、模型和工作区，状态同时用图标与文字表达，并按状态提供“批准 / 回复 / 查看”快捷动作。支持搜索、左滑已读、下拉刷新、进入会话自动已读和首页“全部已读”。

会话详情优先加载 `/api/sessions/<id>/transcript` 完整对话，可切换“只看 Agent”；拿不到 transcript 时自动回退到事件时间线。标题栏展示同一组来源标签，点开后可在来源、完整 cwd、session_id、title_source、模型和分支之间逐项复制。没有事件的会话也会用 `session_id + source` 回复；附件在详情中有事件时按 `event_id` 上传，无事件时按 `cwd` 上传。新建任务会先按 `cwd` 上传附件，再把路径随 `/api/sessions/new` 提交；WorkBuddy 返回 `reused_active` 时会明确弹出“已发到当前活跃会话”，Claude 返回 `request_id` 时等待 SSE `event: session` 补发 session_id 后自动打开会话。

回复支持完整 Markdown（表格、引用、任务列表、代码块语言标签/复制/HTML 预览）、图片缩略图与全屏查看器（缩放、左右切换、保存相册、分享）、HTML/PDF/普通文件卡片、应用内 WebView 浏览器、本机端口链接提示与应用内 `file` 发送。输入框支持相册多选、拍照、任意文件、上传进度、失败重试和系统语音识别。

Android `0.6.1` 起，会话详情优先使用 `/api/sessions/<id>/items` 的结构化条目：用户气泡、Markdown 回复、可折叠思考过程、工具调用（命令摘要、输出尾部、退出码）、文件修改（增删行与彩色 diff）、计划清单、授权卡片和错误卡片分别渲染；`items` 不可用时自动退回 transcript / 事件列表。进入详情后额外订阅 `/api/stream?session_id=<id>` 的 `item` / `turn`，同一 item id 原地插入或替换，用户停留在底部时自动跟随新内容，上滑查看历史时改为显示“有新内容”。模型胶囊、推理强度、停止按钮和授权操作按 `/api/agents` 的 capabilities 显示；WorkBuddy 明确提示不支持切换模型，Claude 明确提示不支持推理强度。`context_usage.limit` 有值时显示进度环，为 `null` 时只显示已用 token；运行中显示计时与“停止”按钮。模型选择只对下一条消息生效，并在该条手机气泡上标注。新建任务页同样支持模型与工作区选择。

Android `0.6.2` 修复真机点击任意会话立即闪退的问题：0.6.1 在会话详情数据尚未加载、`session` 仍为 `null` 时提前读取 `session.model`，现改为安全回退。同时升级 Compose BOM 到 `2025.09.01`（Runtime 1.9.2），修复 Markdown 库 0.38.1 在 Compose 1.8.3 下渲染回复时的 `NoSuchMethodError: getCurrentCompositeKeyHashCode`。会话时间线的 LazyColumn key 加入序号前缀，同 id 的 item/SSE 插入替换不会再触发重复 key；items 按 id 去重，context_usage.limit 为 null、tool 对象缺失、approval model 为 null、files 为空等边界均有渲染测试覆盖。超过 8 万字符的 Markdown 只渲染前 8 万字符并显示提示。会话 ID 继续使用 URL-safe Base64 进入导航参数，含斜杠、冒号、中文和 emoji 的 session_id 均有回归测试。

`0.6.2` 新增崩溃捕获：`CrashReporter` 在 `Application` 中安装全局 `UncaughtExceptionHandler`，协程使用 `CoroutineExceptionHandler`；崩溃时把版本号、时间、最后打开的 session_id、机型/系统、线程、来源和堆栈写入应用私有目录 `files/crash/last-crash.json`，下次启动在首页顶部显示“上次崩溃”卡片，可展开、复制、分享、清除，并静默尝试 `POST /api/client-log`（接口不存在时忽略）。

`0.7.0` 完成 Homing 品牌与界面重做：

- 显示名改为 **Homing**，adaptive icon 背景 `#0F172A`、前景灰鸽，Android 13+ 单色主题图标和通知栏小图标只使用完整轮廓；
- 启动页 148dp 鸽子从「归巢」转入「待机」，标题和副标题错峰淡入，底部小鸽子持续扑翅；
- 底部三个标签：收件箱 / 项目 / 我的。收件箱按“请求授权 → 等你回复 → 出错 → 刚完成”排序，包含运行中会话横条、等宽计时、待处理角标、空状态和一键全部已读；
- 项目页支持按项目 / 按时间切换、agent 筛选、叠放头像和 `/tmp` 临时会话折叠；
- 会话详情的标题栏改为标题 + 等宽来源行，运行中有计时与停止按钮，上下文用量改为细条；思考条目显示由相邻条目时间推导的时长，模型胶囊和强度胶囊分离，模型面板按 provider 分组并标记“当前 / 默认”；
- 新增鸽子动效组件 `HomingPigeon`：用 PathParser 解析 `res/raw/pigeon.json`，支持待机、扑翅、归巢、点头和静态五种状态；系统动画时长缩放为 0 时自动静态化；
- 打包 JetBrains Mono Regular/Medium（SIL OFL 1.1），用于模型名、分支、计时和命令；品牌色、字号、间距、圆角按 `docs/homing-v2/SPEC.md` 实现；
- 已读、置顶、静音、归档（`read / pinned / muted / archived`）以中继 `/api/sessions` 的远端字段为准；App 本地 `homing_session_flags` SharedPreferences 只作为离线缓存和旧中继回退，收到远端字段后立即覆盖。收件箱改用 `/api/inbox` 的 `waiting / running / recent_done / counts`，允许、拒绝或回复成功后 PATCH 对应会话 `read=true`。

Android 端在第二阶段依赖之外新增：Coil 3（`coil-compose`、`coil-network-okhttp`、`coil-gif`，3.3.0）、mikepenz `multiplatform-markdown-renderer-m3` / `-coil3`（0.38.1）。图片选择和文件选择分别使用 AndroidX Photo Picker 与 SAF，WebView、语音识别、MediaStore、DownloadManager 使用系统能力，没有引入其他 UI 或网络框架。

前台服务使用 `remoteMessaging` 类型和 `START_STICKY`，常驻通知会从“正在连接 Mac”更新为“已连接 Mac”。SSE 断开后按 1 秒、2 秒、4 秒，一直退避到 30 秒重连；网络恢复时通过 `ConnectivityManager` 立即重连，重连期间短暂持有 partial wake lock。首页顶部会显示最近一次连接/断开时间和断开原因，便于排查 ColorOS 后台限制。

## 钩子安装状态

### Codex（已安装，待重启 App 生效）

`~/.codex/config.toml` 只改了一行 `notify`，其它键未动。备份在 `~/.codex/config.toml.bak-agentpush`。

> `notify` 在 Codex 进程启动时加载：改完后必须重启 ChatGPT/Codex App（或新开 CLI 会话）。
> 进程启动时间早于 `config.toml` 的修改时间就说明还没重启。

如果重启后真实 Codex 回合结束仍没有 `codex` 事件，说明桌面端走的是 hooks.json 而不是 legacy notify，
备用方案是 `~/.codex/hooks.json` 的 `Stop`：**追加**一个 `http` 条目（不要替换 AgentBell 那条），
并把命令/URL 写进 `[hooks.state."/Users/fanqiege/.codex/hooks.json:stop:1:0"]` 的 `trusted_hash`
或让 Codex 重新弹一次信任确认。改动 `hooks.json` 会让原有条目的 `trusted_hash` 失配而静默跳过。

原先的 notify 是 Computer Use 的 `turn-ended` 通知，所以改成了包装脚本：

```toml
# 改前
notify = ["/Users/fanqiege/.codex/computer-use/Codex Computer Use.app/Contents/SharedSupport/SkyComputerUseClient.app/Contents/MacOS/SkyComputerUseClient", "turn-ended"]

# 改后
notify = ["/Users/fanqiege/Projects/agent-push-poc/hooks/codex-notify-wrapper.sh"]
```

`hooks/codex-notify-wrapper.sh` 把原命令写进 `CHAIN_NOTIFY`，再调用 `hooks/codex-notify.mjs`。`codex-notify.mjs` 把 Codex 传入的同一段 JSON：

1. POST 到 `http://127.0.0.1:18787/hook/codex`；
2. 作为最后一个参数转发给 `CHAIN_NOTIFY`，所以原通知照常工作。

中继不可达或原命令失败时脚本静默退出 0，不会阻塞 Codex。

回滚：

```sh
cp ~/.codex/config.toml.bak-agentpush ~/.codex/config.toml
```

手动验一次（传假事件）：

```sh
/Users/fanqiege/Projects/agent-push-poc/hooks/codex-notify-wrapper.sh \
  '{"type":"agent-turn-complete","thread-id":"manual-test-thread","cwd":"/tmp/demo","last-assistant-message":"手动测试"}'
```

### Claude Code（未安装）

本阶段没有改 `~/.claude/settings.json`。需要时把 `hooks/claude-settings-snippet.json` 的 `hooks` 合并进去（已指向 18787），覆盖 `Stop` 和 `Notification`，保留其它配置。

### WorkBuddy（已安装）

`~/.workbuddy/settings.json` 里追加了 `hooks.Stop` 和 `hooks.Notification`（`http` 类型，指向中继），
其它配置项逐字保留，备份为 `~/.workbuddy/settings.json.bak-agentpush`：

```json
"hooks": {
  "Stop": [
    { "hooks": [ { "type": "http", "url": "http://127.0.0.1:18787/hook/workbuddy", "timeout": 660 } ] }
  ],
  "Notification": [
    { "hooks": [ { "type": "http", "url": "http://127.0.0.1:18787/hook/workbuddy", "timeout": 5 } ] }
  ]
}
```

- 写入时 WorkBuddy 必须处于退出状态（该文件是产品保护文件），写完再启动 App。
- 桌面端会话实测触发：`POST /hook/workbuddy` 收到 `hook_event_name: "Stop"`，中继生成 `workbuddy/reply` 事件。
- 回滚：退出 WorkBuddy 后 `cp ~/.workbuddy/settings.json.bak-agentpush ~/.workbuddy/settings.json`。

通用接口（其它工具/脚本手动打点）：

```sh
curl -sS -X POST http://127.0.0.1:18787/hook/generic \
  -H 'Content-Type: application/json' \
  -d '{
    "source": "workbuddy",
    "kind": "needs_input",
    "session_id": "your-session-id",
    "cwd": "/path/to/project",
    "title": "WorkBuddy 等待输入",
    "text": "任务正文"
  }'
```

`source` 只能是 `claude`、`codex`、`workbuddy`；`kind` 只能是 `reply`、`needs_input`、`permission`、`error`。

## 接口摘要

需要 Bearer token：

- `GET /api/events?since=<id>`
- `GET /api/stream`（SSE，事件流：`event: message` + 新增的 `event: reply_status`）
- `POST /api/read`
- `POST /api/reply` → `{ok, reply_id, status, message}`
- `GET /api/sessions?limit=100&source=&status=` → `{sessions:[SessionSummary], count, next_cursor, generated_at}`
- `GET /api/sessions/<session_id>` → 单个 `SessionSummary`（不存在 404）
- `PATCH /api/sessions/<session_id>`，body 可含 `read / pinned / muted / archived`（只写传入的键）→ `{session_id, source, state, session}`
- `GET /api/inbox` → `{waiting, running, recent_done, counts:{waiting,running}, generated_at}`
- `GET /api/sessions/<session_id>/items?limit=200` → `{session_id, source, file, count, items:[Item], turns:[{state,ts,seq}], reason}`
- `GET /api/agents` → `{agents:[{source, display_name, available, capabilities, models:[{id, label, provider, is_default, efforts}], default_model}], generated_at}`
- `POST /api/client-log`，body `{app_version, ts, session_id, stack, device}`（单条 ≤ 256KB）→ `{ok, saved, bytes, exception}`，追加写入 `relay/data/client-crash.jsonl`
- `GET /api/client-log?limit=5` → `{count, total, entries:[{received_at, app_version, ts, session_id, device, stack, bytes, remote, exception}]}`
- `POST /api/sessions/<session_id>/stop` → `{ok, session_id, via}`；非中继启动的进程 → 409 `{ok:false, error:"unsupported"}`
- `POST /api/approvals/<event_id>`，body `{decision: "allow"|"deny"}` → `{ok, event_id, decision}`；无挂起 → 404 `{error:"not_pending"}`
- `GET /api/replies?session_id=<id>` → `{session_id, count, replies:[{reply_id, event_id, session_id, status, message, ts, text, files}]}`
- `GET /api/away` → `{enabled}`
- `POST /api/away`，body `{enabled: true|false}` → `{ok, enabled}`，持久化在 `relay/config.json` 的 `away`
- `POST /api/upload?event_id=<id>&name=<文件名>`，body 为原始字节 → `{ok, path, name, size, mime}`
- `GET /api/projects` → `{count, projects:[{cwd, project, count, last_ts, sources}]}`
- `POST /api/sessions/new`，body `{source, cwd, text, files}` → `{ok, session_id, status}`（失败 `{ok:false, error}` + 501）
- `GET /api/sessions/<session_id>/transcript?limit=200` → `{session_id, source, transcript_path, count, messages:[{role, text, ts, attachments}], reason}`
- `GET /p/<port>/...` 本机端口反向代理（额外凭证规则见下文）

不需要 token：

- `GET /health`
- `GET /apk`（仅本机与局域网来源）

不需要 token、但只接受本机来源：

- `POST /hook/claude`
- `POST /hook/codex`
- `POST /hook/workbuddy`（映射同 `/hook/claude`：`Stop` → `reply`，`Notification` → `permission` / `needs_input`）
- `POST /hook/generic`

不需要 Bearer、只允许本机与局域网来源：

- `GET /f/<32 位 token>/<相对路径>` 附件能力链接

`/hook/claude` 与 `/hook/workbuddy` 共用同一套映射：`hook_event_name: "Stop"` 取 `last_assistant_message`
记为 `reply`；`Notification` 按 `notification_type == "permission_prompt"` 记为 `permission`，否则 `needs_input`。

## 手机回复怎么送达会话

`POST /api/reply` 有两种定位方式：

- `{event_id, text, files?}`：老用法，事件必须存在；
- `{session_id, source?, text, files?}`：**没有 event_id 也能回**。`source` 省略时按事件 / Codex `session_index` /
  Claude 会话文件 / WorkBuddy 快照自动推断；cwd 取该会话最近一次事件的 cwd，取不到就用 `/api/sessions` 里的 cwd
  （所以只在 `/api/sessions` 里出现、还没产生新事件的会话也能回复，Codex 直接 `exec resume` 注入该 thread）。

按事件来源分两条路：

| 事件来源 | 投递方式 | 返回 status |
| --- | --- | --- |
| `codex` | 立刻用 `codex exec resume --skip-git-repo-check --all <thread> <text>` 注入该 thread 并触发新一轮（`execFile`/`spawn` 参数数组，不拼 shell） | `injected`；该 thread 已有注入在跑则 `queued`，上一轮结束后自动续跑；启动失败或退出码非 0 → `failed` |
| `claude` / `workbuddy` | 若该 session 有挂起中的 Stop 钩子 → 直接把回复交给它（返回 `{"decision":"block","reason":"用户从手机回复：<内容>"}`）；否则入队 | `held` / `queued` |

- 回复状态：`injected`（已注入 Codex）、`held`（已交给挂起中的钩子）、`queued`（暂无挂起的钩子，已排队）、`failed`。
- 状态变化会通过 SSE 推送：`event: reply_status` / `data: {reply_id, event_id, session_id, status, message}`。
- `relay/data/replies.jsonl` 是追加式日志，同一 `id` 多行以最后一行为准（状态会演进）。
- Codex 的 `codex queue --thread <id> --message <text>` 只写 `~/.codex/queue_1.sqlite`，没有本地 daemon 时
  `--resume` 会失败且不会触发新一轮，因此中继改用 `exec resume`。

### 离开模式（`/api/away`）

默认关闭。开启后：

1. `/hook/claude`、`/hook/workbuddy` 收到 `Stop` 先照常推送事件（SSE + `events.jsonl`）；
2. 若该 session 已经有排队中的手机回复 → 立刻返回 `block`，把这些回复当作续聊指令；
3. 否则把这个 HTTP 请求挂起最多 10 分钟等手机回复：
   - 收到 `/api/reply` → 返回 `{"decision":"block","reason":"用户从手机回复：<内容>"}`，agent 继续；
   - 超时 → 返回 `{}`（`HOLD_TIMEOUT_MS` 环境变量可改，默认 600000）；
   - `POST /api/away {enabled:false}` 或中继退出 → 立刻放行所有挂起请求，返回 `{}`。

配套超时：`hooks/claude-settings-snippet.json` 的 `Stop` 与
`~/.workbuddy/settings.json` 的 `Stop` 都设为 `timeout: 660`（> 600 秒挂起窗口）。
`/hook/claude`、`/hook/workbuddy` 的 `Notification` 仍是 `timeout: 5`。

`relay/relay.log` 里会记录 `/api/stream` 的连接审计（来源地址、本机/局域网、连接与断开时间、持续秒数），
以及 `[hold]` / `[reply]` / `[away]` 行，用来排查手机是否连过、回复怎么走的：

```sh
grep -E '\[stream\]|\[hold\]|\[reply\]|\[away\]' ~/Projects/agent-push-poc/relay/relay.log
```

## 会话元数据（`GET /api/sessions`）

```json
{
  "sessions": [
    {
      "session_id": "08fdb08a-4537-4e23-9481-a64e74c72cf5",
      "source": "workbuddy",
      "title": "翻译一段英文文本",
      "title_source": "agent",
      "workspace": "/Users/fanqiege/WorkBuddy/2026-10-16-22-48-45",
      "project": "2026-10-16-22-48-45",
      "branch": null,
      "cwd": "/Users/fanqiege/WorkBuddy/2026-10-16-22-48-45",
      "model": null,
      "status": "done",
      "unread": 10,
      "last_ts": "2026-11-08T14:00:35.041Z",
      "last_preview": "……",
      "event_count": 10,
      "read": false,
      "pinned": false,
      "muted": false,
      "archived": false,
      "is_temp": false,
      "is_empty": false
    }
  ],
  "count": 1,
  "next_cursor": null,
  "generated_at": "2026-11-08T14:05:00.000Z"
}
```

按 `last_ts` 倒序；`?limit=`（默认 100，上限 500）、`?source=`、`?status=`、
`cursor=` 与 `zone=earlier` 可选过滤。返回的 `next_cursor` 是下一批的 offset；
没有更多会话时为 `null`。`zone=earlier` 会排除 `/api/home` 已放入
attention／resume 的会话。

**标题四级回退（永不为空）**，`title_source` 标明取自哪一级：

| 级别 | 来源 | `title_source` |
| --- | --- | --- |
| 1 | Codex `~/.codex/session_index.jsonl` 的 `thread_name`（同 id 取 `updated_at` 最新）；Claude 的 `custom-title` → `ai-title` → `summary`；WorkBuddy `sidebar-list-snapshot.json` 的 `items[].title` | `agent`（Claude/WorkBuddy 的 custom 记为 `custom`） |
| 2 | 首条用户消息，截断 40 字 | `first_prompt` |
| 3 | 项目名（cwd 的 basename） | `project` |
| 4 | `<source>-<id 前 8 位>` | `fallback` |

其它字段：

- `workspace`：WorkBuddy 用快照里的 `items[].space.name`（绝对路径）；Codex / Claude 用 cwd 的**上级目录名**（例如 `/Users/x/Projects/foo` → `Projects`），拿不到时 `null`。想改成绝对路径只需改 `workspaceLabel()` 一行。
- `project`：cwd 的 basename；`cwd`：会话工作目录（与 workspace 分开保留，供“复制路径/打开终端”）。
- `branch`：优先源头元数据（Codex `session_meta.git.branch`、Claude 行内 `gitBranch`）；没有就对 cwd 只读执行
  `git -C <cwd> rev-parse --abbrev-ref HEAD`（参数数组、1 秒超时、60 秒缓存），`HEAD` / 非 git → `null`。
- `model`：Codex 取 rollout 尾部 `turn_context.model` / `thread_settings_applied`；Claude 取消息里的 `model`；WorkBuddy 快照没有该字段 → `null`。
- `status`：`running` | `needs_input` | `permission` | `error` | `done`。由该会话最近一条事件的 `kind` 推导
  （`permission` / `needs_input` / `error` 直接映射），最近有 `injected` / `held` 的回复 → `running`；
  最近事件是 `reply` 且 90 秒内 → `running`，否则 `done`；WorkBuddy 快照 `state = "error"` 也算 `error`。
- `unread`：该会话在 relay 里 `read = false` 的事件条数。
- `last_ts`：relay 事件时间与源头活动时间中较新的一个；`last_preview`：最后一条助手消息首行（≤120 字）。
- `event_count`：relay 里该会话的事件条数（纯原生会话为 0）。
- `read / pinned / muted / archived`：中继持久化的治理状态（`relay/data/session-state.json`）；`pinned` 排在最前，`muted` 不进收件箱 waiting，`archived` 默认不显示。
- `is_temp`：cwd 位于 `/tmp`、`/private/tmp`、`/var/folders` 等临时目录；`is_empty`：没有任何 user/assistant 条目。默认列表不返回空会话，`?include_empty=1` 才返回。

**事件里的 `session_meta`**：每个事件（`/api/events` 与 SSE `event: message` 的 data）都附带

```json
"session_meta": { "title": "…", "title_source": "agent", "workspace": "…", "project": "…", "branch": "…", "model": "…" }
```

会话元数据发生变化（标题 / 状态 / 未读 / 最后时间等）时，SSE 额外推一条 `event: session`，data 是完整的 `SessionSummary`
（首次只建立基线，不会一次性刷 100 条）。

**只读与降级**：三家的会话文件都按 `(path, size, mtime)` 缓存解析结果，Claude 的 1.1GB 目录只读文件头 96KB + 文件尾 192KB，
不整文件读、不碰 `~/.codex/auth.json`、`~/.claude/credentials*`、`~/.workbuddy/keyblob` 等凭据文件；
任何读取/解析失败都降级为 `null` 字段，不影响事件入库。

## v2 会话治理与收件箱

### `PATCH /api/sessions/<session_id>`

```json
{ "read": true }
```

body 只写需要改的键，可用 `read / pinned / muted / archived`。返回：

```json
{
  "session_id": "…",
  "source": "codex",
  "state": { "read": true, "pinned": false, "muted": false, "archived": false },
  "session": { }
}
```

中继把状态写在 `relay/data/session-state.json`，不修改 agent 自己的文件；tmp + rename 原子写入。
旧中继没有这个接口时返回 404/405/501，App 会退回 `POST /api/read` 和本地缓存。

### `GET /api/inbox`

```json
{
  "waiting": [
    {
      "kind": "approval",
      "session": { },
      "event_id": "123",
      "title": "…",
      "summary": "…",
      "at": "2026-11-09T…",
      "pinned": false,
      "read": false
    }
  ],
  "running": [],
  "recent_done": [],
  "counts": { "waiting": 1, "running": 0 },
  "generated_at": "2026-11-09T…"
}
```

`waiting.kind` 为 `approval` / `needs_reply` / `error`，对应中继的 `permission` / `needs_input` / `error`；
每个会话只按最新一条事件归类，`muted` 和 `archived` 的会话不进 `waiting`。App 在搜索激活或接口不可用时，
会从 `/api/sessions` 与本地事件推导同样的分组。

## 条目流、实时推送、停止与授权

### `GET /api/sessions/<id>/items`（原有 transcript 不变）

把三家源头会话文件里的条目归一化。只读文件尾部 2MB，返回最近 `limit` 条（默认 200，上限 1000）。

**`type` 只用这九个值**：`user | assistant | reasoning | tool_call | file_change | plan | approval | error | system`。
`seq` / `role` / `command` / `exit_code` 等保留为附加字段。

| type | 结构 |
| --- | --- |
| `user` / `assistant` | 用户文本 / 助手文本，`text` |
| `reasoning` | `thinking` 与 Codex `reasoning.summary`，`text`（Claude thinking 只取前 300 字） |
| `tool_call` | 命令执行与工具调用合并成一条，嵌套 `tool: {name, input_summary, output_tail, exit_code, status: running\|done\|failed}`；`tool_use` 与 `tool_result` 按调用 id 配成**同一个 id**，结果到达时用同 id 再推一次 `event: item` 表示更新 |
| `file_change` | `apply_patch` / Claude `Edit` / `Write` 统一，`files: [{path, added, removed, diff}]` |
| `plan` | `plan: [{step, status}]`（Codex `update_plan`） |
| `approval` | 挂起中的授权请求，带 `event_id`、`status: pending\|allowed\|denied\|timeout`，手机据此调 `/api/approvals/<event_id>` |
| `error` / `system` | 错误与系统条目 |

每条可带 `model`（该会话当前模型，来自 Codex `turn_context` / Claude 消息 `model` / WorkBuddy `providerData.model`）。

| 来源 | 映射 |
| --- | --- |
| Codex | `message` → `user`/`assistant`；`reasoning.summary` → `reasoning`；`function_call`(`arguments.cmd`) + `function_call_output` → `tool_call`（`call_id` 配对，解析 `Process exited with code N`）；`custom_tool_call name=apply_patch` → `file_change`（`*** Add/Update File:` 解析 path/added/removed/diff）；`update_plan` → `plan` |
| Claude Code | `text` → `user`/`assistant`；`thinking` → `reasoning`（300 字）；`tool_use`/`tool_result`（按 `tool_use_id` 配对）→ `tool_call`，其中 `Bash` 给 `input_summary`，`Edit`/`Write` → `file_change` |
| WorkBuddy | `message`(role) → `user`/`assistant`；`reasoning` → `reasoning`；`function_call`/`function_call_result`（按 `callId` 配对）→ `tool_call`；识别不了的类型退化为 `assistant` 文本 |

`turns` 是该窗口里看到的回合标记（Codex `task_started`/`task_complete`；Claude/WorkBuddy 以 user/assistant 近似）。

### `GET /api/agents`

三家实际可用的模型清单 + 能力矩阵（只读原生元数据，60 秒缓存）：

```json
{
  "agents": [
    {
      "source": "codex",
      "display_name": "Codex",
      "available": true,
      "capabilities": {
        "reply": true, "new_session": true, "switch_model": true,
        "effort": true, "stop": true, "approve": false, "live": true
      },
      "models": [
        { "id": "workbuddy/deepseek-v4.1-flash", "label": "Deepseek V4.1 Flash",
          "provider": "workbuddy", "is_default": true, "default": true,
          "efforts": ["none","minimal","low","medium","high","xhigh","ultra","max"] }
      ],
      "default_model": "workbuddy/deepseek-v4.1-flash"
    }
  ],
  "generated_at": "2026-11-08T16:00:00.000Z"
}
```

能力取值（与能力矩阵一致）：

| source | reply | new_session | switch_model | effort | stop | approve | live |
| --- | --- | --- | --- | --- | --- | --- | --- |
| codex | true | 有 codex 可执行文件 | true | true | true | 仅在 `~/.codex/hooks.json` 里加了本中继的 `PermissionRequest` 钩子时 true | true |
| claude | true | 有 claude 可执行文件 | true | false | true | 仅在 `~/.claude/settings.json` 里合并了含 `PermissionRequest` 的片段时 true | true |
| workbuddy | true | true | false | false | true | false | true |

- `stop` 表示“对中继启动的回合可停止”（WorkBuddy 通过 ACP `session/cancel`）。
- `models[].efforts`：Codex 给 8 档推理强度，Claude / WorkBuddy 为空数组；`label` 是给人看的短名；
  `is_default` 与旧字段 `default` 同步（兼容老客户端）。
- 模型来源：Codex = `~/.codex/config.toml` 的 `model`（默认项）+ 最近 20 个 rollout 出现过的模型；
  Claude = 最近 30 个会话文件里的 `message.model`；WorkBuddy = 最近 15 个会话 JSONL 里的 `requestModelId`。

### 会话摘要新增字段

`/api/sessions` 与 `/api/sessions/<id>` 都带：

| 字段 | 说明 |
| --- | --- |
| `model` | 会话当前模型 |
| `effort` | Codex 的推理强度（`turn_context.effort`）；Claude / WorkBuddy 为 `null` |
| `permission_mode` | Codex 取 `approval_policy`（回退 `sandbox_policy.type`）；Claude 取行内 `permissionMode`；取不到为 `null` |
| `context_usage` | `{used, limit}`：Codex 取 `token_count` 事件的 `last_token_usage.total_tokens` 与 `model_context_window`；Claude 取最后一条 assistant 的 `usage`（input + cache_read + cache_creation + output），`limit` 为 `null`；整体取不到为 `null` |
| `running` | `status === "running"` 的布尔镜像 |
| `running_since` | 正在运行时的起点（ISO，取该会话最后一条事件时间），否则 `null` |

### 实时推送

- `GET /api/stream?session_id=<id>`：只推该会话的 `message` / `reply_status` / `session` / `item` / `turn`（不带参数则推全部）。
- `event: item`，data `{session_id, item}`；`event: turn`，data `{session_id, state: started|finished|failed, ts}`。
- 增量读取：对正在跑的会话保留 tailer（`事件入库时`或`按 session 订阅时`建立），每 2 秒 `stat` 一次，
  只从上次 offset 读新增字节（丢掉不完整尾行），不重读整文件；10 分钟没动静自动回收。

### 回复指定 model / effort

`POST /api/reply` 增加可选 `model`、`effort`：

| 来源 | 做法 | 状态 |
| --- | --- | --- |
| Codex | `codex exec resume --skip-git-repo-check [-m MODEL] [-c model_reasoning_effort=EFFORT] --all <thread> <text>`（参数数组，仅本轮，不改 `config.toml`） | `injected` |
| Claude | `claude -p <text> --resume <session> --model <model>`；会话源文件 15 秒内还在变化 → 视为被桌面端占用 | `injected` / `held` |
| WorkBuddy | 该版本 ACP 没有切换模型接口：照常按当前模型发送 | `switch_model_unsupported`（带说明） |

### 停止

`POST /api/sessions/<id>/stop` 只停**中继自己起的进程**：`codex exec`（回复注入与 `/api/sessions/new`）、
`claude -p`，或对 WorkBuddy 发 ACP `session/cancel`。其它会话返回 409 `unsupported`。

### 授权（PermissionRequest 挂起）

`POST /api/approvals/<event_id>` `{decision:"allow"|"deny"}`。Claude / Codex 的 PermissionRequest 类钩子会把
HTTP 请求挂起最多 10 分钟（与离开模式同一套机制），手机决定后返回 `{"decision":"allow","reason":"手机批准"}`；
超时返回 `{}`，桌面端照常弹窗；没有挂起 → 404 `not_pending`。

启用步骤（**不替用户改信任状态**）：

1. Claude Code：把 `hooks/claude-settings-snippet.json` 里的 `hooks` 合并进 `~/.claude/settings.json`
   （已含 `Stop`/`Notification`/`PermissionRequest`，`Stop` 与 `PermissionRequest` 的 `timeout` 为 660）。
2. Codex：`~/.codex/hooks.json` 里**追加**一条 `PermissionRequest` 钩子：

   ```json
   { "hooks": { "PermissionRequest": [ { "hooks": [ { "type": "http", "url": "http://127.0.0.1:18787/hook/codex", "timeout": 660 } ] } ] } }
   ```

   追加后新条目是 `PermissionRequest[n]`，Codex 会要求**一次性信任**（或删除
   `[hooks.state."/Users/<user>/.codex/hooks.json:permission_request:n:0"]` 让它在下次运行时重新弹确认）；
   原有 AgentBell 条目的 `trusted_hash` 只有在被改动时才会失配。未启用时 `/api/approvals` 一律返回 `not_pending`。

## 客户端崩溃日志与访问日志

### `POST /api/client-log` / `GET /api/client-log`

手机端崩溃后把堆栈回传到本机（手机没连 adb 时也能拿到）：

```sh
curl -X POST http://127.0.0.1:18787/api/client-log \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data '{"app_version":"0.7.0","ts":"2026-11-08T23:52:10+08:00","session_id":"<id>","device":"OnePlus / Android 15","stack":"java.lang.NullPointerException: ...\n\tat ..."}'
```

- 需要 Bearer；单条上限 **256KB**（超过 413）；非法 JSON → 400。
- 落盘 `relay/data/client-crash.jsonl`（一行一条，追加）。
- `relay.log` 同时记一行摘要：`[client-log] v=0.7.0 ts=… session=… bytes=… exception=<堆栈首行>`。
- `GET /api/client-log?limit=5`（上限 50）返回最近几条，含 `exception`（堆栈首行）。
- App 侧需要在崩溃时自行调用这个接口，relay 不会主动拉取。

### 访问日志

`relay.log` 里对 `/api`、`/f`、`/p`、`/health` 的请求记一行：

```text
[access] 2026-11-08T15:53:58.945Z GET /api/sessions/08fdb08a-… 200 506B 0ms
[access][error] <时间> <方法> <路径> 500 <字节> <耗时>
```

字段依次是：时间、方法、路径（不含 query）、状态码、响应字节数、耗时。查慢请求/大响应：

```sh
grep '\[access\]' ~/Projects/agent-push-poc/relay/relay.log | tail -50
```

## 附件、能力链接、上传、完整对话、新建任务、端口代理

### 事件里的附件

事件入库时（`/hook/*` 与 `/api/reply` 产生的回复事件）会扫描正文里的 Markdown 图片、Markdown 链接、
反引号内容和裸路径，满足以下全部条件才收录：

- 文件存在、是普通文件、大小 ≤ 50MB；
- 真实路径（realpath，解析符号链接后）位于**该事件 cwd** 或**用户主目录**之下；
- 不在凭据位置：`.ssh`、`.gnupg`、`.aws`、`.azure`、`.kube`、`.docker`、`keychains`、
  `.codex/auth.json`、`.claude` 下的凭据文件，以及文件名含 `token` / `secret` / `key` / `credential` 的文件。

每个附件形如：

```json
{ "id": "9f1c…", "name": "pic.png", "mime": "image/png", "size": 136, "kind": "image",
  "url": "/f/5ea3c48f4e3ebd05a2f01aec6202e365/pic.png" }
```

`kind` 取 `image` / `html` / `pdf` / `file`。没有附件时 `attachments` 为 `[]`。

正文里出现 `localhost:<port>` / `127.0.0.1:<port>` 时，事件（含 SSE `event: message`）会附带
`localhost_links: [3456, 5173]`（去重升序；没有就不带这个字段）。

### 能力链接 `GET /f/<token>/<相对路径>`

- token 32 位十六进制，映射 `{root, entry, expiresAt}`，有效期 7 天，持久化在 `relay/data/capabilities.json`；
  同一个 `root + entry` 复用同一个 token；中继重启后旧链接仍可用（启动时会按事件正文自愈补齐缺失的 token）。
- **HTML 附件的 `root` 就是该 HTML 所在目录**，所以页面里的 `style.css`、`assets/pic.png` 等相对资源
  用同一个 token 就能加载；根目录有 `index.html` 时请求目录会自动返回它。
- 不需要 Bearer，但只允许本机/局域网来源；`Content-Type` 按扩展名给出；支持 `Range`（206 + `Content-Range`）与 `HEAD`。
- 防穿越：先 `path.resolve` 再 `realpath`，结果必须落在 `root` 内；拒绝 `..`、反斜杠、NUL；只读 `GET`/`HEAD`。

### 上传 `POST /api/upload`

- 需要 Bearer；两种定位：`?event_id=<id>&name=<文件名>`（按事件 cwd 落盘），或
  **`?cwd=<路径>&name=<文件名>`**（新建任务场景，还没有事件）。`cwd` 必须出现在 `GET /api/projects` 的列表里，
  否则 **403**；两者都不给 → 400。`?name=` 是原始文件名。
- 保存目录：事件 cwd 可写 → `<cwd>/.agent-inbox/uploads/`；cwd 缺失或不可写 → `~/AgentInbox/uploads/`。
- 文件名会清洗（去掉路径、控制字符，非法字符替换为 `_`，截断 120 字符），并加 `YYYYMMDD-HHmmss-` 前缀防重名。
- 超过 50MB 返回 413，body 被丢弃、`.`part` 临时文件清理，中继不会崩。
- 返回 `{ok, path, name, size, mime}`，`path` 是绝对路径，可直接放进 `/api/reply` 的 `files`。

### 带文件回复

`POST /api/reply` 支持 `files: ["/绝对/路径", …]`（最多 20 个，必须存在且是普通文件）。
投递时会在正文末尾追加：

```text
用户从手机上传了文件：
- /private/tmp/demo/.agent-inbox/uploads/20261108-215542-mobile-note.txt
```

Codex 走 `exec resume` 注入，Claude / WorkBuddy 走 Stop 钩子的 `block.reason`，两种方式都带这段说明，
由 agent 自己去读文件。回复记录里的 `files` 可通过 `/api/replies` 查回。

### 完整对话 `GET /api/sessions/<session_id>/transcript`

返回 `{session_id, source, transcript_path, count, messages:[{role, text, ts, attachments}], reason}`：

- **Codex**：按 thread id 在 `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-*-<threadId>.jsonl` 里找（30 秒缓存），
  取 `response_item` 的 `message`，过滤 `<environment_context>` / `# AGENTS.md` / `<skills_instructions>` 等噪声，
  并按 `(role, text)` 去重（resume 会重发整段上下文）。
- **Claude / WorkBuddy**：用 Stop / Notification 钩子载荷里的 `transcript_path`（入库时按 session 记下）。
- 只读；找不到文件或解析失败时返回 `messages: []` 并在 `reason` 说明原因。
- 每条消息的 `attachments` 与事件同一套提取规则（单条最多 10 个）。

### 项目列表与新建任务

- `GET /api/projects`：把事件里出现过的 `cwd` 去重，按最近时间倒序，附 `count` 与出现过的 `sources`。
- `POST /api/sessions/new`：

| source | 做法 | 返回 |
| --- | --- | --- |
| `codex` | `codex exec --json --skip-git-repo-check -C <cwd> <text>` 后台起一轮，从 `thread.started` 拿 thread id | `{ok, session_id, status:"started"}` |
| `workbuddy` | 找到 WorkBuddy 运行时的 ACP 端口 → `/api/v1/acp/connect` → `initialize` → `session/new` → `session/prompt` | `{ok:true, session_id, status:"reused_active", warning:"WorkBuddy 当前版本不能新建会话，已发到当前活跃会话"}`；App 没跑或没有 ACP 端口时 `{ok:false, error}` |
| `claude` | `claude -p <text>`（本机有 `claude` 才支持） | `{ok, session_id:null, status:"started"}` |

参数都是数组传参，不拼 shell；`files` 会像 `/api/reply` 一样追加“用户从手机上传了文件”清单。
返回体统一带 **`cwd`**（手机端可直接拿去上传附件）；`claude` 分支先拿不到 `session_id`（返回 `null`），
此时额外返回 **`request_id`**：该任务的首个钩子事件到达后，relay 会补一条 SSE `event: session`
（data 里同时带 `request_id` 和 `session_id`，其余字段同 `SessionSummary`），手机据此把“新建的任务”和后续会话对上。

> ⚠️ WorkBuddy：该版本 ACP 是**会话级**的，`session/new` 返回的其实是当前活跃会话，`cwd` 会被忽略。
> 所以手机端“新任务”不会新建会话，而是发到用户桌面上正在用的那个会话 —— 接口用
> `status: "reused_active"` + `warning` 明确告知，UI 应当提示用户而不是假装新建成功。
> 自测时不要往用户真实的 WorkBuddy / Claude 会话里注入消息，只用 `/tmp` 下的 Codex 测试 thread。

### 本机端口代理（默认关闭）

`relay/config.json`：

```json
"proxy": { "enabled": false, "ports": [] }
```

改成 `{"enabled": true, "ports": [3456, 5173]}` 后重启 LaunchAgent，`GET|POST /p/3456/xxx` 会反向代理到
`http://127.0.0.1:3456/xxx`（只做 HTTP，30 秒超时，`Authorization` 头不会转发给上游）。

鉴权：需要 `Authorization: Bearer <relay token>`，或 `?cap=<任一有效能力 token>`（浏览器里方便用）。
未开启 → 404；端口不在白名单 → 403。

## 已知限制

- 事件、回覆、調度與離開時段會先追加到對應 JSONL，再寫入 `history.db`（SQLite，主檔與 WAL/SHM 均為 0600）；啟動時從 JSONL replay 修復兩者之間的中斷。已讀水位另存 `session-state.json`，重啟不會恢復成未讀。Android 端仍會對已通知事件 ID 去重，避免重複提醒。
- 明文 HTTP 和通知直接回复仅为 PoC。只应在可信局域网使用，token 不要外传。
- `8787` 属于 `ResetWatch`，本项目不再使用该端口；`/apk` 与钩子都只走 `18787`。
- 手机回复目前只能送达 Codex（`codex exec resume` 续跑该 thread，属于另起进程执行同一 thread）；桌面 App 正在显示该 thread 时需要刷新才能看到新回合。
- WorkBuddy / Claude 的续聊依赖 `Stop` 钩子的阻塞语义：离开模式开启时，若手机一直不回复，该会话的钩子会挂满 10 分钟才返回（agent 端表现为这一轮结束较晚）。
- 在真机上通知动作与 ColorOS 后台保活仍未完整回归（第二阶段只验证到回复送达链路）。
- 附件提取是正则 + 存在性校验，正文里只写了相对路径时按事件 cwd 解析；换行截断的长路径可能提取不到。
- 能力链接一旦泄露，在 7 天内等于该目录的只读访问权；链接只应在可信局域网内转发。
- 端口代理只支持 HTTP，不支持 WebSocket/SSE 长连接与 TLS 上游。
- `POST /api/sessions/new` 的 WorkBuddy 分支依赖 App 运行时的会话级 ACP 端口（App 没有活跃会话时该端口不监听），且只能复用当前活跃会话（`reused_active`）。
- `/api/sessions` 首次扫描会把 Codex rollout 索引與 `session_index.jsonl` 建立快取；本機 97 個會話、32 份 rollout 的實測為第一次約 100ms、之後約 30–100ms，`source=codex` 約 30ms。純原生會話的 `status` 只能給 `done` / `error`（沒有事件流）。
- `items` 只读文件尾部 2MB，超长会话更早的条目不会返回；`plan` 类型目前只有 Codex 的 `update_plan` 会产生。
- Claude / WorkBuddy 没有显式的回合标记，`event: turn` 是用 user / assistant 消息近似出来的。
- WorkBuddy 的 ACP 不支持切换模型（`switch_model_unsupported`），也没有 PermissionRequest 概念。
- 授权挂起依赖用户自己把钩子加进 Claude `settings.json` / Codex `hooks.json` 并完成一次性信任；relay 不会替用户改信任状态。
- 新建任务的附件会先按 `cwd` 上传，因此该目录必须已出现在 `GET /api/projects`；手输一个未登记目录时，会话本身可能能启动，但附件上传会返回 403。
- `/api/sessions` 或 `/api/sessions/<id>` 返回 404 时，客户端会退回事件推导模式；这种模式下 Git 分支、模型和“运行中”等字段无法稳定显示。
- 模型切换、推理强度、停止与授权按钮按 `/api/agents` 的 capabilities 显示：WorkBuddy 不支持切换模型，Claude 不支持推理强度；`/api/sessions/<id>/items` 不可用时仍会退回 transcript / 事件列表。
- 模型切换、推理强度、停止和结构化授权都依赖桌面端 agent 真正支持对应能力；中继返回失败时，客户端会给出原因并退回“把同意 / 拒绝作为文字回复发送”的旧路径。
- 详情页的实时条目依赖 `event: item` / `event: turn`；中继未推送这两类事件时，只在进入详情和手动重载时刷新，不会伪装成实时更新。

## 构建

环境要求：JDK 17、Android SDK 35、Build Tools 35、Gradle 8.14.3（wrapper 已包含）。

```sh
cd ~/Projects/agent-push-poc/android
JAVA_HOME=/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home \
  ./gradlew assembleDebug
```

产物：`android/app/build/outputs/apk/debug/app-debug.apk`，重建后 `/apk` 立刻返回新文件（每次请求都读磁盘）。

### 签名 release 构建（开源发布用）

发布密钥保存在仓库外：

- keystore：`~/.homing-keys/homing-release.jks`（RSA 4096，30 年，别名 `homing`）
- 密码只存在 macOS 钥匙串：服务 `homing-android-release`，账户 `homing`
- 密钥目录和文件权限分别为 `700` / `600`

Gradle 只从环境变量读取签名配置，不落盘密码：

```sh
export HOMING_RELEASE_STORE_FILE="$HOME/.homing-keys/homing-release.jks"
export HOMING_RELEASE_STORE_PASSWORD="$(security find-generic-password \
  -s homing-android-release -a homing -w)"
export HOMING_RELEASE_KEY_PASSWORD="$HOMING_RELEASE_STORE_PASSWORD"
export HOMING_RELEASE_KEY_ALIAS=homing

cd ~/Projects/agent-push-poc/android
./gradlew :app:homingRelease
```

或直接运行 `android/scripts/build-release.sh`，脚本会从钥匙串取密码。
产物为 `android/app/build/outputs/apk/release/app-release.apk`；缺少任一签名环境变量时构建会明确失败。
release 包与 debug 包签名不同，不能覆盖安装到已装 debug 包的手机。
