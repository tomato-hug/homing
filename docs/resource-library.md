# Homing 資源庫設計與 L1 落地（2026-11-10）

這份文件保留第一版產品與技術邊界，並記錄已落地的 L1 本機資源抽屜。目標不是再抄一個公開市集，而是先做 **本機個人能力庫**：把散落在各家 Agent 目錄裡的技能、指令、MCP、hooks、子代理與專案模板收成可預覽、可安裝、可回滾的資源。

## 為什麼不是「Magpie 資源頁的複製品」

Magpie 的優勢是大量 Agent 目錄、MCP/skill 索引與一鍵路由；Homing 的差異應該是：

1. **真本機**：知道每個資源實際寫到哪個 Agent 的哪個檔案，以及是否需要重啟。
2. **真權限**：安裝前說清楚會執行的命令、可讀寫的路徑、需要的網路與憑證。
3. **真回滾**：每次都先備份，失敗不留下半套設定；移除只標記與留痕，不默默刪除使用者內容。
4. **跨 Agent**：同一個技能可以投放到 Claude、Codex、Kimi、DSH 等不同 layout，而不是綁死一家。

公開來源、評分和下載量可以後加；第一版沒有可靠審核前，不應顯示「官方認證」或「一鍵安全」。

## 六個板塊

| 板塊 | 使用者問題 | 第一版內容 |
|---|---|---|
| 探索 Discover | 有哪些能力值得裝？ | 搜尋、分類、來源、相容 Agent、最後更新 |
| 已安裝 Installed | 我現在裝了什麼？ | 版本、作用域、來源、健康狀態、更新、移除 |
| 套件 Packs | 一次配好一套工作流 | Agent Pack + 依賴、預覽變更、整體安裝／回滾 |
| 來源 Sources | 這些東西從哪裡來？ | 本機資料夾、GitHub、skills.sh、MCP Registry、自訂 URL |
| 發佈 Publish | 如何把我的能力分享出去？ | manifest 檢查、脫敏、版本標籤、產生簽章／checksum |
| 安全與審計 Audit | 它改了什麼？ | 權限、檔案 diff、安裝紀錄、來源鎖、撤銷紀錄 |

第一版先做「探索／已安裝／套件／發佈預檢／來源／安裝記錄／審計」。這些入口已可在
`/console/library` 使用；套件的本機 manifest、依賴、跨 Agent 投放與整包回滾已落地，
發佈頁只做本機 manifest／checksum／權限邊界預檢。公開市集、簽章與來源撤銷仍留在 L4。

## 資源類型

| 類型 | 例子 | 安裝位置／動作 |
|---|---|---|
| Agent Pack | 「前端 review 工作流」 | 一組 skills + instructions + hooks + MCP |
| Skill | SKILL.md + scripts/resources | `~/.claude/skills/`、`~/.codex/skills/`、`~/.homing/skills/` |
| Instructions / Rules | `AGENTS.md`、`CLAUDE.md` 片段 | 以標記區塊插入，保留原檔備份 |
| MCP Server | stdio / HTTP MCP | 產生各 Agent 的 MCP 設定，先做連線測試 |
| Hook | Stop、Notification、PermissionRequest | 產生 hook script，必要時才改 Agent 設定 |
| Workflow / Subagent | 角色提示、工具白名單、排程 | 產生 Agent 對應的子代理／工作流檔 |
| UI Plugin | IDE 或桌面 Agent 插件 | 只提供已核實的安裝方式，不冒充跨家通用 |
| Context / Memory | 專案背景、決策紀錄 | 明確作用域與過期時間，避免永久注入 |

Agent 目錄本身不是資源庫條目；它是資源的「投放目標」。兩者需要分開建模，否則又會把「Magpie 能路由模型」誤寫成「Homing 能讀會話」。

## 統一 Manifest

建議用 `homing.resource.yaml`，至少包含：

```yaml
apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: example.review-frontend
  name: Frontend Review
  version: 1.2.0
  description: Review accessibility and layout regressions.
  license: MIT
  source: https://github.com/example/review-frontend
  checksum: sha256:...
spec:
  targets:
    - agent: claude
      mode: native-skill
    - agent: codex
      mode: agents-md
  permissions:
    filesystem:
      read: ["${project}"]
      write: ["${project}/.homing-review"]
    network: false
    commands: ["node scripts/review.mjs"]
  install:
    strategy: copy
    backup: true
  uninstall:
    strategy: restore-backup
```

