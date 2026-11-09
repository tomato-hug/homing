# Magpie 桌面端多 agent 管理拆解 → 手机端「多 agent 收件箱」设计规格

> 只读分析。对象：本机已装的 magpie 0.1.3219（`com.yetone.magpie`，`/Applications/magpie.app`）与开源源码 `yetone/magpie@main`。
> 源码已在本地解开：`/tmp/magpie-src`（105 MB，完整仓库）。下文所有 `path:line` 都指该仓库内的相对路径。
> 本机实机核实的部分只读取文件、不写不改、不登录任何账号；样例值已截断并脱敏（无对话正文、无 token）。
>
> 目标读者：`agent-push-poc` 的 Android Compose 客户端与 relay。

---

## 0. 一句话结论

Magpie 的会话页其实只做一件事：**以「工作目录（`cwd`）」为第一分组维度，把不同 agent 的会话并排放在一起**；agent 是顶栏的一层分段筛选，而不是列表的分组维度。会话标题走一条明确的四级回退链（用户命名 → agent 的 AI 标题 → agent 自写标题 → 首条 prompt），列表行把「标题」放大、其余信息压成一行 11–12px 的次要文本，右侧只留高频动作。

手机端要抄的不是它的视觉皮，而是这三条结构决定：

1. **分组维度 = 工作目录，不是 agent**。用户认的是「我在哪个项目里」，agent 只是这个项目的执行者。
2. **标题必须四级回退，且永远有兜底**（`title || "(no prompt)"`），绝不允许空标题或裸 UUID 上屏。
3. **状态不靠颜色单独表达**（magpie 明确因为红绿色盲把「失败」画成 x 而不是红点）。

---

## 1. 信息架构

### 1.1 有哪些页面 / 面板

magpie 是一个「一个本地网关 + 四种外壳」的产品，同一份数据有四个呈现面：

| 外壳 | 入口 | 说明 |
|---|---|---|
| 主窗口 | `magpie` | 完整页面，左侧栏导航 |
| 菜单栏面板 | 点菜单栏图标 / `magpie panel` | 快速面板，多个 tab，高度随 tab 变（`panelMin`–`panelMax`） |
| TUI | `magpie tui` | 终端内，同样的数据 |
| Web | `magpie web` | 浏览器打开的同一页（`internal/gui/web.go`） |

主窗口的页面（左侧栏 tab）：

| 页面 | 职责 | 本任务是否相关 |
|---|---|---|
| **Agents** | 每个 agent 一行：是否连上 magpie、用哪个模型、effort、它自己的设置入口；新用户有 Get started 卡 | 相关（agent 身份与图标来源） |
| **Sessions** | 每个 agent 的会话，按文件夹分组，可 resume / 删除 / 迁移 / 导出 / 换 agent 继续 | **核心** |
| **Models → Routing** | 每个请求走了哪个 provider/key，实时 | 弱相关（`sessionName` 规则可复用） |
| **Models → Groups** | 路由组 | 无关 |
| **Usage** | Overview / Requests / Sessions / Allowances 四个 tab，token、成本、额度窗口 | 弱相关（会话的花费列） |
| **Providers** | 供应商、key、订阅账号、余额 | 无关 |
| **Library** | 写进各 agent 的 instructions / MCP / skills | 无关 |
| **Plugins** | 插件市场与已装插件 | 无关 |
| **Settings** | 通用、外观、隐私、同步 | 相关（隐私开关决定是否读会话历史） |

导航方式：左侧栏 tab（`markNav` 点亮当前页）+ 页内顶栏的分段控件（`segs`）；窄窗时侧栏收起、分段控件退化成下拉（`sessions.css` 里 `.sm-agents` 与 `.sm-agent-pick` 是同一控件的一对形态）。

### 1.2 多个 agent 怎么并列管理

**两层，不是一层：**

```
Sessions 页
├─ 顶栏： [Claude 12] [Codex 31] [WorkBuddy 8] [⋯ 下拉]      ← agent 分段（互斥，单选）
│        搜索框（200px）  回收站开关  选择模式  计数
└─ 主体：按 cwd 分组的文件夹列表（不再是 agent）
    ├─ 📁 agent-push-poc         ~/Projects          ↻ 3 分钟前   7 sessions
    │   ├─ 搭建多端回复推送原型                    codex · gpt-5.6 · 12 条 · 3 分钟前
    │   ├─ 翻译一段英文文本                        workbuddy · deepseek-v4.1 · 24 条 · 1 小时前
    │   └─ …
    ├─ 📁 机场安检模拟经营类游戏   ~/Volumes/ESD-USB  ↻ 昨天       2 sessions
    └─ ⋯ 其它（scratch）
```

关键事实：

- agent 分段是**单选**，切换即换数据源（`chooseAgent`），每项带 `icon + name + count`（`internal/gui/assets/plugins/sessions/client.js:head()`）。
- 分组键是 `s.cwd`，分组名是 `baseName(cwd)`，行内第二行显示父目录（家目录缩成 `~`）——`groupName` / `whereOf`，`client.js:91-110`。
- **scratch 折叠**：磁盘根、`/tmp`、`/var/folders`、`~/.*`（agent 的隐藏目录）这类「不是你的项目」的目录，被单独归到列表末尾的一个折叠里，不排在项目前面。注释里写明了原因：`"/"`、`T` 和 UUID 命名的目录曾经把列表顶满（`client.js:95-103`）。
- 文件夹默认折叠，展开状态记在内存 `opened:Set<cwd>`；搜索时强制全展开（`folder()` 里 `const open = !!q || opened.has(cwd)`）。
- 文件夹内有分页：**每批 100 条**（`SM_PAGE = 100`，`client.js:74`），底部「Show N more」（`drawnIn` 计数）。
- 会话支持父子：子 agent 会话挂在父会话下（`kidsOf`），**嵌套上限 8 层**（`MAX_DEPTH = 8`，`client.js:58`），缩进显示。
- gateway 会话（没有本地文件、只经 magpie 网关见过）用哨兵 `GW = "\x00gateway"` 单独成组，组名是「Seen through the gateway」（`client.js:88-90`）。

