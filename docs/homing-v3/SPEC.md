# Homing v3 開發需求文檔：跨 Agent 會話管理平台

> 日期：2026-11-09　狀態：**已定案**（用戶於 2026-11-09 確認按建議執行，決定記在 §11.1；只剩 `internal/orchestrator` 的負責人待指定）
> 讀者：參與開發的 coding agent（Codex、WorkBuddy、DSH）。動手前先讀 `AGENTS.md` 和 `docs/go-architecture.md`，本文不重複那裡的提交規則和禁止事項。
> 本文只做方案設計，沒有改任何代碼。文中「已核實」指 2026-11-09 在本機讀代碼或跑命令確認過；「待核實」指只有文檔或幫助輸出為依據，實作前必須先驗證。

---

## 0. 一頁摘要

**要做成什麼**：Homing 是跨廠商的 agent 會話管理層。手機端是專業的會話管理端（在手機上得到和電腦前一樣的會話體驗），電腦端是平台化的管理端（對標並超越 Magpie 的會話與 Agent 管理），核心亮點是在任一 agent 的會話裡 `@` 其他 agent 參與協作。

**現在在哪**：Go 後端（本機服務、適配器、協議、雲中轉）已成形，全部測試通過；安卓端 0.7.0 有收件箱、會話詳情、回覆、授權、新建任務；上一輪新增了第一版跨 Agent 調度。但有四個缺口擋在目標前面：

| # | 缺口 | 後果 |
|---|---|---|
| 1 | 手機仍連 Node 中繼（18787），Go 版在 18788 沒有切換 | 上輪做的 `@agent` 調度在手機上用不到 |
| 2 | 手機走雲端時只能收通知，沒有完整 API 通道 | 離開家裡 Wi‑Fi 就看不了會話、回不了消息 |
| 3 | 離開模式只是一個開關，沒有「離開時段」；收件箱按「每會話最新事件 + 24 小時」分桶 | 做不出「離開期間有新消息的會話優先、舊會話折疊」 |
| 4 | 新建、停止、調度這些能力按 agent id 寫死在路由裡（只有 Codex / Claude / WorkBuddy） | 「普遍存在的各家 agent」只能讀、不能操作，也不能被 `@` |

**怎麼走**（五個階段，可部分並行）：

| 階段 | 主題 | 交付後用戶得到什麼 |
|---|---|---|
| P0 | 地基：提交、補即時流、切換到 Go | 手機連上 Go 版，`@agent` v1 可用 |
| P1 | 離開模式收件箱 + 遠程全功能 | 在外面也能看會話、回覆、授權；首頁按「等你處理 / 離開期間新動態 / 繼續工作 / 較早」分區 |
| P2 | 協作 v2 + 通用驅動層（ACP） | `@agent` 的結果回到原會話；Hermes、OpenClaw 等更多 agent 可被新建、停止、`@` |
| P3 | 電腦端會話工作台 + 桌面殼 | 電腦上能看正文、回覆、派發、看協作關係圖；托盤面板 |
| P4 | 平台化 | 資源庫、多台電腦聚合、全文搜尋、自動更新 |

---

## 1. 產品定位與設計原則

### 1.1 定位

- **Homing 管會話，不管模型**。Magpie 的核心是模型網關（provider、路由、額度）；Homing 不代理模型請求，只做三件事：收集各家 agent 的會話與事件、讓人在任何地方處理它們、讓 agent 之間能協作。
- **廠商自家的行動端只管自家**（Claude App 的 Code 分頁、ChatGPT App 的 Codex Remote）。Homing 的價值在跨家：一個收件箱、一套操作、一個協作語法。
- **本機優先**。代碼、憑證、會話文件不離開用戶的電腦；手機是加密遙控器。

### 1.2 設計原則（評審方案時以此為準）

1. **手機端等價，不是手機端簡化**。電腦上能對會話做的事（看正文和工具調用、回覆、停止、授權、切模型、看 diff、派發）手機都要能做；差別只在版面。§6.3 有逐項對照表。
2. **首頁只放「需要你」的東西**。等你處理的、離開期間新出現的排在最上；你自己在電腦前推進的會話不算新動態；舊會話折疊。
3. **新動態區和工作區分開**。「別人（agent）找你」和「你要去找 agent（新建 / 繼續）」是兩種意圖，版面上不能混在一張列表裡。
4. **能力如實呈現**。一個 agent 能通知、能讀、能回、能驅動、能被 `@` 是五件不同的事，介面按實際能力開關功能，不假裝（延續 `docs/agents.md` 的做法）。
5. **加一家 agent 不改 UI**。新 agent 靠清單或驅動配置接入，手機和控制台零改動。
6. **協作可追溯、不自動越權**。子任務的來源、上下文、結果都有記錄；結果預設以卡片回到父會話，由人決定是否轉交給父 agent。

---

## 2. 當前進度梳理

### 2.1 各模組狀態（已核實）

`go build ./...` 通過；`go test ./...` 全部通過（2026-11-09，Go 1.27.3）。

| 模組 | 位置 | 規模 | 狀態 | 說明 |
|---|---|---|---|---|
| 本機 HTTP、收件箱、會話、事件流 | `internal/api` | 3.9k 行 | 完成（G1） | 全後端共 78 條路由；與 Node 對照只剩 `docs/parity.md` 記錄的故意差異 |
| 相容層：鉤子、上傳、新建、停止、授權、離開模式 | `internal/relayapi` | 3.1k 行 | 完成 | 新建只支援 codex / claude / workbuddy（`sessions.go:52`） |
| 內置 agent 掃描器 | `internal/agent/*` | 5.4k 行 | 完成 | Codex、Claude Code、WorkBuddy、Kimi、OpenCode、ZCode |
| 自定義接入：清單、腳本適配器、事件上報、catalog、接入套件 | `internal/adapter` | 5.7k 行 | 完成（G2） | catalog 57 家：6 內置、5 已核實清單、13 hook、33 未核實 |
| 狀態存儲 | `internal/store` | 0.7k 行 | 完成 | JSONL + JSON 文件；事件只在記憶體留最近 5000 條（`store.go:283`） |
| 通知分級 | `internal/notify` | 0.3k 行 | 完成 | P0 立即 / P1 120 秒合併 / P2 60 秒摘要 |
| 協議 v1、雲中轉、局域網 `/v1/ws`、配對 | `internal/protocol` `internal/server` `internal/cloud` | 6.4k 行 | 完成（G3） | 電腦端已實現加密 RPC 和事件轉發（`cloud/rpc.go`、`connector.go:427`）；文件分塊（`file.chunk`）未見實現 |
| 跨 Agent 調度 v1 | `internal/orchestrator` `relayapi/dispatch.go` | 0.7k 行 | 完成（本輪新增） | 只有 `parallel` 模式，只支援 codex / claude，不回傳結果 |
| Agent 設置與鉤子安裝 | `internal/agentconfig` | 0.6k 行 | 完成（本輪新增） | |
| 桌面殼 | `internal/gui` | 6 行 | **未開始（G4）** | 只有 `doc.go` 和托盤圖標素材；直接跑 `homing` 會提示「界面版還沒完成」 |
| TUI、自動更新、發布流水線 | — | — | **未開始（G5）** | |
| 控制台 | `console/` → `web/console/` | 8.4k 行 | 部分 | 原生 TS + Vite，13 個頁面。收件箱頁能授權和回覆；**會話頁是唯讀列表**（`pages/sessions.ts`，117 行），看不了正文 |
| 安卓端 | `android/` | 22.6k 行（含測試） | 0.7.0（versionCode 9） | 三個分頁：收件箱 / 項目 / 我的；會話詳情含工具卡、diff、計劃、模型面板、`@` 輸入 |
| 資源庫 | `docs/resource-library.md` | — | 只有設計 | |

運行現狀：`127.0.0.1:18787` 上監聽的是 **Node 進程**（LaunchAgent `dev.agentpush.relay`），Go 版沒有常駐。

### 2.2 本輪新增但尚未提交的工作

`git status` 有 81 條變更路徑（55 個已追蹤文件相對 HEAD +6491 / −503，另有 24 個未追蹤的文件或目錄；不含本文）。主要內容：

- 後端：`internal/orchestrator/`、`internal/agentconfig/`、`internal/agent/kimi/`、`internal/adapter/catalog*.go` 與 `catalogdata/`、`relayapi/dispatch.go`、`api/agentsettings.go`。
- 安卓：`AgentMentionParser.kt`、會話頁的子任務卡、`/api/dispatch` 客戶端。
- 控制台：Agent 頁重做、`agent-icons.ts`。
- 文檔：`cross-agent-orchestration.md`、`resource-library.md`、`integrations/`。

這些工作互相依賴，建議各目錄負責人在 P0 第一步各自提交，後面的階段才有穩定基線。

### 2.3 與目標的差距

按對目標的阻礙程度排序。每條都給了代碼依據。

**G1　手機沒連上 Go 版。** Go 版的 `/api/dispatch`、`/api/agent-catalog` 在 Node 上不存在（`docs/parity.md`）。此外 Go 版缺一段 Node 有的功能：Node 會跟蹤會話文件並向 SSE 廣播 `item` / `turn` 事件（`relay/server.mjs:4731-4743`），安卓端也在處理這兩種事件（`AgentViewModel.kt:111-112`）；Go 版只發 `message`、`session`、`dispatch`、`reply_status`（全倉 `PublishNamed` 只有這三處命名事件）。切換後會話頁會失去即時刷新。

**G2　手機在雲端通道上只有通知。** `CloudNotificationClient.kt` 的註釋寫明它只接收加密通知；安卓代碼裡沒有任何 `rpc` 請求的發送方。所有會話操作都走 `AgentApi.kt` 的局域網 HTTP。離開模式的典型場景恰恰是人不在局域網內。

**G3　離開模式沒有時間維度。** `store.Config.Away.Enabled` 是布林值（`store.go:110`），沒有開始、結束時間。收件箱（`api/inbox.go`）按每個會話的最新事件分成 waiting / running / recent_done，recent_done 固定取 24 小時。沒有「上次看到哪」的水位，也就分不出「離開期間新增」和「你在電腦前自己推進的」。

**G4　會話級已讀是一次性的（疑似缺陷，待核實）。** `PATCH read:true` 後，`buildInbox` 會跳過該會話的 waiting 事件（`inbox.go:104`），而新事件寫入時不會重置這個標記（`store.go:264-288`）。結果是已讀過的會話之後再請求授權，可能不再出現在「等你處理」。安卓端有本地補償（`clearReadForNewContent`），但服務端語義本身需要修正。§5.3 的水位模型會一併解決。

**G5　操作能力寫死在 agent id 上。** 新建會話（`relayapi/sessions.go:52`）、調度目標（`dispatch.go:23`）、能力位（`api/agents.go:243-290` 的 `dispatchableAgent` / `newSessionFor` / `switchModelFor` / `coreAgent`）都是 `switch id`。`agent.Agent` 接口只有 `Replier`、`ModelLister`、`Watcher` 幾個可選能力，沒有「啟動」「停止」「響應授權」。catalog 裡 57 家 agent 中只有 3 家能從手機新建會話、2 家能被 `@`。

**G6　協作是單程的。** 調度 v1（`docs/cross-agent-orchestration.md`）明確不做：結果回傳、上下文傳遞、排隊重試、多層。`@` 的解析只在安卓端（`AgentMentionParser.kt`），控制台無法共用。子任務和父會話在同一個工作目錄並行寫文件，沒有隔離。

**G7　電腦端不是會話工作台。** 控制台是設置與接入的管理台，沒有會話正文、沒有回覆框、沒有按項目分組、沒有正文搜尋、沒有協作視圖。與 Magpie 的 Sessions 頁相比功能缺一大截，離「超越」更遠。

