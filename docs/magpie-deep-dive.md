# Magpie 深挖：多会话治理 · 多 agent 扩展 · 品牌与通知 → 手机端重设计依据

> 承接 [`magpie-ui-analysis.md`](./magpie-ui-analysis.md)。那一份解决「会话行怎么画」，这一份解决「会话多了怎么不乱、新 agent 怎么加、品牌与提醒怎么做」。
> 只读：不改 magpie、不改任何配置、不登录。源码取自 `yetone/magpie@main`（tarball 落地 `/tmp/magpie-src`），本机核实部分不读取对话正文、不读取任何 token。
> 本机 magpie 版本 0.1.3219；分析时仓库 HEAD 的 `internal/agent` 有 **76 个内置 agent**、`internal/sessions` 有 **13 类会话解析器**。

---

## 0. 三句话结论

1. **magpie 不让列表变乱靠的是结构，不是管理功能**——它没有置顶、静音、已读、收件箱，连「未读」都没有。它靠「agent 单选 → cwd 分组 → 默认折叠 → 空会话不列 → 每批 100 条」把熵压下去。
2. **加一家 agent 是两处注册**：`internal/agent/<x>.go`（接线：写配置、模型、能力）+ `internal/sessions/parserFor` 一个 case（会话发现）。13 类解析器覆盖 76 个 agent，说明多数 agent 共用格式。
3. **品牌与提醒都是「克制」的范例**：一只单色墨鸟贯穿托盘/应用/web；通知只发五类、turn 结束只在「超过设定时长」时才发，且点击 turn 通知**不跳回 magpie**。

---

## 1. 多会话治理：magpie 怎么防止列表变乱

### 1.1 五条结构性收敛（没有一条是「管理功能」）

| # | 机制 | 源码 | 效果 |
|---|---|---|---|
| 1 | **agent 单选分段** | `internal/gui/assets/plugins/sessions/client.js` 的 `head()`：`segs.regions.sm-agents`，每项 `icon+name+count`，`aria-pressed`，切换即换数据源 | 屏幕上永远只有一家 agent 的会话 |
| 2 | **cwd 分组 + 默认折叠** | 同上 `folder()`：`const open = !!q \|\| opened.has(cwd)`，`opened` 是内存 Set，默认空 | 首屏只看到项目名，不看到几百条会话 |
| 3 | **scratch 折叠到末尾** | `client.js:95-103` 的 `scratch(k)`：根目录、`/tmp`、`/var/folders`、`~/.*` 判为「不是你的项目」，归入「其它」 | `/`、UUID 目录不再占据顶部（注释点名了这次事故） |
| 4 | **空会话不列出** | `internal/sessions/sessions.go:2000`：`if s.Title == "" && s.Tokens.zero() { return s, false }` | 只有启动记录、一句话没说的会话根本不进列表 |
| 5 | **每批 100 条分页** | `client.js:74` `SM_PAGE = 100`；`drawnIn` 计数 + 「Show N more」 | 单个项目内也不会一次渲染几千行 |

### 1.2 默认视图显示什么

- **默认选中会话数最多的 agent**：`internal/gui/sessions_manage.go:186-193`——`?agent=` 参数优先，否则 `out.Agents[0]`，而 agent 列表按 `Count` 降序、`Agent` id 升序排序（同文件 179-184）。
- 该 agent 的会话按 **cwd 分组、组内按 `last` 降序**；文件夹全部**收起**。
- 每行显示：标题 + `时间 · agent · 模型 · 消息数 · 体积 · id8`（详见上一份文档 §2.1）。

### 1.3 隐藏规则

| 规则 | 行为 | 源码 |
|---|---|---|
| 空会话 | 不列出（无标题且零 token） | `sessions.go:2000` |
| Codex 归档会话 | **仍然读取**：扫描目录是 `["sessions", "archived_sessions"]` 两个 | `sessions.go:1015` |
| scratch 目录 | 不删除，折到列表末尾 | `client.js:95-103` |
| 网关独有会话（无本地文件） | 单独成组，组名「Seen through the gateway」，哨兵 key `"\x00gateway"` | `client.js:88-90` |
| 网关会话保留期 | **7 天**后过期 | `internal/sessions/gateway.go:27` `GatewayRetention = 7 * 24 * time.Hour` |
| 读不完的文件 | 标 `pending`，1.5s 后补拉一次；**不隐藏** | `client.js:121-140` |

### 1.4 归档 / 置顶 / 静音 / 已读 / 过期

**magpie 一个都没有**。证据：

- `grep -n "pinned\|mute\|Muted\|unread" internal/gui/assets/plugins/sessions/client.js` 只命中搜索状态里的一个 `unread` 旗标（表示「这个 agent 的会话文件读不到，只能搜网关记录的」），与消息未读无关。
- 没有 `inbox` 概念：`grep -rn "inbox" internal/gui/assets/app.js internal/gui/assets/plugins/*/*.js` 零命中。
- 归档只作为**被读取的目录**存在（Codex 的 `archived_sessions`），magpie 自己不提供「归档」动作。

它提供的会话级动作只有五个（`internal/sessions/manage.go`）：

| 动作 | 函数 | 说明 |
|---|---|---|
| 删除 | `Delete(agent, id)` | 进 magpie 自己的回收站 `TrashDir() = <settings>/trash/sessions`，不是真删 |
| 回收站列表 | `Trash()` | 可 `Restore(key)` 还原 |
| 永久删除 | `Purge(key)` | 从回收站清掉 |
| 迁移项目目录 | `askRelocate`（前端）+ relocate_claude.go | 仅 Claude Code，整体搬 `projects/<slug>` |
| 导出 Markdown / 换 agent 继续 | 前端 `sess-export` / `carryPick` | 见上一份 §3.2 |