### 1.3 筛选 / 排序 / 搜索 / 未读 / 状态

| 维度 | magpie 的做法 | 源码 |
|---|---|---|
| 排序 | 每个分组内按 `last`（最后活动时间）降序；文件夹之间也按各自最近会话的时间排 | `client.js:883` |
| 筛选 | 只有 **agent**（顶栏分段）+ **回收站**（trash）两个硬筛选；没有「按模型/分支/时间范围」的筛选器 | `client.js:head()` |
| 搜索 | 单框全文搜索，200px（≤560px 屏宽时 130px）；搜 `title`、`cwd`、`id`、`models[].model`；**还会搜会话正文**（"Searching what was said…"），命中在行内以第二行展示并 `<mark>` 高亮 | `client.js:205, 240-260`；`sessions.css` `.sm-hit mark` |
| 未读 | **没有未读概念**（本地桌面应用，作者不加）；最接近的是 `last` 时间和 `pending`（文件仍在写） | 全文件无 `unread` 字段 |
| 状态 | 会话行不显示 running/awaiting；`pending` 只表示「文件还没读完」，用来 1.5s 后补拉一次；agent 层有状态点 `.ag-dot` | `client.js:121-140` |
| 多选 | 「Select」进入选择模式，行首出现 checkbox，文件夹行有全选（含 folded），底部出现批量删除条 | `client.js:folder()`、`.sm-bar` |

> 对手机端的直接含义：**「未读」是手机端独有的需求，magpie 没有现成规格可抄**。手机端必须自己定义未读（事件驱动：last_ts 之后有推送且未被打开），并在 UI 上明确它是「手机侧未读」而不是「agent 侧未读」。

---

## 2. 会话列表项与会话标题栏

### 2.1 会话行显示哪些字段

会话行（`.row.sm-sess`，`min-height: 44px`，缩进 `padding-left: 29px`，窗口内 32px；`sessions.css:74-79`）：

```
┌──────────────────────────────────────────────────────────────────────┐
│ 搭建多端回复推送原型                          [花费] [Resume] [▸] [⋯] │  ← 13px/500 --fg
│ 3 分钟前 · codex · gpt-5.6 · 12 条消息 · 48 KB · 01a1bad2            │  ← 11.5px --muted
│ └ 命中搜索时：…匹配的那句话（<mark> 高亮）…                           │  ← 12.5px --fg-2
└──────────────────────────────────────────────────────────────────────┘
```

字段与来源（`client.js:1001-1010`）：

| 字段 | 取值 | 缺失时 | 样式 |
|---|---|---|---|
| 标题 | `s.title` | `"(no prompt)"` | `.who .name`，13px/500，单行省略 |
| 时间 | `ago(s.last)` | 空则不显示 | 11.5px `--muted` |
| 模型 | `sessModelsText(s)`（多模型时合并） | 空则跳过 | 同上 |
| 消息数 | `s.messages` → `"{n} messages"` | 空则跳过 | 同上 |
| 体积 | `fmtBytes(s.size)`；gateway 会话为 `"{size} recorded"` | 空则跳过 | 同上 |
| 会话 id | `s.id.slice(0,8)` | **当 `title === id` 时省略**（避免重复） | 同上 |
| WSL 标记 | `s.wsl` 徽章，前置 | 非 WSL 无 | 徽章 |
| provider 漂移 | `s.provider` + tooltip「Made with X: …only Y's sessions now」 | 无则跳过 | `.sm-prov` |
| 花费 | `sessSpent(s)`（与 Usage 页同款） | 无价格则空 | 右侧列 |
| 动作 | Resume / 终端 / Continue in / 删除 | 按 agent 能力出现，**不可用时占位保持列对齐**（`.sm-slot`） | 24px 高按钮 |

文件夹行（`.row.sm-folder`，`min-height: 44px`，`sessions.css:47-72`）：

```
📁 agent-push-poc   ~/Projects                        ↻ 3 分钟前   7 sessions   [⋯]
   └ 13px/500 --fg  └ 11.5px --faint                  └ 11.5px     └ 11px tabular-nums
   第二行（.sm-latest）：最近一个会话的标题，12px --muted
```

行内三段：`fold`（chevron + 名称 + 路径 + 最近标题，`flex:1`）、`when`（时间 + 计数，右对齐列）、`⋯`（**hover 才出现**，`opacity:0 → 1`，`transition: opacity .12s`；`@media (hover:none)` 时恒显；`sessions.css:73-76`）。

文件夹的 `⋯` 菜单（整目录操作）：`Relocate project`（仅 Claude）、`Delete all sessions`。

### 2.2 标题从哪里来：四级回退链

这是整套设计里最值得抄的一条。结构体字段注释就是规格：

```go
// internal/sessions/sessions.go:127
Title string `json:"title"` // the name it was given, else the agent's own title, else the first prompt
```

装配处：

```go
// internal/sessions/sessions.go:1963-1973
// the name the session goes by in its agent: one the user gave it, else
// Codex's thread name (session_index.jsonl), else the title the agent
// made, else the first prompt, else the first message
if s.Agent == "codex" && s.WSL == "" && custom == "" {
    custom = codexTitle(s.ID)
}
if s.Agent == "claude-desktop" && s.WSL == "" {
    custom = cmp.Or(DesktopTitles([]string{s.ID})[s.ID], custom)
}
s.Title = cmp.Or(custom, named, s.Title, first)
```

即优先级：

