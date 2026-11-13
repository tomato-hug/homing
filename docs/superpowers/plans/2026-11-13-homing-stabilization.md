# Homing v3.1 穩定化與交付收口 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將目前已成形的 Homing v3 功能收斂成版本可追溯、手機主流程可驗收、可準備分發的穩定測試版。

**Architecture:** 保持 Go 本機服務／雲中轉、原生 TypeScript 控制台及 Kotlin Android 架構。優先補齊執行版本、傳輸恢復、桌面服務共用與 R1 狀態機的交付缺口，沿用現有能力表、加密協議和 CLI 驅動。本輪不重建已有功能，後續新功能按獨立交付包推進。

**Tech Stack:** Go 1.26、Wails v3、TypeScript + Vite、Kotlin + Compose、Room、WebSocket 加密 RPC、GitHub Actions。

**Spec:** `docs/homing-v3/SPEC.md`；`docs/homing-v3/review-2026-11-12.md` §8 已確認決策；`docs/go-architecture.md` §3 分工；`AGENTS.md`。

## Global Constraints

- 「只改自己负责的目录；要改别人的目录或 internal/agent/agent.go 的接口，先在汇报里说明，由用户协调。」
- 「只 `git add` 自己负责的路径；不要切分支、不要 stash、不要 reset、不要 `git add -A`。」
- 「不往用户真实的 WorkBuddy、Claude、Codex 会话里发消息；测试回复只用 /tmp 下新建的 Codex 测试线程。」多 Agent 真回合需要另外確認隔離測試範圍；自動化測試優先用假 Agent。
- 「不停 8787 端口上的 ResetWatch；不改 Codex hooks.json 的信任状态；不改 Claude 自己的配置。」
- 「安卓发布签名的密码只在钥匙串里；不要把发布签名的 APK 装到用户手机上（签名和调试版不同）。」
- 「簡體中文為主，英文為第二語言」；「官方中轉為預設，自建伺服器為進階選項」；會議室第一版「只做討論和紀要」。
- 控制台保持原生 TS + Vite；自動離開預設關閉；不把 Codex／Claude 的驅動更換當成本輪前置。
- 本文最初交付現況梳理與計畫；用戶後續已要求按建議推進，執行結果見 §7。常駐服務的再次重啟按現有驗收文件要求先由用戶確認。

## Review Focus

1. 工作樹、Go 二進位、內嵌控制台、APK、遠端提交互不一致：Task 1 記錄同一份交付清單；Task 7 檢查 tag 與產物來源。
2. 手機在 LAN／雲端間切換、斷線重連、重複事件：Task 2 驗證 5 秒切換目標、回覆狀態單向合併與事件去重。
3. 撤銷一台仍連線的端到端設備：Task 2 驗證現有 LAN 連線失效、雲端 RPC 被拒及重啟後仍不可恢復權限。
4. 桌面殼遇到已有 Go／Node／其他程序占用端口：Task 3 驗證只附著到可驗證的同一個 Homing 服務，退出視窗不停止外部服務。
5. 會議中途暫停、重啟或有參與者失敗：Task 5 驗證不重複發言、不偷偷新增回合、不重複生成紀要。

---

## 1. 現況快照：2026-11-13

這份快照區分原始碼、測試結果、執行程序和實際交付。舊文件中的完成狀態不能直接代表手機目前正在使用的版本。

| 項目 | 本次核實結果 | 意義 |
|---|---|---|
| 本機原始碼 | 分支 `homing-v3`，HEAD `00fefbc`；未設 upstream | 目前工作不在 `main` |
| 遠端 | `git ls-remote --heads origin` 只有 `main=ceeacc6`；本機 `main...homing-v3` 為 `0 / 10` | 10 個新提交尚未推到此遠端的任何分支；不能把已 commit 視為已備份／已交付 |
| 工作樹 | 12 個狀態項目，包含設備管理修正、文件與 JS 產物替換 | 已由其他工作留下；本次不代為提交或覆寫 |
| 執行服務 | `127.0.0.1:18787/health` 為 `impl=go`、`version=fb38f5e-dirty` | Node→Go 已完成；不再排「首次切換」 |
| 執行中的控制台 | `index-2oMidNgy.js` | 仍是舊資源 |
| 工作樹及本次建置控制台 | `index-BRWKnYVl.js` | 目前設備頁修正尚未反映在服務中的頁面 |
| Android 原始碼／本地 Debug APK 中繼資料 | `0.11.0`、versionCode `12`；APK SHA-256 與 acceptance 記錄相符 | 舊報告的 `0.9.0` 已過時；手機實際安裝版本本次未核對 |
| APK 下載入口 | `/apk` HEAD 為 200，APK MIME 正確 | 下載入口可達；不代表安裝、配對及遠端控制已驗收 |
| GitHub | 倉庫私有，Release 列表為空；最近 CI 成功對應 `ceeacc6` | 尚無 v3 新提交的遠端 CI 與正式分發證據 |
| 隔離服務 | `18788` 沒有監聽 | 這不是正式服務故障；後續驗收需新開隔離程序 |