> 这解释了 magpie 的产品判断：**它把「治理」定义为「别让不该出现的出现」，而不是「让用户去整理」**。置顶/静音/已读是给「已经乱了」的列表打的补丁，magpie 选择从源头减少条目。

### 1.5 搜索与快捷筛选

搜索（`client.js:205`）：

```js
list.filter((s) => hitOf(s) ||
  [s.title, s.cwd, s.id, ...(s.models||[]).map(m => m.model)]
    .some(x => (x||"").toLowerCase().includes(q)))
```

- 字段：`title`、`cwd`、`id`、模型名。
- **还搜正文**：服务端 `sessions/search` 返回 `hits: Map(id → {before, match, after})`，行内以 `<mark>` 高亮追加一行（`client.js:222-260`）。
- 返回还带 `cut`（结果被截断）、`searched`（搜了多少文件）、`total`、`unread`（该 agent 文件不可读的提示）。
- 搜索时**强制展开所有文件夹**（`open = !!q || …`），退出搜索恢复原折叠状态。
- 快捷筛选只有两个：**agent 分段** 与 **回收站开关**（`.sm-trash-btn`）。没有按模型/分支/时间范围的筛选器。

### 1.6 批量操作

- 「Select」进入选择模式：行首出现 checkbox，文件夹行有 indeterminate 全选（含已折叠的会话，`client.js:folder()` 的 `c.indeterminate`）。
- 底部出现 `.sm-bar`：计数、全选、删除、清空选择。
- 文件夹级 `⋯` 菜单：`Relocate project`（仅 Claude）、`Delete all sessions`。
- 危险动作有前置校验：最近一分钟内还在写入的会话不删（`manage.go` 的 active 判定，前端文案「{title} is still being written to; close it in {agent} and try again in a minute」）。

### 1.7 对手机端的直接结论

手机屏比桌面小一个数量级，magpie 的五条收敛**必须全抄**，而且要再收紧一档：

- agent 分段在手机应该变**可横向滚动的 chip 条**或下拉（magpie 自己在窄窗就是这么退化的：`.sm-agents` → `.sm-agent-pick`）。
- cwd 分组在手机建议**默认平铺、可切换分组**（分组视图的行动作是「展开/收起」，在手机上多一次点击）。
- 空会话不列 + 7 天过期可以直接抄，这两条最省事、效果最好。

---

## 2. 多 agent 扩展架构：加一家要写什么

### 2.1 两个注册点（必须都加，缺一个就只有半条命）

```
internal/agent/<x>.go        ← 接线：写 agent 的配置文件，让它走 magpie 网关
internal/sessions/parserFor  ← 发现：读 agent 的会话文件，喂给 Sessions 页
```

第二个注册点是一张 switch：

```go
// internal/sessions/sessions.go:2017
func parserFor(agent string) sessionParser {
	switch agent {
	case "hermes":                 return sessionParser{whole: parseHermes}
	case "alma":                   return sessionParser{whole: parseAlma}
	case "opencode", "zcode":      return sessionParser{whole: parseOpenCode}   // 两家共用
	case "dsh":                    return sessionParser{grow: parseDsh}
	case "cline":                  return sessionParser{grow: parseCline}
	case "grok":                   return sessionParser{grow: parseGrok}
	case "kimi":                   return sessionParser{grow: parseKimi}
	case "droid":                  return sessionParser{grow: parseDroid}
	case "agy":                    return sessionParser{grow: parseAgy}
	case "antigravity":            return sessionParser{whole: parseAntigravity}
	case "cursor":                 return sessionParser{whole: parseCursor}
	case "codex":                  return sessionParser{line: codexBody}
	case "pi", "raft", "cindy", "omp": return sessionParser{line: piParse}
	case "workbuddy", "workbuddy-ai", "codebuddy": return sessionParser{line: workbuddyLine}
	default:                       return sessionParser{line: claudeLine, claude: true}  // Claude 格式兜底
	}
}
```

三种读取策略（`sessionParser` 的字段）：

| 策略 | 含义 | 适合 |
|---|---|---|
| `whole` | 整个文件一次解析 | 结构化 JSON（opencode/zcode、cursor、antigravity、hermes、alma） |
| `grow` | 增量追读（记 offset） | 长 JSONL（cline、grok、kimi、droid、agy、dsh） |
| `line` | 逐行解析 | 标准 JSONL（codex、pi、workbuddy、claude） |

`default` 落 Claude 格式这一条很关键：**Claude Code 的 JSONL 已经成为行业事实标准**，很多 agent（Qoder、CodeBuddy、Kimi 的某些形态）直接复刻，所以 magpie 不必为它们单独写解析器。

### 2.2 Agent 结构体：一家 agent 要声明的东西

`internal/agent/agent.go:122` 起，字段很多，但**与「收件箱」相关的只有前 15 个**：

| 字段 | 用途 | 例 |
|---|---|---|
| `ID` | 稳定 id（`session.source` 用它） | `"claude"`, `"codex"`, `"workbuddy"` |
| `Name` | 显示名（走 i18n） | `"Claude Code"` |
| `Icon` | **内置图标名**（`internal/gui/assets/icons/<name>.svg`） | `"claudecode-color"` |
| `Aliases` | 别名（命令行/旧名兼容） | `["cc"]` |
| `Bin` | 可执行名，用于**探测是否安装** | `"claude"` |
| `Dir` | 配置目录，用于探测 | `"~/.claude"` |
| `Path` | magpie 改写的配置文件 | `~/.claude/settings.json` |
| `Plugin` | 若该 agent 由插件添加，记插件名；内置为 `""` | — |
| `Fields []Field` | 可切换的字段（model / effort / 子代理模型…），每个有 `Get/Set/Options` | `model`, `effort` |
| `Native *NativeConnection` | 非文件式的连接（本地 server / socket） | Codex 的 app-server |
| `Notice func() string` | 改完之后要提示的话（如「需要重启 agent」） | — |
| `UA []string` | 该 agent 的 User-Agent 前缀，网关据此区分请求来源 | `["claude-cli"]` |
| `ListsModels` / `NoNames` | 模型列表的两种特例 | — |
| `Sync` / `Unwire` / `Restore` | 目录变动后的重新同步、断开时还原 | — |