| 级 | 名称 | 来源 | 三种 agent 的具体字段 |
|---|---|---|---|
| 1 | `custom` 用户命名 | 用户显式改名 | Claude/WorkBuddy 的 `custom-title` 记录；Codex 无自定义名，用 thread 名顶上 |
| 2 | `named` agent 的 AI 标题 | agent 自己生成 | Claude 的 `ai-title` / `summary` 记录；WorkBuddy 的 `ai-title`；Codex 的 `~/.codex/session_index.jsonl` → `thread_name` |
| 3 | `s.Title` agent 自写标题 | 会话文件里的标题类行 | 见下 |
| 4 | `first` 首条用户 prompt | 截断 | 各 agent 转录的第一条 user 消息 |
| 5 | 兜底 | 首条 message / `"(no prompt)"` | — |

各 agent 的解析位置：

- **Claude Code**：`internal/sessions/claude.go:390-401` — 逐行扫 `"type":"ai-title"` → `s.Named`、`"type":"custom-title"` → `s.Custom`、`"type":"summary"` → `s.Named`（回退）。
- **WorkBuddy**：`internal/sessions/workbuddy.go:204-211` — 同一套 `custom-title` / `ai-title` 记录（毫秒时间戳）；`cwd` 也从记录里取，或读 `<id>.meta.json`（`workbuddy.go:17-20`）。
- **Codex**：`internal/sessions/codex_titles.go:46-52, 168` — 读 `~/.codex/session_index.jsonl`，字段 `id` + `thread_name`，**截断到 200 字符**（`string(chars[:min(len(chars), 200)])`）。

> 设计要点：**标题从来不是「取一个字段」，而是一条每级都可能为空的链**。任何一级为空就往下掉，最后一定有值（哪怕是 `"(no prompt)"`）。手机端的 `title` 必须同时返回 `title_source`，让 UI 知道这一条来自哪级（决定要不要显示「未命名」提示、能不能被推送覆盖）。

### 2.3 标签的样式

magpie 的会话行**没有彩色标签**（它明确禁止一种东西）：

```css
/* gui-shell.md「Shared UI rules」 */
There are no native <select> elements and no colored left-border stripes.
State is shown with a dot or a swatch. A dot never says good or failed by
colour alone: a failed state's dot is the same box drawn as an x (`--x-mark`,
a mask over its red, scaled 1.5 …), since red and green dots read as one
colour to a red-green colour-blind reader.
```

实际用来区分来源的手段只有三种：

| 手段 | 规格 | 源码 |
|---|---|---|
| **agent 图标** | 13×13（顶栏分段与 agent 列的 `.ic` 都是 13px），单色；图标由服务端下发标识符（`data.agents[].icon`，如 `"generic"`），前端按名渲染 SVG，不靠 CSS 换色；暗色 `--logo-invert: 1` | `sessions.css:10-12`；`client.js:449,462,622`；`app.css:36` |
| **文本标签** | 无底色、无边框，只靠字号/颜色层级：`--fg`（13px/500）→ `--muted`（11.5px）→ `--faint`（11px） | `sessions.css:60-72` |
| **搜索命中** | `background: var(--accent-soft); border-radius: 3px; padding: 0 1px; font-weight: 500` | `sessions.css:33-35` |
| 状态点 | dot 或 swatch；失败态 = 同一个盒子画成 x（`--x-mark` 蒙版），缩放 1.5 让视觉重量与圆点一致 | `app.css:24-27` |

截断规则（三处统一）：

- 名称类：`max-width: 60%`（文件夹名）+ `overflow:hidden; text-overflow:ellipsis; white-space:nowrap`（`sessions.css:65`）。
- 路径类：`flex: 0 1 auto`，可被压到 0，省略号结尾；窄屏（≤560px）**整段隐藏**（`sessions.css:181`）。
- 摘要类：单行省略（`.sm-latest`）。

> 手机端结论：**不要给每个 agent 一个彩色 chip**。用「13px 单色图标 + 文本层级」表达来源；只用一种强调色（`--accent-soft`）留给搜索命中与选中态。要给工作区/分支做标签，规格见 §6.3。

---

## 3. 状态与动作

### 3.1 magpie 有哪几种状态，怎么判定

| 状态 | magpie 里的实际形态 | 判定方式 |
|---|---|---|
| 运行中 | 会话行**不显示**；只有 Routing 页把「请求在飞 / 最近一条 15 分钟内」的会话算 live（`sessions.go` 的 live 判定与 `renderCtxSessions`） | 请求在途或 `now - newest <= 15m` |
| 等待输入 | 无（Sessions 页不表达） | — |
| 需要授权 | 无（magpie 自己不做审批 UI） | — |
| 出错 | agent 层状态点 `.ag-dot`；WorkBuddy 自己的快照有 `state: "error"` | 各 agent 自己的状态字段 |
| 完成 | 无显式状态；`last` 时间即「最后一次活动」 | — |
| 文件仍在写 | `s.pending`：首次读只拿到前几行，1.5s 后自动补拉一次（`fillSoon`） | `client.js:121-140` |

也就是说：**magpie 把「状态」留给了 agent 自己，Sessions 页只负责「存在 + 最近活动时间」**。手机端要做「运行中/等待输入/需要授权」，素材必须来自 relay 的事件流（`kind: permission | needs_input | …`），不能指望从 magpie 抄到现成的状态机。

### 3.2 动作放在哪

| 动作 | 位置 | 源码 |
|---|---|---|
| Resume（复制恢复命令） | 行右，主按钮；点击后变 `Copied` 1.4s 再变回（`data-wide` 保证宽度不变） | `client.js:1020-1040` |
| 在会话终端打开 | 行右，图标按钮（仅当该 agent 支持终端） | `client.js:1041-1046` |
| Continue in（换一个 agent 继续这个会话） | 行右，下拉；菜单里每项是「复制命令」或「转换后复制/打开终端」 | `client.js:1294-1305` |
| 删除 | 行右，垃圾桶；不可删时按钮仍占位并 `aria-disabled`，hover 说明原因 | `client.js:1060-1075` |
| 导出 Markdown / 看对话 | 详情展开区（`.sess-detail`） | `client.js:1252` |
| 整目录操作 | 文件夹行 `⋯`（hover 出现） | `client.js:905-925` |
| 批量 | 选择模式底部条（`.sm-bar`） | `sessions.css:39-45` |

