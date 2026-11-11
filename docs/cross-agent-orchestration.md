# 跨 Agent 調度（2026-11-10）

手機在回覆目前 Agent 時，可以另起一個任務給另一個 Agent（也允許同一 Agent 的另一個 thread）。首版是 **同一台 Mac、同一個工作目錄、平行子會話**，不是把 `@Claude` 原文塞進 Codex，也不是假造一個「已派發」提示。

## 手機上的用法

```text
先修正登入按鈕
@claude 檢查登入流程並補測試
@codex 重跑完整測試
```

- 第一個有效 mention 之前的文字，仍然回覆目前的父會話。
- 從一個 mention 到下一個 mention 之間的文字，是該 Agent 的子任務。
- 只有 mention、沒有父回覆時，只建立子任務，不向父會話發送空訊息。
- `@kimi` 等已知但沒有新會話啟動器的 Agent 不會出現在選單；手機若手動輸入會標成「目前不可調度」並阻止送出，直接呼叫 API 則得到 `409 agent_not_dispatchable`。
- 電子郵件、行內程式碼與 fenced code block 裡的 `@...` 不會被當成調度。
- 附件首版只跟隨父會話回覆，不會複製到子任務。

解析器在 Android 的 `AgentMentionParser.kt`。輸入框的 `@` 按鈕只是插入語法，使用者仍可直接手打 `@claude`。

## 後端流程

```text
手機回覆 Codex
  ├─ 父文字 → POST /api/reply → Codex 原來的話題
  └─ @Claude 任務 → POST /api/dispatch
                       ├─ 保存 parent_session_id / parent_event_id
                       ├─ 用同一個 cwd 啟動新的 Claude 會話
                       └─ 回傳 dispatch_id / target_session_id / request_id
```

父會話與子會話是兩個獨立 Agent 會話。手機的父會話時間線會多一張「跨 Agent 子任務」卡，子會話就緒後可點開；子會話頂部也會顯示「來自 Codex 的跨 Agent 任務」，可直接跳回來源會話。子會話仍可在一般會話列表中操作。

### `POST /api/dispatch`

需要正常的 Bearer 權限。

```json
{
  "parent_session_id": "01a1...",
  "parent_source": "codex",
  "parent_event_id": 123,
  "target_agent": "claude",
  "text": "檢查登入流程並補測試",
  "cwd": "/Users/me/Projects/demo",
  "mode": "parallel",
  "preset": "review",
  "workspace": "readonly",
  "dry_run": false
}
```

- `parent_session_id` 或 `parent_event_id` 至少要有一個；父事件存在時會以事件上的 session/source/cwd 為準。
- `cwd` 可省略，會從父事件或父會話摘要取得；若顯式提供，必須與父會話的目錄一致（首版不跨專案）。
- `mode` 首版只接受 `parallel`。
- `target_agent` 必須是 `/api/agents` 中 `capabilities.dispatch = true` 的 Agent。首版只有 Codex 與 Claude Code。
- `preset` 可選 `ask` / `do` / `review` / `handoff`，預設 `ask`。`ask`、`review`
  會把支援的 Agent 以唯讀權限啟動；`do` 使用一般權限；`handoff` 不回傳結果卡。
- `workspace` 可選 `readonly` / `shared` / `worktree`。`worktree` 只對 git 倉庫生效；
  `readonly` 會傳給支援的 Agent（例如 Claude 的 `plan`、Codex 的 `read-only`），
  不會只在卡片上顯示唯讀而實際放行寫入。
- `dry_run: true` 只做參數、父會話、目標 Agent 與工作區驗證，回傳準備中的
  dispatch，不寫入 `dispatches.jsonl`、不建立 worktree、也不啟動子進程。

成功回應：

```json
{
  "ok": true,
  "dispatch_id": "dsp_c584...",
  "parent_session_id": "01a1...",
  "parent_source": "codex",
  "parent_event_id": 123,
  "target_agent": "claude",
  "target_session_id": "6f1d...",
  "request_id": "",
  "status": "started",
  "mode": "parallel",
  "dispatch": {}
}
```