核實方式：本機 Git、只讀 `/health`／`/console/`／HEAD `/apk`、本機原始碼、`gh repo view`／`gh release list`／`gh run list`。GitHub 連接器對此私有倉庫回 404，因此改用本機已登入的 `gh` 完成唯讀查核。

### 本次驗證

| 檢查 | 結果 | 邊界 |
|---|---|---|
| `go build ./...` | 通過 | macOS linker 有最低目標版本與物件版本不一致的警告；發佈前需統一部署目標 |
| `go test ./... -count=1 -timeout=300s` | 全部通過 | 本機未使用 Go 測試快取；不代表手機真機鏈路已驗收 |
| `go vet ./...` | 通過 | — |
| `cd console && npm run build` | 通過 | TypeScript、8 個 mention 向量及 Vite 建置成功；未同步／重啟正式服務 |
| `cd android && ./gradlew :app:testDebugUnitTest --console=plain` | 成功，UP-TO-DATE | 本次沿用 Gradle 結果；現存報告為 33 suites、142 tests、0 failures/errors/skipped |
| `git diff --check` | 通過 | 本次只新增本計畫文件 |

本次沒有跑真機 UI、移動網絡、真 Agent 發話、簽名公證或 Release 發佈。沒有重新產生或安裝 APK。

## 2. 功能進度

| 工作面 | 已有成果 | 尚未閉合的部分 |
|---|---|---|
| Go 主體 | store／歷史庫、即時流、首頁注意力分區、搜尋、附件、協作、驅動、協議和雲端 RPC | 當前常駐二進位與工作樹不一致；需同版本端到端驗收 |
| 控制台 | 會話工作台、Agent 檢測／接入／安裝、單一配對入口、資源庫、托盤頁 | 新設備頁尚在工作樹；CI 沒有獨立的 console build 檢查 |
| 手機 | 統一 LAN／Cloud 傳輸、離線快照、`@`、授權、排隊回覆、會議室入口、灰鴿 v2 | 真機冷啟動、網絡切換、通知、撤銷、多電腦尚缺本輪證據；閱讀／輸入仍有具體問題 |
| 配對和設備 | `/get` 深鏈、App 內解析落地連結、四詞核對；新改動合併 token 設備與 E2E peers，支援改名與撤銷 | 最新修正待部署後複掃；`OnlinePeers()` 只統計 LAN sockets，雲端單獨在線狀態需補準確語義 |
| 跨 Agent 協作 | mention 向量、compose、結果卡、追問／轉交、worktree 隔離與取消 | 用假 Agent 跑跨傳輸完整回路；真 Agent 回合僅在明確隔離的測試範圍驗收 |
| 會議室 R1 | `internal/relayapi/rooms.go` 已實作輪流發言、紀要、持久化和重啟轉暫停；Android 已有列表和房間頁 | 控制台尚無會議室管理頁；控制接口只有 pause／resume／end／extend，尚無點名；目前 Go 測試主要覆蓋正常輪次、提示上下文、跳過失敗者和輸入校驗 |
| Agent 擴展 | 57 家 catalog、清單引擎、ACP 和 drive-test | catalog 數量不是完整支援數；Hermes／OpenClaw／Kimi 等完整真回合仍需逐家證據 |
| 桌面／分發 | Wails、托盤、TUI、更新器、安裝腳本、Docker、release workflow | 常駐服務與桌面殼端口衝突仍在；完整安裝包、簽名公證、公開下載及官方中轉部署證據未齊 |
| 即時流／資源庫 | 即時流與 L1／部分 L2 可用 | 即時流只保留記憶體 400 跳、圖上最多 9 個 Agent；遠端 Pack／團隊作用域／簽章撤銷未完整交付 |

需停止沿用的舊待辦：267 個未提交檔案、仍在 Node、從零做 R1、從零接灰鴿 v2、重新修已完成的配對 URL 解析。它們應改成目前的保存、部署或驗收任務。

需修正的文件衝突：`acceptance.md` §3 已寫 Go 接管，但 §6 仍要求停 Node／切換；`go-architecture.md` 仍保留舊運行描述；`review-2026-11-12.md` 的開頭是更早的工作樹快照。歷史記錄保留日期，另明確標示現況入口。

