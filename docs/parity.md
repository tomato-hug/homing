# Go / Node 对照：怎么跑，哪些差异是故意留的

切换前，Go 版（`homing serve`）的 `/api/*` 要和 Node 中继（`relay/server.mjs`，127.0.0.1:18787）给手机的内容一致。这份文档记录对照方法和已经确认过、不需要再查的差异。

## 怎么跑

在仓库根目录：

```bash
bash scripts/verify-go.sh
```

脚本依次做：`go build` / `go vet` / `go test` → 把 `~/Library/Application Support/Homing` 拷一份到 `/tmp/homing-parity` → 用这份快照在 127.0.0.1:18788 临时起一个 Go 服务 → 跑 `scripts/parity.sh` 和 `scripts/diag-sessions.sh` → 停掉临时服务。它只读 Node 那一侧，不动真实数据目录。18788 被占用时会直接退出，先关掉手动起的 `homing serve`。

结果在 `.verify/`（已忽略，不进 git）：

| 文件 | 内容 |
|---|---|
| `go-verify.log` | 整次运行的输出 |
| `parity-full.log` | 每个接口的逐字段 diff |
| `diag-sessions.log` | 两边会话列表的差别：只在一边出现的、`is_empty` 不同的、`last_ts` 不同的 |
| `go-serve.log` | 临时 Go 服务的日志 |

对照前先确认 Node 进程是当前代码：`go-verify.log` 里会打印 Node 的启动时间和 `relay/server.mjs` 的修改时间，启动时间更早就先重启（`launchctl kickstart -k gui/$(id -u)/dev.agentpush.relay`）。

对照时自己正在用的 Codex 会话还在写入，`limit=10&include_empty=1` 这类取到边界条目的接口可能出现一次性的 `last_ts` / `title` / `last_preview` 差异。看 `diag-sessions.log` 的「两边都有但 last_ts / event_count 不同」：如果差的只是当前会话，不是代码问题，重跑即可。

## 故意保留的差异

这些项在 `parity.sh` 里仍然显示 DIFF。每一项都查过原因，结论是 Go 的行为正确或更好，不照搬 Node。

| 接口 | 差异 | 原因 |
|---|---|---|
| `/api/sessions` | Go 把一些会话算作非空（`is_empty: false`），Node 算作空并默认隐藏 | Node 的 `isSessionEmptyUncached` 把 `readRecentItems()` 的返回值当数组用，实际是 `{items, turns}`，于是退化成"没有 `last_preview` 就算空"。Go 按注释里的本意判断：有 user / assistant 条目就不算空。切换后手机上会多出这些会话（多数是测试用的短会话） |
| `/api/sessions` | 个别 Codex 会话 Go 有 `cwd`、`last_preview`，Node 是 null | Node 的 rollout 索引没找到这些会话的文件，Go 找到了 |
| `…/items`（Codex） | Go 不显示 developer 角色的上下文消息（`<app-context>`、`<multi_agent_mode>` 等），因此 `codex-iN` 的编号比 Node 小 | Node 把非 assistant 的消息一律当作用户消息显示，这些是 Codex 注入的上下文，不是用户说的话 |
| `…/items` | 工具参数摘要（`input_summary`）的键顺序不同 | Go 的 `encoding/json` 按字母序输出 map 的键，Node 保持原顺序。内容相同 |
| `…/items` | 长输出的 `output_tail` 和 `exit_code` 不同 | Node 先把输出截到前 800 字再取尾部和退出码，长输出时取到的是开头、退出码丢失。Go 取真正的尾部 |
| `…/items` | Node 的 `output_tail` 偶尔是 `[object Object]` | Node 把对象直接转成了字符串。Go 输出文本内容 |
| `…/items` | Codex 工具结果没有退出码时，Node 写 `exit_code: null`，Go 省略该字段 | 客户端把缺失和 null 都当作没有退出码；Go 只在真实拿到退出码时输出 |
| `/api/inbox` | 同一事件在 Go 回傳 `read: false`，Node 可能仍是 `true` | Go 用「已看到的最後事件 id」判斷，回覆或開啟會話後的新事件會重新算未讀；這是修正 Node 一次性已讀的已知缺陷 |
| `/api/connection` | Go 增加 `cloud`，並把雲端／自建中繼選項標為可用 | Go 已具備 connector、加密 RPC、事件轉發與 `file.chunk`；舊版 Node 仍寫「即將推出」 |