必要欄位：`id`、`name`、`version`、`kind`、`targets`、`permissions`、`source`、`checksum`。沒有 checksum 的本機草稿只能進「草稿」，不能標成可分享版本。

## 安裝生命週期

```text
discover → inspect → plan (dry-run) → install → verify → enable
                                      ↘ backup / rollback
```

1. **Inspect**：解析 manifest、檢查 Agent 版本、讀取目前設定。
2. **Plan**：列出將新增／修改／刪除的檔案，以及是否需要重啟 Agent。
3. **Dry-run**：不寫任何東西，回傳 diff。
4. **Install**：暫存目錄原子寫入；設定檔先備份，失敗自動回滾。
5. **Verify**：路徑存在、JSON/YAML 可解析、hook smoke test、MCP 握手。
6. **Enable**：明確列出作用域（全域／專案／單一 Agent）。
7. **Remove**：預設「停用並留痕」，另設「連同備份清理」的高風險操作。

### 信任等級

- `local-draft`：使用者本機建立，未簽章。
- `verified-source`：來源與 checksum 已固定，但未人工審核內容。
- `reviewed`：Homing 維護者看過權限與安裝腳本。
- `blocked`：已知危險或不符政策，不能安裝。

頁面必須同時顯示「來源可信」與「內容已審核」兩個維度，不能只用一顆星。

## 與技能／插件／MCP 的對接

### Skills

- 匯入 `SKILL.md` 與相鄰 `scripts/`、`references/`、`assets/`。
- 保留原始目錄結構與相對路徑，拒絕 `..` 與絕對路徑 symlink。
- 對 Codex／Claude 產生各自入口；不把不同 Agent 的 frontmatter 混在一起。
- 安裝前檢查 scripts 是否可執行，列出會呼叫的 interpreter。

### MCP

- 支援 stdio command 與 HTTP endpoint。
- 安裝前做一次握手（initialize / tools/list），失敗不寫入設定。
- 管理環境變數引用，不把 secret 明文寫進 manifest 或日誌。
- 同一 server 多 Agent 共用一份來源，但每家設定格式分開生成。

### Hooks

- 先選擇事件（Stop / Notification / PermissionRequest…），再選 Agent。
- 產生獨立 `hook.mjs` / `hook.sh`，用 `/hook/generic` 或原生 adapter 上報。
- 修改 Agent 設定時保留 marker，移除只移除 Homing 區塊。
- Codex hooks 仍可能需要使用者在 TUI 確認信任；頁面必須寫明，不能假裝已啟用。

### UI Plugins

- 只有核實過官方插件機制（例如 VS Code extension、Kilo plugin）才顯示安裝。
- 插件不是跨家通用能力；目錄要用 `pluginKind` 與 `agentId` 約束。

## 長期資料模型與 API 草案

```text
resources(id, kind, name, version, source, checksum, trust, metadata)
resource_versions(resource_id, version, manifest, archive_path, published_at)
installations(id, resource_id, version, agent_id, scope, project_path, state)
install_files(installation_id, path, action, backup_path, checksum)
audit_events(id, actor, action, resource_id, diff, created_at)
source_locks(source, revision, fetched_at, signature)
```

```text
GET    /api/library/resources?query=&kind=&agent=
GET    /api/library/resources/{id}
POST   /api/library/inspect
POST   /api/library/plan
POST   /api/library/install
POST   /api/library/installations/{id}/verify
DELETE /api/library/installations/{id}
GET    /api/library/sources
POST   /api/library/sources
POST   /api/library/publish/check
```

所有寫入類 API 都要有 `dry_run` 或分開的 plan/apply 兩步；回傳 diff 而不是只回 `ok: true`。

### L1 已落地 API