交互细节（值得抄的两条）：

1. **按钮占位保列对齐**：某行没有 Resume 时，放一个 `.sm-slot`（`visibility:hidden`）而不是让后面的列左移。注释直接写了这是为了一位用户的反馈（`sessions.css:80-83`）。
2. **点击不移动页面**：整页 `user-select:none`，只有文本内容可选；点击引起的重绘不许把页面顶走（`gui-shell.md`「A click never moves the page」）。

---

## 4. 视觉规范（可从源码取到的确定值）

来源：`internal/gui/assets/app.css`（`:root`、dark、colorblind）、`…/plugins/sessions/sessions.css`、`docs/subsystems/gui-shell.md`。

### 4.1 配色 token

| Token | 浅色 | 深色 | 用途 |
|---|---|---|---|
| `--bg` | `#f4f4f6` | `#1a1a1e` | 页面底 |
| `--card` | `#ffffff` | `#232327` | 卡片/浮层 |
| `--card-2` | `#f9f9fb` | `#1f1f23` | 展开的编辑器 |
| `--pill` | `#f1f1f4` | `#2d2d33` | 按钮/胶囊底 |
| `--pill-hover` | `#e9e9ee` | `#36363d` | 胶囊 hover |
| `--seg-track` | `#e6e6eb` | `rgba(255,255,255,.035)` | 分段控件轨道 |
| `--seg-thumb` | `var(--card)` | `rgba(255,255,255,.15)` | 分段控件滑块 |
| `--line` | `#e3e3e8` | `#333339` | 主分隔线/描边 |
| `--line-2` | `#ececf0` | `#2c2c32` | 次级分隔线（**文件夹之间的 hairline 用它**） |
| `--fg` | `#1c1c21` | `#ededf1` | 主文本 |
| `--fg-2` | `#4b4b53` | `#b9b9c1` | 次文本 |
| `--muted` | `#85858d` | `#8b8b94` | 三级文本（时间、副行） |
| `--faint` | `#b9b9c2` | `#56565f` | 四级（路径、计数） |
| `--ctl-fg` | `#63636b` | `#a1a1aa` | 控件静默文字（保证 ≥4.5:1） |
| `--accent` | `#4f46e5` | `#a5b4fc` | 强调/焦点 |
| `--accent-soft` | `#eeedfd` | `#2b2c47` | 搜索命中底、选中底 |
| `--sel` | `#ecebfb` | `#33345a` | 选中行 |
| `--green` / `--green-soft` | `#189a58` / `#e4f6ec` | `#6fd99b` / `#1e3a2b` | 成功 |
| `--amber` / `--amber-soft` | `#b45309` / `#fbf0df` | `#f2b544` / `#3f3220` | 警告/额度 |
| `--red` / `--red-soft` | `#d64545` / `#fbe9e9` | `#f28b82` / `#45272a` | 错误（**必须配 x 形状，不单靠红**） |
| `--drift` / `--drift-soft` | `#ce2c31` / `#feebec` | `#ff9592` / `#3b1219` | 「配置漂移」态 |
| `--pop-bg` | `rgba(255,255,255,.94)` | `rgba(38,38,43,.96)` | 浮层/菜单 |
| `--shadow` | `0 16px 44px rgba(20,22,30,.16), 0 2px 8px rgba(20,22,30,.06), 0 0 0 .5px rgba(20,22,30,.1)` | 同结构、更重 | 浮层阴影 |
| `--c1…--c7` | `#4f46e5 #0ea5e9 #10b981 #f59e0b #ec4899 #8b5cf6 #64748b` | 更亮的一套 | 图表序列（provider/agent/model 区分） |

色盲模式（`data-colorblind="1"`）把 `--c1…--c7` 换成 Okabe–Ito：`#0072b2 #e69f00 #009e73 #d55e00 #cc79a7 #56b4e9 #64748b`（暗色另有一套）。

字体栈：`--font: -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", system-ui, sans-serif`；等宽 `--code: ui-monospace, "SF Mono", Menlo, Consolas, monospace`。

### 4.2 字号层级（app.css 里 font-size 的实际使用频次）

| 字号 | 出现次数 | 角色 |
|---|---|---|
| 12px | 127 | 正文默认 |
| 11px | 113 | 标签、计数 |
| 11.5px | 103 | 次要信息行（时间、模型、路径） |
| 10.5px | 54 | 极小标签（分段里的计数 `em`） |
| 12.5px | 52 | 搜索命中行、说明 |
| 13px | 21 | 名称/标题（会话名、文件夹名） |
| 10px / 9.5px / 9px | 13 / 9 / 4 | 徽章、菜单栏小字 |
| 14 / 15 / 16px | 9 / 6 / 4 | 页标题 |
| 38px | 1 | 空状态大数字 |

Sessions 页专用值：文件夹名 13px/500、路径 11.5px、最近标题 12px、时间 11.5px、计数 11px `tabular-nums`、搜索高亮 12.5px。

### 4.3 间距、圆角、尺寸

| 项 | 值 | 来源 |
|---|---|---|
| 列表行高 | 44px（文件夹行与会话行同高） | `sessions.css:47,79` |
| 行内 gap | 8px | `sessions.css:1,47` |
| 会话行缩进 | 29px（窗口内 32px） | `sessions.css:74-75` |
| 卡片内 padding | 0 11px（工具条）/ 8px 10px（对话区） | `sessions.css:39,109` |
| 圆角 | 7px（按钮）/ 9px（对话区）/ 3px（高亮 mark） | `sessions.css:84,109,34` |
| 搜索框宽 | 200px（≤560px 时 130px） | `sessions.css:24,181` |
| 页面内容宽 | 最多 1200px 居中，窄窗左右留 16px：`padding: max(16px, calc(50% - 600px))` | `gui-shell.md` |
| 对话框宽 | 600px；长文本编辑器 880px | `gui-shell.md` |
| 图标 | 分段/agent 13px；chevron 10–11px；垃圾桶 13px；⋯ 14px | `sessions.css`、`client.js` |

