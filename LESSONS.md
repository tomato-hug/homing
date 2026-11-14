# LESSONS

- Codex 的 apply_patch 是 custom_tool_call，不按 name 映射会显示成普通 tool_call；在 internal/agent/codex 里用 parsePatchFiles 映射成 file_change，并保留 files。
- Codex 的退出码不止 "Process exited with code"，常见 "Exit code: 0"；用和 Node 相同的正则 `(?i)(?:Process exited with code|exit code|Exit code:?)\s*(-?\d+)`。
- Codex 注入的系统提示 role=developer，且不都以 `<` 开头；按 role 跳过，用户消息仍按 Node 的 NOISE_PATTERN 过滤，别用前缀 `<` 一刀切。
- verify-go.sh 对照期间当前 Codex 会话仍在写，include_empty 边界条目会有一次性的 last_ts / title 差异；先看 diag-sessions.log 是否只是当前会话。
- 在 zsh 里不要把循环变量命名为 `path`：它和 `PATH` 绑定，会覆盖命令搜索路径，后面 `curl`、`ls` 等会突然找不到。
- DSH / OpenClaw 的正文在 `data.message` / `message` 嵌套层，`generic-jsonl` 会读成 0 条正文；用专用 `format: dsh` / `openclaw`。
- catalog 里没有推送信号的条目必须把 `push_signals` 编成 `[]` 而不是 `null`；前端或脚本按 `.length` 读时，`null` 会让整页渲染失败。
- Magpie 的 `agent-*` 外挂是把该 Agent 的模型请求接到 Magpie gateway；不要据此推断 Homing 能读它的会话或收生命周期事件。
- 接入套件模板用 `text/template` 的 `missingkey=error` 时，字段名大小写必须与 struct 完全一致；新增条目后要为全部 catalog 跑一次生成与 `node --check`。
- Claude Code 的 `claude -p` 启动时不给 session id；跨 Agent 调度要先用 request_id 建 `waiting_session`，等同 source/cwd 的 Stop hook 到达后再绑定子会话，不能把 `started` 当成已关联。
- Android 云端 RPC 的 fake relay 测试要在 finally 先关服务器端 WebSocket 再 `MockWebServer.shutdown()`；`runBlocking` 测试方法要明确返回 `Unit`，否则 JUnit 会以 InvalidTestClassError 初始化失败。
- 会话页 SSE 只在带 `session_id` 时启动 `internal/live` 尾随；第一版每 1 秒重读 `ItemReader` 尾部并按 `seq` 去重，首个轮询只建立水位不补发，避免 32 格 named hub 被历史条目一次性塞满后丢帧。
- 设备 `scopes` 为空必须解释成“旧设备拥有全部权限”，不能当成“没有任何权限”；否则升级 Go 版后所有已配对手机会立刻 403。
- 审计日志不能只靠调用方自觉不传正文；`Deps.Audit` 用允许字段白名单写 `audit.jsonl`，新增字段时要显式加进白名单。
- Android 的 `@agent` 输入框预解析只用于 UI 提示；正式发送走 `/api/compose`，服务端返回 409 时不要保留“失败”的乐观父消息，因为父会话根本没有收到。
- 控制台页面先 `append` 容器再 `clear(root)` 会把容器從文件移除，後續 `paint()` 只更新 detached node，畫面永遠停在載入中；載入佔位符要放進其中一個常駐容器。
- 協作頁不要用 `Promise.all` 等 500 條會話掃描才畫調度紀錄；先畫 dispatch，再背景補標題，並對 6 秒輪詢加 in-flight guard。
- 權限模式必須在 `/api/reply`、`/api/sessions/new`、`/api/compose` 的副作用前用 `/api/agents.permission_modes` 校驗，否則非法值會穿透到 CLI。
- SQLite FTS5 的 `content=''` 表不能直接 `DELETE`；索引要用 `contentless_delete=1`，舊資料庫需先 drop FTS 表並從 `indexed_items` 重建。
- ACP 實測（2026-11-10）：Hermes `hermes acp` 在 `/tmp` 新目錄啟動後回報 `ACP dependencies not installed`，需先安裝 `.[acp]` 才能握手；OpenClaw 第一次 75 秒逾時，第二次以 180 秒完成 `initialize → session/new → session/prompt → end_turn`，只收到 commands／info／usage 更新，沒有正文，不能用一次短逾時判定 ACP 不可用。
- SSE 的 `WriteHeader` 後要立即 `Flush`；大幀或首帧較慢時，瀏覽器／Android 可能等到連線關閉才看到事件。另全局 `/api/stream` 不可用單一 `seq` 去重，必須以 `session_id + seq` 或 `item.id` 區分會話。
- Console 的即時 item 合併不能只更新資料陣列：`insertTranscriptItem` 必須渲染合併後物件，否則短 `partial` 幀仍會把畫面覆蓋回較短文字；`truncated` 預覽也要保留既有較完整的 text／tool／diff。
- 快速接入的端到端驗收要用臨時 `HOME` 與資料目錄：`dsh` 生成的 hook 可在 18791 寫入 demo 事件，移除只產生 `.deleted-*`；不能用真實 `~/.homing` 或已接入 Agent 當測試資料。
- OpenClaw 的 ACP 短逾時（30 秒）足以完成 initialize／session/new，但不足完成 session/prompt；要驗證完整回合需保留長逾時並確認 Gateway 在線，不能把短逾時解讀成握手失敗。
- Codex 的排隊撤回測試不能在持有 `threadMu` 時再呼叫 `CancelQueuedReply`；先完成取消與狀態斷言，再單獨鎖住讀取佇列，並用 `t.Cleanup` 恢復全域 `threads`。
- 後端自訂標題的 `title_source` 是 `homing`，不是舊的 `custom`；Android `resolveSessionTitle` 的 native sources 必須同時包含兩者，否則伺服器已保存的標題會被前端丟回專案名。
- `HomingHomeScreen` 拆分後，公開的 `HomingHomeScreen` 只負責組裝；`HomeAttentionZone` 等 internal composable 不可在組裝檔重複定義，否則 Kotlin 會報 conflicting overloads。
- Console 外殼不可把 `/api/connection` 與較慢的 `/api/home` 綁在同一個 `Promise.all` 才顯示上線；先畫連線，再補會話計數，否則首屏十幾秒都會誤報「本機服務未回應」。輪詢本身也要 in-flight guard，避免慢請求重疊。
- Codex 會話列表慢不是 rollout 解析本身，而是 `metaFor` 每次重讀 600+ 行 `session_index.jsonl`，且 `rolloutPathFor` 對已被清理的 session 反覆重建整份索引；兩者都要用 mtime/size 快取，負結果也要在 TTL 內直接回空，97 個會話可從 6 秒降到 30–100ms。
- 在 macOS 上從一次性工具殼以 `nohup` 啟動預覽服務，工具退出後子進程仍會被清掉；改用 `launchctl submit -l <label> ...`，或保持在同一個 PTY 會話中啟動。
- `@agent` 覆蓋項要按 mention 出現順序對齊，不能在掃描時改動候選陣列；續接既有協作線時，`workspace: auto` 應視為沿用原線，不是重新按 preset 猜工作區。
- `/api/home` 對已清除的離開時段直接不輸出 `away.period`；不要只靠 Android／控制台的畫面條件過濾，否則新客戶端會重新顯示已看完的摘要條。
- 協作 worktree 要在建立時保存 `repo_cwd` 與 `base_commit`；完成後才能從 checkpoint 產生 `git diff --stat`，清理時也不必猜主倉庫。丟棄接口只接受資料目錄 `worktrees/` 下的 checkout，且執行中任務必須先取消。
- ACP 能力不能只看目錄條目：`Check` 的 `--help` 輸出要用 `CheckRequire` 確認含 `--acp`，否則舊版 binary 會被誤報成可驅動；內置掃描器（Kimi／OpenCode）由 relayapi 通用回退接到 driver，不要為了加啟動能力而覆蓋原本的 scanner。
- 資源庫更新不得直接覆蓋既有目錄：先在同一父目錄 stage，舊版 rename 到 `~/.homing/library/backups/`，再原子換入；更新後的 rollback 要把新版移到 trash、把備份搬回目的地，並把記錄恢復成舊版本／`installed`，只留 `rolled_back_at` 作為痕跡。
- 多家 Agent 會用 symlink 共用同一份技能（例如 Gemini／Antigravity／OpenCode 指回 Claude）。資源庫要把 symlink 目錄列成該 Agent 的只讀 target，統計與預覽時解析目標，但絕不能把連結本身或連結目標當成 Homing 管理目錄刪除。
- Agent Pack 的 plan token 不能只保存畫面文字：apply 前要重算每個成員 checksum、重建每個 target 的目的地並拒絕重複路徑；先全部 stage 再 rename，記錄寫入失敗時也要回收本次已換入的目錄，否則會留下無法回滾的半套安裝。
- 控制台的全域快捷鍵要先判斷是否有模態開啟；否則在新會話輸入框按 `⌘Enter` 會穿透去送背後的父會話訊息。模態關閉後的焦點返還也不能保存舊 DOM 節點，左欄輪詢會重建「新會話」按鈕，要用資料屬性重新查詢並在下次重繪保留焦點。
- 會話資訊抽屜不要在所有桌面寬度都當第四欄：1440 px 下四欄會把正文壓到約 360 px；
  停靠門檻設在 1560 px，以下改成右側覆蓋，關閉後才把焦點還給抽屜入口。