啟動失敗會保留 `dispatch_id` 與失敗記錄，HTTP 不是 200。錯誤碼：

| HTTP | code | 原因 |
|---|---|---|
| 400 | `target_required` / `text_required` / `parent_required` / `cwd_invalid` / `mode_unsupported` | 參數不完整或目錄不可用 |
| 401 | — | 未帶有效 Bearer |
| 404 | — | 父事件或父會話不存在 |
| 409 | `agent_not_dispatchable` | 該 Agent 沒有新會話啟動器 |
| 413 | `text_too_long` | 任務超過 12,000 字元 |
| 502 | `launch_failed` | 執行檔不在、服務未開或啟動器回報失敗 |

### 狀態

| status | 意思 |
|---|---|
| `starting` | 關聯已保存，啟動器尚未回報 |
| `started` | 已有子會話 ID（Codex 直接回傳；新版 Claude 由 `--session-id` 預先指定） |
| `waiting_session` | 進程已啟動，但 Agent 尚未回傳 session id；等待對應 hook |
| `failed` | 沒有假裝成功，錯誤在 `message` |

Claude Code 支援 `--session-id` 時，Homing 會在啟動前產生 UUID 並直接回報，子會話不必等待 hook。較舊的 CLI 沒有這個選項時，才暫存 `request_id`，在該 cwd 的第一個同來源 hook 事件到達時綁定，並透過 SSE `dispatch` 事件更新父會話卡。

### `GET /api/dispatches`

```text
GET /api/dispatches?parent_session_id=<id>&target_agent=claude&status=started&limit=100
```

回傳 `{ "dispatches": [...], "count": n }`，最新的排前面。這個列表不含子會話正文；正文仍由原本的 `/api/sessions/<id>/items` 或事件流讀取。

## 持久化

調度記錄在資料目錄的 `dispatches.jsonl`，是一行一筆的 append-only 檔，同一 `dispatch_id` 最後一行生效。它不改變 Node 相容的 `events.jsonl` 與 `replies.jsonl` 格式。

本輪新增：

- `internal/orchestrator/`：驗證、狀態機、持久化關係。
- `internal/relayapi/dispatch.go`：HTTP 路由與 Codex / Claude 啟動器。
- `internal/mcpserver/`：固定父會話的 stdio MCP 橋接，只暴露 `homing.dispatch`。
- Android `AgentMentionParser.kt`、`AgentDispatch`、`/api/dispatch` 客戶端與會話內子任務卡。

## 給 Agent 的 MCP 入口

不讓任何 Agent 直接取得本機 Bearer token，也不讓它自行指定父會話。必須由使用者
在啟動 Agent 時明確掛上一個固定父會話的橋接：

```sh
homing mcp \
  --parent-session 01a1... \
  --url http://127.0.0.1:18787
```

也可以使用 `HOMING_MCP_URL`、`HOMING_MCP_PARENT_SESSION`、
`HOMING_MCP_PARENT_SOURCE` 與 `HOMING_MCP_TOKEN` 環境變數。未提供 token 時，
程式只讀取 `config.json` 的 owner token，並要求檔案權限不可對 group/world 開放。

MCP 只提供一個工具：

```json
{
  "name": "homing.dispatch",
  "arguments": {
    "target_agent": "claude",
    "task": "檢查登入流程並補測試",
    "preset": "review",
    "dry_run": false
  }
}
```

- 父會話固定在啟動參數，工具參數不能改父會話；啟動前會用
  `/api/sessions/{id}` 驗證來源，設定 `--parent-source` 時再做額外比對。
- 只接受回環 URL，拒絕 userinfo、query、fragment、非根路徑與非回環重定向。
- 任務文字裡的 `@` 不會被再次解析成其他 mention；工具也不能讀會話正文、
  回覆父 Agent、轉交結果或自動注入。