没有实现、也不挡切换的两个接口：`POST/GET /api/client-log`、`GET /api/sessions/{id}/transcript`。安卓端对它们的 404 有容错。

## 控制台

Go 版现在内嵌 `web/console/` 并提供 `/console`（只对本机回环地址开放，HTML 下发 HttpOnly `homing_local` cookie，前端路由回落到 `index.html`）。控制台启动时依赖的接口已经补齐：

`/api/connection`、`/api/stats`、`/api/logs`、`/api/setup`、`/api/agent-paths`、`/api/autostart`、`/api/push/pause`、`/api/pair/offer`、`/api/pair/latest`、`/api/pair/offer/{id}`、`/api/pair/claim`、`/api/pair/cancel`。

Agent 页也已补齐：`/api/agents/settings`、`/api/agents/{id}/settings`、`/api/agents/order`、`/api/agents/{id}/hooks`、`/api/agents/{id}/test-push`、`/api/agents/{id}/disconnect`。设备列表用的 `/api/devices` 由 `internal/relayapi` 提供。

Agent 页的目录改为读 Go 版新增的 `/api/agent-catalog`（安装 / 移除在 `/api/agent-catalog/{id}`，接入套件在 `/api/agent-catalog/{id}/kit`，一键接入在 `/api/agent-catalog/{id}/quick`）。Node 中继没有这组路由，所以 `parity.sh` 不比较它；catalog 安装只写 `~/.homing/agents/<id>.yaml` 和 Homing 自己的设置，套件只写 `~/.homing/integrations/<id>/`，不碰 agent 的真实配置。只做事件通知的 agent 标记为 `readiness: hook`，不会被当成会话 scanner；未核实的 draft 也不会被自动加载。快速接入同样不执行第三方安装命令，移除套件只改名留痕。

`/api/adapters` 现在同时返回控制台需要的 `builtin` / `installed` / `custom` / `community` / `formats` / `root` 形状，以及旧的 `items` / `dirs` / `counts` 字段；`formats` 只列真正有解析器的 `jsonl` / `json` / `sqlite`。`POST /api/adapters` 和 `/api/adapters/test` 同时接受 YAML 和控制台物件。Hook 安装只自动改 Claude Code / WorkBuddy 的 `settings.json` 并留 `.bak-homing-*` 备份，Codex 仍只返回手工片段，不改信任状态。`test-push` 已接到真實 LAN／雲端通知通道：成功回 `delivered` 數量，沒有手機可送時回 503，不再回 placeholder。

跨 Agent 调度是 Go 版新增能力：`POST /api/dispatch`、`GET /api/dispatches`、`POST /api/compose`、dispatch 的 `forward`／`reply`／`cancel` 和 `dispatches.jsonl` 在 Node 中继里没有对应实现，`parity.sh` 不比较这组路由。手机要实际使用该能力，必须连到运行新 Go 版的端口；Node 18787 仍在服务时不会出现这些接口。

`POST /api/replies/{id}/cancel` 也是 Go 新增的后相容路由：只撤回仍在 Agent 記憶體佇列中的回覆，已開始執行的回合要走 `/stop`。目前由 Codex 實作 `agent.QueuedReplyCanceller`；不支援的 Agent 回 `409 reply_cancel_unsupported`，不會假裝成功。`replies.jsonl` 仍維持 append-only，撤回會追加一筆 `status: "cancelled"`。

`GET /api/devices` 現在合併 `devices.json`（舊設備令牌）與 `cloud/peers.json`（端到端配對）。
新增 `transport: "token" | "e2e"`；端到端項目的 `paired_via` 是 `e2e`，可在同一個
`DELETE /api/devices/{id}` 移除。端到端配對沒有舊式 read/reply/drive scopes，控制台
不會顯示可切換的權限按鈕；嘗試 PATCH scopes 會回 400，而不是假裝保存。