`Fields` 的结构（同文件 `:100`）：`Key / Label / Get / Set / Options / Quiet / Live / Follows`——`Quiet` 表示「跟随别的字段、为空时不列出」，`Live` 表示「改了立即生效、不用重启 agent」。

### 2.3 新增一家 agent 的完整清单

1. **`internal/agent/<x>.go`**：构造 `*Agent`（ID/Name/Icon/Bin/Dir/Path/Fields），实现配置读写（多数用现成的 `jsonGet/jsonSet`、`tomlGet/tomlSet`）。
2. **挂进注册表**：`internal/agent/agents.go` 的 `onMachine()` 返回的切片（当前 76 项，`agents.go:240-320`）。函数名即构造器名，如 `zcode(home)`。
3. **`internal/sessions/<x>.go`**：写解析器（或复用 `parseOpenCode`/`claudeLine`），产出 `Session{ID, Cwd, Title, Start, Last, Models, Tokens…}`。
4. **`parserFor` 加一个 case**（上面那张 switch）。
5. **图标**：`internal/gui/assets/icons/<name>.svg`（单色）或 `<name>-color.svg` / `<name>.png`（彩色），并在 `app.js` 的 `pngIcons` 集合里登记 PNG（`:366`）。
6. **文案**：`internal/gui/assets/plugins/sessions/i18n.js` 与 `app.js` 的 i18n 需要该 agent 的显示名（zh / zh-TW / ja / de 四套，缺一条测试就红）。

> 想更省事可以走**插件**：`Agent.Plugin` 非空即表示由插件添加（`internal/agent/plugged.go`），magpie 的插件可以注册 provider（如 WorkBuddy 那个 `@magpie-community/opencode-workbuddy-auth`），但**会话发现仍要落进 `parserFor`**——插件目前解决的是「模型/供应商」，不是「会话列表」。

### 2.4 目前支持的 agent（76 个内置）

按接入形态分四类（全部出自 `internal/agent/agents.go:240-320` 的 `onMachine()`）：

| 类型 | agent |
|---|---|
| **CLI agent**（改配置文件指向网关） | Claude Code、Codex、Gemini CLI、Antigravity CLI、OpenCode、OpenChamber、MiMo Code、DevEco Code、Pi、StepCode、MothX、PenguinHarness、Aside、OmO、Prime Agent、Goose、Cursor CLI、Copilot CLI、Crush、dsh、DS Bot、Reasonix、Command Code、fx、omp、Devin、Hermes、Mister Morph、Kimi Code、Qwen Code、Muse、Empryo、Ante、AstrBot、QwenPaw、OpenClaw、jcode、MiniMax Code、Droid、Cline、Qoder、Qoder CN、Grok Build、ZCode、Kimi Code Desktop、Pencil、OpenPencil、OpenBot、NewMax、LiveAgent、Raycast、Kiro Crew、T3 Code、OpenMausBot、Hanako、AtomCode、Snow、Alma、Cindy、AionUi、Cherry Studio、Lody、Zoo Code |
| **桌面 App**（各自 GUI） | Claude Desktop、Claude Science、Codex App（`codex_app.go`）、WorkBuddy、WorkBuddy AI、CodeBuddy、Grok App、Cursor Private Inference |
| **IDE 扩展** | VS Code、VS Code Insiders、VSCodium、Zed、JetBrains Air / Air IDE、Copilot（JetBrains） |
| **本地 / 其他** | WSL 内的同名 agent（`wslAgents()`，按发行版列出）、`ompProfiles()`、`qwenpawAgents()` 动态生成的多实例 |

会话解析器覆盖的 13 类格式：Claude 系 JSONL（含 `qoder`/`qoder-cn`/`codebuddy`/`kimi` 的复刻）、Codex rollout、OpenCode/ZCode、Cursor、Antigravity、Agy、Cline、Grok、Droid、Hermes、Alma、WorkBuddy、Pi/raft/cindy/omp。

### 2.5 对手机端的结论

手机端不要按 magpie 的「两处注册」来设计自己的扩展点。手机只需要**一个会话元数据源接口**，因为：

- 手机不写 agent 配置，不接网关（relay 才是主机侧）。
- 手机只消费 `GET /api/sessions` 的 `source` 字段。

所以建议 relay 侧把「新增 agent」收敛成**一个 provider 函数**：`scan<Agent>() []SessionMeta`，注册到一张表里即可。magpie 的 `parserFor` 那张 switch 就是它的原型，只是把 Go 的 switch 换成 relay 里的一个 `Map<string, Scanner>`。具体接口草案见上一份文档 §6.6。

---

## 3. agent 视觉区分

### 3.1 图标的两套资源

| 形态 | 文件名约定 | 渲染方式 | 用在哪 |
|---|---|---|---|
| **单色** | `<id>.svg`（无 `-color` 后缀） | CSS mask + `background: currentColor`（`app.css:185`） | 列表行、分段控件、菜单项——**跟随文字色**，深色主题自动适配 |
| **彩色** | `<id>-color.svg`，或登记进 `pngIcons` 的 `<id>.png` | `<img>`（`app.js:396-403`） | Agents 页头像、插件卡、品牌区 |
| **兜底** | `generic` | 内联 SVG 六边形线框 + `--muted` | 未知 agent、图标加载失败（`app.js:379-385`） |

判定逻辑（`app.js:395`）：

```js
if (name.endsWith("-color") || pngIcons.has(name)) { /* <img> */ }
else { /* mask，加载失败则换成 generic */ }
```

`pngIcons` 集合里是「只有位图」的 27 家（`crush, zcode, alma, hanako, cindy, lody, typesafe, atomcode, snow, mlx-serve, mothx, zoo-code, kimi-code-desktop …`，`app.js:366`）。