## 3. 下一輪範圍與順序

建議以 **「現有核心流程可靠可用」** 作為下一輪目標。以下是工作量估計，不是承諾日期；可用手機、官方測試中轉和簽章／分發條件會影響日程。

| 順序 | 交付包 | 建議負責 | 粗估 | 完成條件 |
|---|---|---|---|---|
| P0-1 | 保存基線、對齊執行版本與文件 | 各目錄負責人；整合由用戶協調 | 0.5 日 | 10 個提交有遠端保存；新設備修正有獨立提交；二進位／JS／APK 可追溯到同一份清單 |
| P0-2 | 配對→控制→斷線恢復→撤銷 | Codex：cloud／protocol／Android；WorkBuddy 配合控制台；relayapi 歸屬先協調 | 1–2 日 | 自動化完整回路通過；手機 LAN／移動網／冷啟動驗收有證據 |
| P0-3 | 桌面殼共用已有 Go 服務 | WorkBuddy | 0.5–1 日 | 開機服務運行時仍能開視窗；關窗／退出殼不停止外部服務 |
| P1-1 | 手機閱讀、輸入與模型選擇 | Codex；需要服務端附件變更時協調 API 負責人 | 1–2 日 | 長標題、鍵盤、長模型列表、失敗圖片在窄屏可用 |
| P1-2 | R1 補齊控制台、點名與恢復流程 | WorkBuddy 做控制台，建議主責現有 relayapi；Codex 做 Android；後端歸屬開始前確認 | 2–3 日 | 兩端皆可管理 2 人 2 輪會議；點名、暫停／繼續／結束、失敗與重啟恢復通過 |

五項是本輪主要範圍。Task 6 的能力核實可以由 DSH 獨立準備；Task 7 的候選版／官方中轉準備是對外測試前置。若只有一位執行者，按上表順序做，避免同時展開多條大型改造。

## 4. 可執行工作包

### Task 1：交付基線與狀態清單

**Files:** `docs/homing-v3/acceptance.md`、`docs/homing-v3/core-flow-review-2026-11-12.md`、`docs/go-architecture.md`、`docs/parity.md`；現有待提交的 `internal/cloud/hub.go`、`internal/relayapi/{misc.go,relayapi_test.go}`、`console/src/{lib/api.ts,pages/devices.ts}`；`web/console/` 只經建置產生。

**Interfaces:** 沿用 `/health` 的 `impl`／`version`、`web/console/index.html` 的資源引用及 Android `output-metadata.json`；產出提交 SHA、版本、JS／APK SHA-256 和驗收環境清單，不記錄令牌或會話內容。

- [ ] 各負責人先核對自己留下的修改，完成 `go build ./...`、各自套件測試、console build 與提交前 gitleaks；只暫存本人路徑。
- [ ] 由整合者處理目前 `homing-v3`／`main` 的同步。不得直接照抄 `git push origin main` 就聲稱 v3 已上傳：目前本機 main 仍是舊提交。本計畫不授權切分支或重置共用工作樹。
- [ ] 在隔離端口、`/tmp/homing-*` 新資料目錄建置並核對上述清單；可使用目前空閒的 18788，但啟動前重新檢查占用。
- [ ] 準備新舊二進位和回滾步驟後，在用戶確認的時段更新常駐服務；再檢查 `/health`、控制台 JS 和 `/apk`。不得將版本字串相近視為同一產物。
- [ ] 由文件維護者更新現況及最小下一步，移除「尚需首次切 Go」的執行指令，保留歷史對照日期。

**驗收：** 新 clone 可取得本輪代碼；執行中控制台與交付清單一致；真機安裝版本有獨立記錄。最後提交依各自範圍完成，避免把其他人的半成品混入。

### Task 2：手機連線與設備生命週期

**Files:** `internal/cloud/{cloud_test.go,control_test.go,hub.go,connector.go}`、`internal/relayapi/relayapi_test.go`；Android `TransportRouter.kt`、`CloudTransport.kt`、`CloudNotificationClient.kt` 及相應 `src/test`；`console/src/pages/devices.ts`。

**Interfaces:** 保持 `/api/cloud/pair/*`、`/api/devices`、`transport: token|e2e`、加密 `/v1/ws` 與既有 RPC。沿用 `Hub.RevokePeer(peerID string) error`；設備 scopes 的既有 token／E2E 差異不偽裝成可設定。