Go 版另有本地歷史層：事件、回覆、調度與離開時段先寫 JSONL，再寫 `history.db`。JSONL 仍是相容／匯出格式，啟動時 replay 進 SQLite；事件已讀標記存 SQLite 的獨立列，重啟或 replay 都不會把已看過的事件改回未讀。資料庫主檔與 WAL/SHM 都是 0600，`homing serve` 收到 SIGINT/SIGTERM 後會先 drain HTTP，再 `Deps.Close()` 讓 SQLite 正常收尾。這層不改變任何 `/api/*` 的 JSON 欄位，所以不在 Node 逐字段對照範圍內。

Go 版另新增：`GET /api/home`（注意力三区）、`GET /api/search`（标题／项目／路径，`scope=content` 时再查最近 300 条正文）、`GET /api/sessions/{id}/commands`、`/export`、`GET /api/fs/complete`。这些是向后兼容的新路由，不影响 Node 对照；旧 Android 对 404 有容错。

`GET /api/connection` 现在多返回 `cloud`（`configured` / `connected` / `server` / `peers`），并把 `cloud`、`self_hosted` 选项标为可用；`PUT /api/cloud/config` 可在不重启服务的情况下保存或清除中继地址。Node 版没有这组能力，旧客户端不受影响。连接页只在 `server` 已配置时显示「已配置」，实际连上后才显示「已连接」，不会把可配置误写成已连接。

即时流 `item` / `turn` 已补齐：会话页带 `session_id` 订阅 `/api/stream` 时，Go 每秒轮询该 Agent 的 `ItemReader`，只发布 `seq` 增加的新条目，闲置两个轮询周期后发 `turn: idle`。第一版没有引入 fsnotify；Codex／Claude 的文件写入频率下 1 秒延迟可接受，后续可在 `internal/live` 内换成文件偏移量跟踪而不改 SSE 契约。

SSE 续传与大体量条目也已补齐：

- 单会话 `/api/stream?session_id=…` 的帧带 `id:`，断线重连会读 `Last-Event-ID`；`/api/sessions/{id}/items` 支持 `after_seq`，两者都能从最后成功帧继续。
- 单帧超过 48 KB 时，即时事件只带截断预览并标 `truncated: true`，完整内容仍由 `/items` 取回；驱动合并中的增量帧标 `partial: true`。Android 与控制台都不会用较短的预览覆盖已载入的完整文字、工具输出或 diff。
- 全局 `/api/stream` 的 `item` 事件保留实际 `session_id`，按会话分别去重；不能把所有会话共用同一个 `seq` 水位，否则一个会话的条目会误删另一个会话的增量。
- 云端加密连接现在转发 `message`、`reply_status`、`item`、`turn`、`session`、`dispatch` 与 `collab_result` 命名事件；`notify` 保持原通知通道，避免同一事件重复推送。

控制台左上「新会话」现在是完整的新建流程，不再跳回 Agent 页。它只列出
`capabilities.new_session=true` 的 Agent，支持最近项目或手动 cwd、模型、推理强度、
权限模式、任务正文与附件；送出走 `POST /api/sessions/new`。桌面是居中对话框，
760 px 以下是底部弹层。模态内有 Tab 焦点循环，`Esc`／取消／点遮罩可关闭；关闭后
焦点回到新会话按钮。模态打开时其它工作台快捷键暂停，实测 `⌘Enter` 不会穿透到
背后的父会话输入框。

### 2026-11-10 回歸補強

