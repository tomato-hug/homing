# Homing v3 驗收狀態

> 日期：2026-11-12
> 這份文件不是「全部需求已完成」的宣告。它把 SPEC §10 的結果分成：
> **本機已驗證**、**需切換手機中繼後驗證**、**受第三方依賴或憑證限制**、
> **尚未實作**。交接時只看這一頁也能知道下一步該做什麼。

## 1. 本輪可重現的驗證

以下命令在 2026-11-11 的工作樹上通過：

```sh
go test ./... -count=1 -timeout=300s
go vet ./...
cd console && npm run build
cd android && ./gradlew :app:testDebugUnitTest :app:assembleDebug --console=plain
git diff --check
```

另外用隔離資料目錄與 18810 端口做了瀏覽器驗收：

- `/console/sessions?new=1` 的「新會話」在桌面與 390 px 寬度都能開啟。
- Agent 切換會刷新模型與權限模式；Codex 顯示可調推理強度，Claude Code 顯示自己的權限模式。
- 空目錄或空任務時送出按鈕保持停用；取消、Esc 與點遮罩都能關閉。
- 模態開啟時 `⌘Enter` 不會穿透到背後的父會話，也不會偷偷送出。
- 會話資訊抽屜在 1800 px 以第四欄停靠，1440 px 與 390 px 改為右側覆蓋；
  `Esc`、關閉按鈕與遮罩都能收起，焦點會回到「會話信息」入口。
- 390 px 抽屜寬 374 px、頁面 `scrollWidth` 保持 390 px，沒有水平溢出。
- 設定頁可查看正文索引的會話數、正文條數、大小與最後更新時間；開關、重建、
  刪除與路徑複製均已接上。刪除會同時關閉索引，停用狀態下刷新不會重建空資料庫。
- 390 px 下索引卡片的三列操作會換行，頁面 `scrollWidth` 保持 375 px，沒有水平溢出。
- 控制台工作台、Agent 支援庫、資源庫、總覽與托盤頁都使用同一套深色卡片與窄屏版面。
- 資源庫「發佈」頁接到 `/api/library/publish/check`：用 `/tmp` 合成 manifest 顯示
  targets、權限、checksum 與需人工複核的命令宣告；切換分頁時搜尋／篩選／導入
  按鈕不再殘留；缺 `metadata.checksum` 的草稿會顯示阻塞而不是「可分享」。
  1440 px 與 390 px 都可操作，390 px 的 `scrollWidth` 為 375 px。
- 會話列、工作台標題、正文作者列與 `@Agent` palette 使用各 Agent 原廠 SVG；
  Android 端以離線 path 內嵌常用 Agent 標誌，未知 Agent 才回退字母。
- `/api/sessions/{id}/items`、單會話／全局 SSE 與 Markdown export 預設過濾
  `reasoning`／`thinking`；只有明確帶 `include_reasoning=1` 才回傳。Android
  需從右上選單主動要求才顯示思考過程，控制台檢視器保留展開能力。
- `title_source=agent` 的 `标题：`、`会话标题：`、`Title:` 與 Markdown heading
  包裝已清理；`homing`／`custom` 使用者自訂標題不改寫。

2026-11-12 最新驗證的內嵌資源為
`web/console/assets/index-BRWKnYVl.js`、`web/console/assets/index-BDwArKze.css`。
Debug APK 的 SHA-256 為
`2cbe7cf7f2d90b2cd75fb206f7977722f97b00df5b5c8215d37e01a0c60e8d7e`；
它只供測試，不是發佈簽名包。

## 2. 已完成並有程式／測試依據