- [ ] 擴展現有 `TestLANHubPairingRPCAndEvent`／`TestConnectorPairsThroughRelayAndRunsRPC` 測試設施，用假 Agent 接通讀取、回覆、授權、事件回傳；資料名稱一律 `demo-*`。
- [ ] 新增 `TestRevokePeerDisconnectsLANAndRejectsCloudRPC`：撤銷後原 LAN socket 失效、雲端 RPC 不再進入業務 handler，重新載入 peers 後仍不可通過。先驗證測試能捕捉缺失，再做必要修正。
- [ ] 核對雲端單獨在線的設備呈現。現有 `Hub.OnlinePeers()` 只代表 LAN 連線，不能據此斷言手機已離線；UI 至少準確標示「未局域網直連」，若加入雲端在線訊號則需以實際 peer 證據驅動。
- [ ] 在現有 `TransportRouterTest`／`ReplyReconciliationTest` 補斷線、晚到回執、重複事件的跨傳輸案例，驗證同一回覆不重送、狀態不倒退、同一事件不重複通知。
- [ ] 執行 `go test ./internal/cloud ./internal/protocol ./internal/relayapi -count=1` 及 Android 單元測試。
- [ ] 用測試手機／測試電腦完成系統相機與 App 掃碼兩條入口、舊配對冷啟動、LAN 斷開後 5 秒內雲端接管、恢復 LAN、飛行模式快照、設備撤銷；主操作覆蓋首頁／正文／回覆／授權／新建／派發。

**依賴：** 移動網驗收需可用的公網中轉及 TLS。缺少部署條件時可完成本地假中轉測試，但此項維持「真機待驗證」，不能宣稱遠程全功能完成。

### Task 3：桌面殼與常駐服務共存

**Files:** `cmd/homing/main.go`；建議新增 `cmd/homing/desktop_service.go`、`cmd/homing/desktop_service_test.go`；`internal/gui/desktop.go`。

**Interfaces:** 保持 `gui.Run(ctx, gui.Options{URL, DataDir, Version, Log})`。建議新增內部函式 `probeDesktopService(ctx context.Context, baseURL, token string, client *http.Client) (bool, error)`：可驗證的同一 Go 服務返回 true；確無服務返回 false；端口被其他服務占用或鑑權不符返回錯誤。

- [x] 寫 `TestDesktopAttachesToExistingGoService`：已有同一 Homing 時沿用 URL，沒有第二次 listen／store 開啟，退出殼不呼叫外部 server shutdown。
- [x] 寫 `TestDesktopRejectsForeignOrUnauthenticatedService` 和 `TestDesktopStartsOwnedServiceWhenAbsent`，使用本地假 HTTP server／臨時資料，覆蓋 Node、其他程序、錯誤 token、無監聽及啟動競態。
- [x] 實作服務探測與 owned／attached 生命週期分支；健康檢查之外核對本機鑑權，不自動停止占用端口的程序。
- [x] 保留無服務時的自啟動，僅由建立服務的一方執行 Flush／Close／Shutdown。關窗維持既有隱藏語義。
- [x] 執行 `go test ./cmd/homing ./internal/gui -count=1`、`go build ./...`，在隔離端口實際開關視窗和托盤驗收。

### Task 4：手機閱讀與輸入優化

**Files:** Android `ui/screens/{SessionComposer.kt,SessionHeader.kt,SessionTimeline.kt}`、`ui/{ModelPicker.kt,RichMarkdown.kt}`、`res/values/strings.xml`；測試沿用 `SessionDetailRenderTest.kt`、`SessionPresentationTest.kt`，按需要新增模型選擇／圖片降級測試。

**Interfaces:** 保留現有發送、附件、mention、權限和模型 capability 回呼。圖片仍走 `Attachment`／既有能力連結與加密檔案傳輸，不能讓手機直接存取電腦任意絕對路徑。

- [ ] 先保存長標題、長模型 ID、鍵盤彈出、長模型列表及失敗圖片的合成資料基準圖，核對實際手機版本。
- [ ] 將空輸入框由 2 行改為 1 行，最多 6 行；將次要工具收納，保留明確的 `@` 和送出入口；標題單行省略並可查看全文。
- [ ] 模型超過 12 個時提供搜尋，依 label／ID 比對；保留目前／預設／本次覆蓋區分與 capability 限制。不可切換原因在可讀的說明區呈現。
- [ ] 對未匹配附件的本機圖片路徑或載入失敗，顯示檔名和可行的下一步，不留下大塊空白。服務端自動補能力連結另由 API／files 負責人處理，先驗證檔案授權範圍。
- [ ] 僅把本輪觸及的界面文字移入資源檔，補簡體／英文；不以一次全量文字搬遷阻塞主流程。
- [ ] 在窄屏、鍵盤、長內容及大字體下做渲染／真機驗收；執行 Android 單元測試，確認回覆、附件、`@`、授權操作仍可達。