- 正文索引的「刪除」必須同時關閉，且 `GET /api/search/status` 在索引檔不存在時不得
  `Open` 一個新的 SQLite；否則設定頁一刷新就會把剛刪掉的索引重建回空檔。
- 設定頁執行非同步操作後要先清 `busy` 再 `load()`／重繪；若在 `finally` 前重畫，
  新產生的按鈕會沿用舊的 disabled 狀態，直到手動刷新才恢復。
- Console 資源庫的分頁切換不能只重畫內容；搜尋框、類型篩選與導入按鈕屬於 toolbar，
  必須同時 `paintToolbar()`，否則切到「發佈／來源／審計」仍會看到探索頁的操作。
- 發佈預檢的 `ok` 只能代表本機 manifest 與檔案衛生全部通過；缺 checksum、manifest
  損壞或命令宣告都要如實分開顯示，不能把 checksum 說成簽章，也不能執行 manifest 命令。
- 「思考過程不要傳到手機」不能只改 REST：`/items`、SSE `item`、`/export` 與雲端
  bridge 是四條獨立出口；預設過濾後還要讓 Android 對舊 Node 中繼自行再擋一次，
  否則切換實作前仍會漏。桌面要保留思考檢視時，由控制台明確帶 `include_reasoning=1`。
- 把 SVG 品牌 logo 內嵌成 Compose `Path` 時要依原 SVG 的 `fill-rule="evenodd"`
  設定 `PathFillType.EvenOdd`；否則多層 path 會填成一塊實心色，縮到 20 px 就像
  普通方塊，失去原 logo 的辨識度。