- `/api/reply`、`/api/sessions/new`、`/api/compose` 會在讀寫任何會話或啟動子進程前校驗 `permission_mode`。模式必須出現在該 Agent 的 `/api/agents.permission_modes`；不支持的值回 `400 permission_mode_unsupported`，不會穿透到 CLI。
- `/api/compose` 是全有或全無：先驗證所有 mention、建立需要的 worktree，並以 `orchestrator.Prepare` 做無副作用額度檢查；全部成功後才啟動子任務與發送父回覆。任一階段失敗時，父訊息不送出，當次與尚未啟動的 worktree／branch 會清理。
- `api.WriteJSON` 先緩衝完整 JSON 再寫 `Content-Length`；編碼失敗不會留下半截 200 回應。這不改變 JSON 欄位，只修正串流式編碼在中途失敗時的協議問題。
- 控制台協作頁先讀調度紀錄並立即繪製，父會話標題在背景補齊；輪詢有 in-flight 防重入，不再因 500 條會話掃描把首屏卡住。
- 控制台工作台以 `item.id` 就地插入／替換 DOM，普通刷新用 `after_seq` 增量，重連才做整份補齊；1000 條會話與 2000 條正文分批渲染上限已生效。瀏覽器驗收時 18788 載入 97 個會話，390 px 寬度仍可切換列表與正文。
- 控制台工作台已補 `⌘K` 聚焦搜尋、`J / K` 切換會話、`E` 歸檔、`⌘Enter` 發送；歸檔同時保留可見按鈕，快捷鍵不是唯一入口。
- 控制台會話工作台已補可收起的資訊抽屜：工作目錄、分支、模型、推理強度、權限模式、
  上下文用量、事件數、已讀位置、協作線、匯出與歸檔。超寬螢幕（>1560 px）以第四欄
  停靠；1440 px 與手機寬度改為右側覆蓋，避免正文被壓成窄條。`Esc`、關閉按鈕與
  遮罩皆可收起，關閉後焦點回到入口。
- 跨 Agent 結果的 `return: inject` 已補上控制台設定開關，並由
  `/api/home/settings.collab_return` 持久化。未開啟時明確請求 `inject` 回 403；
  完成事件重複到達時以既有 `dispatch.result` 去重，只轉交一次。
- `workspace: worktree` 的協作完成後，控制台與 Android 可用
  `POST /api/dispatches/{id}/discard` 確認清理 Homing 自己建立的 worktree 與
  `homing/dsp_…` 分支；接口只接受資料目錄 `worktrees/` 下的路徑，執行中任務回
  409，成功後寫入 `worktree_discarded_at` 與 `audit.jsonl`。自動轉交
  （`return: inject`）也會留下 forward 審計記錄，但不保存結論正文。
- `do` 建立的 worktree 會保存 `base_commit`；子任務完成時把
  `git diff --stat <base_commit>` 寫入 `result.diff_stat`，控制台與 Android
  在結果卡顯示，不再只給文件數與分支名。
- 控制台工作台的輸入區已支援附件選檔與拖放，上傳走既有 `/api/upload?cwd=…`，
  送出時把本機路徑放進 `/api/compose.files`；附件列可逐項移除，沒有 cwd 的會話
  會明確拒絕上傳。
- 設定頁已接上 `GET /api/search/status` 與 `POST /api/search/index`：可看索引大小、
  會話／正文數與更新時間，並執行開啟、關閉、重建、刪除。狀態查詢不會為了讀數字而
  新建資料庫；刪除是「關閉並刪除」，避免重新整理時立刻重建空索引。這是 Go 新增的
  管理契約，Node 中繼沒有對應路由。

### 2026-11-10 手機操作補齊