**範圍校正：** `ProjectsScreen.kt` 已有 Agent 篩選面板，灰鴿 v2 也已有 `HomingPigeonScene.kt`；先驗收，不按舊清單重做。

### Task 5：會議室 R1 補齊與穩定化

**Files:** `internal/relayapi/{rooms.go,rooms_test.go}`；新增 `console/src/pages/rooms.ts`，修改 `console/src/{main.ts,lib/api.ts}`；Android `Rooms.kt`、`ui/screens/RoomsScreen.kt`、`RoomsTest.kt`。

**Interfaces:** 沿用 `/api/rooms`、`/api/rooms/{id}`、`/messages`、`/control` 與現有 `roomSpeaker.Speak` 假實作注入點。新增 `/control` 請求 `{"action":"call","agent":"codex"}`，在當前發言完成後優先請指定參會者發言；暫停時只記錄選擇，需按繼續才執行。新增 `renderRooms(ctx: PageContext, query: URLSearchParams): PageResult`，掛到 `/console/rooms`。R1 維持只讀輪流討論和紀要；現有上限為 2–4 人、預設 2 輪、最多 5 輪。

- [ ] 新增 `TestRoomCallSchedulesParticipantWithoutInterrupting`：只接受房內可發言參與者，當前回合不被中斷、不重複計輪；非參與者返回 400，已結束／正在生成紀要返回 409，暫停時不自行恢復。依此補 additive 點名控制與兩端入口。
- [ ] 建立控制台房間列表、建房、討論／紀要和操作區，直接使用既有 Room JSON 欄位及 API；補 `main.ts` 路由／導航與 `lib/api.ts` 型別／請求。採目前原生 TS 樣式，窄屏切為單欄；頁面 dispose 時停止輪詢。
- [ ] 新增 `TestRoomPauseResumeDoesNotDuplicateTurn`，以可阻塞的 fakeRoomSpeaker 固定暫停時機，驗證同一發言只落盤一次，恢復後沿用原子會話。
- [ ] 新增 `TestRoomReloadPausesRunningRoom`、`TestRoomEndProducesMinutesOnce`，覆蓋 running／summarizing 恢復及連續點結束。
- [ ] 補全部參與者失敗、發言超時、授權掛起時的可恢復狀態；顯示下一步，避免永久轉圈。
- [ ] 依測試修正現有狀態機；不以新增 R2 或大搬目錄解決 R1 問題。
- [ ] 執行 `go test ./internal/relayapi -run 'TestRoom|TestNextRoom' -count=1`、console build 和 Android 測試；兩端完成 2 人 2 輪、點名、暫停／繼續、結束和重連操作。

**真回合邊界：** 先用假 Agent 完成自動化；不得直接使用 `acceptance.md` 裡「真實 Codex 會話」字樣向用戶正在工作的會話發話。涉及 Codex／Claude 雙方的真回合，先落實隔離會話授權和目錄。

### Task 6：逐家能力核實，作為後續接入的依據

**Files:** DSH 的 `internal/adapter/`、`internal/driver/`、`adapters/`；能力驗收記錄由對應文件維護者更新。

**Interfaces:** 沿用 `homing agents drive-test <id> --cwd /tmp/...` 與 `/api/agents.capabilities`；UI 以能力表為準。

- [ ] 先盤點可用 binary／登入／依賴，分清「可發現、可讀、可回覆、可新建、可授權、可停止」和驗證日期。
- [ ] 以安裝條件已具備的一家 ACP 為第一個完整樣本，使用隔離目錄驗證完整回覆、授權允許／拒絕／超時、取消和結果回傳。
- [ ] 在已授權的隔離真 Agent 範圍內執行 drive-test；未具備條件的條目標記 blocked 原因，不以握手成功代替完整回合。
- [ ] 僅對實測不符的適配器補回歸與修正；涉及 `agent.go` 接口由 WorkBuddy／用戶先協調。

**本輪不做：** 不因舊報告的建議直接重寫為 Codex app-server 或 Claude Channels。兩者若要採用，另做官方接口與版本能力核實、相容性試驗及回滾設計。

### Task 7：候選版和官方中轉的交付前置

這是獨立的發佈子計畫；對外測試前需完成，涉及憑證／部署的部分不計入純開發工時。

**Files:** `.github/workflows/{ci.yml,release.yml}`、`scripts/install.sh`、`android/scripts/build-release.sh`、`internal/gui/desktop.go`、`docs/self-host.md`；若加入官方預設地址，由 Codex 維護 `internal/cloud` 和 Android，WorkBuddy 維護控制台入口。