- 會話列品牌圖示不能沿用 16 px 的字母徽章尺寸；官方 Codex 圖標本身含白色底板，
  縮得太小只剩白方塊。列表用 24 px 容器、18–20 px 圖標，Android 小頭像再把 logo
  佔比提高到 78%，才能保留原標誌輪廓。
- T3 Code 的「待處理貼近輸入區」只能是同一事件的可操作副本，不能拿 dock 取代時間線；
  否則使用者失去前後文，搜尋與新未讀定位也會找不到原始節點。
- 排隊回覆沒有原地編輯 API 時，必須先成功撤回舊 `reply_id`，再把文字放回輸入框；
  先填入再撤回會讓使用者以為同一訊息已修改，實際仍可能重複排隊。
- 手勢整理不能成為唯一入口：滑動置頂／歸檔必須同時保留長按選單與 Undo，否則誤觸後
  使用者無法在鍵盤、螢幕閱讀器或單手模式完成同一操作。
- 控制台把寬屏停靠門檻提前到 1500 px 時，必須同時覆蓋舊 `max-width:1560` 的
  `position:absolute` 抽屜規則；只改 grid-template-columns 會留下空白第四欄。
- 借鑒 T3 Code 只搬資訊層級與操作位置；不要在 Compose 硬仿 Expo Glass、Live Activity
  或它的 runtime file API，Homing 沒有等價契約時會做出不可操作的假面板。
- 工具调用是过程而不是结果：Android 时间线要把连续 `tool_call` 折成一个可展开摘要，
  默认不渲染参数、输出和退出码；过滤只发生在显示层，搜索与原始 item 数据必须保留。
- `codex exec resume` 会与桌面 Codex 争同一个 thread writer，典型错误是 `thread-store
  conflict / active writer`；优先用 `codex queue --resume` 并确保 app-server daemon 在線。
  `queue` 可能整回合存活，探測 800ms 後就可回報已接收，但不要提前殺掉子進程。
- relayapi 的 compose 測試若會送父回覆，必須在 PATH 放假的 `codex`；否則新的同步
  queue 探測會把 demo session 送進開發者真正的 Codex daemon，測試會以 502 失敗。
- `SwipeToDismissBox` 的置頂／歸檔背景只能在 `dismissDirection != Settled` 時繪製，
  會話列本身要用不透明 Surface 並 `clipToBounds()`；否則手勢提示會透到標題與時間下。
- 收件箱模式只能留一份來源：`MobileInboxPreferences` 決定手機可見範圍，`/api/away`
  只做後端同步；兩邊各自可切換會讓 UI 同時出現兩個「離開」概念。
- Node 的 ESM import 早於 `server.mjs` 解析 argv；`--data-dir` 必須在
  `relay/platform/index.mjs` 計算 `dataDir` 前先寫入 `HOMING_DATA_DIR`，否則隔離
  測試會誤讀正式資料目錄。驗證時先看啟動日誌的「数据目录」再發任何回覆。
- 「新建任務可選 Agent」不能用 `installed` 判斷：WorkBuddy 需要桌面端進程正在運行，
  一般 Agent 也要確認 `capabilities.new_session` 與 ACP readiness；一律以
  `/api/agents` 的 `available && capabilities.new_session` 為準。