### 4.4 深/浅色

- 三套状态：跟随系统（`@media (prefers-color-scheme: dark)`）、强制浅（`[data-theme="light"]`）、强制深（`[data-theme="dark"]`）。
- 浅色是「近白底 + 1px 描边」，深色是「近黑底 + 低对比描边」，两套都靠 `--line/--line-2` 分层，不用阴影分卡片（阴影只给浮层）。
- 图标在深色下用 `--logo-invert: 1` 反相，而不是换一套图标。

### 4.5 动效

| 交互 | 时长 / 曲线 | 源码 |
|---|---|---|
| 面板 tab 切换（高度滑到新 tab） | `TAB_GLIDE = 240ms`，`--ease-out`；`prefers-reduced-motion` 时直接跳 | `gui-shell.md` |
| 文件夹 `⋯` 浮现 | `opacity .12s` | `sessions.css:73-76` |
| 按钮文字切换（Resume→Copied） | 1.4s 后回切 | `client.js:1035-1040` |
| 行 hover | 底色 `color-mix(in srgb, var(--pill) 40%, transparent)` | `sessions.css:52` |
| 滚动到顶/底 | 平滑滚动，尊重 reduced-motion | `gui-shell.md` |

原则性规定：动效只用透明度与高度过渡，不做位移动画；`prefers-reduced-motion` 一律降级为瞬变。

---

## 5. 数据来源：三种 agent 的原生会话元数据（本机只读核实）

核实方式：直接读本机文件（`ls` / `sqlite3 .tables` / 读 JSONL 首行与标题行），不写、不改、不登录。样例值截断脱敏。

### 5.1 Codex

| 用途 | 路径 | 字段 | 本机实测样例 |
|---|---|---|---|
| **标题** | `~/.codex/session_index.jsonl`（601 行） | `thread_name`（另含 `id`、`updated_at`） | `"搭建多端回复推送原型"` |
| 工作区 | `~/.codex/sessions/<Y>/<M>/<D>/rollout-<ISO>-<id>.jsonl` 首行 `session_meta.payload.cwd` | `cwd` | `/Volumes/ESD-USB/机场安检模拟经营类游戏` |
| 分支 | 同上 | `payload.git.branch` / `payload.git.commit_hash` | `codex/exp…` / `f285097c…` |
| 会话 id | 同上 | `payload.id` | `01a1bad2-568a-7b33-…` |
| 来源（CLI/桌面） | 同上 | `payload.originator` | `Codex Desktop` |
| 版本 | 同上 | `payload.cli_version` | `0.167.0-alpha.3.2` |
| 模型 | 会话内 `turn_context` / `thread_settings_applied` 事件 | — | — |
| 增量投影 | `~/.codex/thread_history_1.sqlite`（6.2 GB，`thread_turns` / `thread_items`）、`~/.codex/state_5.sqlite` | `thread_id, item_type, rollout_ordinal, created_at_ms` | 只读；接库时务必 `mode=ro` |

magpie 的解析：`internal/sessions/codex_titles.go:46-52`（索引路径 `filepath.Join(CodexDir(), "session_index.jsonl")`）、截断 200 字符（`:168`）、`codex.go` / `codex_cwd.go` 读 `session_meta` 的 `cwd` 与 `git`。

> 注意：`session_index.jsonl` 是**追加写**的索引，同名 id 可多次出现，取 `updated_at` 最新的一条。

### 5.2 Claude Code

| 用途 | 路径 | 字段 | 本机实测样例 |
|---|---|---|---|
| 会话文件 | `~/.claude/projects/<cwd-slug>/<sessionId>.jsonl` | 每行都有 `sessionId`、`cwd`、`gitBranch`、`version` | `cwd=/private/tmp/agentpush-new-session`、`gitBranch=HEAD`、`version=2.1.90` |
| **AI 标题** | 同上 | `type:"ai-title"` 行的 `aiTitle` | 本机 17 个项目目录中该类行未命中（见下注） |
| **用户改名** | 同上 | `type:"custom-title"` 行的 `customTitle` | 同上 |
| 摘要（旧格式） | 同上 | `type:"summary"` 行的 `summary` | 本机 `grep '"type":"summary"'` 全量无命中 |
| 首条 prompt | 同上 | 第一条 `type:"user"` 的 `message.content` | 用于四级回退的第 4 级 |
| 项目路径全集 | `~/.claude.json` 的 `projects` 键 | 键名即完整 cwd | 15 项，如 `/Users/fanqiege/Desktop/claude-code-haha` |
| 输入历史 | `~/.claude/history.jsonl` | `{display, project, sessionId, timestamp}` | `project="/Users/fanqiege"`，`display` 为首句（截断 50 字内） |

目录名编码：cwd 的 `/` 全部换成 `-`，且开头的 `/` 也变成一个 `-`，例：`/private/tmp/agentpush-new-session` → `-private-tmp-agentpush-new-session`；`/Users/fanqiege/.shoptwin` → `-Users-fanqiege--shoptwin`（点也保留）。

> 本机实测结论：这台机器上的 Claude Code 2.1.90 **没有写出 `ai-title` / `summary` 行**（全量 grep 零命中）。所以在本机，Claude 会话的标题实际只会落到第 4 级（首条 prompt）。magpie 源码仍是三级都读（`claude.go:390-401`），说明别的版本会写。**手机端不要把「Claude 一定有 AI 标题」当成前提。**