- 會話標題可從標題或右上選單重命名，使用 `PATCH /api/sessions/{id}` 的 `title`；後端回傳 `title_source: "homing"`，Android 與舊 `custom` 值都會視為使用者命名。
- 排隊回覆在輸入框上方顯示「已排隊 N 條」，可撤回。只有服務端確認的 `reply_id > 0` 會顯示；Codex 的記憶體佇列可精確按 id 移除，Android 本地尚未送出的 optimistic 項目不會被誤算。
- `HomingHomeScreen.kt` 已按 M8 拆成 `AwayBar`、`HomeAttentionZone`、`HomeResumeZone`、`HomeEarlierZone`；舊 `InboxScreen.kt` 不再路由，`LastCrashCard` 已移到 `CrashCard.kt` 後刪除。
- 手機輸入含 `@agent` 時，發送按鈕先呼叫 `/api/compose` 的 `dry_run`，在輸入框上方展開「父訊息／每個子任務的 Agent、預設、工作區與任務文字」預覽，確認後才真正發送；設定頁「協作 → 發送前預覽」可關閉。無 mention 的回覆仍直接走 `/api/reply`。
- 手機的 `@Agent` 標籤現在可逐個調整預設、新開／接續、背景段與工作區（共享／唯讀／
  worktree）；改預設只替換輸入框中的 mention token，不改任務文字。`/api/compose` 以
  `mentions` 覆蓋陣列按順序套用，dry-run 會回傳每個 mention 最終的 target、context 與
  workspace；continue 找不到可接續協作線時整個請求回 409，不會半送出。
- 配對多台電腦時，手機透過雲端 RPC 分別讀取每台 `/api/home`，再合併
  attention／running／resume 並為會話加上電腦標籤；點擊卡片會先切換 active
  computer 再導航。雲端事件按 active computer 過濾，沒有電腦標籤的舊事件仍兼容。
  同一 `session_id` 出現在不同電腦時，以 `(session_id, computer_id)` 保持獨立。
  配對成功後電腦會把 `lan_servers`（可能有多個 IPv4 位址）連同
  `pair.accepted` 交給手機；`CloudNotificationClient` 對每台電腦的每個候選位址
  各建一條 WebSocket，RPC 優先用已連上的候選，失敗時在剩餘 timeout 內換下一個。
  同一事件可能同時從雲端與局域網到達，手機以 `from:id` 在 10 分鐘、2000 筆的
  有界窗口內去重，重複幀仍 ACK。舊版配對資料沒有 `lan_servers` 時，仍只走原本
  的雲端或單一局域網位址；要取得多位址 fallback 需重新配對或由新服務轉送。

### 隔離驗收（2026-11-10）

- 在 18791、全新 `HOME` 與資料目錄下執行一次完整快速接入：DeepSeek Harness 生成 manifest、hook、wrapper 與草稿，透過生成的 hook 發送一筆 `demo-session-quick-1` 事件成功；移除時只改名到 `.deleted-*`，不刪使用者內容。`dsh` 執行檔不在本機，因此能力位仍正確回報不可啟動。
- `homing agents drive-test hermes` 在 `/tmp` 新目錄啟動後回報 `ACP dependencies not installed`，本輪不安裝第三方套件；`openclaw acp` 完成 `initialize` 與 `session/new`，但 30 秒內沒有等到 `session/prompt` 回覆。兩者都不能據此宣稱 57 家 Agent 都可完整驅動。

### 2026-11-11 會話顯示與推理隱私

- `/api/sessions/{id}/items`、單會話 `/api/stream`、`/export` 與雲端 `item` 轉發預設都
  不帶 `reasoning`／`thinking`；只有明確帶 `include_reasoning=1` 才回傳。這不是刪除
  資料，原始 rollout／transcript 仍保留思考段落，本機全文索引也不受影響。
- 控制台是明確的桌面檢視器，因此在取正文、SSE 與 Markdown 匯出時自行帶
  `include_reasoning=1`；Android 預設不帶，右上選單的「查看思考過程」才會再發一次
  帶 opt-in 的請求，「隱藏思考過程」只從目前已載入清單移除，不增加伺服器負擔。
- Android 對舊 Node 中繼有客戶端防護：即使伺服器忽略 query，未訂閱的 reasoning 仍會
  在 REST 合併、SSE `item` 與本機快取還原時被丟棄。Node 中繼也同步實作相同預設，
  但正在服務手機的舊進程要重啟後才生效。
- Agent 原生標題只在 `title_source=agent` 時清理 `标题：`、`会话标题：`、`Title:` 等
  包裝與 Markdown 標題前綴；`homing`／`custom` 的使用者命名完全不動。Go API、Node
  中繼與 Android 顯示層共用同一規則，因此尚未切換到 Go 的手機也能拿到乾淨標題。