| SPEC 區域 | 狀態 | 依據 |
|---|---|---|
| 即時條目流、SSE `item` / `turn`、`after_seq`、大幀截斷 | 已完成 | `internal/live`、`internal/api/sse_*` 與套件測試 |
| 注意力水位、離開時段、`/api/home` 分區、清除時段 | 已完成 | `internal/store/home_state_test.go`、`internal/api/home_test.go` |
| 自動離開（macOS 鎖屏／HID 閒置，預設關閉） | 已完成 | `internal/awayauto` 與注入式 probe 測試 |
| 通用能力表、CLI / HTTP / ACP 驅動介面 | 已完成 | `internal/agent`、`internal/driver`、`internal/api/agents_test.go` |
| ACP `session/update`、授權多選項、取消與 `drive-test` | 已完成程式與假 agent 測試 | `internal/driver/acp_test.go`、`internal/api/acp_reply_test.go` |
| 服務端 mention 解析、共享測試向量、`/api/compose` | 已完成 | `internal/orchestrator`、`internal/relayapi/compose_test.go`、Console/Android 向量測試 |
| 協作結果卡、追問、轉交、取消、worktree 隔離與丟棄 | 已完成 | `internal/relayapi/dispatch_test.go`、`internal/relayapi/worktree_test.go` |
| 逐 mention 的 target / context / workspace 覆蓋 | 已完成 | `TestComposeDryRunAppliesPerMentionOverrides`、Android `AgentMentionParserTest` |
| 控制台會話資訊抽屜：路徑、分支、模型、上下文、事件數、協作線、匯出、歸檔 | 已完成 | `console/src/pages/sessions.ts`、1800／1440／390 px Playwright 驗收 |
| 設備 scopes、審計、頻率限制、結果當不可信文本 | 已完成 | `internal/api/audit_test.go`、`internal/api/ratelimit.go` 與相關 handler 測試 |
| 控制台會話工作台：正文、回覆、授權、`@` 派發、協作卡、匯出 | 已完成 | `console/src/pages/sessions.ts`、390 px Playwright 驗收 |
| 控制台新會話：Agent／cwd／模型／強度／權限／任務／附件 | 已完成 | `/api/sessions/new`、新會話 modal 與瀏覽器驗收 |
| 控制台托盤頁：離開、等你處理、單行回覆、授權、運行中、暫停推送 | 已完成 | `console/src/pages/tray.ts`、`/console/tray` |
| Agent catalog、快速接入、接入套件、能力階梯、drive 啟用 | 已完成本機管理流程 | `internal/adapter/catalog*_test.go` |
| 資源庫 L1 與部分 L2：掃描、導入計劃、回滾、來源、審計、本機發佈預檢 | 已完成 | `internal/library`、`internal/library/publish_test.go` 與 Console 驗收 |
| 全文搜尋、索引設定、命令列表、檔案補全、Markdown 導出 | 已完成 | `internal/api/search*`、設定頁索引卡片、`session_extras.go`、`export.go` |
| 雲端加密 RPC、事件轉發、`file.chunk` | 已完成程式與協議測試 | `internal/cloud`、`android/.../CloudTransport.kt` |
| 多電腦首頁聚合、LAN 多位址 fallback | 已完成程式與單元測試 | `HomeAggregation.kt`、`CloudNotificationClient` 測試 |
| 會話品牌圖標、Agent 原生標誌 | 已完成控制台與 Android 常用 Agent 覆蓋 | `console/src/lib/agent-icons.ts`、`android/.../ui/AgentLogos.kt` |
| 思考過程預設不下發、手機按需請求 | 已完成 Go／Node 過濾與 Android opt-in | `internal/api/sessions.go`、`internal/api/events.go`、`relay/server.mjs`、Android `AgentStore` |
| Agent 自動標題前綴清理 | 已完成 Go／Node／Android | `internal/agent/scanlib/title_test.go`、Android `SessionPresentationTest` |
| 端到端配對手機出現在設備頁並可撤銷 | 已完成 API 與控制台，待真機複掃 | `/api/devices.transport`、`TestDevicesIncludesEndToEndPeersAndRevokesThem` |
| 桌面殼、托盤、TUI、更新器、Docker 與 release workflow | 已落地程式 | `internal/gui`、`internal/tui`、`internal/updater`、`.github/workflows/release.yml` |

## 3. 18787 切換後仍需真機完成的最終驗收

`127.0.0.1:18787` 已由 Go 版接管，`/health` 回報 `impl=go`。以下項目在程式碼、
隔離端口與單元測試已通過，但尚未在手機的真實連線上逐項驗收：

- 手機重啟後，不改任何設定就連到 Go 版；舊配對資料仍可讀取會話。
- 用控制台 `/get` 二維碼重新配對一台手機，設備頁能立即看到並可移除。
- 在真實 Codex 會話回覆 `@claude …`，看到子任務卡、子會話與結果回到父會話。
- 手機在局域網斷線後切到雲端，再用行動數據完成首頁、正文、回覆、授權、新建與派發。
- 多台電腦聚合與事件去重的最終體感。

回滾仍保留 Node LaunchAgent 的舊設定；任何再次重啟服務的操作都要先取得用戶確認。

## 4. 受第三方依賴或憑證限制

| 項目 | 現況 | 影響 |
|---|---|---|
| Hermes ACP | 本機 `hermes acp` 存在，但 ACP 依賴未安裝 | 只能驗證錯誤訊息，不能宣稱完整回合可用 |
| OpenClaw ACP | 已完成 `initialize` / `session/new` 握手 | `session/prompt` 的完整回覆尚未驗證 |
| Kimi ACP | 預覽環境找不到對應 binary | `drive-test` 不能作為已通過 |
| DSH ACP | 實驗性 profile plugin | 需在安裝了對應 profile 的機器重驗 |
| Codex / Claude CLI 真回合 | 程式與舊版 smoke test已存在 | 本輪沒有向真實會話發測試消息，避免打擾使用者 |
| macOS 簽名／公證、GitHub Release | 流程已寫好 | 仍需 Developer ID 憑證與可用 tag；本輪不簽、不發佈 |

因此 57 家 catalog 條目代表「可被發現、可選擇接入方式」，不表示 57 家都已具備
L1–L4 能力。UI 必須繼續按 `/api/agents.capabilities` 如實開關。

## 5. 尚未實作或尚未完成 L3+

- 資源庫的遠端 Pack 更新、專案／團隊作用域、簽章／撤銷、公開發佈。
- Codex / Claude 以外的 agent 之完整真實 ACP 回合；要逐家在乾淨 `/tmp` 目錄驗收。
- 公開發佈所需的本機簽章、公證與 Release 上傳。
- 真正手機上的飛行模式、冷啟動快照與多電腦切換驗收。
- 工作流的自動合併；目前 worktree 只提供結果、分支名、轉交與確認後丟棄。

## 6. 交接時的最小下一步

1. 由用戶確認切換時段，備份 Node LaunchAgent 設定。
2. 停 Node、安裝 Go 開機自啟、把端口改回 18787，執行回滾檢查。
3. 手機重啟 App，完成 §3 四條真機驗收。
4. 對已安裝 ACP 的 Hermes / OpenClaw / Kimi 各跑一次 `homing agents drive-test`，只使用 `/tmp` 新目錄。
5. 在持有憑證的機器完成簽名、公證與 Release；不要把 debug APK 當成發佈包。