### 3.2 尺寸与视觉重量

| 项 | 值 | 说明 |
|---|---|---|
| `.ic` 盒子 | **16×16** | 全局基准（`app.css:183`） |
| 会话页列表 | **13×13** | `.sm-agents .opt .ic`、`.sm-agent-pick .ic`（`sessions.css:10-12`） |
| 实心/贴边 logo | **82%** | 有一份 27 家的名单（`app.css:190`），因为「画到盒子边缘的 tile 或 disc 比旁边的线稿 logo 看起来大一号」（注释原文） |
| 叠放圆盘 | 20px 圆 + `--card` 底 + 1px `--line` 描边 + `margin-left: -4px` | `.ic-stack .disc`（`app.css:196-198`） |
| 深色主题 | `--logo-invert: 1` | 反相，而不是换一套图标 |

### 3.3 颜色：**没有 agent 品牌色**

- 全仓 `grep agentColor / brandColor / agentColors` 零命中。
- 图表用的 `--c1…--c7` 是**序列色**（provider/agent/model 三者共用一套，按出现顺序分配），不是「Claude=紫、Codex=绿」这种绑定。
- 所以 magpie 里区分 agent 只有三种手段：**图标形状 + 显示名 + 文字层级**。

> 这是本次分析里最反直觉的一条：一个支持 76 家 agent 的产品，**主动拒绝了给每家上色**。理由是「一屏只有一家 agent」（§1.1 第 1 条），根本不需要颜色区分。

### 3.4 命名

- `Name` 是显示名（`"Claude Code"`、`"ZCode"`），走 i18n（`gui-zh-tw.test.cjs` / `gui-ja.test.cjs` / `gui-de.test.cjs` 会因缺一条翻译而失败）。
- `Aliases` 只用于命令兼容，不上屏。
- 会话页顶栏显示 `icon + Name + count`；`agent` 的 id 只在需要时以等宽小字出现（会话行的 `id.slice(0,8)`）。

### 3.5 徽标（badge）

| 徽标 | 触发 | 样式 |
|---|---|---|
| WSL | `s.wsl` 非空 | 行首前置小徽章（`client.js:1008` `sub.prepend(wslBadge(s), " ")`） |
| provider 漂移 | `s.provider` 与当前 agent 不一致 | `.sm-prov` 文本 + tooltip「Made with X: …only Y's sessions now」 |
| 搜索命中 | `hitOf(s)` | 追加一行，`<mark>` 用 `--accent-soft` 底（`sessions.css:33-35`） |
| pending | 文件未读完 | 无徽标，仅内部状态自动补拉 |

### 3.6 多家同时出现时怎么保持简洁

magpie 的做法只有两条，但很有效：

1. **同屏只显示一家**（agent 分段互斥）。真正「多家同屏」的场合只有三处：Agents 页（每家一行）、Usage 页的 agent 列表（每行一个图标 + 名字 + 数字）、Routing 页的会话块（`sessionName` 一行）。
2. 需要并排时用**叠放圆盘**（`.ic-stack`，20px + `-4px` 重叠）而不是一排彩色 chip——颜色不参与，只靠圆盘里的图形轮廓区分。

> 手机端的取舍：卡片上必然要同时出现「agent + 项目 + 分支」三个维度，无法像 magpie 那样「一次只显示一家」。建议：**agent 用图标（单色 13px）+ 名字，项目用唯一一处淡底标签，分支/模型用纯文本**——即把 magpie 的「只有一个强调位」平移过来，而不是给三者各配一个色。

---

## 4. 通知与提醒

### 4.1 只有五类，且全是「阈值型」

设置项（`internal/gui/api.go:823`）：

```go
if s.UsageAlert > 0 || s.BalanceAlert > 0 || s.ResetReminder > 0 || s.TurnDoneAlert > 0 || s.MaskAlert
```

| 类别 | 触发 | 可调 |
|---|---|---|
| `UsageAlert` | 用量超过阈值 | 阈值（>0 才开） |
| `BalanceAlert` | 余额低于阈值 | 阈值 |
| `ResetReminder` | 额度窗口即将重置且余量很多 | 阈值 |
| `TurnDoneAlert` | agent 一轮结束，**且耗时超过设定值** | 时长阈值 |
| `MaskAlert` | 某敏感值第一次被遮罩 | 开关 |

### 4.2 turn 结束通知：抑制短任务

```go
// internal/gui/quota_alert_notify.go:191-195
// an agent's turn done, past the length set (itieniu on X)
gateway.TurnDone = func(t gateway.TurnEnd) {
    title, body := turnDoneText(trayLang(...), agentName(t.Agent), t)
    n.sendAs(turnNotice, title, body)
}
```

文案（`internal/gui/turn_done.go`）：标题 `"<Agent> 已完成"`，正文 `"用时 12 秒"` + `" · <工作目录>"`（家目录缩写）。

`past the length set` 是关键：**短于一秒级的 turn 不发**，这是 magpie 唯一的「降噪」手段——不是合并，是**根本不起念**。

### 4.3 去重与合并

通知 id 前缀分三类（`quota_alert_notify.go:95-101`）：

| 前缀 | 形态 | 语义 |
|---|---|---|
| `magpie-turn-` + 纳秒时间戳 | **每次一个新 id** | 不合并：跑几轮就弹几次 |
| `magpie-mask-` + 值指纹 + `-` | **同一被遮值同一个 id** | 合并：同一个 secret 只提一次 |
| `magpie-alert-` + 纳秒 | 每次新 id | 不合并 |

也就是说：**只有「同一个值反复出现」这一类做了合并**，会话完成类通知不做聚合。

### 4.4 点击行为

`clickView()`（`quota_alert_notify.go:136-150`）：