```text
GET    /api/library
GET    /api/library/sources
GET    /api/library/sources/available
POST   /api/library/remote/plan
POST   /api/library/remote/install
GET    /api/library/installations
POST   /api/library/installations/{id}/update/plan
POST   /api/library/installations/{id}/update
POST   /api/library/installations/{id}/rollback
GET    /api/library/packs
POST   /api/library/packs/plan
POST   /api/library/packs/install
POST   /api/library/packs/{id}/rollback
GET    /api/library/audit
GET    /api/library/resources/{id}
POST   /api/library/inspect
POST   /api/library/plan
POST   /api/library/install
DELETE /api/library/resources/{id}
POST   /api/library/publish/check
```

讀取需要 `read` scope，寫入另外需要 `drive` scope，並受每分鐘 20 次的資源庫寫入限流。目前實作範圍：

- 掃描 Homing 自己的 skills／instructions／integrations，以及本機已核實的 Codex skills／rules、Claude skills／subagents、WorkBuddy skills、Agent Skills、Kimi、Gemini、Antigravity、OpenCode（含 `~/.config/opencode`）與 DSH presets。未知或沒有樣本的目錄不猜。
- 同一技能跨多個目錄時合併成一張資源卡，詳情列出每個實際路徑；只有 `~/.homing` 下的路徑標為可移除。Agent 原生技能若是 symlink（例如 Gemini／Antigravity／OpenCode 共用 Claude 技能），仍會列出該 Agent 目標，但標記為只讀，不會跟隨連結刪除目標。
- 匯入只接受本機檔案或目錄，先 inspect 與 plan，拒絕 symlink、裝置檔與越界路徑；確認後才複製到 `~/.homing`。
- 移除只把 Homing 管理目錄改名移入 `~/.homing/trash`，不碰 Agent 原生目錄，也不執行第三方安裝腳本。
- 審計只記錄時間、設備、動作、資源 id 與類型，不保存技能正文。
- 遠端匯入已支援 GitHub repository／tag／commit、skills.sh、直接檔案或 `.tar.gz`／`.tgz`／`.zip`，以及 MCP Registry 的 `server.json` 描述匯入；下載上限 40 MB。
- 遠端流程固定為「plan → 顯示 checksum／diff → install」，下載內容先落到 `~/.homing/cache/library`，確認後才複製；`installations.jsonl` 保留來源、解析後 URL、SHA-256 與回滾狀態。MCP 只保存描述，不安裝或啟動 server。
- 遠端安裝記錄可先 `update/plan` 再 `update`：plan 會重新下載並比對 manifest 版本與內容 checksum，apply 只接受同一個安裝目的地，並在覆蓋前把舊目錄改名到 `~/.homing/library/backups/`。
- 更新後 rollback 會把新版目錄移入回收區，再從備份還原舊版；記錄同步回到舊版本與 `installed` 狀態，並保留 `rolled_back_at` 作為操作痕跡。初次安裝沒有可還原備份時，rollback 仍標記為 `rolled_back`。所有動作都不碰 Agent 原生檔案。
- Agent Pack 支援一個 `homing.pack.yaml` 宣告多個 skill／instructions／hook；先 plan
  再 install，plan 會檢查依賴拓撲、checksum、重複目的地與現有衝突，apply 只接受同一個
  plan id，並在安裝前重新核對 checksum。
- 每個 Pack 成員可以同時投放多個 target。技能已核實的 layout 包括 Homing、Claude
  Code、Codex、WorkBuddy、Kimi、Gemini CLI、Antigravity、OpenCode 與 Agent Skills；
  其他資源類型目前只投放到 `~/.homing`，不會猜測未核實的 Agent 設定格式。
- Pack 安裝先複製到 plan cache 的 stage，全部成功後才逐項 rename 到目的地；中途失敗會
  移除本次已寫入的目錄。回滾只移動這個交易建立的成員目錄，保留各 Agent 的根目錄，
  資源名稱與 checksum 會留在 `pack-installations.jsonl`。
- `POST /api/library/publish/check` 只讀取本機目錄：要求目錄內有可通過
  `homing.resource.yaml` 驗證的 manifest，回傳內容 SHA-256、檔案預覽、targets、
  filesystem／network／commands 權限、manifest 名稱與版本。manifest 缺失／損壞、
  缺 `metadata.checksum` 或缺內容 checksum 是 blocking `problems`；kind 不一致、
  檔案衛生、network 與命令宣告是需要人工看的 `warnings`。任何一類都會讓 `ok=false`。
  此接口不簽章、不上傳，也不執行 manifest 宣告的命令。