### 5.3 WorkBuddy（腾讯桌面端 5.9.2）

三处来源，各自完整度不同：

**(a) 会话索引快照（最省事，含标题+状态+工作区）**

`~/.workbuddy/<userId>/sidebar-list-snapshot.json`（本机 `userId=27e00082-…`，166 条）：

```jsonc
{
  "version": 3,
  "savedAt": 1794146375563,
  "items": [
    {
      "id": "08fdb08a-4537-4e23-9481-a64e74c72cf5",
      "title": "翻译一段英文文本",                    // 已是 AI 生成的中文标题
      "state": "idle",                                // 实测取值：idle | error
      "transport": "local",
      "kind": "task",
      "lastActivityAt": 1794145492879,                // 毫秒
      "isPinned": false,
      "conversationOrigin": "local",
      "updatedAt": 1794145793994,
      "space": { "type": "local", "name": "/Users/fanqiege/WorkBuddy/2026-10-16-22-48-45" }
    }
  ]
}
```

`space.name` 就是**工作区/项目目录**——这正是「所属工作区」标签最干净的来源。

**(b) SQLite 索引（权威，字段最全）**

`~/.workbuddy/workbuddy.db` → `sessions` 表（41 列），与本任务相关的列：
`id, cwd, title, custom_title, status, created_at, updated_at, last_activity_at, deleted_at, model, project_id, unread, transport('local'|'cloud'), permission_mode`。
另见 `workspaces`、`session_usage`、`workspace_trust_decisions` 表。

**(c) 转录 JSONL（标题的原始出处）**

`~/.workbuddy/projects/<cwd-slug>/<sessionId>.jsonl`，记录类型含 `ai-title` / `custom-title`（带毫秒 `timestamp`、`sessionId`、通常带 `cwd`）；旁挂 `<sessionId>.meta.json` 存 cwd。
magpie 解析：`internal/sessions/workbuddy.go:174-211`。

> 采集优先级建议：**先读 (a) 快照**（一次读全、含状态与工作区），缺失再回落到 (b) SQLite，最后才解析 (c)。三者的 `title` 语义一致（三级回退已由 WorkBuddy 自己做过一遍）。

### 5.4 三种 agent 的字段覆盖对照

| 字段 | Codex | Claude Code | WorkBuddy |
|---|---|---|---|
| 原生标题 | ✅ `session_index.jsonl:thread_name` | ⚠️ 取决于版本（本机无） | ✅ 快照 `title` / `ai-title` |
| 用户改名 | ❌（用 thread 名顶替） | ✅ `custom-title` | ✅ `custom_title` / `custom-title` |
| 工作区 cwd | ✅ `session_meta.cwd` | ✅ 行内 `cwd` | ✅ `space.name` / `sessions.cwd` |
| git 分支 | ✅ `session_meta.git.branch` | ✅ `gitBranch`（常为 `HEAD`） | ⚠️ 未见原生字段，需按 `cwd` 现算 |
| 模型 | ⚠️ 需扫 `turn_context` | ⚠️ 需扫消息的 `model` 字段 | ✅ `sessions.model` / 消息 `providerData.model` |
| 状态 | ❌（无） | ❌（无） | ✅ 快照 `state` |
| 未读 | ❌ | ❌ | ⚠️ 有 `unread` 列，但桌面端在线时恒 0 |
| 最后时间 | ✅ rollout 文件 mtime / 末行 `timestamp` | ✅ 末行 `timestamp` | ✅ `lastActivityAt` |

---

## 6. 移动端落地建议（Compose）

现有结构（只读参考，本会话不改动）：`android/…/ui/screens/InboxScreen.kt`（`SessionCard` / `FilterRow` / `SwipeableSessionRow` / `SessionSummary`）、`SessionScreen.kt`、`ui/theme/Theme.kt`（Material3）、`Models.kt`（`AgentBrand{claude,codex,workbuddy}`、`AgentEvent`、`ReplyStatus`）。

### 6.1 首页线框（会话卡片）

```
┌───────────────────────────────────────────────────────────┐
│  Agent 收件箱                            ⟳ 12 条未读  ⚙  │  ← 22px/500，右侧 12px
│                                                           │
│  [ 全部 ] [ Claude ] [ Codex ] [ WorkBuddy ]              │  ← 分段，高度 32，选中=accent-soft
│  ⌕ 搜索会话 / 路径 / 分支…                                │  ← 40px 高，圆角 10
│                                                           │
│  未读 3 · 需要你处理 1                                     │  ← 12px muted，仅当有
├───────────────────────────────────────────────────────────┤
│  ⚠ 需要授权 · 2 分钟前                                     │  ← 状态条，12px，amber
│  ┌─────────────────────────────────────────────────────┐  │
│  │ ⬤C  修复 relay 的 SSE 重连                            │  │  ← 15px/600 标题，2 行省略
│  │     agent-push-poc  ⎇ main  ·  claude · sonnet-5     │  │  ← 12px 标签行（见 §6.3）
│  │     “把 relay 的心跳间隔调成 15s，然后…”               │  │  ← 12.5px muted 单行摘要
│  │     2 分钟前 · 3 条未读                               │  │  ← 11.5px faint
│  │                              [ 批准 ] [ 查看 ]        │  │  ← 32px 高按钮
│  └─────────────────────────────────────────────────────┘  │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ ⬤W  翻译一段英文文本                                 │  │
│  │     WorkBuddy/2026-10-16-22-48-45 · workbuddy        │  │
│  │     1 小时前                                          │  │
│  └─────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────┘
```

与 magpie 的对应关系：**agent 分段抄它的顶栏分段**（icon+name+count，窄屏退化下拉）；**分组抄它的 cwd 折叠**——手机屏更小，建议默认「按时间平铺 + 可切到按项目分组」，分组的标题行直接复用它的文件夹行布局（名称 13px/500 + 路径 11.5px faint + 计数 11px）。