| 通知 | 点击后 |
|---|---|
| 额度/余额/重置 | 打开 Usage 页 |
| 某值被遮罩 | 打开脱敏页并定位到那次请求 |
| 登录态过期 | 打开该 provider 的账号页 |
| **turn 结束** | **不跳转**——注释：`where the user goes is their agent, not magpie` |

最后一条是很硬的产品判断：**完成通知只告知，不导流**。手机端如果做成「点了就跳详情」，反而违背这个原则；建议「点通知进 App、但不强制跳该会话」，把选择权留给用户。

### 4.5 系统权限与「为什么没看到」

`notifyProblem()`（`api.go:581`）把「系统通知权限被拒 / 专注模式 / 未授权」的原因回传给设置页，让用户知道**通知没生效**而不是以为「没有事件」。手机端同样需要这一层：推送被系统杀掉时，App 内要能显示「最近一条推送失败的原因」，而不是静默。

### 4.6 magpie 没有的

- 没有「免打扰时段」、没有「按项目静音」、没有「通知分组折叠」、没有「摘要推送」。
- 因为它只服务一个人、一台机器，默认通知量就很小（且 turn 通知本身有阈值门槛）。

> 手机端不一样：手机会同时收三个 agent 的推送，**必须有 magpie 没有的聚合层**。建议规则见 §7 第 7 条。

---

## 5. 品牌：一只墨鸟

### 5.1 命名与造型

- 产品名 **magpie = 喜鹊**，图标就是这只鸟——**名字与图形是同一件事**，不需要文字标识「这是 magpie」。
- 源文件：`build/icon/magpie.svg`（`viewBox="0 0 44 44"`，纯 path 无填充色，即「墨色剪影」）；托盘用精简版 `build/icon/magpie-small.svg`。
- 风格：单色、无渐变、贴边线稿；没有任何圆角矩形背景（背景是后加的 tile）。

### 5.2 三种成品的规格（`build/icon/gen.go`）

| 成品 | 构图 | 数值 |
|---|---|---|
| **macOS 托盘** | 黑色 template 图（black + alpha），鸟居中 | 画布 44px（22pt @2x）；鸟占 `trayK = 0.8`，即约 18×14pt（注释：画满 22pt 时「感觉这只鸟有些大」） |
| **macOS App 图标** | 圆角白方块 + 极浅灰渐变 + 墨鸟，按 macOS 图标网格 | tile 占画布 **80%**（`inset = S*0.1`），圆角半径 = 边长 **×0.225**；macOS 26 用分层 `build/darwin/AppIcon.icon` + `Assets.car` |
| **Windows 图标** | 同款 tile 但**填满方块** | `inset = n/32`（≥32px 才留 1px 边），圆角 = 边长 **×0.2** |

Windows 填满的理由写在注释里：macOS 网格上 80% 的 tile 在 Windows 任务栏「比邻居小五分之一」（`dzxx0496`）。

### 5.3 托盘点击动画

点击菜单栏图标时，鸟会**扇一下翅膀**：

| 参数 | 值 |
|---|---|
| 总时长 | `flapSecs = 0.9` 秒 |
| 帧间隔 | `flapEvery = 0.03` 秒（30ms） |
| 帧数 | 30 帧计算，实际入库 **29 帧**（`flap-01.png` … `flap-29.png`），首尾两个静止帧去掉 |
| 实现 | 三段独立变换（身体上下 `bob`、尾 `tail`、翅 `wing`）按关键帧插值后合成，每帧一张 PNG |

这是 magpie 唯一的装饰性动效，且**只在用户主动点击时播放**——不是常驻循环。

### 5.4 应用内 logo 用法

- 窗口标题栏、托盘菜单、web 页头用同一只鸟；`--logo-invert` 在深色主题下只做反相。
- **不把 logo 当装饰重复铺**：页面里出现的图形几乎都是 agent/provider 的图标，magpie 自己的鸟只在「身份位置」出现（标题栏、托盘、通知里没有）。

### 5.5 有没有移动端 / 远程访问

有，但不是原生 App：

| 功能 | 说明 | 源码/证据 |
|---|---|---|
| `magpie web` | 把同一份 UI 用浏览器打开，可绑定 `--addr`、`--lan`、TLS | `internal/gui/web.go` |
| **局域网共享** | 「Share on local network」+ 为每个客户端发**命名 gateway key**，各自有日/周/月 token 与费用上限 | README「Sharing」段；`lan-*` 测试 |
| 手机连接菜单 | 面板里专门有一块「用手机连上来」的引导 | `internal/gui/tests/phone-connect-menu.test.cjs`、`phone-rows.test.cjs`、`pop-phone-room.test.cjs` |
| 移动网页适配 | 有专门的移动 web 测试与顶栏适配 | `mobile-web.test.cjs`、`phone-web.test.cjs`、`usage-phone-head.test.cjs` |
| Remote magpie | 笔记本用桌面那台 magpie 的 providers/groups | README「Remote magpie」 |
| Docker / Termux | 服务器/NAS 上跑、Android 终端跑 | README |

**关键事实**：magpie 的「移动端」只是**把桌面 UI 塞进手机浏览器**，没有原生壳、没有推送、没有通知、没有后台。也就是说——**手机会话收件箱这件事，magpie 本身并没有解决**，它甚至没有到手机的通知路径。这正是本项目存在的空间。

---

## 6. 新 agent 预研：谁能接、怎么接、多难

### 6.1 本机实测（2026-11-09，只读）