**Interfaces:** 保持安裝器的 `homing_<os>_<arch>`／`checksums.txt` 下載契約；伺服器產物使用現有 `homing-server_<os>_<arch>` 命名。官方中轉與自建均使用既有 E2E 協議，不改成明文傳輸。

- [ ] CI 加上 console 型別／向量／建置與內嵌資源一致性檢查；確保遠端 CI 跑的是本輪提交。
- [ ] 修正 release artifact 清單：目前 `dist/homing_*` 不會匹配 `homing-server_*`；兩類檔案均須被收集、校驗及發佈。
- [ ] 手動 release 的 tag 需綁定 checkout ref；驗證產物來自指定 tag，而不只是把版本字串改成 tag。
- [ ] 依目標平台補正式交付物：Mac `.app/.dmg` 與簽名公證、Android 簽名 APK／校驗碼；Windows 安裝器另作可獨立驗收的包。統一 macOS deployment target，正式桌面包關閉 `DevToolsEnabled`。
- [ ] 落實可公開下載的候選版渠道；私有倉庫的 `/releases/latest/download` 不能直接當成未登入新用戶的分發方案。
- [ ] 整理官方中轉的域名／TLS／運行與健康檢查證據，完成行動網驗收後才把「離開 Wi‑Fi 也能用」作為預設承諾。自建入口沿用既定進階選項。
- [ ] 在乾淨環境驗收安裝、升級、版本顯示與回滾；簽名 release APK 不覆蓋用戶現有 debug 安裝。

## 5. 本輪出口與下一輪新開發

本輪主要工作完成的判斷：

- [ ] 新提交及修正已保存到遠端，工作樹／運行程序／控制台／APK 有明確版本對應。
- [ ] 配對、回覆、授權、斷線恢復、離線快照、撤銷及多電腦選路有同版本證據；移動網未驗證時保留未完成狀態。
- [ ] 開機服務與桌面視窗共存，手機不因關閉視窗掉線。
- [ ] 手機主要閱讀／輸入問題可用窄屏截圖和操作重現驗證。
- [ ] R1 控制台管理與點名缺口已補齊，兩端正常流程和中斷恢復通過；未驗收的 Agent 能力如實顯示。
- [ ] 若要對外分發，Task 7 的渠道、產物來源和安裝驗收全部通過。

下一輪新增開發建議依次為：

1. **會議室 R2 的最小切片：紀要行動項→派發→結果回房間。** 複用已驗收的 compose／worktree，保留人工確認；之後再做主持／議會模式和房間記憶。
2. **更多 Agent 完整驅動。** 每新增一家就交付完整能力與故障矩陣，先累積可用支援，不以 catalog 條目數衡量進度。
3. **長期運行體驗。** 即時流歷史的保留／清除策略、超過 9 個節點的檢視、分批 i18n；設計持久化時明確處理 preview 可能含正文的保留範圍。
4. **平台化。** 遠端 Pack 更新、團隊作用域、簽章撤銷等 L3+ 功能另立規格與驗收，不混入此次穩定化。

暫不展開：R3 自由討論、自動合併程式碼、全部 57 家完整驅動、iOS、全新前端框架、全量驅動替換。

## 6. 分工與交接

- **Codex 可直接負責：** `android/`、`packages/`、`internal/cloud/`、`internal/protocol/`、`internal/server/`、`cmd/homing-server/`。
- **WorkBuddy 可直接負責：** `cmd/homing/`、`internal/api/`、`internal/gui/`、`internal/agent/` 既有接口和主要內置 Agent、`console/` 及建置的 `web/console/`。
- **DSH 可直接負責：** `internal/adapter/`；依 SPEC 已定分工維護 `internal/driver/`，新增 Agent 各自包。
- **需整合者先補歸屬：** `internal/relayapi/`、`internal/orchestrator/` 的跨模組改動和會議室主責，以及發佈流水線。現有架構表部分仍寫 G1／G5，不能自行把建議人選當成已授權。

建議開始點是 Task 1，接著由 Codex 做 Task 2、WorkBuddy 做 Task 3。下列記錄追蹤用戶要求繼續後的實際執行；未完成項目仍按原分工推進。

## 7. 首批執行記錄（2026-11-13）

用戶已要求「按你的建議繼續推進」。本批完成 Task 1 的遠端保存，以及 Task 2 的一批 Codex 範圍修正與自動化回歸，沒有把整輪計畫標成完成。