### 6.2 会话详情顶部标题栏

```
┌───────────────────────────────────────────────────────────┐
│ ←   修复 relay 的 SSE 重连                          ⋯     │  ← 返回 + 标题 2 行省略 + 溢出菜单
│     ⬤C claude · sonnet-5 · effort high                    │  ← 12px 标签行
│     agent-push-poc  ⎇ main  ·  2 分钟前 · 未读 3           │  ← 12px 第二行
├───────────────────────────────────────────────────────────┤
│  （对话/事件流）                                           │
├───────────────────────────────────────────────────────────┤
│  [ 回复 ]                                    [ 停止 ]     │  ← 底部固定，仅在有会话时出现
└───────────────────────────────────────────────────────────┘
```

折叠规则：向上滚动时标题栏收缩为单行（标题 + agent 圆点），标签行并入副标题；这是移动端空间换算，magpie 无对应物，属于移动端自定。

### 6.3 标签行规格（“一眼认出属于哪里”）

magpie 不用彩色 chip（§2.3），但手机端卡片只有一屏，需要比桌面更强的「归属感」。建议**只用一种强调色 + 一种弱底**，靠图标与文本区分来源：

| 标签 | 内容 | 样式 | 缺省 |
|---|---|---|---|
| agent | 13px 单色图标 + 品牌名 | 图标 `onSurface`，文字 `onSurface` 13px/500 | 未知 → `A` 图标 + “Agent” |
| 工作区/项目 | `project`（cwd 的 basename） | 12px，`--accent-soft` 底、`--accent` 文字、圆角 4、padding 2×6 | 无 cwd → 显示 “无项目”，用 faint |
| 分支 | `⎇ main` | 12px，`outlineVariant` 描边 1px、透明底、等宽字体 | 无分支/`HEAD` → 不显示（`HEAD` 视为 detached，不占位） |
| 模型 | `sonnet-5` / `deepseek-v4.1-flash` | 12px `onSurfaceVariant`，不描边 | 无 → 不显示 |
| 状态 | 运行中/需要授权/出错/完成 | 见 §6.5 | — |

宽度预算：一行最多 3 个标签，超出以 `+N` 折叠（点击展开）。项目名 `max-width: 45%` 后省略，分支名 `max-width: 30%`，模型名 `max-width: 35%`。

### 6.4 字段优先级（列表卡片）

按「卡片三行」分配，每行内部按优先级取第一个有值的：

1. **第一行（标题）**：`title_source=custom` → `title_source=agent`（AI 标题）→ `title_source=first_prompt` → `"(无标题)"`。绝不用 session_id 当标题。
2. **第二行（归属）**：`project`（必显）→ `branch`（有且非 `HEAD`）→ `model`（有）→ `source`（作为图标已表达，文字可省）。
3. **第三行（时间/未读）**：`last_ts` 相对时间（`<1m` → `刚刚`，`<1h` → `N 分钟前`，`<24h` → `N 小时前`，否则 `M-D`）；有未读追加 `· N 条未读`；`last_preview` 有值则插在第二、三行之间。

状态优先级高于全部：`needs_authorization` / `error` 时，卡片顶部出现状态条并改用 `amber` / `error` 容器色。

### 6.5 状态映射（relay 事件 → 卡片）

| 状态 | 触发（relay 侧） | 卡片表现 |
|---|---|---|
| `running` | 收到该会话任何事件且 90s 内还有心跳 | 标题前 pulse 圆点（`--green`），无文字 |
| `needs_input` | `kind=needs_input` | 状态条「等待你的输入」+ `[回复]` 主按钮 |
| `needs_authorization` | `kind=permission` | 状态条「需要授权」+ `[批准]` `[拒绝]` |
| `error` | `kind=error` 或 agent 原生 `state=error`（WorkBuddy 快照有） | 状态条「出错」+ `[查看]`；**图标用 x 形，不只用红色** |
| `done` | 收到 `stop` / `turn complete` 且无后续事件 | 无状态条，仅更新 `last_ts` |

`done` 与 `running` 互斥：同一会话收到新事件即从 `done` 回到 `running`。magpie 的等价物只有「`pending`（文件仍在写）」和 Routing 页「15 分钟内算 live」，不要把这两者误当业务状态。

### 6.6 relay 新增接口草案：`GET /api/sessions`

与现有 `/api/events`、`/api/stream`、`/api/read`、`/api/reply` 并列（不改动它们）。

```
GET /api/sessions?source=all&status=any&unread=0&limit=50&cursor=<opaque>
Authorization: Bearer <token>        // 与现有 /api/* 同鉴权

200
{
  "sessions": [
    {
      "session_id": "01a1bad2-568a-7b33-ba69-9344a0470cad",
      "source": "codex",
      "title": "搭建多端回复推送原型",
      "title_source": "agent",
      "workspace": "/Volumes/ESD-USB/机场安检模拟经营类游戏",
      "project": "机场安检模拟经营类游戏",
      "branch": "codex/exp-2026-11",
      "cwd": "/Volumes/ESD-USB/机场安检模拟经营类游戏",
      "model": "gpt-5.6-terra",
      "status": "done",
      "unread": 0,
      "last_ts": "2026-11-08T11:03:51.909Z",
      "last_preview": "已经把 relay 的心跳改成 15s，重启后验证通过"
    }
  ],
  "next_cursor": null,
  "generated_at": "2026-11-08T12:00:00Z"
}
```

字段语义：