| agent | CLI 在 PATH | 配置/数据目录 | 会话数据 | 结论 |
|---|---|---|---|---|
| **OpenCode** | ✗（无 `opencode` 命令） | ✅ `~/.local/share/opencode`、`~/.config/opencode` | ✅ **`opencode.db`（SQLite，含 `-wal`/`-shm`）** | 装过、留了库，可直接读 |
| **ZCode（智谱）** | ✗ | ✗ `~/.zcode` 不存在 | ✗ | 未装 |
| **Gemini CLI** | ✗ | ✅ `~/.gemini`（但只有 `config/`、`skills/`、`antigravity*`、`settings.json` 里一个 `mcpServers`） | ✗ 无 `tmp/<project>/chats/` | 未真正跑过会话 |
| **Qwen Code** | ✗ | ✗ `~/.qwen` 不存在 | ✗ | 未装 |
| **Cursor CLI** | ✗ | ✗ `~/.cursor` 不存在 | ✗ | 未装 |
| **Kimi CLI/Code** | ✗ | ✗ `~/.kimi*` 不存在 | ✗ | 未装 |
| （对照）Claude Code | ✅ `~/.npm-global/bin/claude` | ✅ `~/.claude` | ✅ `projects/*.jsonl` | 已在 relay 里 |
| （对照）Codex | ✅ `~/.local/bin/codex` | ✅ `~/.codex` | ✅ `session_index.jsonl` + `sessions/**/rollout-*.jsonl` | 已在 relay 里 |

### 6.2 OpenCode：本机唯一「白捡」的新数据源

`~/.local/share/opencode/opencode.db`（SQLite，新版；旧版是 `storage/session|message|part/*.json`，本机已是 db）。只读打开后的表（`sqlite3 "file:…?mode=ro&immutable=1" ".tables"` 实测）：

```
account  account_state  control_account  credential  event  event_sequence
instruction_blob  instruction_entry  instruction_state  kv  migration  permission
project  project_directory  session_inbox  session_message  session_pending
session_v2  workspace  worktree
```

对本项目有用的四张表（schema 实测）：

```sql
CREATE TABLE session_v2 (
  id text PRIMARY KEY, project_id text NOT NULL, workspace_id text, parent_id text,
  fork_session_id text, fork_boundary text, slug text NOT NULL,
  directory text NOT NULL,        -- ← cwd
  path text, title text,          -- ← 标题
  version text NOT NULL, share_url text,
  summary_additions integer, summary_deletions integer, summary_files integer, summary_diffs text,
  metadata text, cost real, tokens_input integer, tokens_output integer,
  tokens_reasoning integer, tokens_cache_read integer, tokens_cache_write integer, revert text, …
);

CREATE TABLE project (
  id text PRIMARY KEY, worktree text NOT NULL, vcs text, name text,   -- ← 项目名
  icon_url text, icon_url_override text, icon_color text,             -- ← 项目配色
  time_created integer, time_updated integer, time_archived integer,  -- ← 归档时间
  sandboxes text NOT NULL, commands text
);

CREATE TABLE permission (id, project_id, action, resource, time_created, time_updated);
CREATE TABLE session_inbox (id, session_id, type, payload, delivery, enqueued_seq, time_created);
CREATE TABLE workspace (id, provider, binding, created_at, last_used_at);
```

**这四张表几乎就是本项目想要的数据模型**：

| 需求 | OpenCode 已有 |
|---|---|
| 会话标题 | `session_v2.title`（另有 `slug`） |
| 工作区 / 项目 | `session_v2.directory` + `project.name` / `project.worktree` |
| 项目视觉区分 | **`project.icon_color` / `icon_url`**（原生项目配色） |
| 待授权 | `permission` 表（`action` / `resource`） |
| 收件箱 | **`session_inbox`**（`type` / `payload` / `delivery` / `enqueued_seq`）——OpenCode 自己的待投递队列 |
| 事件流 | `event` / `event_sequence`（可做增量订阅） |
| 归档 | `project.time_archived` |
| 分支 | `project.vcs` + `worktree`（分支本身要 `git -C` 现算） |

magpie 怎么读它：`internal/sessions/opencode.go`，`OpenCodeDir()`（`$XDG_DATA_HOME/opencode`，否则 `~/.local/share/opencode`，Windows 同路径）、`openCodeDB()`（`$OPENCODE_DB` 可覆盖，否则 `opencode.db`），会话结构 `ocInfo{ID, ParentID, Directory, Title, Time{Created,Updated}}`（`opencode.go:69-88`）。

### 6.3 ZCode（智谱）：OpenCode 的改版，库名不同

magpie 的注释直接写清了格式（`internal/sessions/zcode.go:9-18`）：

- 路径：**`~/.zcode/cli/db/db.sqlite`**（`zcodeDB()`），Windows 上即便 app 的数据在别处，这个库也在 `USERPROFILE` 下。
- 结构：**OpenCode 的表**，额外有 `title_source`（标题怎么来的——正好对上我们要的 `title_source` 字段）。
- 消息与 parts 存 JSON 文档；模型字段 `modelId`（旧版 `modelID`）；用户消息带 `semantics.origin`，`real_user` 表示真人输入。
- 子代理会话 = parent 指向它的那个会话（同 OpenCode）。
- 限制：**「ZCode is a desktop app: no command picks a session up again」**——没有 resume 命令，手机端「打开会话」只能展示，不能续跑。

结论：**接入方式与 OpenCode 完全一致**（同一套表），magpie 甚至共用 `parseOpenCode`（`parserFor` 里 `case "opencode", "zcode"`）。难度≈低，只要用户装了 ZCode。

### 6.4 未装的其余几家：按 magpie parser + 官方仓库给接入方式