- 原有 10 個提交已經 gitleaks 掃描並保存到 `origin/homing-v3`；本機及遠端 main 未移動。
- `0b59be9`：保存 `Hub.RevokePeer`，新增真實本機 relay／LAN 測試，確認撤銷關閉 LAN、阻擋雲端 RPC，重新載入 peers 後仍拒絕。
- `070536d`：Android 路由層及加密 RPC 層只自動重送讀取請求；寫入回執丟失時回報原錯誤，下一次明確操作可用新路由。取消不再觸發重試。
- 正在收事件時每 3 秒重查路由；SSE 取消用 `awaitClose` 關閉活躍 OkHttp Call，避免阻塞讀取拖住換路。WebSocket `onClosing` 會及時結束掛起請求，讀取可改走其他地址。
- 舊式 HTTP token 空白時不再因公開 `/health` 成功而選中 HTTP；E2E 配對可繼續走加密 RPC。
- Android 候選版為 `0.11.1-dev`／versionCode `13`，單獨產生於 `output/homing-v3-stabilization/android/app/outputs/apk/debug/app-debug.apk`。SHA-256：`2c3fb42623a78d757dfee6f6e5669c02afb47bef17d24317a3b2aba0555940ac`。

**驗證：** Go build、全套 test（不使用測試快取）、vet 通過；cloud 撤銷／配對鏈路的 race 測試通過；Android 152 項測試全部通過；獨立目錄的 Debug APK 組裝成功；每次提交前 gitleaks 通過。新增失敗案例均先重現再修正。獨立審查指出阻塞 SSE 問題，已用真實延遲 socket 測試完成 RED→GREEN 修正。

**執行決策：** 遵守共用工作樹規則，未切分支或建 worktree；遠端保存使用現有分支名 `homing-v3`，避免把舊 main 誤當成本輪內容。沒有跨目錄接手桌面／控制台及尚未指定負責人的 relayapi 改動。每個活躍事件收集器會增加每 3 秒一次的輕量探測；寫入結果不明時需要使用者先核對狀態，避免自動重送。

**仍待完成：** Task 1 的其餘作者修改／運行版本對齊；真機掃碼、行動網切換、通知、離線冷啟動與多電腦驗收；cloud→LAN 反向串流及晚到／重複事件整合覆蓋；Task 3–7。桌面／控制台由既有負責人繼續，relayapi／orchestrator／發佈流水線仍需整合者確認歸屬。本批未重啟服務、安裝手機 App、發佈 Release 或向真實會話發消息；對外服務及 `/apk` 仍使用原版本。

### 遠端 CI 跟進

已手動觸發程式碼提交 `070536d` 的 CI：<https://github.com/tomato-hug/homing/actions/runs/43456131412>。協議向量、Node 雲中繼、Android 單元測試與 Debug APK 組裝均已通過；Go job 在 vet 階段因 Ubuntu 缺 Wails 所需的 GTK／WebKit 開發套件失敗，未進入 Go 測試。

已準備 `output/homing-v3-stabilization/ci-headless.patch`：將 Ubuntu Go job 的 vet／test 改用已存在的 `headless` build tag，並更正 job 名稱。補丁套用檢查、本機 headless vet／全套測試及 Linux amd64 靜態交叉編譯皆通過。用戶已確認由 Codex 接手此項，已在 `43ef680` 套用補丁並推送，重跑 CI：<https://github.com/tomato-hug/homing/actions/runs/43457398616>。此授權範圍是本次 CI 修正，桌面／控制台及其餘發佈工作仍沿用既有分工。

修正後的 Ubuntu Go job 已能通過 vet 並執行完整測試；`internal/cloud`（含本批撤銷測試）通過。但這次 Linux CI 暴露另外兩個未由本批改動的問題：

| 測試 | CI 現象 | 交接處理 |
|---|---|---|
| `internal/api.TestHomeAwayOnlyShowsNewActivityAndKeepsFinishedInResume` | 離開前完成的 `demo-home-done` 也進入 attention，預期 1 張卡得到 2 張；本機重跑 100 次未重現 | WorkBuddy 檢查 `home.go` 的 `event.Ts >= StartedAt` 毫秒邊界及測試時鐘；必要時用開始離開時的事件水位區分，不用延長 sleep 掩蓋 |
| `internal/relayapi.TestAutoAwaySettingsRoundTrip` | Linux 啟用自動離開回 409 `auto_away_unavailable`，測試卻一律期待 200 | relayapi 負責人依 GET 回報的 available 分別驗證支援及不支援情況；不應在 Linux 假裝支援 macOS 探測 |