- 控制台會話列、正文作者列與工作台標題使用 `@lobehub/icons-static-svg` 的原始品牌
  標誌；Android 把 Codex、Claude、WorkBuddy、Kimi、DeepSeek Harness、OpenCode、
  Gemini CLI、CodeBuddy、Cursor、Copilot CLI、Qwen Code、Cline、Windsurf、Kiro
  的 24×24 path 內嵌成 Compose Canvas 圖標（EvenOdd 填色），未知 Agent 才回退
  品牌字母，不再用數字或通用機器人圖示冒充。

### 2026-11-11 T3 Code 移動端與寬屏控制台

- 參考 `pingdotgg/t3code` commit `d2c25950`，只借鑒互動模式，不搬 React Native 元件。
  完整清單、刻意不搬的範圍與後續候選見 `docs/mobile-t3-code.md`。
- Android 會話底部新增待處理 dock：目前授權／等待輸入固定在輸入框上方，兩個以內選項
  直接操作，更多選項才展開；原時間線卡仍保留，dock 不會取代或自動送出。
- Android 會話內搜尋可查已載入的訊息、事件、工具輸出、檔案路徑與計畫步驟，最多 50 條，
  點結果會跳回時間線；未訂閱 reasoning 時搜尋不會額外抓思考。
- Android 時間線把連續的 `tool_call`（例如 `apply_patch`、`exec_command`）合併成一個
  「N 個工具步驟」摘要列，預設不渲染命令參數、輸出與退出碼；點摘要列才展開原本的
  工具卡。失敗或執行中的步驟仍在摘要列標紅／標記，最終回覆、授權與錯誤不受影響。
- 工具過程分組只改顯示層，不刪資料；會話內搜尋仍能命中已折疊的命令與輸出，搜尋結果
  會跳到對應的工具過程摘要。
- 專案列表支援右滑置頂、左滑歸檔與 Undo；長按快捷選單仍是可見的替代入口。搜尋、
  Agent 篩選與分組控制改成按需展開，不再常駐佔用三行工具列。
- 首頁「等你處理」預設只列三件，其餘可展開；離開模式改為頂欄按鈕，不再使用一張
  大型狀態卡。
- 排隊回覆改成可管理的 sheet：可逐條撤回或全部撤回；編輯時先成功撤回舊回覆，才把
  文字放回輸入框，避免同一訊息重複排隊。
- 控制台寬屏斷點改為 1500 px 起固定三欄／資訊欄、1700 px 起加寬會話樹。1500 px
  以上正文 900–940 px、13.5–14.25 px；資訊抽屜同步提高字級。1500–1560 px 會覆蓋
  舊的覆蓋式抽屜規則，避免留下空白第四欄。開始正文搜尋時自動收起詳情欄。

### 2026-11-11 注入相容、輸入框與收件箱單一化

- Codex 回覆注入改走 `codex queue --thread … --message … --resume`，由 Codex
  app-server 的 thread store 接單，不再用第二個 `codex exec resume` writer 與桌面
  Codex 爭用同一條 thread。缺少常駐 daemon 時會自動啟動。
- `codex queue --resume` 在回合執行期間可能一直保持子進程存活；Go 與 Node 都只等
  800ms 探測立即失敗，之後回報 `injected` 讓手機離開排隊狀態，同時保留子進程至回合
  結束。稍後若進程真的失敗，回覆狀態會再更新為 `failed`；不支援 `queue` 的舊 CLI
  才回退到序列化的 `codex exec resume`。
- Android 會話輸入框改成全寬多行（2–6 行），模型／推理強度／權限移到上方可橫向
  捲動的狀態列；`@ Agent`、附件、語音與發送放在輸入框下方工具列，不再與兩個狹窄
  的文字框互相擠壓。
- Android 只保留一個「收件箱」模式：頂欄與會話頁按鈕共用同一個
  `MobileInboxPreferences`，並把開關同步到後端 `/api/away`；不再另外提供離開模式
  卡片或第二套狀態。歷史會話只有在此模式開啟後的新活動，或使用者明確設定的常駐
  會話，才會出現在手機列表。