| agent | 会话存储（magpie 源码依据） | 标题来源 | 钩子/通知 | 本地接口 | 难度 |
|---|---|---|---|---|---|
| **Gemini CLI** | `~/.gemini/tmp/<project>/chats/*.json`（`traces_gemini.go:41` 用 `~/.gemini/tmp`） | 会话 JSON 里的首个 prompt / 摘要 | 无官方 hook；可用 shell wrapper | 无 HTTP/ACP（只有 MCP 客户端身份） | 中 |
| **Qwen Code** | magpie 有接线（`internal/agent/qwen.go`），无独立 parser → 落 **Claude 格式兜底**（`parserFor` default） | 首条 prompt | 无 | 无 | 中（格式需实测确认） |
| **Cursor CLI** | `~/.cursor`（`$CURSOR_DATA_DIR` 可覆盖），`cursor.go:48-66` 读 transcripts | transcript 里的标题或首条 | 无 | 无（桌面版有 clone 机制） | 中 |
| **Kimi Code / Kimi CLI** | `<sessions>/*/*/agents/*/wire.jsonl` + 同级 `state.json` + `session_index.jsonl`（`kimi.go:65-127`） | `session_index.jsonl` / `state.json` | 无 | CLI `kimi --session <id>` 可续跑 | 中 |
| **Cline** | `$CLINE_DATA_DIR` / `$CLINE_SESSION_DATA_DIR`，`sessions/<ts>_<id>/<id>.json` + `.messages.json`（`cline.go`） | 会话 JSON 的 `task` 字段 | 无 | 无 | 低（JSON 结构清晰） |
| **Grok Build** | `$GROK_HOME`，`sessions/<urlenc-cwd>/<id>/summary.json` + `updates.jsonl`（`grok.go`） | `summary.json` | 无 | 无 | 低 |
| **Droid (Factory)** | `$FACTORY_HOME_OVERRIDE`，`sessions/<slug>/<id>.jsonl` + `.settings.json` | 首条 prompt | 无 | 无 | 低 |
| **Hermes / Alma / Antigravity** | 各自目录（`hermes.go` / `alma.go` / `antigravity_app.go`） | 各自字段 | 无 | Antigravity 有 IDE 侧协议 | 中 |

> 说明：「钩子/通知」与「本地接口」两列来自 **magpie 的接线方式**（它若能靠写配置文件接上，就说明该 agent 没有现成的本地 API 可用），未逐家核对其官方文档；标「无」表示 magpie 没有用到这类机制，不等于该 agent 绝对没有。接入前建议对目标 agent 单独验证一次 hooks / ACP / HTTP server 是否存在。

### 6.5 接入难度表（推荐排序）

| 优先 | agent | 本机 | 数据可得性 | 标题 | 工作区 | 待授权 | 难度 | 关键障碍 |
|---|---|---|---|---|---|---|---|---|
| ★1 | **OpenCode** | ✅ 有库 | SQLite 单文件，schema 稳定 | `title` | `directory` + `project.name` | `permission` 表 | **低** | 需处理 `-wal`（只读打开要用 `immutable=1` 或先拷贝） |
| ★2 | **ZCode** | ✗ | 同 OpenCode 的库 | `title` + `title_source` | 同上 | 同上 | **低** | 无 resume；用户需先装 |
| ★3 | **Cline** | ✗ | 目录里纯 JSON | `task` | 目录路径 | 无 | **低** | 需用户装 Cline |
| ★4 | **Grok Build** | ✗ | `summary.json` + JSONL | `summary` | 目录名（url 编码的 cwd） | 无 | **低-中** | cwd 要 url 解码 |
| ★5 | **Kimi Code** | ✗ | `wire.jsonl` + `state.json` | `session_index.jsonl` | 目录层级 | 无 | **中** | 层级深、fork 语义复杂（要看 `forked` 标记） |
| ★6 | **Gemini CLI** | ✗（目录在但空） | `tmp/<proj>/chats/*.json` | 首 prompt | 目录名 | 无 | **中** | 无标题字段，需截断首句 |
| ★7 | **Qwen Code** | ✗ | 走 Claude 兜底 | 首 prompt | 行内 cwd | 无 | **中** | 格式未实测，要先跑一次 |
| ★8 | **Cursor CLI** | ✗ | transcripts（`~/.cursor`） | transcript 元数据 | transcript 字段 | 无 | **中-高** | 格式随版本变 |

难度口径：

- **低** = 单文件、结构稳定（SQLite/JSON），能直接拿到 title + cwd + 时间；magpie 已有 parser 可对照。
- **中** = 结构清楚但标题要自己造（首条 prompt）、或层级深、或需要用户先产生会话才能验证。
- **高** = 二进制/加密存储，或无本地可读会话，只能靠 hook 或 PTY 包装。

### 6.6 对 relay 的接入建议

1. **先接 OpenCode**：本机就有库、schema 最全（连 `permission` 与 `session_inbox` 都有），一次接入能把「状态 / 待授权 / 收件箱」三个概念都填满，是验证 `/api/sessions` 设计的最佳样本。
2. **ZCode 与 OpenCode 共用扫描器**：relay 里写一个 `scanOpenCodeLike(dbPath)`，两家只是路径不同（`~/.local/share/opencode/opencode.db` vs `~/.zcode/cli/db/db.sqlite`）。这与 magpie 的 `parserFor` 把两家并列是同一个判断。
3. **绝不要写这些库**：全部 `file:…?mode=ro&immutable=1` 打开；OpenCode 带 WAL，正在写时 `immutable=1` 可能读到稍旧的快照——对「会话列表」够用，但**不要**据此做「已读水位回写」。
4. **新 agent 上线时补一条自测**：用 magpie 的 `parserFor` 当检查单——如果 magpie 有专门 parser，说明格式稳定、可放心解析；如果落 `default`（Claude 格式兜底），说明要先跑一次真实会话确认字段。

---

## 7. 手机端最重要的 10 条设计建议（按优先级）

每条都标了依据的 magpie 做法。

**1. 会话卡片只保留三段：标题 / 归属 / 时间+未读。**
依据：magpie 会话行是「标题 13px/500 + 一行 11.5px 副行 + 右侧动作」。手机更小，副行里 `messages / size / id8` 全部砍掉，只留「项目 · 分支 · 模型」。
理由：magpie 之所以能塞下 6 个字段，是因为它一行有 900px；手机 360dp 塞 6 个字段等于什么都没说清。

