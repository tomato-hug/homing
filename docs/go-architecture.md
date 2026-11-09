# Homing Go 架构（2026-11-09 定）

电脑端和云端中转从 Node（homingd + Tauri 壳）改为 Go，做法参照 Magpie（yetone/magpie：Go + Wails，一个程序同时是菜单栏应用、窗口、TUI、网页和命令行）。安卓端不变，仍是 Kotlin + Compose。

为什么改：一个语言、一个进程、一个可执行文件。Node sidecar + Tauri 壳是两种语言、两个进程，安装包 60–100 MB；Go 版带界面估计 15 MB 左右，纯命令行版不到 10 MB，交叉编译简单，还能直接跑在 Linux 服务器、远程开发机和 Docker 里（很多人的 agent 跑在那里）。

## 1. 不变的东西

- 手机端一行代码都不用改：Go 版 `/api/*` 的路径、参数和 JSON 字段与 Node 中继逐字一致（docs/relay-v2.md、docs/desktop.md §4、docs/adapters.md）。
- 协议 v1（docs/protocol.md）、加密算法和 `packages/protocol-js/test-vectors.json` 不变；Go 实现必须跑通同一组向量。
- 自定义 agent 的清单格式（~/.homing/agents/*.yaml）、脚本适配器位置（~/.homing/adapters/*.mjs）和事件上报接口不变。
- 控制台页面（console/，WorkBuddy 已做）保留，构建产物放进 `web/console/` 用 `go:embed` 打进程序。
- 切换完成前，Node 中继继续在 127.0.0.1:18787 服务手机（LaunchAgent dev.agentpush.relay），不要停。

## 2. 一个程序，几种用法

| 命令 | 作用 | 阶段 |
|---|---|---|
| `homing` | 托盘 / 菜单栏 + 窗口（Wails v3） | G4 |
| `homing serve` | 只跑本机服务，不带界面；同时提供 /console 网页 | G1 |
| `homing ls` / `homing status` | 列出 agent、会话、连接状态 | G1 |
| `homing pair` | 在终端里打印配对二维码和 6 位码 | G3 |
| `homing agents test <清单>` | 试读一个自定义 agent 清单 | G2 |
| `homing tui` | 终端界面 | G5 |
| `homing update` | 手动检查更新（平时后台自动） | G5 |
| `homing-server` | 云中转 / 自建服务器 | G3 |

纯命令行版用 `CGO_ENABLED=0` 编译（Linux、Docker、Termux）；带界面的 Mac / Windows 版在各自的 CI 机器上编译。

## 3. 目录与归属

```
go.mod                       module github.com/tomato-hug/homing（谁加依赖谁提交 go.mod/go.sum）
cmd/homing/                  电脑端入口                                   WorkBuddy
internal/cli/                子命令注册表（homing pair、homing agents …）   各自注册
cmd/homing-server/           云中转入口                                   Codex
internal/agent/              Agent 接口与注册表（已有，改接口先在汇报里说） WorkBuddy
internal/agent/<id>/         内置 agent：codex claude workbuddy opencode zcode …  WorkBuddy；新 agent 由 DSH 加
internal/agent/builtin/      一行一个 import，汇总内置 agent              谁加 agent 谁加一行
internal/api/                本机 HTTP：/api/*、SSE、/console              WorkBuddy
internal/files/              附件、能力链接与 /f 文件解析                   G1 兼容层
internal/relayapi/           hooks、上传、会话操作、设备与下载兼容路由      G1 兼容层
internal/store/              已读/置顶/静音/归档、已配对设备、令牌          WorkBuddy
internal/notify/             P0/P1/P2 分级、30 秒合并、暂停推送              WorkBuddy
internal/platform/           数据目录、各 agent 路径、WSL、开机自启          WorkBuddy
internal/adapter/            自定义 agent：清单引擎、脚本适配器、事件上报    DSH
internal/protocol/           协议 v1 的 Go 实现                            Codex
internal/cloud/              连云中转 + 局域网直连 /v1/ws                   Codex
internal/server/             homing-server 的实现                          Codex
internal/gui/                Wails v3：托盘、托盘面板、主窗口、点击动画帧   WorkBuddy
web/console/                 控制台静态页（由 console/ 构建）               WorkBuddy
build/icon/                  图标与托盘动画帧生成程序                       WorkBuddy
android/  packages/          不变                                          Codex
relay/  server/  desktop/    旧 Node / Tauri 版，Go 版对照通过后移到 legacy/
```

插拔方式（避免几个人同时改同一个文件）：
- 内置 agent 在自己包的 `init()` 里 `agent.Register(...)`，再在 `internal/agent/builtin/builtin.go` 加一行 import。
- 运行时才知道的 agent（清单、脚本）用 `agent.RegisterProvider(...)`。
- 路由在各自包的 `init()` 里 `api.Register(name, order, func(mux, deps){...})`，用 Go 1.22 的路由写法（`"GET /api/agents"`）。
- 子命令同理：在自己包的 `init()` 里 `cli.Register(cli.Command{Name: "pair", Summary: "…", Run: …})`（`internal/cli`），`homing help` 会自动列出；`homing pair` 由 internal/cloud 注册，`homing agents test` 由 internal/adapter 注册。
- `cmd/homing` 已经空导入 `internal/adapter`、`internal/cloud`、`internal/agent/builtin`，DSH 和 Codex 不用改 main.go。

## 4. Agent 接口

骨架已提交，见 `internal/agent/agent.go`。必须实现：`ID / Name / Icon / PushSignal / Capabilities / Detect / ListSessions / Meta`；可选：`ReadItems / IsEmptySession / Reply / Models / Watch`，实现了就算支持，`/api/agents` 的 `capabilities` 由此生成。JSON 字段先按 Node 版写了，WorkBuddy 迁第一个 agent 时以 Node 实际输出为准校正；校正后其他人以代码为准。

会话读取沿用 Magpie 的三种策略：`line`（标准 JSONL 逐行）、`grow`（长 JSONL 记偏移量增量读）、`whole`（整个 JSON / SQLite 一次读）。认不出格式的 JSONL 默认按 Claude Code 格式解析（很多 agent 照抄了它）。

## 5. 协议在 Go 里怎么对上 libsodium

| libsodium | Go |
|---|---|
| Ed25519 身份 | `crypto/ed25519` |
| `crypto_sign_ed25519_pk_to_curve25519` / `_sk_` | `filippo.io/edwards25519`：公钥 `BytesMontgomery()`；私钥取种子的 SHA-512 前 32 字节再夹紧 |
| `crypto_box_easy` / `_open_easy` | `golang.org/x/crypto/nacl/box` 的 `Seal` / `Open`（输出同为 MAC‖密文） |
| `crypto_generichash`（BLAKE2b-128 / 64） | `golang.org/x/crypto/blake2b.New(16, nil)` / `New(8, nil)` |
| ULID | `github.com/oklog/ulid/v2` |
| WebSocket | `github.com/coder/websocket` |

SQLite 一律用 `modernc.org/sqlite`（纯 Go，不需要 cgo）。

## 6. 从 Node 切到 Go（对照测试）

1. Go 版先跑在 `homing serve --addr 127.0.0.1:18788`，数据目录用临时目录，首次启动从 relay/data 导入状态（只读拷贝，不改原文件）。
2. `scripts/parity.sh`：同时请求 18787（Node）和 18788（Go）的 `/api/agents`、`/api/sessions`、`/api/sessions?source=…`、`/api/inbox`、`/api/projects`，用 jq 去掉时间类字段后逐字段比较；PATCH 只在 Go 一侧的临时数据目录上测。
3. 回复注入只在 /tmp 下新建的 Codex 测试线程里测，不碰用户真实会话。
4. 全部一致后再换：停 LaunchAgent dev.agentpush.relay → 装 homing 的开机启动 → 端口改回 18787。这一步要先问用户。

### 对照记录（2026-11-09）

环境：Node 版继续由 LaunchAgent `dev.agentpush.relay` 跑在 `127.0.0.1:18787`；Go 版由当前工作树构建，跑在 `127.0.0.1:18788`，两边读同一个数据目录。只做只读请求，没有向真实会话写入。

命令：`scripts/parity.sh 18787 18788`。

顶层 12 个接口：

| 接口 | 结果 | 说明 |
|---|---|---|
| `GET /api/projects` | OK | |
| `GET /api/inbox` | OK | |
| `GET /api/away` | OK | 新增只读对照 |
| `GET /api/devices` | OK | 新增只读对照 |
| `GET /api/sessions?source=workbuddy&limit=20` | OK | |
| `GET /api/replies` | OK | |
| `GET /api/events?since=0` | OK | |
| `GET /api/agents` | DIFF | Go 的模型元数据还缺 `provider`、`is_default`、`efforts` 等规范化字段；OpenCode 的模型清单与 Node 不同 |
| `GET /api/sessions?limit=50` | DIFF | Go 发现 48 个会话，Node 40 个；Go 的扫描范围更大 |
| `GET /api/sessions?source=codex&limit=20` | DIFF | 同上，Go 多发现 Codex 会话 |
| `GET /api/sessions?source=claude&limit=20` | DIFF | 同上，Go 多发现 Claude 会话 |
| `GET /api/sessions?limit=10&include_empty=1` | DIFF | 同上，空会话过滤范围不同 |

会话条目抽样 5 个：完整字段对照下 5 个 DIFF（条目数量和顺序一致）。差异集中在 `tool` 对象内容、顶层 `command` 字段缺失、`file_change`/`tool_call` 归类，以及个别 `error` 字段；这些属于 DSH 正在补的 `agent.Item` 富字段映射，本轮没有改 `internal/agent`。

新增兼容接口的 11 项状态检查全部一致：

- `GET /hook/{claude,workbuddy,codex,generic}` 两边都是 405；正常的 POST 写事件由 `internal/relayapi` 的临时数据测试覆盖，不在真实数据上跑。
- 未带 Bearer 的 `/api/upload`、`/api/sessions/new`、`/api/approvals/<id>`、`/api/sessions/<id>/stop` 两边都是 401。
- `GET /p/1/` 两边都是 404；`HEAD /apk` 两边都是 200 且 `Content-Type` 为 `application/vnd.android.package-archive`。
- `/f/...` 取当前第一条带附件的旧事件，两边都返回 404（文件已不在），状态与 `Content-Type` 一致；真实附件的 200、目录穿越防护和 Range 由 `internal/files` 测试覆盖。

未对照：`/api/v1/acp` 不是 Homing 的入站路由，而是 Node 调 WorkBuddy 桌面 ACP 的出站客户端；手机和控制台都不会请求它。Go 版在 `/api/sessions/new` 内部完成同类出站 ACP 调用，因此不新增这个路由。

## 7. 界面（G4）

- Wails v3（锁定确切版本号，不用 @latest）。托盘用它自带的托盘接口：Mac 用模板图标，Windows 用彩色图标；托盘面板是挂在托盘图标上的窗口。
- 控制台页面就是 web/console，窗口直接加载内嵌页面；浏览器访问 http://127.0.0.1:18787/console 看到的是同一套。
- 托盘点击动画：照 Magpie 的做法，在构建时由 `build/icon/gen.go` 按 docs/homing-v2/pigeon_v2.json 的「轻点」场景逐帧生成 PNG（30 ms 一帧，约 20 帧），运行时逐帧换图标；状态图标（有新消息、等你处理、离线、出错）也由它生成。设计稿在画布「动效 v2 分镜」页。

## 8. 发布（G4–G5，参照 Magpie）

- Mac：.dmg（arm64 + x64），用本机钥匙串里的 Developer ID 签名并公证；Windows：安装程序（先不签名，后面走 SignPath）；Linux：纯命令行版；Docker：`ghcr.io/tomato-hug/homing`（homing serve）和 homing-server。
- `curl -fsSL …/install.sh | sh`、`go install github.com/tomato-hug/homing/cmd/homing@latest`。
- 后台自动更新，校验 sha256 后替换。
- 打 tag 自动出版本（GitHub Actions）。

## 9. 阶段

| 阶段 | 内容 | 谁 |
|---|---|---|
| G1 | store、platform、notify、api；把 codex / claude / workbuddy / opencode / zcode 从 relay/scanners 翻成 Go（含回复注入）；`homing serve`、`homing ls`；对照测试全过 | WorkBuddy |
| G2 | internal/adapter：清单引擎、脚本适配器（Go 内嵌一个 runner.mjs，用子进程跑 node 或 bun，标准输入输出一行一个 JSON 调用）、事件上报和令牌、/api/adapters 系列；adapters/ 示例在 Go 版下跑通；`homing agents test` | DSH |
| G3 | internal/protocol 跑通测试向量；homing-server 移植（server/ 的测试用例照搬）；internal/cloud；`homing pair` | Codex |
| G4 | Wails v3 界面、托盘动画帧、控制台内嵌、Mac / Windows 安装包、切换开机启动 | WorkBuddy |
| G5 | TUI、自动更新、install.sh、发布流水线、Docker 镜像 | 待定 |

G1 / G2 / G3 可以同时开工，各写各的目录。