- `ask` / `review` 映射唯讀工作區，`do` 映射一般工作區，`handoff` 不等待結果卡。
- 工具輸出不包含 token；`dry_run` 只回傳 dispatch 準備結果，不宣稱已啟動。
- MCP annotations 將 `destructiveHint` 標為 `true`，因為 `do` 可能修改工作區；
  客戶端應在執行前要求使用者確認。

## v1 邊界（仍保留相容）

- 只支援真的能建立獨立新會話的 Codex、Claude Code；其他 Agent 的目錄與 hook 套件不等於可調度。WorkBuddy 目前的新會話 API 會重用當前活躍會話，為避免把任務誤送進使用者正在用的對話，暫不列入。
- v1 只做一層父子關係，不做遞歸調度鏈、排隊、重試或自動回傳結果給父 Agent。
- 新版 Claude CLI 會用預先產生的 UUID 嚴格對應子任務。只有舊版 CLI 的 hook 後備路徑仍以同 cwd 最早啟動者先綁定。
- v1 的 `@` 解析由手機做；直接用 `/api/dispatch` 的客戶端要自己把 target 與 task 分開。v2 改用 `/api/compose` 的服務端解析。
- 工作目錄必須是父會話可見的同一台 Mac 路徑；不支援遠端機器或容器內路徑映射。

v2 已補上結果回傳、`collab_result`、追問／轉交／取消、worktree 隔離與 server-side mention 解析；WorkBuddy 仍因「新會話會重用活躍對話」而刻意排除。ACP 可讓更多 Agent 逐步升格，但只有完成握手與單回合驗證的條目才能宣告可完整驅動。

## v2：`POST /api/compose`（2026-11-10）

v1 的 `/api/dispatch` 與 `GET /api/dispatches` 保留；手機與控制台的新流程改用 `/api/compose`，
一次提交「父回覆 + 多個 `@` 任務」，由服務端解析，避免三端各寫一套 mention 規則。

```jsonc
{
  "session_id": "01a1...",
  "source": "codex",
  "text": "先修正登入按鈕\n@claude:review 檢查登入流程並補測試\n@codex:do 重跑完整測試",
  "files": ["/Users/me/.homing/uploads/shot.png"],
  "collab": { "context": ["recent","files"], "workspace": "auto", "return": "card" },
  "dry_run": false
}
```

- `dry_run: true` 只回傳 `plan`，不寫回覆、不建立 dispatch，供手機顯示「這條會發給誰」。
- 任一 mention 不合法、不可調度或工作目錄失效時，整個請求回 409／400，父會話不會先收到訊息。
- 執行順序固定為「驗證全部 → 準備 worktree → 啟動全部子任務 → 發送父回覆」。子任務啟動失敗時，父回覆不會送出，當次已建立的 worktree 與 branch 會清理；尚未啟動的 worktree 也一併移除。
- `permission_mode` 在建立任何分支、worktree 或子進程前驗證；不支持的模式直接 400，不會由父 Agent 的 CLI 自行拒絕。
- 解析規則、`@agent:preset`、email／行內代碼／fenced code 的忽略規則都與 Android
  `AgentMentionParser.kt` 共用同一組測試語義；服務端結果為準。
- `ask` / `do` / `review` / `handoff` 四個 preset 決定工作區與回傳方式：
  `ask`、`review` 預設唯讀共享目錄；`do` 在 git 倉庫建立 `homing/<dispatch_id>` worktree；
  `handoff` 固定 `return: none`，即使 `collab.return` 傳 `card` 也不回傳結果；父會話的
  協作卡會標記「已移交給 @Agent」。
- worktree 建立時保存 `base_commit`；完成後 `dispatch.result.diff_stat` 帶
  `git diff --stat <base_commit>`，手機與控制台在結果卡顯示改動規模。
- 上下文包由 `recent`、`files`、`diff` 三段組成，總長上限 12,000 字元；取不到的段落省略，
  不編造正文。

### 逐 mention 調整與手機標籤