**2. 标题走四级回退且必须带 `title_source`。**
依据：`sessions.go:127` 的 `Title` 注释 + `:1963-1973` 的 `cmp.Or(custom, named, s.Title, first)`。
手机端额外要求：`title_source=first_prompt` 时在标题旁标一个极淡的「未命名」点，让用户知道这条不是 agent 起的名字——否则他会以为是 agent 取的名字而去找不着。

**3. 首屏默认「按项目分组、全部折叠」，可一键切「按时间平铺」。**
依据：magpie 的 `folder()` 默认 `opened` 为空（全折叠），组内按 `last` 降序。
手机端差异：分组视图多一次点击，所以给平铺视图作为默认，但**保留分组视图**——用户要「我在哪个项目」时切过去。这是唯一需要偏离 magpie 的地方。

**4. agent 不做彩色标签，只用「13px 单色图标 + 名字」。**
依据：§3.3——magpie 支持 76 家 agent 却没有 agent 品牌色；图标单色 mask 跟随文字色，暗色主题靠 `--logo-invert` 反相。
手机端把唯一的强调色留给「需要你处理」的状态条，不要给 agent 上色。

**5. 状态用「形状 + 文案」，不用颜色单独表达；失败态必须是 x 形。**
依据：`app.css:24-27` 的 `--x-mark` 注释——「红绿点在红绿色盲读者眼里是一个颜色」。
手机端：`needs_authorization` 用 amber 底 + 「!」圆点；`error` 用 red 底 + **x 形**图标；`running` 用 animating dot（唯一允许的循环动效，参考 magpie 托盘扇翅只播一次的原则——但 running 必须常驻，所以做成低频呼吸而非旋转）。

**6. 空会话不进列表 + 网关类会话 7 天过期。**
依据：`sessions.go:2000`（空会话 return false）、`gateway.go:27`（`GatewayRetention = 7 * 24h`）。
这是投入产出比最高的两条治理规则，实现成本一个 if，效果立竿见影。

**7. 通知分三级，手机端必须做 magpie 没做的聚合。**
依据：magpie 五类通知 + turn 通知「超过设定时长才发」（`quota_alert_notify.go:191`）；但它**不做聚合**（turn 每次新 id）。
手机端建议：
- P0 立即弹：`needs_authorization` / `error`（用户被阻塞）
- P1 合并弹：`needs_input` / `done`——**同一会话 120s 内合并**，不同会话**按项目合并**成一条「{项目} 有 3 条新进展」
- P2 静默：`running` 不推送，只在 App 内显示
理由：magpie 只服务一个人一台机，桌面通知天然有阈值；手机一天可能收几十条，不聚合会被关通知权限。

**8. 点推送进 App 但不强制跳会话。**
依据：`clickView()` 对 turn 通知返回空——注释「where the user goes is their agent, not magpie」。
手机端：点推送打开收件箱并**高亮**那条会话，但不进详情；用户自己决定要不要进去。这避免了「推送把人拽走」的打断感。

**9. 品牌上不要造吉祥物，用一个单色记号贯穿。**
依据：magpie 就是一只墨鸟——托盘 44px 黑色 template（鸟占 0.8）、App 图标圆角白底 tile 80%、Windows 填满；`.ic` 统一 16px，实心 logo 缩 82%。
手机端：logo 只在启动页、关于页、空状态出现；列表里永远不出现自己的 logo（magpie 页面里也只用 agent/provider 的图标）。若要一个记忆点，抄它的「点击时扇一下翅」——**一次性微动效**，别做常驻动画。

**10. 加新 agent 只加一个 scanner，不要改 UI。**
依据：magpie 的 `parserFor` 是一个 switch，新增一家只需一个 case + 一个 `*Agent` 声明；13 类 parser 覆盖 76 家 agent，说明**格式是收敛的**。
手机端落法：relay 里 `Map<string, Scanner>`，`/api/sessions` 的 `source` 直接透传；Compose 侧的 `AgentBrand` 保持「未知即 generic」（图标兜底 = magpie 的 `generic` 六边形）。新增 agent 时 UI 零改动——这是判断扩展架构好坏的唯一标准。

---

## 附：本文件的事实来源

| 结论 | 依据 |
|---|---|
| 治理五条（分段/分组/scratch/空会话/分页） | `internal/gui/assets/plugins/sessions/client.js`；`internal/sessions/sessions.go:2000, 1015` |
| 默认 agent = 会话最多者 | `internal/gui/sessions_manage.go:152-193` |
| 无 pin/mute/unread/inbox | 全仓 grep 零命中（命令与结果见 §1.4） |
| 删除/回收站/还原/清除 | `internal/sessions/manage.go:347, 386, 730, 769, 792` |
| agent 注册结构 | `internal/agent/agent.go:100-240`；`internal/agent/agents.go:208, 240-320` |
| 会话解析分派 | `internal/sessions/sessions.go:2017`（`parserFor`） |
| 图标单色/彩色/叠放/82% | `internal/gui/assets/app.js:366-405`；`internal/gui/assets/app.css:183-198` |
| 通知五类/去重 id/点击路由 | `internal/gui/quota_alert_notify.go:95-201`；`internal/gui/turn_done.go` |
| 品牌规格（tile 80%、圆角 0.225/0.2、托盘 0.8、flap 0.9s/30ms） | `build/icon/gen.go:88-300`；`build/icon/magpie.svg`；`Makefile:64,86` |
| 移动/远程能力 | `internal/gui/web.go`；`internal/gui/tests/phone-*.test.cjs`、`mobile-web.test.cjs`；README「Sharing / Remote / Docker / Termux」 |
| OpenCode / ZCode 存储 | 本机 `opencode.db` 只读 schema 实测；`internal/sessions/{opencode.go:45-88,zcode.go:9-40}` |
| 其他 agent 存储 | `internal/sessions/{cline,grok,kimi,cursor}.go` 的目录与 glob |