- 橫向 `LazyRow` 的離線卡片若在畫面外，Compose 測試會先找不到節點；先
  `performScrollToNode`，再以 `useUnmergedTree=true` 讀取被父層 clickable semantics
  合併掉的「在線／離線」文字。
- `com.goterl:lazysodium-android:5.1.0@aar` 在 Gradle 8.13 的 Android runtime
  依賴樹不會自動帶入 POM 宣告的 JNA；只寫 lazysodium 會在第一次配對時以
  `NoClassDefFoundError: com.sun.jna.Native` 崩潰。必須明寫
  `net.java.dev.jna:jna:5.12.1@aar`，並用 `apkanalyzer` 確認 DEX 有
  `com.sun.jna.Native`、APK 有 `libjnidispatch.so`。
- Android 16 目標仍要用 `zipalign -c -P 16 -v 4` 檢查 native library；
  JNA 5.12.1 的 arm64 `libjnidispatch.so` 的 LOAD segment 是 64 KB 對齊，
  可通過 16 KB 檢查，不要只憑版本號假設不相容。
- 回覆狀態不是「最後寫入一定贏」：`injected`／`held` 之後若再收到 queued，
  必須視為過期快照。Node 的追加式回放、Go Store、HTTP 回執與 SSE 合併都要
  共用同一條單向規則，否則已送達訊息會永久留在可撤回的排隊管理。
- 掃描器的 `installed`／`available` 只證明有本機資料或程式檔案，不能推導出
  可新建。Android 缺少 `new_session` 必須 fail-closed；Node 也要分開回報
  `new_session_ready`，否則 OpenCode 會因資料庫存在而出現在新任務清單。
- WorkBuddy 5.9+ 的 `18488` 不是舊 ACP bridge，`/api/v1/acp/connect` 回 404；
  舊 bridge 的 `session/new` 又會重用使用者目前會話。只能保留讀取／回覆，
  在新版 app-server 協議核實前不要把 WorkBuddy 列為可新建。
- 新任務 API 對不支援的來源應回 `409 + code`（`new_session_not_ready` 或
  `agent_not_driveable`），不要回「source 只能是…」；後者會讓使用者誤以為
  只要改 source 就能繞過真正的驅動缺失。
- Codex `queue --resume` 的 `failed to resume` warning 可能在 800ms 探測後才出現；
  此時訊息已交給原生隊列，不能在 close handler 再降回 queued。用探測時是否已
  handed off 區分「尚未接手」與「接手後命令才結束」。
- queued 回覆何時算被消費，不能靠固定時間猜；同一 session 後續的 `reply` 事件
  是最可靠的送達證據。多條 queued 必須一對一消費，不能一條回覆事件就清空全部。
- Android 負數樂觀 reply 收到第一個帶正數 reply_id 的 SSE 時要立刻重映射 key；
  否則 POST 回執較晚到達會刪掉負數 key，再以舊 queued 覆蓋已 injected 的狀態。
- 回覆或新建請求的回執丟失，不代表服務端沒有執行；HTTP／加密 RPC 換路只自動重送讀取請求，取消必須直接向上傳遞。
- SSE 卡在阻塞讀取時，普通 invokeOnCompletion 要等協程結束才取消 socket，會拖住 flatMapLatest 換路；用 awaitClose 立即取消活躍的 OkHttp Call，並在 execute 前檢查取消。
- E2E 配對只保存公鑰和電腦資料，不提供舊式 HTTP token；公開 /health 成功不能讓無 token 的手機選中 HTTP，應走加密 RPC。
- 離開起點與事件可能落在同一毫秒；新時段記錄 started_after_event_id，以事件水位分先後，舊時段保留時間相容判斷。
- 桌面共用常駐服務時，要核對 Go 身分、本機令牌與絕對資料目錄；附著的視窗不能另開 Store，也不能在退出時關閉外部服務。
- Markdown 圖片降級要解析引用定義並沿用渲染器反轉義；將引用與角括號網址轉成渲染器可用的行內格式，程式碼範例維持原文。
- 真機渲染鏈上的連結改寫也必須解析 Markdown AST；只補圖片降級仍擋不住上游 regex 改寫程式碼或把電腦絕對路徑拼成錯誤的 HTTP URL。
- 会议恢复须先保存 active_turn 槽位、派发 ID、基线与 submitting 边界，重启只回收原派发；投递不明或写盘失败就暂停，不能继续排程或自动重送。
- Android 模拟器在 ExFAT 上会卡在 hard-link 文件锁；本轮改用外接盘上的临时 APFS 映像，测试数据保留在内部盘以避免 HTTP 验收被外接 I/O 超时干扰。