手機輸入框會把每個有效的 `@agent[:preset]` 顯示成可點擊的標籤。點開後可以直接改：

- 預設：咨詢、動手、審查、交棒。改預設只重寫輸入框裡的 mention token，不改任務文字。
- 協作線：自動、新開、接續最近一條。`continue` 沒有可用協作線時，dry-run 回
  `409 continue_target_missing`，不會先發父訊息或建立子任務。
- 背景：最近對話、改動文件、git diff，可以任意組合或全部取消。

客戶端把標籤設定按 mention 順序放進 `mentions` 覆蓋陣列；服務端仍以文字解析出的
mention 為準，覆蓋項只用來調整 plan。`agent` 可以不填，填了就必須與同一位置的
mention 相符。沒有這個陣列時沿用 `collab.context`；明確傳空 context 表示不要背景。

```jsonc
{
  "session_id": "01a1...",
  "text": "先確認範圍\n@claude:review 檢查登入流程\n@codex:do 跑測試",
  "dry_run": true,
  "collab": { "context": ["recent", "files"], "workspace": "auto" },
  "mentions": [
    { "agent": "claude", "target": "new", "context": ["diff"], "workspace": "readonly" },
    { "agent": "codex", "target": "new", "context": [], "workspace": "shared" }
  ]
}
```

接續既有協作線時，`workspace` 傳 `auto` 或省略會沿用原協作線的工作區；要覆蓋時才傳
`shared`、`readonly` 或 `worktree`。覆蓋項和 mention 的數量不必完全相等，但多出
mention 數量的項目會回 `400 mention_override_invalid`。

### 狀態與後續動作

子會話 hook 事件會更新 dispatch：

`starting → started / waiting_session → running → completed`

授權、提問、錯誤會進入 `needs_you`；完成時把最後回覆與改動文件寫入
`dispatch.result`，並在父會話寫一條 `collab_result` 事件，推送同名 SSE。

| 接口 | 作用 |
|---|---|
| `POST /api/dispatches/{id}/forward` | 把子任務結論回覆給父 Agent |
| `POST /api/dispatches/{id}/reply` | 在原子會話追問，狀態回到 `running` |
| `POST /api/dispatches/{id}/cancel` | 停止 Homing 啟動的子會話或標記取消 |
| `POST /api/dispatches/{id}/discard` | 確認後移除已結束的 Homing worktree 與 `homing/dsp_…` 分支 |

手機的協作卡已完成：執行中顯示取消／打開會話；完成後顯示結論、改動文件數、分支，
可「追問」「轉交」或打開子會話。`workspace: worktree` 的任務結束後，手機與控制台
都有「丟棄」入口；移除前必須確認，只刪 `~/Library/Application Support/Homing/worktrees/`
底下的 checkout 與對應的 Homing 分支，不碰主工作區。已丟棄的記錄會保存
`worktree_discarded_at`，重複請求不會再刪任何東西。

「轉交」在手機與控制台都會先開可編輯文字框；有 worktree 分支時預填合併指示。
分支名另有「複製分支」按鈕，方便直接貼到父 Agent 或自己的終端。

### 自動轉交（`return: inject`）

自動轉交預設關閉。控制台「設定 → 常規 → 自動轉交子 Agent 結論」提供明確開關，
API 為 `GET/PATCH /api/home/settings` 的 `collab_return`（`card` 或 `inject`）。

- 未開啟時，請求明確帶 `return: inject` 會回 `403 collab_inject_disabled`，
  不會建立 dispatch 或先送出父訊息。
- 開啟後，未指定 `return` 的新任務預設使用 `inject`；客戶端仍可逐次指定
  `card` 或 `none`。
- 子任務完成時先寫 `collab_result`，再把「【@子 Agent 的結論】＋最終回覆」
  作為父會話回覆送出；最終回覆最多保留 8,000 字。
- 同一子會話可能連續送來 `reply`、`done`、`stop`；完成處理以
  `dispatch.result != nil` 去重，並以進程內互斥鎖序列化，自動轉交只發生一次。