尚未落地：遠端 Pack 的下載與更新、專案作用域／團隊共享、簽章與來源撤銷、公開評分與
社群發佈。頁面不會把這些能力顯示成已完成。

### 本輪驗收

- `/console/library` 實機掃描到 40 項資源、34 個技能、4 個工作流、1 個 Homing 接入套件與多個本機來源；Kimi 桌面版套件標為可回滾，Agent 原生技能標為只讀。
- 搜尋與類型篩選、資源詳情、來源頁、remote plan 表單、安裝記錄與審計頁都已接到 Go API。手機寬度下卡片改為單欄，詳情使用覆蓋式抽屜。
- 「發佈」頁用 `/tmp` 內的合成資源實測：manifest 名稱、v1.0.0、兩個 targets、
  filesystem 讀寫、network=false、commands 與 sha256 都正確顯示；宣告命令時顯示
  「需要人工確認」，不會執行命令。390 px 寬度下頁面 `scrollWidth` 為 375 px，沒有
  水平溢出。

## 分階段落地

### L1：本機資源抽屜

- 掃描 `~/.homing/skills`、`~/.homing/integrations` 與已接入 Agent 的 skills/instructions。
- 顯示類型、來源、作用域、最後修改、能否移除。
- 匯入單一本機資料夾；先只支援 copy，不跑第三方命令。

### L2：Plan / install / rollback

- GitHub raw / archive + skills.sh + MCP Registry 匯入。
- manifest 驗證、checksum、plan diff、備份、安裝、健康檢查。
- 安裝紀錄與一鍵回滾。

### L3：Agent Pack 與跨 Agent 投放

- 一包多資源、依賴解析、每個 Agent 的 layout adapter。
- 專案作用域與團隊共享；衝突偵測。

已落地的部分：

- 本機 Pack manifest、資源 ID 依賴與 cycle 偵測；
- 同一技能同時投放 Homing／Claude／Codex／WorkBuddy／Kimi／Gemini／Antigravity／
  OpenCode／Agent Skills；
- plan token 30 分鐘有效，apply 前重新計算每個成員 checksum；
- 全量 staging、逐項換入、失敗回收與整包回滾；
- `pack-installations.jsonl` 只記錄 pack、版本、成員、目的地與 checksum，不保存正文。

仍待後續：遠端 Pack 來源、專案作用域、團隊共享與跨機器同步。

### Pack manifest 範例

```yaml
apiVersion: homing.dev/v1alpha1
kind: AgentPack
metadata:
  id: demo.frontend
  name: Frontend Workflow
  version: 1.0.0
  description: Review and test a frontend change
  source: local://demo
spec:
  resources:
    - id: review
      path: resources/review
      kind: Skill
      name: Frontend Review
      targets:
        - agent: homing
          mode: managed
        - agent: claude
          mode: native-skill
    - id: tests
      path: resources/tests
      kind: Skill
      name: Frontend Tests
      requires:
        - review
      targets:
        - agent: codex
          mode: native-skill
```

目錄中的 `resources/review` 與 `resources/tests` 各自需要 `SKILL.md` 或可辨識的入口檔。
路徑必須在 Pack 根目錄內；絕對路徑、`..`、符號連結與特殊檔會被拒絕。plan 不會寫入任何
Agent 目錄，install 也會先重新核對來源，避免「預覽的是舊版、裝的是新版」。

### L4：發佈與治理

- 簽章、版本鎖、權限掃描、來源撤銷。
- 只在這一步考慮公開搜尋、評分與社群提交。

## 與跨 Agent 調度的接點

調度解決「任務去哪個 Agent 執行」；資源庫解決「那個 Agent 有什麼能力」。兩者共用 Agent 目錄與工作目錄，但不要混成同一個開關：

- 一個 Agent 可以可讀會話、不可回覆、也不可被調度。
- 一個 Skill 可以投放給多個 Agent，但不是所有 Agent 都能啟動新會話。
- 調度卡片可以顯示目標 Agent 已安裝的 pack，但首版不必自動選擇技能。
- 未來的「Agent Pack」可以宣告 `dispatchable: true` 與啟動入口，通過驗證後才進入調度選單。