| 字段 | 类型 | 说明 |
|---|---|---|
| `session_id` | string | agent 原生会话 id（Codex thread id / Claude sessionId / WorkBuddy session uuid） |
| `source` | enum | `claude` \| `codex` \| `workbuddy`（与 `AgentBrand.wire` 对齐） |
| `title` | string | 已按四级回退算好，**永不为空** |
| `title_source` | enum | `custom` \| `agent` \| `first_prompt` \| `fallback` |
| `workspace` | string? | 会话的工作目录绝对路径 |
| `project` | string? | `workspace` 的 basename，供 UI 直接显示 |
| `branch` | string? | git 分支；`HEAD`/空/非 git → `null` |
| `cwd` | string? | 与 `workspace` 同值，保留给「打开终端/复制路径」用 |
| `model` | string? | 会话最近使用的模型 |
| `status` | enum | `running` \| `needs_input` \| `needs_authorization` \| `error` \| `done` |
| `unread` | int | **手机侧未读**（`last_ts` 之后未被 `/api/read` 标记的推送条数） |
| `last_ts` | ISO8601 | agent 侧最后活动时间 |
| `last_preview` | string? | 最后一条助手消息的首行，≤120 字 |

三 agent 的取值实现：

| 字段 | Codex | Claude Code | WorkBuddy |
|---|---|---|---|
| `session_id` | `session_meta.payload.id`（rollout 文件名后缀同值） | `<uuid>.jsonl` 文件名 / 行内 `sessionId` | 快照 `items[].id` / `sessions.id` |
| `title` | `session_index.jsonl` 最新 `thread_name` → 无则首条 user 消息 → `"(无标题)"` | `custom-title` → `ai-title` → `summary` → 首条 user → `"(无标题)"` | 快照 `title` → `custom_title` → `ai-title` → 首条 user → `"(无标题)"` |
| `title_source` | 有 `thread_name` = `agent`，否则 `first_prompt` | `custom-title`=`custom`；`ai-title`/`summary`=`agent`；否则 `first_prompt` | `custom_title`=`custom`；快照 `title`=`agent`；否则 `first_prompt` |
| `workspace`/`cwd` | `payload.cwd` | 行内 `cwd`（或从 `~/.claude/projects` 目录名反解） | `space.name` → `sessions.cwd` |
| `project` | `basename(cwd)` | 同 | `basename(space.name)` |
| `branch` | `payload.git.branch` | 行内 `gitBranch`（`HEAD` → `null`） | 无原生字段：relay 侧 `git -C <cwd> rev-parse --abbrev-ref HEAD`（缓存 30s），或前端不显示 |
| `model` | 扫 `turn_context` / `thread_settings_applied` 最后一条 | 扫消息 `message.model` 最后一条 | `sessions.model` / 最后一条消息 `providerData.model` |
| `status` | 由 relay 事件流推导（无原生） | 同 | 快照 `state`（`idle`/`error`）+ 事件流推导 |
| `unread` | relay 自算 | relay 自算 | relay 自算（`sessions.unread` 恒 0，不可用） |
| `last_ts` | rollout 末行 `timestamp` 或文件 mtime | jsonl 末行 `timestamp` | `lastActivityAt`（毫秒 → ISO） |
| `last_preview` | 最后一条 `response_item/message`（assistant）首行 | 最后一条 assistant 消息首行 | 最后一条 `type:"message", role:"assistant"` 首行 |

实现约束（踩过的坑，直接写进 relay 的实现约定）：

1. **只读打开**：Codex 的 `thread_history_1.sqlite` 有 6.2 GB，一律 `file:...?mode=ro&immutable=1`，绝不 `VACUUM`/写锁；JSONL 用流式读，不整文件 `readFileSync`。
2. **增量缓存**：按 `(path, size, mtime)` 缓存解析结果；只解析新增字节（magpie 自己也是这么做的：`statFiles` + 偏移）。
3. **标题索引要去重**：`session_index.jsonl` 追加写，同 id 取 `updated_at` 最新。
4. **不要碰凭据文件**：`~/.workbuddy/keyblob`、`credentials/`、`~/.codex/auth.json`、`~/.claude/credentials*` 一律不读。
5. **隐私开关**：magpie 首次打开 Sessions 页要用户显式授权读会话历史（`client.js:375-379`）。手机端建议同样做一次「允许 relay 读取本机会话元数据」的显式确认，并把开关放进设置。

### 6.7 实施拆分建议（按依赖顺序）

| 步骤 | 内容 | 产出 |
|---|---|---|
| 1 | relay 增加 `/api/sessions`（先只做 Codex：`session_index.jsonl` + `session_meta`） | 手机能列出 Codex 会话与标题 |
| 2 | 接 Claude Code（`~/.claude/projects` 目录名 + 行内 `cwd`/`gitBranch`） | 双 agent 列表 |
| 3 | 接 WorkBuddy（先读 `sidebar-list-snapshot.json`，最省事） | 三 agent 列表 + 原生状态 |
| 4 | 手机端：`SessionSummary` 扩字段 → 卡片第二行标签 → 状态条 | 卡片与详情页改版 |
| 5 | 未读：relay 记 `read` 水位，配合现有推送 | 未读角标 |
| 6 | 分组视图切换（时间 / 项目） | 对齐 magpie 的文件夹分组 |

---

## 附：本文件的事实来源

| 结论类型 | 依据 |
|---|---|
| 页面/导航/共享 UI 规则 | `docs/subsystems/gui-shell.md`；`docs/subsystems/README.md` |
| 会话列表结构、分组、标题、动作 | `internal/gui/assets/plugins/sessions/{client.js,sessions.css,i18n.js}` |
| 标题四级回退链 | `internal/sessions/sessions.go:127, 1963-1973` |
| 各 agent 标题解析 | `internal/sessions/{claude.go,workbuddy.go,codex_titles.go}` |
| 配色/字号/间距/动效 | `internal/gui/assets/app.css`（`:root` / `prefers-color-scheme` / `[data-theme]` / `[data-colorblind]`） |
| 三 agent 本机路径与字段 | 本机只读核实（见 §5 各表「本机实测样例」列） |
| 手机端现状 | `android/app/src/main/java/dev/agentpush/poc/{Models.kt,ui/screens/InboxScreen.kt,ui/theme/Theme.kt}`（只读） |