**G8　安卓端沒有本地持久化。** `AgentStore.kt` 是記憶體狀態加 SharedPreferences（只存標記和診斷）。冷啟動必須等網路；弱網時首頁是空的。另外 `SessionScreen.kt` 單文件 2778 行，會話頁要加協作條、命令面板、diff 全屏這些功能前需要先拆分。

---

## 3. 調研結論

### 3.1 廠商自家的行動端（單會話設計的參考）

**Claude（Claude App 的 Code 分頁）**——依據官方文檔 [Claude Code on mobile](https://code.claude.com/docs/en/mobile)：

- 沒有獨立 App，手機只是客戶端，代碼不在手機上跑。四種連接對象：雲端會話、Projects、Remote Control（連本機會話）、Dispatch（發任務給桌面端）。
- Remote Control：本機跑 `claude remote-control` 或在會話裡 `/remote-control`，手機掃碼或在列表裡選會話。
- 推送：長任務結束或需要決策時推送；也可以在提示詞裡要求「測試跑完通知我」。
- 權限模式可在手機切換（Manual / Accept edits / Plan / Auto），不能選 Bypass。
- 附件：照片存到 `~/.claude/uploads/` 並把路徑告訴 Claude；其他文件下載到本機後以 `@` 文件引用傳入。
- 限制：只在終端有效的命令（`/plugin`、`/resume`）手機不可用。

第三方報導（非官方、未逐條核實）提到：會話列表用電腦圖標加綠點標在線會話；2026 年 8 月起列表頂部有每台電腦一張的「設備卡」，點卡選目錄即可在那台電腦上新建會話；有第三方應用批評官方列表顯示的是無意義的 ID，並把「在列表上直接批准工具調用」作為賣點。

**Codex（ChatGPT App 的 Remote）**——依據 OpenAI 的公告 [Work with Codex from anywhere](https://openai.com/index/work-with-codex-from-anywhere/) 和第三方指南（後者未核實）：手機覆蓋所有 thread 而不是單個任務，可批准命令、查看輸出、切換模型、發起新工作；diff、終端輸出、截圖、測試結果即時串流到手機；代碼和憑證不離開主機；掃碼配對加中轉。主機端只能是桌面 App，CLI 用戶無法發起配對。

**可借鑒的單會話設計**：

| 做法 | Homing 的對應 |
|---|---|
| 手機是監督者，執行留在主機 | 已是如此 |
| 列表上直接處理授權 | 已有（收件箱授權卡）；擴展到任意會話行的滑出操作 |
| 設備卡 → 選目錄 → 新建 | 「繼續工作」區的「新會話」流程：電腦 → 項目 → agent |
| 權限模式可切、Bypass 不開放 | §5.7：新建和回覆時可選權限模式，不提供繞過 |
| 附件落盤後傳路徑 | 已有（`/api/upload` + `DeliveryText`） |
| 即時串流 diff 和終端輸出 | §5.2 即時條目流 |
| 「跑完通知我」 | §6.7：會話級「完成時提醒我」開關 |

**它們沒有的**（Homing 的空間）：跨廠商收件箱、跨 agent 協作、離開時段、自建中轉。

### 3.2 Magpie（電腦端的對標）

倉庫已有兩份詳細拆解（`docs/magpie-ui-analysis.md`、`docs/magpie-deep-dive.md`），本機裝的是 `/Applications/magpie.app`。結論只列與本方案相關的：

- 一個程序四種外殼：窗口、菜單欄面板、TUI、Web。Homing 的 Go 架構（`docs/go-architecture.md` §2）已經照此規劃。
- Sessions 頁：agent 單選分段 → 按工作目錄分組 → 預設折疊 → 空會話不列 → 每批 100 條；搜尋含正文；動作只有刪除（回收站）、遷移目錄、導出 Markdown、換 agent 繼續。
- **Magpie 沒有**：未讀、收件箱、置頂、靜音、回覆、授權、通知聚合、手機端。它只讀會話，不操作會話。

「超越 Magpie」的具體含義因此是：在它的結構性收斂（分組、折疊、空會話過濾、正文搜尋）之上，加上它沒有的**操作**（回覆、授權、派發）和**注意力管理**（未讀、離開時段、協作關係）。

### 3.3 第三方多 agent 手機端

搜尋到 Happy（MIT，自建中轉，Claude / Codex 等）、Paseo、T3 Code、Nimbalyst、Claude Code UI 等。資料多來自各家自己的對比頁，可信度有限，只取兩點共識：

- 多數是「單 agent 會話的遙控器 + 基礎列表」，少數做了看板。沒有看到以「離開時段」組織收件箱的產品。
- 沒有看到在一個會話裡 `@` 另一家 agent 並把結果帶回來的產品；多 agent 並行通常靠看板或 worktree 並排。

### 3.4 ACP（Agent Client Protocol）——驅動層的關鍵

依據 [協議概覽](https://agentclientprotocol.com/protocol/overview)、[傳輸](https://agentclientprotocol.com/protocol/transports)、[Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)、[Tool Calls](https://agentclientprotocol.com/protocol/tool-calls)、[Agents 列表](https://agentclientprotocol.com/get-started/agents)：

- **傳輸**：agent 作為客戶端的子進程，stdio 上跑換行分隔的 JSON-RPC 2.0（一行一條，消息內不得有換行）；stderr 是日誌。HTTP 傳輸尚未定稿。
- **客戶端調 agent**：`initialize`、`authenticate`、`session/new`、`session/prompt`；可選 `session/load`（需 `loadSession` 能力）、`session/set_mode`；通知 `session/cancel`。
- **agent 調客戶端**：`session/request_permission`（選項的 `kind` 為 `allow_once` / `allow_always` / `reject_once` / `reject_always`，回覆 `{outcome:"selected", optionId}` 或 `{outcome:"cancelled"}`）；可選 `fs/*`、`terminal/*`；通知 `session/update`。
- **`session/update` 的種類**：`user_message_chunk`、`agent_message_chunk`、`agent_thought_chunk`、`tool_call`、`tool_call_update`、`plan`、`available_commands_update`、`current_mode_update`、`config_option_update`、`session_info_update`、`usage_update` 等。`tool_call` 帶 `toolCallId`、`title`、`kind`（read / edit / delete / move / search / execute / think / fetch / switch_mode / other）、`status`（pending / in_progress / completed / failed）、`content`（文本、`diff{path, oldText, newText}`、終端）。
- **回合結束**：`session/prompt` 的回應帶 `stopReason`（`end_turn` / `max_tokens` / `max_turn_requests` / `refusal` / `cancelled`）。
- **支援面**：官方列表有 51 家，含 Claude（經 `claude-agent-acp` 適配器）、Codex CLI（經 `codex-acp`）、Gemini CLI、Cursor、Cline、Copilot、Goose、Kimi CLI、OpenCode、OpenClaw、Hermes Agent、Qwen Code、Kiro 等。
- **本機實況（已核實）**：`hermes acp` 和 `openclaw acp` 子命令存在；`claude` 2.1.90 有 `--session-id`、`--resume`、`--output-format stream-json`、`--permission-mode`；`codex` 0.167 有 `exec --json`、`exec resume`、實驗性的 `app-server`。兩個 ACP 適配器的 npm 包本機沒裝。握手本身尚未實測。

ACP 的事件模型和 Homing 的 `agent.Item` 幾乎一一對應，授權請求也能直接映射到現有的授權掛起機制。這意味著一個通用 ACP 客戶端就能讓一大批 agent 具備「新建、續聊、停止、授權、即時流、被 `@`」的全套能力，而不用每家手寫啟動器。

### 3.5 對方案的啟示

1. 首頁按「離開時段」組織是差異化設計，沒有現成規格可抄，需要自己把規則定死（§5.3）。
2. 驅動層採「雙軌」：Codex 和 Claude 繼續用已經跑通的 CLI 驅動（不需要用戶額外安裝）；其餘 agent 走通用 ACP 驅動（§5.4）。
3. `@` 協作要從「另起一個會話」升級為「結果回到這個會話」，這是和所有競品拉開距離的地方（§5.5）。
4. 電腦端先補會話工作台，再做桌面殼；殼只是容器，價值在工作台（§7）。

---

## 4. 總體方案

### 4.1 架構

```
┌─────────────── 手機（Android）────────────────┐      ┌──────── 電腦端外殼 ────────┐
│ 收件箱 │ 會話 │ 我的                           │      │ 窗口 │ 托盤面板 │ /console │
│ Transport：局域網 HTTP+SSE ⇄ 雲端加密 RPC      │      └────────────┬───────────────┘
└───────────────┬────────────────────────────────┘                   │ 同一套 /api
                │ 協議 v1（端到端加密）                              │
        ┌───────▼────────────────────────────────────────────────────▼───────┐
        │                        homing（Go，本機）                          │
        │  api ── home/attention ── live ── orchestrator ── notify ── store  │
        │                     │            │                                 │
        │              agent 註冊表   driver（cli │ acp │ http）             │
        └─────────┬───────────┬────────────┬─────────────────────────────────┘
                  │讀文件     │鉤子/事件   │子進程 / 本地服務
        Codex · Claude Code · WorkBuddy · OpenCode · Kimi · Hermes · OpenClaw · 清單接入的其他 agent
```

新增四個後端部件：`home`（注意力與離開時段）、`live`（即時條目流）、`driver`（通用驅動層）、升級後的 `orchestrator`（協作 v2）。

### 4.2 能力階梯

每個 agent 在每一級上獨立成立，`/api/agents` 如實報告，UI 據此開關功能。

| 級 | 名稱 | 含義 | 判定依據 | 現況 |
|---|---|---|---|---|
| L0 | 通知 | 能收到它的生命週期事件 | 鉤子 / `/api/ingest` / `/hook/generic` | catalog 的 hook 條目 |
| L1 | 讀取 | 能列會話、讀正文 | 實現 `ListSessions` + `ItemReader` | 6 內置 + 5 清單 |
| L2 | 回覆 | 能往已有會話發消息 | 實現 `Replier` | Codex、Claude、WorkBuddy、OpenCode（需 serve） |
| L3 | 驅動 | 能新建、停止、響應授權、即時串流 | 實現 `SessionStarter` 等（§5.4） | 寫死的 3 家 |
| L4 | 協作 | 能作為 `@` 的目標並回傳結果 | L3 + 能拿到回合結束與最終回覆 | 寫死的 2 家，且不回傳 |

### 4.3 核心概念

| 概念 | 定義 |
|---|---|
| 水位（seen） | 每個會話一個「用戶已看到的最後事件 id」。任何 Homing 客戶端打開會話、回覆、授權，都會推進水位 |
| 新動態 | 事件 id 大於水位的事件 |
| 離開時段（away period） | 一次離開模式從開啟到關閉的時間段，有 id、開始、結束、觸發來源 |
| 等你處理（needs_you） | 會話存在未解決的授權、提問或錯誤 |
| 協作線（collab thread） | 父會話裡針對某個目標 agent 的一條子任務線，可多輪 |

---

## 5. 後端需求（Go）

每項標了規模（S ≈ 1 天內、M ≈ 數天、L ≈ 一週以上，僅供排序參考）和建議負責目錄。接口都是**新增**，現有接口和字段不改不刪，舊版 App 繼續可用。

### 5.1 B1　切換到 Go（P0，前置）

1. **各負責人提交本輪工作**（§2.2）。提交前按 `AGENTS.md` 跑 `go build ./...`、自己包的測試、gitleaks。
2. **補即時條目流的最小版**（詳見 §5.2 的 P0 範圍），否則切換後會話頁退化。
3. **核實 `notify` 事件在 Go 的 `/api/stream` 上是否送達手機**。Node 有 `event: notify`（`docs/relay-v2.md` §3）；Go 的通知走 `service.deliver → push`（`service.go:139`），SSE 上沒看到同名事件。安卓的 `PushService.kt:406` 在處理它。缺則補。
4. **跑 `scripts/verify-go.sh`**，差異只允許是 `docs/parity.md` 表裡的項。
5. **切換**（要先問用戶，`go-architecture.md` §6 第 4 步）：停 LaunchAgent `dev.agentpush.relay` → 裝 Go 版開機啟動 → 端口改回 18787。保留回滾腳本：重新 `launchctl load` 原 plist。

驗收：手機不改任何設置，重啟 App 後收件箱、會話頁、回覆、授權、新建全部可用；在會話裡輸入 `@claude …` 能建立子任務卡。

### 5.2 B2　即時條目流 `internal/live`（P0 最小版 → P2 完整版，M）

**目標**：會話頁的條目在 1 秒內出現在手機和控制台，不靠輪詢。

**接口不變**：沿用 Node 的 SSE 事件名和形狀。

```
event: item    data: {"session_id":"…","item":{…agent.Item…}}
event: turn    data: {"session_id":"…","state":"running|idle","ts":"…"}
```

**來源一：文件跟蹤（P0）。** 對滿足以下任一條件的會話跟蹤其會話文件：有客戶端帶 `session_id` 訂閱了 `/api/stream`；會話處於 running。上限 20 個同時跟蹤，按最近活動淘汰。

- 實現：新增可選接口 `agent.ItemTailer`，返回會話文件路徑與「從偏移量增量解析」的函數；Codex 和 Claude 的解析器已有按文件抽取條目的函數（`claude.ExtractFileItems`、Codex 的 rollout 解析），改成可從偏移量續讀。用 `fsnotify`（新依賴）監聽，失敗時退回 1 秒輪詢 `stat`。
- SQLite 類（OpenCode / ZCode）不跟蹤文件，維持「有 serve 時走事件流、否則 3 秒輪詢 `ReadItems` 比對尾部」。
- 條目 id 必須和 `GET …/items` 返回的一致（同一解析器、同一編號規則），客戶端按 id 去重。

**來源二：驅動輸出（P2）。** Homing 自己啟動的會話（CLI 的 JSON 輸出、ACP 的 `session/update`）直接映射成 `Item` 發布，不必等文件落盤。ACP 的 chunk 需要合併：同一條消息的 `agent_message_chunk` 累積到同一個 `item.id`，每 150 毫秒或 400 字符發一次更新幀，幀裡帶 `partial: true`。

**斷線續傳（P2）。** `GET /api/sessions/{id}/items` 增加 `after_seq` 參數；SSE 幀帶 `id:`，重連時讀 `Last-Event-ID`。

**雲端通道**：`internal/cloud` 已按事件名轉發（`connector.go:427 SendEvent`），需確認 `item` / `turn` 也被訂閱並轉發；單幀超過 48 KB 時截斷 `diff` 和 `output_tail`，並標 `truncated: true`，客戶端按需再取。

### 5.3 B3　注意力模型與離開時段（P1，M，`internal/store` + `internal/api`）

這是首頁重做的數據基礎。

#### 5.3.1 數據

**水位**：`session-state.json` 的每條記錄增加兩個字段（舊字段不動）。

```jsonc
{ "read": true, "pinned": false, "muted": false, "archived": false,
  "seen_event_id": 1284,          // 新增：已看到的最後事件 id
  "seen_at": "2026-11-09T06:12:00.000Z" }  // 新增
```

**推進水位的時機**：

| 觸發 | 行為 |
|---|---|
| 任一客戶端打開會話（現有的 `PATCH read:true` 或 `POST /api/read`） | `seen_event_id = 該會話當前最大事件 id` |
| 從 Homing 回覆、授權、停止該會話 | 同上 |
| 用戶在電腦上原生繼續了會話（桌面活動推斷） | 見下 |

**桌面活動推斷**：如果會話正文裡出現一條晚於某事件的用戶消息，且這條消息不是 Homing 注入的（不在 `replies.jsonl` 裡），說明用戶在電腦前看過並繼續了，水位推進到該事件。實現：`agent.Session` 增加可選字段 `LastUserTS`（掃描器在讀會話尾部時順手取，取不到留空）；構建首頁時比較。取不到的 agent 不做推斷。

**`read` 的相容語義**：對外仍輸出 `read`，但改為計算值：`read = (seen_event_id ≥ 該會話最大事件 id)`。這樣新事件到達會自動變回未讀，修正 G4。`PATCH read:false` 把水位設回 0。

**離開時段**：新文件 `away-periods.jsonl`（追加式，同一 id 最後一行生效，與 `dispatches.jsonl` 同樣的寫法）。

```jsonc
{ "id": "awp_9f2c…", "started_at": "…", "ended_at": null,
  "source": "manual|auto_lock|schedule", "device_id": "…",
  "cleared_at": null }   // 用戶點了「全部已看」或逐個看完後寫入
```

`config.json` 的 `away.enabled` 保留，作為「當前是否有未結束時段」的快取。

#### 5.3.2 接口

`GET /api/away`（擴充，舊客戶端只讀 `enabled`）：

```jsonc
{ "enabled": true,
  "current": { "id": "awp_…", "started_at": "…", "source": "manual" },
  "last":    { "id": "awp_…", "started_at": "…", "ended_at": "…", "cleared_at": null } }
```

`POST /api/away {enabled, source?}`：開啟時新建時段；關閉時寫 `ended_at` 並放行所有掛起的鉤子（現有行為）。

`POST /api/away/periods/{id}/clear`：標記該時段的新動態全部已看（水位推進到各會話當前最大事件 id）。

`GET /api/home`（新增，首頁專用；`/api/inbox` 原樣保留）：

```jsonc
{
  "away": { "enabled": false, "period": { "id":"awp_…","started_at":"…","ended_at":"…","cleared_at":null } },
  "attention": [                       // 區 A：新動態，已排好序
    { "session": { /* 與 /api/sessions 的摘要同形 */ },
      "reason": "approval|needs_reply|error|collab_result|done|progress",
      "since_away": true,
      "event_id": 1290, "unseen_count": 3,
      "headline": "重跑完整測試", "summary": "全部通過，改了 3 個文件", "at": "…",
      "actions": ["approve","deny","reply","open","mark_seen"],
      "approval": { "command": "rm -rf build/", "options": [ … ] },   // reason=approval 時
      "collab": { "children": 2, "running": 1, "needs_you": 0 } }      // 有子任務時
  ],
  "running": [ /* 運行中橫條，摘要同形 */ ],
  "resume":  [ /* 區 B：最多 5 條，摘要同形 */ ],
  "earlier": { "count": 23, "next_cursor": "…" },                      // 區 C：只給數量
  "counts":  { "needs_you": 2, "away_new": 4, "running": 1, "earlier": 23 },
  "generated_at": "…"
}
```

`GET /api/sessions` 增加參數 `cursor`（分頁）、`zone=earlier`（排除已出現在區 A / B 的會話）。

#### 5.3.3 分區規則（必須逐條實現並有單元測試）

先排除：已歸檔、空會話。靜音會話不進區 A，但可以出現在區 B / C。

**區 A「新動態」**——滿足任一條即進入：

1. **等你處理**：會話有未解決的 `permission` / `needs_input` / `error` 事件。「已解決」的判定：授權已有決定；或該事件之後有回覆發出；或該事件之後會話出現了新的 `reply` / `done` 類事件；或用戶手動標為已看。**不看離開與否，永遠在。**
2. **協作結果**：有未看的 `collab_result` 事件（§5.5）。同樣不看離開與否。
3. **離開期間新動態**：存在事件滿足 `id > seen_event_id` 且 `ts ≥ P.started_at`。P 取當前未結束的時段；沒有的話取最近一個已結束、未清除（`cleared_at` 為空）、且結束不超過 12 小時的時段。
4. 設置項「新動態範圍」為「所有未讀」時（預設是「僅離開期間」），第 3 條去掉 `ts ≥ P.started_at` 的限制。

**區 A 排序**：授權 → 等回覆 → 錯誤 → 協作結果 → 已完成 → 有進展；同級內置頂優先，再按時間倒序。每個會話只出現一次，取其最高優先級的原因。

**區 B「繼續工作」**：不在區 A、非臨時目錄的會話，按「用戶最後一次參與的時間」倒序取 5 條（有 `LastUserTS` 用它，否則用 `last_ts`）。不帶摘要和未讀數。

**區 C「較早」**：其餘全部，只返回數量；展開時分頁拉取（每頁 20）。臨時目錄（`/tmp` 等）的會話固定歸這裡。

**不在離開模式、也沒有未清除時段時**：區 A 只有第 1、2 條。用戶自己在電腦前跑完的任務不會佔首頁，會帶一個小圓點出現在區 B。

#### 5.3.4 離開模式下的通知

- 離開時段內，`done` 類事件從 P2（60 秒合併成「N 個會話已完成」）提升為 P1（按會話單獨推，120 秒內同會話合併）。理由：人不在電腦前時，任務跑完正是他在等的消息。
- 結束離開時不推送；首頁頂部出現摘要條（§6.2）。

#### 5.3.5 自動離開（P2，預設關閉，待實測）

電腦鎖屏超過 N 分鐘（預設 5）自動開啟時段，`source: auto_lock`；解鎖後自動結束。實現建議：桌面殼版本訂閱 macOS 的 `com.apple.screenIsLocked` / `com.apple.screenIsUnlocked` 分佈式通知；純命令行版退回讀 `ioreg -c IOHIDSystem` 的 `HIDIdleTime` 做空閒閾值。兩種方式都需要在本機（Darwin 25）實測後再定。Windows 和 Linux 後補。

### 5.4 B4　通用驅動層（P2，L，接口在 `internal/agent`，實現建議放新目錄 `internal/driver`）

**改 `internal/agent/agent.go` 的接口需要先在匯報裡說明，由用戶協調**（`AGENTS.md`）。以下是建議的新增內容，全部是可選接口，不影響現有實現。

```go
// 新建一個會話。
type SessionStarter interface {
    StartSession(ctx context.Context, req StartRequest) (StartResult, error)
}
type StartRequest struct {
    Cwd, Text      string
    Files          []string
    Model, Effort  string
    PermissionMode string // "default" | "plan" | "accept_edits" | "read_only"，各家自行映射，不支援則忽略
    Origin         string // "phone" | "console" | "dispatch:<id>"
}
type StartResult struct {
    SessionID string // 立刻知道就填
    RequestID string // 要等鉤子綁定時填
}

// 停止正在跑的回合。
type Stopper interface {
    StopSession(ctx context.Context, sessionID string) error
}

// 響應一次授權請求。
type PermissionResponder interface {
    RespondPermission(ctx context.Context, sessionID string, eventID int64, decision PermissionDecision) error
}
type PermissionDecision struct {
    Allow    bool
    OptionID string // 可選：ACP 的具體選項
}
```

`agent.Capabilities` 擴充為可序列化的完整能力表（現有三個字段保留）：`NewSession`、`Stop`、`Approve`、`Live`、`Dispatch`、`SwitchModel`、`Effort`、`PermissionModes []string`。`/api/agents` 的 `capabilities` 從接口實現和這張表生成；`api/agents.go` 裡按 id 判斷的四個函數刪除。**對 Codex、Claude、WorkBuddy 的輸出必須與現在逐字段一致**（用現有的 parity 測試守住）。

`relayapi` 的 `newSession`、`stopSession`、`relayLauncher` 改為查註冊表調接口，不再 `switch source`。

#### 5.4.1 三種驅動

| 驅動 | 適用 | 現況 | 本期工作 |
|---|---|---|---|
| `cli` | Codex、Claude Code | 已有，散在 `relayapi/sessions.go` 和各 agent 包 | 搬到各 agent 包實現上面的接口；Claude 增加 `--output-format stream-json --verbose` 讀即時輸出（旗標組合在 2.1.90 上待核實）；兩家都接受 `PermissionMode`（Claude `--permission-mode`，Codex `-s`） |
| `http` | OpenCode（serve）、WorkBuddy（桌面 ACP over HTTP） | 已有 | 包成同樣的接口；WorkBuddy 的「新會話會重用當前活躍會話」問題不解決前 `Dispatch=false` |
| `acp` | 其餘支援 ACP 的 agent | **新做** | 見下 |

#### 5.4.2 ACP 驅動

**配置**：清單（`~/.homing/agents/*.yaml`）和 catalog 條目增加 `drive` 段。

```yaml
drive:
  kind: acp
  command: [hermes, acp]      # 參數數組，不經 shell
  env: {}                     # 可選
  dispatch: true              # 是否允許作為 @ 目標
```

**客戶端行為**（一個會話一個子進程；同 agent 的多會話也各起各的，簡單可靠）：

1. 啟動子進程，stdin / stdout 換行分隔 JSON-RPC；stderr 寫入 Homing 日誌（截斷，不含正文）。
2. `initialize`：`clientCapabilities` 中 `fs.readTextFile`、`fs.writeTextFile`、`terminal` 全部聲明為不支援——讓 agent 用它自己的工具，Homing 不代理文件和終端。
3. 新會話 `session/new {cwd, mcpServers: []}`；續聊且 agent 聲明了 `loadSession` 時用 `session/load`，否則該會話不提供續聊。
4. `session/prompt {sessionId, prompt:[{type:"text", text}]}`。附件先落盤（現有上傳邏輯），以路徑寫進文本。
5. 處理 `session/update`，按下表映射成 `agent.Item` 經 `live` 發布，同時產生 Homing 事件。
6. 收到 `session/request_permission`：寫一條 `permission` 事件（`data.acp` 保存請求 id 和選項），走現有的授權掛起（`relayapi/holds.go`）。決定為允許 → 選第一個 `allow_once`；拒絕 → 選第一個 `reject_once`；帶 `option_id` 則按指定選項。掛起超時 → 回 `cancelled` 並發 `session/cancel`。
7. `session/prompt` 返回 `stopReason`：`end_turn` → 寫 `done` 事件（文本為最後一條 agent 消息）；`refusal` / `max_*` → 寫 `error` 事件；`cancelled` → 寫 `stop` 事件。
8. 停止：發 `session/cancel`，並對所有掛起的授權回 `cancelled`（協議要求）。
9. 回合結束後子進程保留 10 分鐘等下一輪，超時退出。同時存活的 ACP 子進程上限 4 個，超出時新請求返回 429 和明確文案。

**`session/update` → Homing 的映射**：

| ACP | Item.type / role | 說明 |
|---|---|---|
| `user_message_chunk` | `message` / user | |
| `agent_message_chunk` | `message` / assistant | 同一消息的 chunk 合併，見 §5.2 |
| `agent_thought_chunk` | `reasoning` / assistant | |
| `tool_call`（kind=execute） | `tool_call`，`tool.name=title`，`command` 取 `rawInput` | status：pending / in_progress → `running`；completed → `ok`；failed → `error` |
| `tool_call`（kind=edit / delete / move）且 content 含 `diff` | `file_change`，`files[]` 由 `diff` 生成（`oldText` / `newText` 算增刪行，生成 unified diff） | |
| `tool_call`（其他 kind） | `tool_call` | `input_summary` 取 `rawInput` 的短 JSON |
| `tool_call_update` | 更新同 `toolCallId` 的條目 | 只覆蓋非空字段 |
| `plan` | `plan` | |
| `available_commands_update` | 不產生條目；快取為該會話的命令列表 | 供 §5.7 的 `/` 命令面板 |
| `current_mode_update`、`usage_update` | 更新會話摘要的 `permission_mode`、`context_usage` | |

**與原生會話的關係**：ACP 驅動起的會話由 agent 自己落盤，掃描器照常能讀到；兩個來源的條目按 id 去重。用戶同時在 agent 原生介面打開同一個會話時，不從 Homing 驅動它（沿用現有 `ErrHeld` 的判斷）。

**第一步是驗證而不是鋪開**：先做一個命令 `homing agents drive-test <id> --cwd /tmp/<新目錄>`，對本機的 `hermes acp` 和 `openclaw acp` 走完「initialize → session/new → 一句無害的提示 → 讀完 update → 結束」，把實際收到的消息形狀記到 `LESSONS.md`。只在 `/tmp` 下的新目錄測，不碰任何真實會話。握手通過後再寫正式驅動。

**Codex 和 Claude 不走 ACP 的理由**：兩家的 ACP 都要另裝 npm 適配器，而現有 CLI 驅動已經跑通、有測試、不需要用戶多裝東西。等 ACP 驅動穩定後，可以把適配器作為可選項（P4）。

### 5.5 B5　協作 v2（P2，L，`internal/orchestrator` + `internal/relayapi`）

v1 的 `POST /api/dispatch` 和 `GET /api/dispatches` 保留不變。

#### 5.5.1 統一發送入口

`POST /api/compose`——回覆和派發一次提交，`@` 由服務端解析（手機和控制台共用一套規則）。

```jsonc
// 請求
{ "session_id": "01a1…", "source": "codex",
  "text": "先修正登入按鈕\n@claude:review 檢查登入流程並補測試\n@hermes 查一下這個報錯的上游 issue",
  "files": ["/…/shot.png"], "model": null, "effort": null,
  "collab": { "context": ["recent","files"], "workspace": "auto", "return": "card" },
  "dry_run": false }

// 回應
{ "ok": true,
  "plan": {
    "message": "先修正登入按鈕",
    "mentions": [
      { "agent": "claude", "preset": "review", "text": "檢查登入流程並補測試",
        "target": "new", "workspace": "readonly", "dispatchable": true },
      { "agent": "hermes", "preset": "ask", "text": "查一下這個報錯的上游 issue",
        "target": "continue", "thread_id": "dsp_…", "dispatchable": true } ] },
  "reply": { "reply_id": 88, "status": "injected" },      // message 為空時為 null
  "dispatches": [ { /* Dispatch */ }, { /* Dispatch */ } ] }
```

`dry_run: true` 只返回 `plan`，供客戶端在發送前顯示「這條會發給誰」。任一 mention 不可調度時整個請求返回 409，不發送任何部分（避免半成功）。

**解析規則**放在 `internal/orchestrator/mention.go`，與 `AgentMentionParser.kt` 現有規則一致並擴充：

- `@<agent>`：agent id 或 catalog 裡登記的別名，大小寫不敏感。
- `@<agent>:<preset>`：預設見下表；省略時為 `ask`。
- 第一個有效 mention 之前的文字發給當前會話；每個 mention 到下一個 mention 之間的文字是它的任務。
- 郵箱、行內代碼、代碼塊裡的 `@` 不解析。
- 共享測試向量：新建 `internal/orchestrator/testdata/mentions.json`（輸入 → 期望的 plan），Go、Kotlin、TS 三邊的單元測試讀同一份文件。安卓端保留本地解析器只用於輸入框高亮，發送以服務端 `plan` 為準。

#### 5.5.2 預設（preset）

| 預設 | 語義 | 回傳 | 工作區 | 子任務的權限模式 |
|---|---|---|---|---|
| `ask`（預設） | 諮詢、調查、給意見 | 結果卡 | 共享目錄 | 唯讀優先（Claude `plan`、Codex `read-only`；不支援則普通） |
| `do` | 動手完成一塊獨立的工作 | 結果卡 | git 倉庫 → 獨立 worktree；否則共享 | 普通 |
| `review` | 審查父會話最近的改動 | 結果卡 | 共享目錄，唯讀 | 唯讀優先；上下文強制帶改動文件與 `git diff --stat` |
| `handoff` | 交棒：由目標 agent 接著做 | 不回傳 | 共享目錄 | 普通；父會話標記「已移交給 X」 |

v1 的 `mode: "parallel"` 等價於 `do` + 不回傳 + 共享目錄，繼續接受。

#### 5.5.3 上下文包

子任務拿到的提示詞由服務端按固定模板拼出（不調用任何模型，結果可預期、可測試）：

```text
[Homing 協作任務] 來自 {父 agent 名} 的會話「{父會話標題}」
工作目錄：{cwd}（分支 {branch}）

任務：
{用戶在 @ 後寫的內容}

--- 背景，僅供參考 ---
用戶最近的要求：
{父會話最後一條用戶消息，最多 1500 字}

{父 agent 名} 最近的回覆：
{父會話最後一條助手消息，最多 2500 字}

最近改動的文件：
{父會話最近 file_change 條目的路徑與增刪行，最多 30 行}
--- 背景結束 ---

完成後先用一段話給出結論，再寫細節。
```

`collab.context` 控制帶哪幾段：`recent`（兩段對話）、`files`（改動文件）、`diff`（`git diff --stat`，僅 git 倉庫）。預設 `["recent","files"]`。背景段落來自 `ReadItems`，取不到就省略該段，不編造。整個提示詞上限 12000 字符（沿用 `MaxTaskRunes`），超出時先截背景。

#### 5.5.4 狀態機與結果回傳

```
starting → started / waiting_session → running → completed
                                   ↘ needs_you ↗        ↘ failed / cancelled
```

- `running`：子會話有活動事件。
- `needs_you`：子會話出現授權或提問。這條事件同時讓**父會話**進入首頁區 A，卡片上標明「@Claude 的子任務在等你」。
- `completed`：收到子會話的回合結束（鉤子的 Stop、CLI 進程退出、ACP 的 `end_turn`）。此時：
  1. 取最終回覆文本（鉤子的 `last_assistant_message`，或最後一條助手條目），截到 8000 字；統計子會話的改動文件。
  2. 寫入 `Dispatch.result = {text, files_changed, finished_at, branch?}`。
  3. 在**父會話**寫一條事件：`kind: "collab_result"`，`source` 為子 agent，`session_id` 為父會話，`data.dispatch_id`。它進入父會話的條目流（合成條目 `type: "collab"`）、觸發 P1 通知、讓父會話進入首頁區 A。
- 舊版 Claude CLI 的 `waiting_session` 綁定邏輯（`sessions.go:264 claimLaunch`）保留。

**後續動作**：

| 接口 | 作用 |
|---|---|
| `POST /api/dispatches/{id}/forward {text?}` | 把結果轉交給父 agent：向父會話發一條回覆，預設文本「【@{子 agent} 的結論】\n{result.text}」，可編輯 |
| `POST /api/dispatches/{id}/reply {text}` | 在這條協作線上追問：向子會話發消息，狀態回到 `running` |
| `POST /api/dispatches/{id}/cancel` | 停止子會話 |
| `GET /api/dispatches?parent_session_id=` | 已有；返回體增加 `result`、`preset`、`workspace`、`thread` 字段 |

**同一目標的多輪**：父會話裡再次 `@claude` 時，如果存在 24 小時內、未取消的同目標協作線，`plan.target` 為 `continue`（向原子會話追問）；否則 `new`。客戶端可在發送前改成另一種。

**回傳方式 `collab.return`**：`card`（預設，人來決定是否轉交）、`none`、`inject`（完成後自動轉交給父 agent）。`inject` 預設關閉，需在設置裡打開——自動把一個 agent 的輸出餵給另一個 agent 有提示詞注入風險，必須是用戶的明確選擇。

#### 5.5.5 工作區隔離

`workspace: "auto"` 的決定規則：預設為 `ask` / `review` → `readonly`（共享目錄 + 唯讀權限模式）；預設為 `do` 且 cwd 是 git 倉庫 → `worktree`；其餘 → `shared`。

`worktree` 的做法：`git -C <cwd> worktree add <數據目錄>/worktrees/<dispatch_id> -b homing/<dispatch_id>`，子會話的 cwd 設為該目錄。完成後結果卡顯示分支名和 `git diff --stat <基準>...homing/<dispatch_id>`。**本期不做自動合併**：卡片提供「轉交給父 Agent：請合併分支 homing/…」和「複製分支名」；丟棄時 `git worktree remove` 並刪分支（要確認）。

**限制**：每個父會話同時活躍的子任務 ≤ 5；同一 cwd 上 `shared` 且非唯讀的子任務 ≤ 2；只做一層（子會話不能再作為父會話派發）；超限返回 429 和原因。

#### 5.5.6 會話摘要的協作字段

`/api/sessions` 和 `/api/home` 的會話摘要增加：

```jsonc
"collab": { "role": "parent|child|none",
            "parent": { "session_id":"…", "source":"codex", "dispatch_id":"dsp_…" },   // role=child
            "children": [ { "dispatch_id":"dsp_…", "agent":"claude", "status":"running", "preset":"review" } ] }
```

子會話預設不單獨出現在首頁區 A（它的動態歸到父會話卡上），但在會話庫裡照常可見並縮進顯示在父會話下。

### 5.6 B6　存儲與搜尋（P3–P4，M）

- **搜尋**：`GET /api/search?q=&scope=title|content&source=&limit=` → `{hits:[{session, snippet:{before,match,after}, item_id}], searched, truncated}`。標題、項目、路徑的搜尋直接在記憶體摘要上做（P3）。正文搜尋用旁路索引 `index.db`（`modernc.org/sqlite` 的 FTS5，P4）：只索引用戶與助手的文本，每會話上限 200 KB，按文件修改時間增量更新。中文沒有空格，分詞器建議用 `trigram`，查詢短於 3 個字符時退回 `LIKE`；modernc 的構建是否啟用了 FTS5 與 trigram 需先寫一個小測試確認。索引文件權限 0600，設置裡可關閉並刪除。
- **事件存儲**：現在每個請求都全量掃描記憶體裡的事件（`buildInbox`、`relayIndex`、`handleAgents`）。5000 條上限內沒有問題，本期不動。P4 再考慮把事件、回覆、調度遷到 SQLite；遷移時保留 JSONL 導出。

### 5.7 B7　會話操作補全（P1–P3，各項 S）

| 接口 | 作用 | 階段 |
|---|---|---|
| `PATCH /api/sessions/{id}` 增加 `title` | Homing 側的自定義標題，存在 `session-state.json`，`title_source: "homing"`；不改 agent 的文件 | P1 |
| `POST /api/sessions/new` 增加 `model`、`effort`、`permission_mode` | 新建時選模型和權限模式；`source` 不再限三家，按能力表校驗 | P2 |
| `POST /api/reply` 增加 `permission_mode` | 僅對支援的 agent 生效 | P2 |
| `POST /api/approvals/{id}` 增加 `option_id` | ACP 的多選項授權（僅此次 / 總是允許 / 拒絕） | P2 |
| `GET /api/sessions/{id}/commands` | 該會話可用的 `/` 命令（ACP 的 `available_commands_update`；其他 agent 返回內置的靜態表或空） | P3 |
| `GET /api/fs/complete?cwd=&prefix=` | `@文件` 補全。只在 cwd 子樹內；git 倉庫用 `git ls-files`，否則遍歷並跳過隱藏目錄；最多 50 條 | P3 |
| `GET /api/sessions/{id}/export?format=md` | 導出 Markdown | P3 |

### 5.8 B8　安全（貫穿各階段）

1. **設備權限範圍**。已配對設備增加 `scopes`：`read`（看）、`reply`（回覆、授權）、`drive`（新建、派發、停止）。預設三者都有；控制台的設備頁可以降為唯讀。`drive` 類接口在缺權限時返回 403。
2. **審計日誌**。`audit.jsonl` 記錄新建、派發、授權、轉交、取消：時間、設備、接口、會話 id、目標 agent。不記錄正文。
3. **命令來源**。`drive.command` 只接受參數數組，不經 shell；catalog 自帶的驅動命令只對已核實的條目生效；用戶自己寫的清單由用戶負責。
4. **結果是數據**。協作結果、子會話輸出一律當作不可信文本渲染；`return: inject` 預設關閉。
5. **頻率限制**。`/api/compose`、`/api/sessions/new`、`/api/dispatch` 每設備每分鐘 20 次。
6. 延續現有約定：掛鉤接口只接受回環來源；`/console` 只對回環地址開放；匯報和日誌不出現令牌與會話正文。

---

## 6. 行動端需求（Android）

負責：Codex（`android/`）。界面規範沿用 `docs/homing-v2/SPEC.md` §2（配色、圓角、觸控目標、不用 emoji 當圖標、任何滑動操作都要有按鈕替代）。

### 6.1 M1　遠程全功能：統一傳輸層（P1，L）

**現狀**：會話操作只走局域網 HTTP；雲端只有通知。**目標**：不在局域網時，所有功能照常可用，用戶不需要關心走哪條路。

```kotlin
interface HomingTransport {
    suspend fun request(method: String, path: String, body: JSONObject? = null): TransportResponse
    fun events(sessionId: String? = null): Flow<TransportEvent>   // name + data，與 SSE 事件同名
    suspend fun upload(path: String, bytes: ByteArray, name: String): JSONObject
    suspend fun download(url: String): ByteArray
    val kind: TransportKind   // LAN | CLOUD
}
```

- `LanTransport`：包住現有的 `AgentApi`（HTTP）和 `SessionLiveStream`（SSE）。
- `CloudTransport`：複用 `CloudNotificationClient` 的 WebSocket 和加密。發送 `{type:"rpc", rid, method, path, body}`，等同 `rid` 的 `rpc_result`（超時 20 秒）；接收 `{type:"event", name, data}` 交給同一個事件處理入口。協議見 `docs/protocol.md`「加密內容」，電腦端已實現（`internal/cloud/rpc.go`）。
- **選路**：啟動和網路變化時對局域網地址做一次 800 毫秒的 `/health`；通就用 LAN，不通用 CLOUD；LAN 中途連續失敗兩次則切到 CLOUD 並在後台繼續探測。頂欄的連接膠囊顯示「局域網」或「雲端」。
- **文件**：協議規定大於 64 KB 走 `file.chunk`（單文件上限 20 MB），**電腦端還沒實現**（`internal/cloud` 裡搜不到 `file.chunk`）。P1 先讓 CLOUD 下的上傳和附件預覽限制在 64 KB 以內，超出時提示「連回局域網後可用」；P2 兩端補齊分塊。
- `AgentStore` 和 `AgentViewModel` 改為只依賴 `HomingTransport`。
- 單元測試：用假的 WebSocket 伺服器驗證 RPC 往返、超時、事件分發、選路切換（參照現有 `CloudNotificationClientTest`）。

驗收：關掉手機 Wi‑Fi 只用行動數據，首頁、會話頁、回覆、授權、新建、`@` 派發全部可用；延遲體感不超過局域網的兩倍。

### 6.2 M2　首頁重做（P1，L）

底部分頁改為：**收件箱 / 會話 / 我的**（「項目」改名「會話」，內容見 §6.5）。

數據來自 `GET /api/home`；服務端不支援（404）時退回現有的 `/api/inbox` 版面。

```
┌────────────────────────────────────────────┐
│ [鴿] Homing          [我的 Mac · 雲端]  [搜] │  頂欄（現有）
├────────────────────────────────────────────┤
│ 離開中 · 已 2 小時 14 分             [結束] │  離開條
│ 2 件事等你 · 4 個會話有新動態               │
├────────────────────────────────────────────┤
│ [運行中膠囊橫條：Codex · homing 03:12 …]    │  現有 RunningStrip
│                                            │
│ 等你處理  2                                │  ── 區 A ──
│ ┌──────────────────────────────────────┐   │
│ │ [盾] 請求授權   Codex · homing · main │   │
│ │ ┌ rm -rf build/ ──────────────────┐  │   │
│ │              [ 拒絕 ]   [ 允許 ]      │   │
│ └──────────────────────────────────────┘   │
│ ┌──────────────────────────────────────┐   │
│ │ [問] 等你回覆   Claude · shop-twin    │   │
│ │ 要用方案 A 還是方案 B？               │   │
│ │ [ 輸入回覆…                ] [麥] [發] │   │
│ └──────────────────────────────────────┘   │
│                                            │
│ 離開期間的新動態  4            全部已看     │
│ ┌──────────────────────────────────────┐   │
│ │ [勾] 重跑完整測試              14:02 ● │   │
│ │ Codex · homing · 改了 3 個文件         │   │
│ │ 全部通過。                             │   │
│ │ └ @Claude 審查完成 · 查看結論          │   │  協作摘要行
│ └──────────────────────────────────────┘   │
│  …                                         │
│                                            │
│ ┌┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┐   │  ── 區 B ──
│ ┆ 繼續工作                  [ + 新會話 ] ┆   │  描邊容器，底色同頁面
│ ┆ [C] 搭建多端回覆推送        昨天    ›  ┆   │  單行，無摘要、無未讀數
│ ┆ [A] 翻譯一段英文文本        週五    ›  ┆   │
│ ┆ [W] 登入頁改版              週四  · ›  ┆   │  小圓點 = 你在電腦前後有新內容
│ ┆                          全部會話  ›  ┆   │
│ └┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┘   │
│                                            │
│ 較早的 23 個會話                        ⌄  │  ── 區 C ── 預設折疊
└────────────────────────────────────────────┘
│   收件箱 (2)        會話           我的     │
```

（方括號裡的字只是示意圖標位置，實作用單色矢量圖標。）

**三個區的視覺區分**——這是用戶明確要求的：

| | 區 A 新動態 | 區 B 繼續工作 | 區 C 較早 |
|---|---|---|---|
| 容器 | 實底卡片（`#1B2336`），每會話一張 | 一個描邊容器（1dp `#475569`，無底色），內部是行 | 單獨一行，無容器 |
| 內容密度 | 標題 + 歸屬 + 摘要 + 操作 | 只有圖標 + 標題 + 時間 | 只有數量 |
| 強調色 | 琥珀（等你）、青（未讀點） | 無強調色；唯一的青色是「新會話」按鈕 | 無 |
| 點擊 | 進會話，定位到新內容 | 進會話，輸入框自動聚焦 | 展開或跳到「會話」分頁 |
| 空狀態 | 現有的「都處理完了」鴿子 | 始終顯示（至少有「新會話」） | 數量為 0 時不顯示 |

**離開條**的三種狀態：

1. 不在離開、無未清除時段：不顯示；大標題沿用「N 件事等你」。
2. 離開中：「離開中 · 已 {時長}」+ 兩個計數 +「結束」按鈕。
3. 剛回來（時段已結束未清除）：「你離開了 {時長}：{n} 個會話有新動態」+「全部已看」（調 `/api/away/periods/{id}/clear`）。全部看完後自動消失。

開啟離開模式的入口：頂欄連接膠囊旁加一個開關圖標（現在在「我的」和舊收件箱頁，太深）。

**卡片行為**：

- 區 A 的卡片類型沿用現有三種（授權、等回覆、錯誤），新增兩種：**已完成**（摘要 + 改動文件數 + 未讀點）和**協作結果**（子 agent 圖標 + 結論前兩行 +「轉交給 {父 agent}」「查看」）。
- 卡片左滑或長按出操作：標為已看、置頂、靜音、歸檔。每個操作在長按菜單裡都有按鈕。
- 打開一個會話再返回，該卡片從區 A 消失（水位已推進），如果是最近活躍的會話則出現在區 B。
- 區 C 展開後在原地分頁加載（每頁 20，行樣式同區 B）；底部有「在會話分頁查看全部」。
- 浮動的「新任務」按鈕保留，文案改為「新會話」，與區 B 的按鈕進同一個流程。

**刷新**：收到任何 `message` / `dispatch` / `collab_result` / `session` 事件後 600 毫秒去抖重新拉 `/api/home`（沿用 `scheduleInboxRefresh`）。

### 6.3 M3　會話頁：與電腦端等價（P1–P3）

先做一次不改行為的拆分（M8），再加功能。

**等價對照表**（「電腦上」指在 agent 原生介面裡能做的事）：

| 能力 | 現況 | 要做的 | 階段 |
|---|---|---|---|
| 看對話、思考、工具調用、文件改動、計劃 | 有 | — | — |
| 內容即時出現 | 依賴 `item` 事件（Go 版缺） | 後端 §5.2；客戶端按 `item.id` 更新，支援 `partial` 幀的打字效果 | P0 / P2 |
| 回覆、帶附件、語音 | 有 | — | — |
| 運行中繼續發消息 | 有（排隊） | 輸入框上方顯示「已排隊 1 條」，可撤回 | P1 |
| 停止 | 有（限 Homing 啟動的進程） | 按能力表顯示；不支援時按鈕置灰並說明原因 | P2 |
| 授權 | 有（允許 / 拒絕） | 有多選項時顯示「僅此次 / 總是允許 / 拒絕」 | P2 |
| 切模型、思考強度 | 有 | — | — |
| 切權限模式 | 無 | 輸入框上方第三個膠囊：預設 / 計劃 / 自動接受編輯 / 唯讀（按能力表） | P2 |
| 看完整 diff | 卡片內展開 | 全屏 diff 檢視：按文件切換、橫向滾動、字號縮放 | P3 |
| 看完整命令輸出 | 只有尾部 | 點工具卡進全屏輸出（按需請求完整輸出） | P3 |
| `/` 命令 | 無 | 輸入 `/` 彈出命令面板（`/api/sessions/{id}/commands`） | P3 |
| `@文件` | 無 | 輸入 `@` 的選單分兩組：Agent、文件（`/api/fs/complete`） | P3 |
| `@agent` | v1 | 見 §6.4 | P2 |
| 重命名 | 無 | 標題點擊可改（`PATCH title`） | P1 |
| 導出、分享 | 無 | 菜單：導出 Markdown | P3 |
| 上下文用量 | 有 | — | — |
| 回到新內容 | 無 | 進入時定位到水位後的第一條，上方畫一條「新內容」分隔線；右下角「回到底部」按鈕 | P1 |
| 本機網頁預覽 | 有（端口代理） | — | — |

**頂部信息**：返回 + 標題 + 等寬副行「Codex · homing · main」+ 運行狀態；其下新增**協作條**（有協作時才出現）：

```
[C] Codex ● 運行中    [A] Claude ✓ 審查完成    [H] Hermes … 運行中    + 
```

每個膠囊是一個參與的 agent，點子 agent 進它的會話，點「+」等於在輸入框插入 `@`。子會話頂部對應顯示「來自 Codex 會話『…』的協作任務」，可跳回（現有 `InboundDispatchPanel`）。

### 6.4 M4　`@` 協作介面（P2，M）

1. **觸發**：輸入框打 `@`（或點 `@` 按鈕）彈出底部面板。上半「Agent」：列出能力表 `dispatch=true` 的 agent（圖標、名稱、狀態點）；不可調度的置灰並寫原因（「只能讀會話」「未安裝」）。下半「文件」（P3）。
2. **選中後**在輸入框裡變成一個可點的標籤 `@Claude · 諮詢`。點標籤可改：預設（諮詢 / 動手 / 審查 / 交棒）、是新開還是接著上一條協作線、附帶哪些上下文（最近對話 / 改動文件 / diff）。直接手打 `@claude:review` 效果相同。
3. **發送前預覽**：有 mention 時，發送按鈕文案變為「發送並派發 2 個」；點擊後先調 `dry_run`，在輸入框上方展開一張預覽：「發給 Codex：先修正登入按鈕」「派給 Claude（審查，唯讀）：檢查登入流程並補測試」。確認後正式發送。設置裡可關閉預覽。
4. **時間線裡的協作卡**（替換現有 `CrossAgentDispatchCard`）：

   ```
   ┌ [A] Claude · 審查 · 運行中 02:31 ──────────────┐
   │ 檢查登入流程並補測試                           │
   │ 正在讀 login.test.ts …                 ← 子會話最新一行，隨 item 事件更新
   │                        [ 取消 ]  [ 打開會話 ]  │
   └────────────────────────────────────────────────┘
   完成後變為：
   ┌ [A] Claude · 審查 · 完成 ──────────────────────┐
   │ 結論：登入流程有兩處問題……（最多 6 行，可展開） │
   │ 改了 2 個文件 · 分支 homing/dsp_c584           │
   │   [ 追問 ]  [ 轉交給 Codex ]  [ 打開會話 ]     │
   └────────────────────────────────────────────────┘
   ```

   「追問」把輸入框切到這條協作線（輸入框上方出現「正在回覆 @Claude 的協作線 ×」）；「轉交」彈出可編輯的文本後調 `forward`。
5. 子任務等授權時，卡片變琥珀色並直接帶「允許 / 拒絕」。

### 6.5 M5　「會話」分頁（P3，M）

由現有「項目」頁（`ProjectsScreen.kt`）擴展，定位是**會話庫**：找舊會話、整理。

- 分組方式分段控件：按項目（預設）/ 按 Agent / 按時間。
- 篩選膠囊：Agent 多選、狀態（運行中 / 等你 / 有協作）、僅未讀。
- 搜尋：標題、項目、路徑即時過濾；輸入後回車搜正文（`/api/search`，P4 才有，之前隱藏該入口），命中的片段高亮顯示在行下。
- 項目分組預設折疊（沿用 Magpie 的做法），臨時目錄折到最底「臨時會話 N 個」，歸檔單獨一組（現有）。
- 父子關係：子會話縮進顯示在父會話下，帶目標 agent 圖標和預設名。
- 多選模式：批量歸檔、標為已看、靜音。
- 每個項目卡右上「在此新建」直接帶上 cwd 進新會話流程。

**新會話流程**（`NewSessionScreen.kt` 調整）：選電腦（只有一台時跳過）→ 選項目（最近項目 + 手輸路徑）→ 選 agent（按能力表只列 `new_session=true` 的）→ 模型 / 強度 / 權限模式 → 輸入任務（這裡也支援 `@`，等於新會話一開始就帶協作者）。

### 6.6 M6　本地持久化與離線（P1 最小版，P3 完整，M）

- 引入 Room（新增 KSP 依賴）。表：`sessions`（摘要 JSON + 排序鍵）、`home_snapshot`（最近一次 `/api/home`）、`items`（每個打開過的會話最近 300 條）、`drafts`（每會話的未發送草稿與 mention 標籤）、`outbox`（離線時待發的回覆）。
- 冷啟動先渲染快照，同時發起刷新；頂欄膠囊顯示「正在更新」。
- 離線時回覆進 `outbox`，卡片顯示「待發送」，連上後按順序發出；授權和派發離線時禁用並說明。
- P1 只做 `home_snapshot`、`sessions`、`drafts`；P3 補 `items` 和 `outbox`。
- 清除本地數據的入口放在設置頁。

### 6.7 M7　通知（P1–P2，S）

- 通知渠道按原因分：等你處理、協作結果、已完成、運行異常。用戶可在系統設置裡分別關。
- 離開時段內的完成通知單獨推（配合 §5.3.4）。
- 通知動作：授權通知帶「允許 / 拒絕」；提問通知帶直接回覆（現有 `ReplyActionReceiver`）；協作結果通知帶「轉交」。
- 點通知進會話並定位到觸發事件。（`magpie-deep-dive.md` 第 8 條建議「點推送只進收件箱」，這裡有意不採納：Homing 的通知都指向具體待辦，直達更省事。）
- 會話頁菜單增加「完成時提醒我」：對該會話，即使不在離開模式也單獨推完成通知（一次性）。
- 同一父會話的多個協作結果 120 秒內合併成一條。

### 6.8 M8　工程重構（P1 開頭，S–M，不改行為）

- `SessionScreen.kt`（2778 行）拆為：`SessionHeader.kt`、`SessionTimeline.kt`、`SessionComposer.kt`、`CollabViews.kt`、`SessionSheets.kt`。現有的 `SessionDetailRenderTest` 必須不改斷言通過。
- `HomingInboxScreen.kt`（1199 行）在 M2 重做時按區拆分：`HomeAttentionZone.kt`、`HomeResumeZone.kt`、`HomeEarlierZone.kt`、`AwayBar.kt`。
- 刪除已不再路由到的舊 `InboxScreen.kt` 前，先把其中仍被引用的 `LastCrashCard` 移到公共文件。
- 版本號：P1 交付為 0.8.0，P2 為 0.9.0。`applicationId` 不改（`SPEC.md` v2 的約束仍然有效）。

---

## 7. 電腦端需求

負責：WorkBuddy（`console/`、`internal/gui`）。技術路線：**保持現有的原生 TS + Vite，不引入前端框架**。唯一的重互動頁是會話工作台，用現有的 `h()` 加一個按 `item.id` 做增量更新的 `TranscriptView` 即可；Markdown 渲染和代碼高亮各引入一個輕量庫，diff 直接渲染 `Item.files[].diff` 的 unified 文本。

### 7.1 信息架構

```
使用      總覽 · 會話 · 協作
Agent     已接入 · 目錄與接入 · 資源庫
手機      設備 · 通知
連接      中轉與局域網
設置      通用 · 運行日誌 · 關於
```

對比 Magpie：沒有 Models / Providers / Usage（Homing 不做模型網關）；多了總覽（注意力）、協作、手機三塊。

### 7.2 D1　會話工作台（P3，L）——替換現有的唯讀會話頁

```
┌ 側欄 ┬─ 項目 / Agent ──┬─ 會話列表 ───────────┬─ 會話 ───────────────────────────┐
│ 總覽 │ [全部 Agent ▾]  │ [搜尋…        ] [⋯]  │ 搭建多端回覆推送   Codex · main  │
│ 會話 │ ▾ homing     12 │ ● 重跑完整測試  3 分 │ [C]● [A]✓ [H]…         [停止]   │
│ 協作 │ ▸ shop-twin   5 │   修登入按鈕   1 時 │──────────────────────────────────│
│ …    │ ▸ 課題工坊    3 │   └ @Claude 審查 ✓  │  用戶：先修正登入按鈕…           │
│      │ ▸ 其它（臨時） │   翻譯英文文本 昨天 │  助手：已修改 LoginButton.tsx    │
│      │                 │   … 顯示更多         │  [工具卡] [文件改動 +12 −3 ▸]    │
│      │                 │                      │  [協作卡：Claude · 審查 · 完成]  │
│      │                 │                      │──────────────────────────────────│
│      │                 │                      │ [模型▾][強度▾][權限▾]            │
│      │                 │                      │ [ 輸入，@ 召喚其他 agent… ] [發] │
└──────┴─────────────────┴──────────────────────┴──────────────────────────────────┘
```

- **左欄**：Agent 篩選（單選或全部）+ 按工作目錄分組的項目樹，預設折疊，臨時目錄歸「其它」，沿用 Magpie 的五條收斂。與 Magpie 不同的是預設「全部 Agent」——同一個項目下不同 agent 的會話並排，才看得出協作關係。
- **中欄**：會話行 = 狀態點 + 標題 + 時間，副行「agent · 模型 · 分支」；子會話縮進；未讀加粗。多選後出批量條（歸檔、已看、導出）。每批 100 條。
- **右欄**：會話正文（與手機同一套條目類型）、協作條、輸入區。輸入區與手機等價：`@agent`、`@文件`、`/` 命令、模型 / 強度 / 權限、附件拖放、發送前預覽。走 `/api/compose`。
- **右側抽屜**（可收起）：會話信息——路徑、分支、模型、上下文用量、事件數、協作線列表、導出、歸檔。
- 窄窗（< 900px）退化為兩欄，再窄為單欄加返回（為手機瀏覽器訪問 `/console` 留路）。
- 鍵盤：`⌘K` 全局搜尋會話，`J / K` 上下切換，`E` 歸檔，`⌘Enter` 發送。
- 即時：訂閱 `/api/stream?session_id=`。

超出 Magpie 的點：能回覆、能授權、能派發；有未讀和狀態；跨 agent 並列；父子關係可見。

### 7.3 D2　協作視圖（P2 簡版，P3 完整，M）

- **簡版**：一張表，每行一條協作線——父會話、目標 agent、預設、狀態、開始時間、耗時；可篩選狀態；點行跳到工作台對應會話。數據來自 `GET /api/dispatches`。
- **完整版**：按父會話分組的泳道圖。橫軸時間，每條協作線一個條塊，顏色按狀態（配合形狀圖標，不單靠顏色），點條塊右側出結果卡（與手機同樣的追問 / 轉交 / 取消）。這是「可視化」的主要落點：一眼看出哪個會話派了誰、誰還在跑、誰在等你。

### 7.4 D3　總覽（P1，S–M）

現有收件箱頁改造：三欄並排「等你處理 / 運行中 / 離開期間新動態」，數據用 `/api/home`。頂部一條離開模式條：開關、當前時段時長、自動離開設置入口。寬屏三欄，窄屏疊成一列。

### 7.5 D4　Agent 能力階梯（P2，S）

現有 Agent 頁的每張卡增加一行五格的能力階梯：通知 / 讀取 / 回覆 / 驅動 / 協作。每格三態（可用、可接入、不支援），懸停顯示依據（例如「驅動：ACP，命令 `hermes acp`」）。對能接 ACP 的條目提供「啟用驅動」按鈕：寫入清單的 `drive` 段並跑一次握手測試，失敗不啟用。

### 7.6 D5　資源庫（P4，按 `docs/resource-library.md` 的 L1）

本期不展開，照該文檔的 L1（本機資源抽屜：掃描、展示、導入單個本機文件夾）實現。與本文的接點：協作面板可以顯示目標 agent 已裝的技能包，但不自動選擇。

### 7.7 D6　桌面殼與托盤面板（P3，M–L，即 G4）

按 `docs/go-architecture.md` §7 執行，補充托盤面板的內容定義（寬 360，內容高度自適應）：

- 頂部：離開模式開關 + 連接狀態。
- 「等你處理」最多 5 條：授權直接帶允許 / 拒絕，提問帶單行回覆。
- 「運行中」計數與前 3 條。
- 底部：打開主窗口、配對手機（彈二維碼）、暫停推送。

托盤圖標狀態：正常、有新動態（點）、等你處理（琥珀點）、離線、出錯。點擊動畫照已有的幀生成方案。

---

## 8. 接口契約匯總

全部為新增或向後相容的擴充。舊版 App（0.7.0）在新服務端上必須照常工作。

| 接口 | 變化 | 階段 | 章節 |
|---|---|---|---|
| SSE `item` / `turn` | Go 版補齊（Node 已有） | P0 | 5.2 |
| SSE `notify` | 核實並補齊 | P0 | 5.1 |
| `GET /api/home` | 新增 | P1 | 5.3 |
| `GET /api/away` | 增加 `current` / `last` | P1 | 5.3 |
| `POST /api/away/periods/{id}/clear` | 新增 | P1 | 5.3 |
| `GET /api/sessions` | 增加 `cursor`、`zone`；摘要增加 `collab`、`seen_event_id` | P1 / P2 | 5.3 / 5.5 |
| `PATCH /api/sessions/{id}` | 增加 `title`；`read` 改為水位語義 | P1 | 5.3 / 5.7 |
| `GET /api/agents` | `capabilities` 由接口生成，增加 `permission_modes`；三家核心 agent 輸出不變 | P2 | 5.4 |
| `POST /api/sessions/new` | `source` 不限三家；增加 `model` / `effort` / `permission_mode` | P2 | 5.7 |
| `POST /api/approvals/{id}` | 增加 `option_id` | P2 | 5.7 |
| `POST /api/compose` | 新增 | P2 | 5.5 |
| `POST /api/dispatches/{id}/forward` `/reply` `/cancel` | 新增 | P2 | 5.5 |
| `GET /api/dispatches` | 返回體增加 `result` / `preset` / `workspace` / `thread` | P2 | 5.5 |
| SSE `collab_result` | 新增；`dispatch` 事件增加字段 | P2 | 5.5 |
| `GET /api/sessions/{id}/items` | 增加 `after_seq` | P2 | 5.2 |
| `GET /api/sessions/{id}/commands`、`/export` | 新增 | P3 | 5.7 |
| `GET /api/fs/complete` | 新增 | P3 | 5.7 |
| `GET /api/search` | 新增（標題 P3、正文 P4） | P3 / P4 | 5.6 |
| 協議 `file.chunk` | 兩端補齊 | P2 | 6.1 |
| 設備 `scopes`、`audit.jsonl` | 新增 | P2 | 5.8 |

新增數據文件（都在數據目錄，權限 0600）：`away-periods.jsonl`、`audit.jsonl`、`worktrees/`、`index.db`（P4）。`session-state.json` 和 `dispatches.jsonl` 增加字段。清單 YAML 增加 `drive` 段。

接口文檔的落點：實現者在完成每個接口時，把請求 / 回應示例寫進 `docs/relay-v2.md`（會話、首頁類）或 `docs/cross-agent-orchestration.md`（協作類），本文不作為最終接口文檔。

---

## 9. 里程碑與分工

分工依據 `docs/go-architecture.md` §3 的目錄歸屬。三個新目錄中，`internal/live` 歸 WorkBuddy、`internal/driver` 歸 DSH 已定；`internal/orchestrator` 的負責人待用戶指定（§11.1 第 7 項），下表中寫「待指定」的任務在指定前不開工。

### P0　地基

| 任務 | 目錄 | 建議負責 | 規模 | 依賴 |
|---|---|---|---|---|
| 提交本輪工作 | 各自目錄 | 各負責人 | S | — |
| 即時條目流最小版（文件跟蹤 + `item` / `turn`） | `internal/live`、`internal/agent/{codex,claude}` | WorkBuddy | M | — |
| 核實並補 SSE `notify` | `internal/api`、`internal/service` | WorkBuddy | S | — |
| 對照驗證 + 切換到 18787 | `scripts/` | WorkBuddy，**用戶確認後執行** | S | 上兩項 |
| 安卓雲端 RPC 的技術驗證（只做往返一個 `GET /api/agents`） | `android/` | Codex | S | — |

出口標準：§5.1 的驗收。

### P1　離開模式收件箱 + 遠程全功能

| 任務 | 目錄 | 建議負責 | 規模 | 依賴 |
|---|---|---|---|---|
| 水位、離開時段、`/api/home`、分區規則與測試 | `internal/store`、`internal/api`、`internal/relayapi` | WorkBuddy | M | P0 |
| 離開時段的通知升級 | `internal/notify` | WorkBuddy | S | 上一項 |
| 會話自定義標題 | `internal/api` | WorkBuddy | S | — |
| 統一傳輸層（LAN / CLOUD）與選路 | `android/` | Codex | L | P0 驗證 |
| 會話頁、收件箱頁拆分（不改行為） | `android/` | Codex | M | — |
| 首頁重做（三區、離開條、新卡片） | `android/` | Codex | L | `/api/home`、拆分 |
| 本地持久化最小版（快照、草稿） | `android/` | Codex | M | — |
| 會話頁：定位新內容、排隊提示、重命名 | `android/` | Codex | S | — |
| 控制台總覽頁 | `console/` | WorkBuddy | M | `/api/home` |

出口標準：§10.1、§10.2。

### P2　協作 v2 + 驅動層

| 任務 | 目錄 | 建議負責 | 規模 | 依賴 |
|---|---|---|---|---|
| ACP 握手驗證命令（Hermes、OpenClaw，僅 `/tmp`） | `internal/driver` | DSH | S | — |
| Agent 可選接口與能力表；`relayapi` 去掉按 id 的分支 | `internal/agent`、`internal/api`、`internal/relayapi` | WorkBuddy（接口改動先匯報） | M | — |
| CLI 驅動遷入接口（Codex、Claude），含權限模式與即時輸出 | `internal/agent/{codex,claude}` | WorkBuddy | M | 上一項 |
| ACP 驅動 + 清單 `drive` 段 + 授權映射 | `internal/driver/acp`、`internal/adapter` | DSH | L | 握手驗證、接口 |
| 即時流完整版（驅動輸出、續傳、截斷） | `internal/live` | WorkBuddy | M | 驅動 |
| 服務端 mention 解析 + 共享測試向量 | `internal/orchestrator` | 待指定 | M | — |
| `/api/compose`、預設、上下文包、狀態機、結果回傳、後續動作 | `internal/orchestrator`、`internal/relayapi` | 待指定 | L | 解析、接口 |
| worktree 隔離與限額 | `internal/orchestrator` | 待指定 | M | 上一項 |
| 設備權限範圍、審計、頻率限制 | `internal/store`、`internal/api` | WorkBuddy | M | — |
| `file.chunk` 兩端 | `internal/cloud`、`android/` | Codex | M | P1 傳輸層 |
| 安卓：`@` 面板、標籤、預覽、協作卡、協作條、權限模式、多選項授權 | `android/` | Codex | L | `/api/compose` |
| 控制台：能力階梯、協作表 | `console/` | WorkBuddy | M | 能力表、dispatches |
| 自動離開（實測後決定是否交付） | `internal/platform`、`internal/gui` | WorkBuddy | M | P1 |

出口標準：§10.3、§10.4。

### P3　電腦端工作台 + 桌面殼

| 任務 | 目錄 | 建議負責 | 規模 |
|---|---|---|---|
| 會話工作台（三欄、正文、輸入區、抽屜、鍵盤） | `console/` | WorkBuddy | L |
| 協作泳道圖 | `console/` | WorkBuddy | M |
| 桌面殼、托盤面板、安裝包、開機啟動切換 | `internal/gui`、`build/` | WorkBuddy | L |
| `/commands`、`/fs/complete`、`/export`、標題搜尋 | `internal/api` | WorkBuddy | M |
| 安卓：會話分頁、全屏 diff / 輸出、`/` 與 `@文件`、導出、持久化完整版 | `android/` | Codex | L |

P3 的控制台和桌面殼部分不依賴 P2 的協作後端，可以和 P2 並行。出口標準：§10.5。

### P4　平台化（排序待定）

資源庫 L1–L2；多台電腦在一個收件箱聚合（手機端 `PairedComputerStore` 已支援多台，缺的是聚合視圖和每張卡上的電腦標籤）；正文全文搜尋；事件存儲遷 SQLite；Codex / Claude 的可選 ACP 適配器；TUI、自動更新、發布流水線（原 G5）；讓 agent 自己發起協作（提供一個 MCP 工具 `homing.dispatch`，需要單獨的安全設計）。

---

## 10. 驗收標準

所有涉及真實 agent 的驗收只用 `/tmp` 下新建的測試會話（`AGENTS.md`）。

### 10.1 遠程全功能

- [ ] 手機關 Wi‑Fi 用行動數據：首頁、會話正文、回覆、授權、新建全部成功。
- [ ] 局域網斷開後 5 秒內自動切到雲端，頂欄膠囊變化；恢復後切回。
- [ ] 雲中轉的數據庫和日誌裡沒有任何明文（沿用 `internal/server` 現有的斷言方式）。

### 10.2 離開模式收件箱

用隔離數據目錄 + `/hook/generic` 造事件，逐條驗證：

- [ ] 開啟離開 → 給會話 X 造一條 `done` → `/api/home.attention` 含 X，`since_away: true`。
- [ ] 不開離開 → 給會話 Y 造一條 `done` → Y 不在 `attention`，在 `resume`。
- [ ] 不開離開 → 給會話 Z 造一條 `permission` → Z 在 `attention` 首位。
- [ ] 打開 X（`PATCH read:true`）→ X 離開 `attention`；再造一條 `needs_input` → X 重新出現（驗證 G4 已修正）。
- [ ] 關閉離開 → `away.period.ended_at` 有值，未看的會話仍在 `attention`；調 `clear` 後清空。
- [ ] 歸檔、空會話不出現在任何區；靜音會話不出現在 `attention`。
- [ ] 離開期間 `done` 的通知級別為 P1。
- [ ] 手機首頁：三個區在視覺上可區分（截圖評審）；區 C 預設折疊；舊服務端（無 `/api/home`）下退回舊版面不崩潰。
- [ ] 冷啟動在飛行模式下顯示上次的首頁快照。

### 10.3 驅動層

- [ ] `/api/agents` 對 codex / claude / workbuddy 的輸出與改造前逐字段一致（parity 測試）。
- [ ] `homing agents drive-test hermes --cwd /tmp/…` 走完整個回合；結果記錄在 `LESSONS.md`。
- [ ] 在清單裡加 `drive` 段後，不改 Go 代碼，該 agent 在 `/api/agents` 裡 `new_session`、`stop`、`dispatch` 變為 true，手機新建流程裡能選到它。
- [ ] ACP 會話請求授權時，手機收到授權卡；允許 / 拒絕 / 超時三種情況 agent 側行為正確。
- [ ] 停止一個 ACP 會話：子進程收到 `session/cancel`，掛起的授權被回覆 `cancelled`。

### 10.4 協作 v2

- [ ] `mentions.json` 測試向量在 Go、Kotlin 兩邊通過（含郵箱、代碼塊、未知 agent、空任務、`:preset`）。
- [ ] 在 Codex 測試會話裡發「說明 A\n@claude 做 B」：父會話收到「說明 A」；Claude 新會話的提示詞包含任務 B 和背景段；協作卡出現並隨子會話更新。
- [ ] 子會話結束後：父會話時間線出現結果卡；首頁區 A 出現「協作結果」；通知到達。
- [ ] 點「轉交」：父會話收到帶結論的回覆。點「追問」：消息進入原子會話而不是新會話。
- [ ] `do` 預設在 git 倉庫裡：子會話的 cwd 是獨立 worktree，父目錄無改動；結果卡顯示分支名。
- [ ] 任一 mention 不可調度時整體 409，父會話沒有收到任何消息。
- [ ] 超過並發限額返回 429。
- [ ] 唯讀設備調 `/api/compose` 返回 403；`audit.jsonl` 有派發記錄且不含正文。

### 10.5 電腦端

- [ ] 工作台裡能完成：找到會話、讀正文、回覆、授權、`@` 派發、看協作卡、導出。
- [ ] 1000 條會話、單會話 2000 條條目時列表和正文滾動流暢（分批渲染）。
- [ ] 手機瀏覽器寬度下 `/console` 的會話頁可用。
- [ ] `homing`（無參數）啟動托盤和窗口；托盤面板能授權和開關離開模式。

---

## 11. 風險與需要用戶決定的問題

### 11.1 已定案的決定（2026-11-09，用戶確認）

實現時以本表為準；要偏離其中任何一條，先在匯報裡說明，由用戶決定。

| # | 問題 | 決定 | 落點 |
|---|---|---|---|
| 1 | 何時從 Node 切到 Go | P0 的補齊項完成、`scripts/verify-go.sh` 對照通過後立即切。執行切換的那一步（停 LaunchAgent、改端口）仍按 `AGENTS.md` 的規定，先向用戶匯報對照結果、拿到當次確認再動手 | §5.1 |
| 2 | `@agent` 的預設行為 | 預設 `ask`：諮詢，結果以卡片回到原會話。v1 的「並行、不回傳」保留為 `do` + `return: none`，`mode: "parallel"` 繼續接受 | §5.5.2 |
| 3 | 首頁「新動態」的範圍 | 預設「等你處理 + 協作結果 + 離開期間新動態」；設置裡可改為「所有未讀」 | §5.3.3 |
| 4 | 自動離開（鎖屏即離開） | 做，預設關閉。在本機實測可靠後才交付；是否改為預設開啟屆時另行決定 | §5.3.5 |
| 5 | Codex / Claude 是否改走 ACP | 不改，繼續用 CLI 驅動。ACP 適配器只作為 P4 的可選項 | §5.4.2 |
| 6 | 控制台是否引入前端框架 | 不引入，保持原生 TS + Vite | §7 |
| 7 | 新目錄的負責人 | `internal/live` 歸 WorkBuddy；`internal/driver`（含 `acp`）歸 DSH；**`internal/orchestrator` 待指定**（見下） | §9 |
| 8 | iOS | 不在本計劃內。若日後要做，先把傳輸層和協議抽成可共享的規格 | §11.3 |

**唯一未定的一項：`internal/orchestrator` 由誰負責。** 上一輪的建議裡沒有給出人選。現在補一個建議：交給 Codex——它負責 `android/`，mention 語法和協作卡的兩端由同一個 agent 維護，共享測試向量不容易走樣。這一條還沒有經過用戶確認；確認前，P2 裡標「待指定」的三項任務不要開工，其他任務不受影響。

目錄歸屬定下來後，需要由 `docs/go-architecture.md` 的維護者把這三個新目錄補進該文檔 §3 的歸屬表（本文沒有改那份文檔）。

### 11.2 風險

| 風險 | 說明 | 緩解 |
|---|---|---|
| ACP 實際行為與文檔不符 | 握手、授權、`loadSession` 各家實現不一；本機兩家只確認了子命令存在 | 先做驗證命令，逐家記錄；驅動對未知消息寬容忽略 |
| Claude `stream-json` 旗標組合 | 2.1.90 的 `-p` 下是否需要 `--verbose`、輸出形狀如何，未實測 | 實現前在 `/tmp` 會話實測；失敗則退回「文件跟蹤」取即時內容 |
| 並行寫同一目錄 | 兩個 agent 同時改文件會互相覆蓋 | 唯讀預設 + worktree + 並發限額（§5.5.5） |
| 提示詞注入 | 子 agent 的輸出被轉交給父 agent | 預設人工轉交；自動轉交需顯式開啟；轉交文本可編輯 |
| 桌面活動推斷不準 | 取不到最後一條用戶消息時間的 agent 無法推斷 | 取不到就不推斷，寧可多顯示 |
| 雲端通道延遲和體積 | 會話正文經加密中轉，大條目慢 | 分頁、截斷大字段、按需再取；`file.chunk` |
| 切換風險 | Go 版行為差異影響正在用的手機 | 對照腳本 + 回滾腳本 + 用戶確認 |
| 多人並行改接口 | `agent.go`、`store.go` 被多個任務觸及 | 接口改動集中在 P2 開頭一次完成並先匯報；其餘任務只加文件 |
| 自動離開的平台差異 | macOS 鎖屏檢測方式隨系統版本變化 | 預設關閉，實測後交付 |

### 11.3 本文沒有覆蓋的

模型網關與用量統計（不做）；Windows / Linux 上的桌面殼細節（跟隨 G4）；雲中轉的運維與計費；安卓發布簽名與上架；資源庫 L2 以後的內容。

---

## 附錄 A　事實來源（本機核實，2026-11-09）

| 結論 | 依據 |
|---|---|
| Go 版編譯與測試全過 | `go build ./... && go test ./...` |
| 18787 上是 Node | `lsof -iTCP:18787 -sTCP:LISTEN` → node |
| 路由清單 | 全倉搜尋 `"METHOD /path"` 路由字串，去重後 78 條 |
| Go 無 `item` / `turn` 廣播 | `PublishNamed` 只在 `relayapi/sessions.go:305`、`relayapi/dispatch.go:260`、`api/bridge.go:141`；Node 在 `relay/server.mjs:4731-4743` |
| 安卓雲端只有通知 | `CloudNotificationClient.kt` 類註釋；安卓源碼無 `rpc` 發送 |
| 電腦端有雲端 RPC | `internal/cloud/rpc.go`、`connector.go:427` |
| 離開模式是布林值 | `internal/store/store.go:110`、`relay_state.go:13-27` |
| 收件箱分桶規則 | `internal/api/inbox.go:58-143` |
| 會話級已讀不隨新事件重置 | `inbox.go:104`、`store.go:264-288` |
| 能力按 id 寫死 | `internal/api/agents.go:243-290`、`relayapi/sessions.go:52`、`relayapi/dispatch.go:23` |
| 調度 v1 的邊界 | `docs/cross-agent-orchestration.md`「首版邊界」 |
| 控制台會話頁唯讀 | `console/src/pages/sessions.ts` |
| 安卓無本地數據庫 | `AgentStore.kt`（僅 SharedPreferences）；`build.gradle.kts` 無 Room |
| 本機 agent 與 ACP 子命令 | `claude --version` 2.1.90；`codex --version` 0.167.0-alpha；`hermes --help`、`openclaw --help` 含 `acp` |
| Magpie 的結構與缺失項 | `docs/magpie-ui-analysis.md`、`docs/magpie-deep-dive.md` |

## 附錄 B　外部參考

官方文檔：

- [Claude Code on mobile](https://code.claude.com/docs/en/mobile)
- [Work with Codex from anywhere（OpenAI）](https://openai.com/index/work-with-codex-from-anywhere/)
- [ACP：協議概覽](https://agentclientprotocol.com/protocol/overview)、[傳輸](https://agentclientprotocol.com/protocol/transports)、[Prompt Turn](https://agentclientprotocol.com/protocol/prompt-turn)、[Tool Calls](https://agentclientprotocol.com/protocol/tool-calls)、[Agents 列表](https://agentclientprotocol.com/get-started/agents)

第三方資料（未逐條核實，只作背景）：

- [Claude Code Now Available to Start a Session From Your Phone](https://pasqualepillitteri.it/en/news/12249/claude-code-start-session-from-phone)
- [I built a mobile app to control Claude Code and Codex from my phone](https://dev.to/k9i/i-built-a-mobile-app-to-control-claude-code-and-codex-from-my-phone-4d84)
- [Codex Mobile: Remote Agent Control from Your Phone via the ChatGPT App](https://codex.danielvaughan.com/2026/05/15/codex-mobile-chatgpt-app-relay-architecture-remote-agent-control/)
- [seshboard issue #257：ACP 會話方法的穩定時間](https://github.com/estruyf/seshboard/issues/257)
- [awesome-agent-clients](https://github.com/somewherelostt/awesome-agent-clients)
- [Best Phone Apps to Control Coding Agents in 2026](https://amux.io/guides/best-phone-apps-for-coding-agents-2026/)
- [Happy Coder vs Alternatives](https://happy.engineering/docs/comparisons/alternatives/)