- 自動轉交失敗不改變「子任務已完成」的事實；dispatch 訊息會記錄失敗原因，
  用戶仍可在結果卡按「轉交」重試。

### 安全

- `/api/compose`、`/api/sessions/new`、`/api/dispatch` 每台設備每分鐘最多 20 次。
- 設備 `scopes` 為 `read` / `reply` / `drive`；缺少 `drive` 時派發與新建回 403，
  缺少 `reply` 時回覆與授權回 403。沒有 `scopes` 欄位的舊設備視為三者全有。
- `audit.jsonl` 只記錄時間、設備、動作、會話／dispatch id、目標 Agent 與目錄，
  不記錄正文；權限 0600。

## 驗證

```sh
go test ./internal/orchestrator ./internal/relayapi ./internal/store ./internal/api
cd android && ./gradlew :app:testDebugUnitTest \
  --tests dev.agentpush.poc.AgentMentionParserTest \
  --tests dev.agentpush.poc.SessionDetailRenderTest
```

已用隔離資料目錄與 `/tmp` 測試專案實際啟動過一個新 Codex thread，取得 `target_session_id` 後立即 `/api/sessions/<id>/stop`；沒有使用或改動任何真實會話。

### 目前實作狀態（2026-11-10）

- `/api/dispatch`、`/api/dispatches`、`/api/compose`、forward / reply / cancel 與父會話 `dispatch`／`collab_result` 事件都已接到 Go 服務；Android 與控制台都有子任務卡、來源會話入口與結果操作。
- Claude 等待 hook 綁定時，`claimLaunch` 只取同 source／cwd 最舊的一筆 pending，其他候選保留給後續事件。這個行為有 `TestClaimLaunchUsesOldestPendingForSameAgentAndCwd` 回歸測試，避免第一筆 hook 一次吃掉多個等待中的 dispatch。
- 手機目前仍連 Node 18787，而 Node 沒有 `/api/compose` 與 `/api/dispatch`；切換到 Go 之前，手機上的 cross-agent 選單只能算預覽，不能當成已可用。切換仍須由使用者確認，不能為測試停掉 18787。
- 可完整派發的路徑現在有兩類：Codex／Claude 走各自的 CLI 啟動器；其餘有 ACP 規格的
  Agent 走通用 `internal/driver`。內置掃描器（Kimi、OpenCode）即使沒有實作
  `SessionStarter`，也會由 `relayapi` 的通用回退接到 ACP，因此不會為了加驅動而
  覆蓋原本的會話讀取器。
- 目前內建 ACP 規格：Hermes、OpenClaw、Kimi、DeepSeek Harness、Gemini CLI、
  OpenCode、Cline、GitHub Copilot CLI、Kiro CLI、OpenHands、Goose。除 Hermes／
  OpenClaw／Kimi／DSH 之外的條目標為 experimental；本機沒有安裝的執行檔只會顯示
  「未安裝」，不會被當成可用。`--help` 自檢可要求輸出包含 `--acp` 等旗標，缺少
  旗標時如實失敗；真正啟用仍以隔離目錄中的 ACP 握手為準。
- WorkBuddy 仍刻意排除：它的新會話 API 會重用使用者正在使用的活躍對話，不能拿來
  當跨 Agent 子任務的安全啟動器。
- 本輪用隔離 `HOME`、資料目錄與假的 `kimi acp` 子進程做過端到端驗證：
  `POST /api/sessions/new` 建出 ACP 工作階段，回合收到
  `HOMING_FAKE_OK`；`POST /api/dispatch` 同樣走通用回退，完成後在
  `/api/dispatches` 取得 `result.text`，`/api/home` 也出現 `collab_result`。
  這驗證的是通用路徑，不代表本機真的安裝了 Kimi 或已完成其真實登入。
- 尚未 commit／push；工作區內含多位 Agent 的本輪未提交改動。