- 專案會話列的置頂／歸檔滑動提示只在手勢位移時繪製；列內容使用不透明卡片並
  `clipToBounds()`，避免未滑動時提示文字與標題、時間重疊。

### 2026-11-11 新建任務的 Agent 在線狀態

- Android「新建任務」不再使用寫死的 Codex／Claude／WorkBuddy 清單，而是讀
  `/api/agents`，只列出真正聲明可新建的 Agent。
- 「可新建」的判定與後端副作用條件一致：`available=true` 且
  `capabilities.new_session=true`、`capabilities.new_session_ready=true`。
  `available` 對一般 Agent 表示執行檔可被偵測，
  對 WorkBuddy 表示桌面端進程正在運行，因此不能只看 `installed`。
- 在線 Agent 排在前面並顯示綠色狀態點與「在線」；離線 Agent 仍保留在橫向清單中，
  但降低透明度、顯示「離線」且不可點擊，方便使用者知道缺少哪一家接入。
- 頁面每 20 秒重新讀取目錄，也可用標題列刷新按鈕立即重試；若目前選中的 Agent
  在刷新後離線，會自動切到第一個在線 Agent。沒有任何在線 Agent 時，啟動按鈕不可用。
- 離線卡片仍可能位於橫向 `LazyRow` 的畫面外；Robolectric 測試必須先
  `performScrollToNode`，且卡片內狀態文字要用 `useUnmergedTree=true` 讀取。

### 2026-11-12 新建能力 fail-closed

- Node 的 scanner 條目（OpenCode、ZCode）只負責讀取，沒有啟動器時明確回
  `new_session=false`、`new_session_ready=false`；Android 缺少這兩個欄位時也預設
  不可新建，避免把「資料庫存在」誤判成「目前在線、可啟動」。
- WorkBuddy 5.9+ 已移除舊 `/api/v1/acp/connect`，而且舊橋的 `session/new` 會
  重用目前活躍會話。兩端因此都停止宣告及執行 WorkBuddy 新任務，保留讀取、回覆
  與事件能力；控制台顯示「已接入 · 新建未就緒」。
- `/api/sessions/new` 對 WorkBuddy 回 `409 new_session_not_ready`，對其他沒有
  驅動的 Agent 回 `409 agent_not_driveable`，不再回容易誤導的 source 白名單錯誤。

### 2026-11-11 排隊回覆的送達確認

- `injected`／`held` 是已送達狀態，不能被較晚到達的舊 `queued` 快照降級。
  Node 在寫入狀態與啟動回放 `replies.jsonl` 時都遵守這個單向規則；Go Store
  的 `UpdateReplyStatus` 與 `latestReplies` 同樣保留已送達狀態。
- Codex `queue --resume` 可能在 800ms 探測後才輸出 `failed to resume` warning，
  但訊息已交給 Codex。Node 只看探測前的 warning 決定是否排隊，探測後即使命令
  關閉也不再降回 queued。
- Agent 對同一會話產生新的 `reply` 事件時，代表至少最舊的一條 queued 回覆已被
  消費。Node 與 Go 會把它改成 `injected` 並推送 `reply_status`；啟動時也會用
  歷史事件做同樣的一對一修復，避免舊資料永久留在「排隊管理」。
- Android 的 HTTP 回執與 SSE 可能亂序：SSE 先到時會把負數的樂觀 reply id
  重映射成伺服器 id；較晚到達的 queued 回執、`/api/replies` 快照都不能覆蓋
  `injected`／`held`／`cancelled`／`failed`。`switch_model_unsupported` 表示
  已用目前模型送出，也按已送達處理，不再當成 queued。

## 新发现一处差异时

先在 `relay/server.mjs` 里找到对应逻辑，判断是 Go 写漏了还是 Node 本身有问题。Go 写漏了就改 Go；Node 有问题就在上表加一行，写清原因。不要为了让对照变绿去复制 Node 的 bug。