以上位於既有 WorkBuddy／尚未指定負責人的目錄；遵守 `AGENTS.md`，本批僅定位及交接，沒有代改，也沒有為了 CI 綠燈略過這些測試。完整假 Agent 回覆／授權回路、反向換路與晚到事件整合、真機驗收仍列為 Task 2 待辦。

第二輪 CI 已全部結束：Android 測試／Debug APK、協議向量、Node 雲中繼均成功；Go vet 成功，Go 測試因上表兩項失敗而使整體 CI 保持 failure。程式碼驗證目標為 `43ef680`；其後只有進度文件提交。

## 8. 第二批執行記錄（2026-11-13）

用戶明確要求接續 CI、桌面啟動與手機介面優化。本批已完成以下開發及驗證：

| 範圍 | 結果 | 驗證 |
|---|---|---|
| 離開期間事件邊界 | 新時段在 Store 同一把鎖內記錄 `started_after_event_id`，按事件順序分類；空事件庫的零水位也會保存；舊時段缺欄位仍用原時間相容邏輯 | 同時間戳／偏移時間的確定性回歸、零與非零水位重開、舊資料相容測試 |
| 自動離開跨平台 | 用例依 `/api/away/settings.available` 驗證；不支援平台應回 409 且不能改動設定，仍可保存停用設定 | 本機 Go 測試及 Linux CI 通過 |
| 桌面啟動 | 已有服務時驗證 loopback、Go 身分、令牌和絕對資料目錄；共用已驗證服務，沒有才自行啟動；只關閉自己建立的服務 | 附著、不同 profile、錯誤令牌、redirect、相對路徑與自建／退出測試；競態檢查通過 |
| 原生桌面驗收 | 以獨立 `.app` 共用 18820 隔離服務，主視窗可用；關窗只隱藏，退出殼後外部服務仍回 200 | CUA 實際視窗／關閉／退出 + HTTP；沒有操作真實會話 |
| 手機輸入／標題 | 輸入框單行起始、最多 6 行；標題單行，點擊可查看全文並選擇重命名；模型唯讀原因在說明視窗 | 390 dp 合成場景的尺寸、文字佈局與點擊測試；原生圖形渲染截圖 |
| 模型選擇 | 超過 12 個模型可按友好名稱或 ID 搜尋，保留選擇；清單縮小時不保留看不見的篩選；使用 Homing 主色 | 大清單、搜尋無結果、清單刷新、選中／套用回歸 |
| 圖片降級 | 本機無法讀取及網絡載入失敗時顯示檔名和提示；支援引用式圖片、轉義與角括號網址；不改寫程式碼範例 | AST 單元測試、真實 MockWebServer 404 → UI 提示測試 |
| 文案資源 | 新增／修改的提示抽入簡體及英文資源 | 既有文字未作全量國際化遷移 |

程式碼提交：`ee299a1`（首頁／平台用例／桌面服務生命週期）、`518a202`（手機介面與圖片處理）。`ee299a1` 的 Linux Go CI 已轉綠；包含手機變更的完整 CI：<https://github.com/tomato-hug/homing/actions/runs/43466942110>。

**完整遠端 CI 已通過：** 上述 `518a202` 的 Go backend vet／tests、協議向量、Node 22 雲中繼、Android tests／Debug APK 四個 job 均為 success。其後提交僅更新進度文件。提交歷史的 gitleaks 掃描亦通過；本機及遠端 main 均未移動。

本機驗證：Go build／全套測試／vet、headless 全套測試與相關 race 檢查通過；Android **164 項測試、零失敗**，Debug APK 組裝及簽章檢查通過。獨立審查指出的三項問題（相對資料目錄、引用式圖片、轉義 URL）均先用回歸重現再修正。

候選產物：

- Android `0.11.2-dev`／versionCode `14`：`output/homing-v3-stabilization/android/app/outputs/apk/debug/app-debug.apk`。
- APK SHA-256：`df6fde7c2897adfd8c40a02db2de5bbb02abd9885cab4b8943dd8973f1850566`。
- 桌面原生候選：`output/homing-v3-stabilization/homing-desktop`，從 `518a202` 的乾淨 git archive 建置，沒有混入其他作者尚未提交的設備／控制台修改。
- 來源／校驗清單：`output/homing-v3-stabilization/manifest.json`；輸入區截圖：`output/homing-v3-stabilization/ui/composer.png`。

本批沒有重啟 18787、安裝手機 APK 或發佈 Release；對外服務仍是 `fb38f5e-dirty`，原 `/apk` 未被替換。剩餘驗收是新版本真機的鍵盤／大字體、同版本掃碼、行動網切換與離線／通知；下一個功能工作包仍是會議室 R1 補齊。正式簽名／公證與安裝器另按發佈計畫處理。
