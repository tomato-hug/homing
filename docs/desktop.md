# Homing 电脑端（M3）

> 桌面壳 + 本机控制台 + 跨平台打包。手机端（`android/`）由 Codex 负责，中继 v2 的会话采集与推送由 DSH 负责，
> 本文只描述电脑端这一层：`console/`、`desktop/`，以及 `relay/` 里新增的平台层与两处挂载点。

---

## 1. 组成

| 目录 / 文件 | 作用 |
|---|---|
| `console/` | 本机控制台网页（Vite + TypeScript，无框架），构建成静态文件，由 homingd 挂在 `/console` |
| `console/dist/` | 构建产物；打包时会被内嵌进 homingd 这个单文件里 |
| `desktop/` | Tauri 2 桌面壳：托盘菜单、窗口、开机自启、sidecar 拉起 |
| `desktop/scripts/build-sidecar.mjs` | 把 `relay/` 打包成单文件 `homingd`（esbuild + Node SEA） |
| `desktop/scripts/make-icons.mjs` | 从 `docs/homing-v2/pigeon.json` 的路径直接画托盘 / App 图标 |
| `relay/platform/` | 平台层：数据目录、agent 家目录（含 WSL）、开机自启、暂停推送、配对与设备存储 |
| `relay/console.mjs` | `/console` 静态页 + 配对 / 设备 / 日志 / 统计 / 装钩子等本机接口 |

进程关系：

```
Homing.app (Tauri)  ──spawn──▶  homingd (单文件, 127.0.0.1:18787)
        │                              │
        └── 窗口 ◀── http://127.0.0.1:18787/console ──┘
```

一个 homingd 同时服务浏览器和桌面壳：浏览器直接开 `http://127.0.0.1:18787/console` 也是完整界面。

---

## 2. 构建

```bash
# 1) 控制台页面
cd console && npm install && npm run build          # → console/dist/

# 2) homingd 单文件（会自动带上 console/dist）
cd desktop && npm install
node scripts/build-sidecar.mjs                      # → desktop/src-tauri/binaries/homingd
node scripts/build-sidecar.mjs --arch x64           # Intel Mac 用的那份
node scripts/build-sidecar.mjs --arch win           # Windows 用的 homingd.exe

# 3) Tauri 安装包
cd desktop
npx tauri build --target aarch64-apple-darwin       # → Homing_0.7.0_aarch64.dmg
npx tauri build --target x86_64-apple-darwin        # → Homing_0.7.0_x64.dmg
```

**为什么是 Node SEA 而不是 `bun build --compile`**：`relay/scanners/lib/opencode-db.mjs` 用 `node:sqlite`
读 OpenCode / ZCode 的库，bun 编译出的二进制里没有这个内建模块（实测报
`No such built-in module: node:sqlite`），所以改用 Node 自带单文件方案（`node --experimental-sea-config`
+ `postject` 注入 blob）。页面通过 SEA assets 内嵌，所以 homingd 真的只有一个文件。

`--arch` 交叉构建时，脚本会从 npmmirror 下载对应平台的 node 运行时（`registry.npmmirror.com/-/binary/node/`），
再把同一份 JS blob 注入进去。blob 与 CPU 架构无关，因此三个平台共用一份。

Windows 的安装程序（NSIS）必须在 Windows 上构建，见 `desktop/scripts/build-windows.ps1`。

### 2.1 两个构建上的坑（在这台机器上实际踩到的）

**Tauri 的 dmg 打包在 macOS 26 上会挂**：`bundle_dmg.sh` 里有一句 `grep -E '/dev/disk[0-9]+s'`，
在新系统上 AppleScript 返回的挂载点变了，脚本报 `bad regex '/dev/disk5s': brackets not balanced`，
还会留下一个挂载着的 `rw.*.dmg`。所以 App 用 `npx tauri build --bundles app` 产出，
dmg 交给 `desktop/scripts/make-dmg.sh`（`hdiutil create -srcfolder`，带 `/Applications` 软链）自己做：

```bash
npx tauri build --bundles app                                   # 只出 .app
bash scripts/make-dmg.sh ../desktop/src-tauri/target/release/bundle/macos/Homing.app ../dist/Homing_0.7.0_aarch64.dmg
```

**x64 那套是手工装的 rust-std**：这台机器上 `rustup target add x86_64-apple-darwin` 被代理改写到
清华镜像后 404（`dist/2026-03-26/rust-std-1.94.1-x86_64-apple-darwin.tar.xz` 在那边不存在），
而 `static.rust-lang.org` 本身是通的。于是直接从官方源下 tar.xz，解到 toolchain 里：

```bash
curl -O https://static.rust-lang.org/dist/<日期>/rust-std-1.94.1-x86_64-apple-darwin.tar.xz
tar xf rust-std-1.94.1-x86_64-apple-darwin.tar.xz
cp -R rust-std-1.94.1-x86_64-apple-darwin/rust-std-x86_64-apple-darwin/lib/rustlib/x86_64-apple-darwin \
      ~/.rustup/toolchains/stable-aarch64-apple-darwin/lib/rustlib/
echo rust-std-x86_64-apple-darwin >> ~/.rustup/toolchains/stable-aarch64-apple-darwin/lib/rustlib/components
```

装完之后 `rustc --print target-libdir --target x86_64-apple-darwin` 能找到 std，`cargo build --target` 正常；
但 `rustup target list --installed` 不认（缺 `manifest-rust-std-*`），所以 `npx tauri build --target x86_64-apple-darwin`
会在它自己的前置检查那一步拦住。x64 的 App 是这么拼出来的：

```bash
cd desktop
node scripts/build-sidecar.mjs --arch x64 --skip-console
cd src-tauri && cargo build --release --target x86_64-apple-darwin
# 用 arm64 的 .app 当模板，替换两个二进制（MacOS/homing-desktop 与 Resources/binaries/homingd），重签
cp -R target/release/bundle/macos/Homing.app /tmp/Homing-x64/
cp target/x86_64-apple-darwin/release/homing-desktop /tmp/Homing-x64/Homing.app/Contents/MacOS/
cp binaries/homingd                                  /tmp/Homing-x64/Homing.app/Contents/Resources/binaries/
rm -rf /tmp/Homing-x64/Homing.app/Contents/_CodeSignature
codesign --force --deep --sign - /tmp/Homing-x64/Homing.app
bash scripts/make-dmg.sh /tmp/Homing-x64/Homing.app ../dist/Homing_0.7.0_x64.dmg
```

在有正常 rustup 源的机器上不需要这些，`rustup target add` + `npx tauri build --target` 即可。
注意 `build-sidecar.mjs` 两种架构都写同一个文件名 `binaries/homingd`，打完 x64 记得再跑一次不带 `--arch`
的把它换回本机架构，否则下次 arm64 打包会塞进 x64 的 sidecar。

---

## 3. 安装与首次运行

1. 打开 dmg，把 Homing 拖进「应用程序」。
2. 第一次启动：托盘出现灰鸽图标 → 窗口打开 `/console` → 因为还没走过首次设置，会直接进 `/console/setup`。
3. 三步向导：选 agent（可装钩子）→ 选连接方式（这一版只有「局域网直连」可选）→ 配对手机。
4. 关窗口只是收起来，托盘还在；「退出 Homing」才会同时停掉 sidecar。

**不会重复起 homingd**：启动时先探 `http://127.0.0.1:18787/health`，通了就直接复用。
所以这台 Mac 上原有的 `~/Library/LaunchAgents/dev.agentpush.relay.plist`（指向 `relay/server.mjs`）
还在跑时，桌面端只挂上去，不会再拉一个 sidecar 抢端口。控制台的「连接」页会明确写出检测到了这个 launchd 服务。

开机自启有两套，互不干扰：

| 场景 | 谁管 | 落点 |
|---|---|---|
| 桌面 App 自启 | `tauri-plugin-autostart` | macOS 登录项 / Windows 当前用户 Run 键 |
| 纯命令行 homingd | 控制台「连接」页的开关 | `~/Library/LaunchAgents/dev.homing.homingd.plist` / `HKCU\...\Run\Homing` |

---

## 4. 配对接口（手机端对接用）

这一版二维码里是「局域网地址 + 一次性配对令牌」，扫码后换发长期设备令牌。
端到端加密放在 M4，但令牌这条链路已经按「可替换」写好：以后加 E2E 只需要在 `claim` 的响应里多发一个公钥字段。

### 4.1 时序

```
电脑                                    手机
 │  POST /api/pair/offer（本机）            
 │  ← { code: "601770", token: "...", qr_payload: "homing://pair?..." }
 │                                         扫二维码 / 手输 6 位码
 │  ◀──────── POST /api/pair/claim {token|code, device:{...}} ────────
 │  ────────▶ { device_token: "hm_...", server:{lan_url,...} } ───────▶
 │                                         之后所有 /api/* 带 Bearer device_token
 │  ◀──────── POST /api/devices/heartbeat（可选，90 秒内有心跳算在线）
```

### 4.2 二维码内容

```
homing://pair?v=1&host=<urlencode("192.168.1.13:18787")>&token=<32 位十六进制>
```

手机端解析：`host` 前拼 `http://`，然后 `POST http://<host>/api/pair/claim`，body `{"token": "<token>", "device": {...}}`。
解析不了就退回手输 6 位码，走同一个接口。

### 4.3 接口清单

鉴权列的含义：**本机** = 只接受 127.0.0.1 且带控制台 cookie（或 `Authorization: Bearer <config.json 里的 token>`）；
**局域网** = 允许本机 + 局域网来源，免鉴权；**设备** = `Bearer hm_...`。

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/pair/offer` | 本机 | 生成一次性配对（5 分钟、6 位码）。会作废之前没用过的 offer |
| GET | `/api/pair/offer/<offer_id>` | 本机 | 查状态：`waiting` / `claimed` / `expired` |
| GET | `/api/pair/latest` | 本机 | 取当前还有效的那一个（没有则 `{offer:null}`） |
| POST | `/api/pair/claim` | 局域网 | **唯一免鉴权写接口**，用 `token` 或 `code` 换 `device_token` |
| POST | `/api/devices/heartbeat` | 设备 | 上报 `app_version`，返回暂停状态 |
| GET | `/api/devices` | 本机 | 已配对手机列表（在线状态、App 版本、最近在线） |
| PATCH | `/api/devices/<device_id>` | 本机 | 改名字 |
| DELETE | `/api/devices/<device_id>` | 本机 | 撤销，令牌立刻失效 |

`POST /api/pair/offer` 响应（`token` 只在这里出现一次）：

```jsonc
{
  "offer_id": "1ea878d61bc5da31",
  "code": "543259",
  "status": "waiting",
  "created_at": "2026-11-09T04:27:43.280Z",
  "expires_at": "2026-11-09T04:32:43.280Z",
  "ttl_s": 300,
  "lan_url": "http://192.168.1.13:18787",
  "lan_urls": ["http://192.168.1.13:18787"],
  "qr_payload": "homing://pair?v=1&host=192.168.1.13%3A18787&token=<token>",
  "token": "<32 位十六进制，一次性>",
  "claimed_device_id": null
}
```

`POST /api/pair/claim` 请求 / 响应：

```jsonc
// 请求：token 与 code 二选一
{ "token": "<配对令牌>", "device": { "name": "Pixel 8", "platform": "android", "app_version": "0.7.0" } }

// 200
{
  "ok": true,
  "device": {
    "device_id": "6f7fd39686d1", "name": "Pixel 8", "platform": "android",
    "app_version": "0.7.0", "created_at": "...", "last_seen_at": "...",
    "online": true, "paired_via": "code", "last_ip": "192.168.1.7"
  },
  "device_token": "hm_<43 位 base64url>",
  "server": {
    "name": "fanqiegedeMac-mini-541", "platform": "Mac",
    "lan_url": "http://192.168.1.13:18787", "lan_urls": ["..."],
    "version": "0.7.0", "token_type": "device"
  },
  "token_type": "device"
}
```

错误码：

| 状态 | 含义 | 手机端建议文案 |
|---|---|---|
| 400 | 既没 token 也没 code | 重新扫码 |
| 403 | 来源不是本机/局域网 | 确认在同一个 Wi-Fi |
| 404 | 令牌或码不存在 | 请在电脑上换一个配对码 |
| 409 | 这个码已经用过 | 同上 |
| 410 | 超过 5 分钟 | 请换一个 |
| 429 | 同一 offer 试太多次（12 次上限） | 重新开始配对 |

### 4.4 用 curl 走一遍（不需要手机）

```bash
BASE=http://127.0.0.1:18787
# 1) 拿一个配对 offer（本机可以用 config.json 里的 token 走 Bearer）
curl -s -X POST $BASE/api/pair/offer -H "Authorization: Bearer <本机 token>"
# → 记下 code 与 token

# 2) 模拟手机扫码 claim
curl -s -X POST $BASE/api/pair/claim \
  -H 'Content-Type: application/json' \
  -d '{"code":"543259","device":{"name":"curl 模拟机","platform":"android","app_version":"0.7.0"}}'
# → 拿到 device_token

# 3) 用设备令牌访问接口
curl -s $BASE/api/inbox -H "Authorization: Bearer hm_xxx"

# 4) 心跳（手机端前台时每分钟一次即可）
curl -s -X POST $BASE/api/devices/heartbeat \
  -H "Authorization: Bearer hm_xxx" -H 'Content-Type: application/json' \
  -d '{"app_version":"0.7.0"}'

# 5) 电脑侧看列表 / 撤销
curl -s $BASE/api/devices -H "Authorization: Bearer <本机 token>"
curl -s -X DELETE $BASE/api/devices/6f7fd39686d1 -H "Authorization: Bearer <本机 token>"
```

### 4.5 与旧 App 的关系

`relay/config.json` 里的那个 32 位 token 仍然有效，0.6.2 的 App 一行都不用改就能继续用。
设备令牌是新加的一条并行通道：只存在 `<数据目录>/devices.json` 里（存 sha256，不存明文），可以逐台撤销。

### 4.6 其他本机接口（控制台用）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/connection` | 版本、端口、局域网地址、数据目录、开机自启、安全状态 |
| GET | `/api/stats` | 今日推送数 / 合并数 / P0·P1·P2 分布 + 暂停状态 |
| GET | `/api/logs?limit=200` | homingd 日志尾部（token 与 `hm_` 令牌会打码） |
| GET/POST | `/api/setup` | 首次设置状态 / 标记完成 |
| GET/POST | `/api/autostart` | 读 / 设开机自启（仅命令行版需要） |
| GET | `/api/agent-paths` | 各 agent 的数据目录（含 WSL 里的） |
| GET/POST/DELETE | `/api/push/pause` | 暂停推送（分钟）/ 恢复 |
| POST | `/api/agents/<id>/hooks` | 装钩子（只自动改 Claude 的 settings.json，先备份） |
| GET/PUT | `/api/agents/settings`、`/api/agents/order` | 每个 agent 的接入设置（推不推手机、推送方式、回复方式、模型）+ 卡片顺序 |
| GET/PATCH | `/api/agents/<id>/settings` | 改单个 agent 的接入设置 |
| POST | `/api/agents/<id>/test-push` | 试发一条到测试会话（**占位**：等 relay 接上推送管线） |
| POST | `/api/agents/<id>/disconnect` | 断开 Homing 这边的接入（不动 agent 自己的配置文件） |
| GET/POST | `/api/adapters` | 接入清单列表 / 保存到 `~/.homing/agents/<id>.yaml`（**清单解析归 relay**） |
| POST | `/api/adapters/test` | 测试读取某个路径（只看形状，不落盘） |
| DELETE | `/api/adapters/<id>` | 删除清单文件 |

---

## 5. 控制台信息架构

整体参照 Magpie：左侧分组导航 + 顶部一条面包屑，中间一栏最宽 760px 居中，内容一律用细边框卡片列表，不做数字卡。

```
使用   收件箱（待处理数角标）· 会话 · Agent
手机   设备 · 通知
连接   中转与局域网
扩展   接入 ─ 内置 / 发现 / 已安装 / 自定义
──────────────────────────────
       设置 · 本机服务运行中 127.0.0.1:18787
```

| 页面 | 路由 | 要点 |
|---|---|---|
| 收件箱（首页） | `/console/` | 等你处理 / 运行中 / 刚完成三组；授权可直接「允许 / 拒绝」，出错的给「重试」，任意一条能原地展开回复框 |
| 会话 | `/console/sessions` | 搜索 + 最近活动排序，15 秒轮询 |
| Agent | `/console/agents` | 已接入的卡片可拖动排序（顺序存 `/api/agents/order`），展开后有读取会话授权、推送方式（通知钩子 / 监听文件 / 轮询）、回复方式 + 试发、模型；卡片底部写明改的是哪个配置文件，并给「断开并还原」。下面是「在这台电脑上检测到，未接入」的列表，再下面是社区适配器提示行和虚线框「添加自定义 agent」 |
| 设备 | `/console/devices` | 已配对手机 + 收起的配对卡（二维码、6 位码、倒计时、下载安卓版），展开时才起计时器和轮询 |
| 通知 | `/console/notifications` | 今日推送量 / 暂停开关 / 分级规则 / 最近推送 |
| 中转与局域网 | `/console/relay` | 三种连接方式、本机与局域网地址、安全标签、运行信息、命令行版开机自启 |
| 接入 | `/console/adapters[/discover|/installed|/custom]` | 内置五个 agent 的接入方式；社区适配器占位；已安装（内置 + 自定义清单）；自定义页是三张方式卡片 + 表单 + 右侧实时 YAML 预览 |
| 设置 | `/console/settings` | 开机自启、重新走首次设置、运行日志入口、数据目录、版本与运行环境 |
| 运行日志 / 关于 | `/console/logs`、`/console/about` | 从「设置」进，不占导航位 |

两个约定：

1. **接入状态以配置文件为准**。`/api/agents/settings` 里没被用户动过的 agent，`connected` 由「配置文件里有没有 `127.0.0.1:<port>/hook/`」推出来，否则升级上来的老用户会看到所有 agent 都「未接入」。
2. **接入页的写操作分两半**。清单文件的解析、`/api/ingest`、脚本适配器加载归 relay（DSH）；控制台这边只做界面 + 三个接口的形状，`/api/agents/<id>/test-push` 目前返回 `placeholder: true`。

---

## 6. 平台层 `relay/platform/`

| 文件 | 内容 |
|---|---|
| `index.mjs` | 数据目录、端口、局域网地址、原子读写、运行时信息、旧数据迁移 |
| `homes.mjs` | 各 agent 的家目录；Windows 上额外扫 `\\wsl$\<发行版>\home\<用户>` |
| `autostart.mjs` | 命令行版开机自启（LaunchAgent / HKCU Run），并检测旧的 `dev.agentpush.relay` |
| `pause.mjs` | 暂停推送状态（`notify.mjs` 出通知前问一句） |
| `devices.mjs` | 配对 offer 与设备令牌 |
| `agent-settings.mjs` | 每个 agent 的接入设置（`data/agents/<id>.json`），只管控制台的态度，不碰 agent 自己的配置 |
| `adapters.mjs` | 自定义接入清单：极简 YAML 读写、校验、测试读取（`~/.homing/agents/*.yaml`） |

数据目录：

| 平台 | 路径 |
|---|---|
| macOS | `~/Library/Application Support/Homing/` |
| Windows | `%APPDATA%\Homing\` |
| Linux | `$XDG_DATA_HOME/homing` 或 `~/.local/share/homing` |

里面的文件：`config.json`（本机 token，0600）、`devices.json`、`session-state.json`、`events.jsonl`、
`replies.jsonl`、`setup.json`、`push-pause.json`、`notify-stats.json`、`app.json`（运行信息）。

**迁移**：启动时如果新目录里没有对应文件，会从旧的 `relay/data/` 复制一份（只复制、不删除），
所以升级后原来的 token 与配对关系不会失效。

agent 数据路径（界面上以等宽字显示）：

| agent | 路径 |
|---|---|
| Codex | `~/.codex` |
| Claude Code | `~/.claude` |
| WorkBuddy | `~/.workbuddy` |
| OpenCode | `~/.local/share/opencode` |
| ZCode | `~/.zcode` |

Windows 上 `~` 是 `%USERPROFILE%`；WSL 里的同名目录也会被探测到，并在 `/api/agent-paths` 的 `wsl` 字段里返回。

---

## 7. 命令行版 homingd

单文件本身就是命令行版，桌面壳不带界面也能跑：

```bash
homingd                     # 带 /console 网页界面
homingd --no-console        # 只留接口，适合服务器
homingd --port 18800 --data-dir /var/lib/homing
homingd --version
homingd --help
```

`--no-console` 时 `/console` 返回 404 并说明原因，`/api/*`、钩子、配对全部照常。

---

## 8. 本机验证记录

产物都在 `dist/`：

| 文件 | 说明 |
| --- | --- |
| `Homing_0.7.0_aarch64.dmg` | Apple 芯片安装包（41 MB），本机已装到 `/Applications` 跑通 |
| `Homing_0.7.0_x64.dmg` | Intel Mac 安装包（44 MB），Rosetta 下验证过 |
| `homingd-win-x64.exe` | Windows 单文件命令行版（83.5 MB），也是 Windows 安装包的 sidecar；安装包本体要在 Windows 上用 PS1 脚本构建 |

环境：Mac mini（Apple 芯片）、macOS 26、屏幕 1920×1080、Node 22.22.2。

| 项 | 怎么验的 | 结果 |
| --- | --- | --- |
| 控制台改版（Magpie 式布局）12 个页面 | 无头 Chrome（CDP）1280×900 逐页截图核对：收件箱 / 会话 / Agent（含展开详情）/ 设备（含展开配对）/ 通知 / 中转 / 接入四页（含保存并接入）/ 设置 / 运行日志 / 关于 | 全部正常；改掉了日志页标题被路径挤断、自定义清单 id 出现中文（改 ASCII + 稳定哈希）、清单不填回复命令时被 400 拦下（改成只读 `mode: none`） |
| 本机接口 | `PORT=18799 HOMING_DATA_DIR=/tmp/homing-test` 起测试实例，curl 全跑一遍 | `/console/` 200 且带 `Set-Cookie: homing_local`；offer → claim → 设备令牌访问 `/api/inbox` → heartbeat → devices → stats → agent-paths → logs 全通 |
| homingd 单文件（arm64 / x64） | 直接跑 SEA 产物 | `/health` ok、`/console/` 200、`/console/favicon.svg` 200（assets 内嵌生效）；真实数据目录下 2 秒就绪 |
| dmg 安装 | `hdiutil attach` → 拷到 `/Applications` → 启动 | 日志四行齐全；托盘图标出现在菜单栏（约 13×14 px 的灰鸽剪影），退出后消失；窗口 1180×820、标题 Homing、加载 `/console` |
| 复用已有 homingd | 18787 上已有服务时启动 | 日志「18787 上已经有 homingd 在跑，直接复用」，没有再起第二个 |
| 冷启动拉起 sidecar | 18787 空着时启动 | 日志「已拉起 sidecar: …/Contents/Resources/binaries/homingd」，服务随后可访问。**注**：这台机器上有 LaunchAgent 常驻 `relay/server.mjs` 占着 18787，会和新起的 sidecar 抢端口，所以端到端那条走的是「单文件独立起服务 + App 拉起日志」两段验证 |
| 首次设置 | 无头 Chrome 驱动走完三步 | `setup.completed=true`、`chosen_agents=[codex,claude,workbuddy,opencode]`、`method=lan`、`devices=1`；完成后自动进收件箱 |
| 手机配对（curl 模拟） | 从局域网地址 `POST /api/pair/claim` | 返回 `device_token`（`hm_` 前缀、46 字符）与设备记录（`paired_via=qr`）；页面 2 秒内从「等待手机扫码…」翻成「手机已配对 · 搞定了，可以直接完成设置」，按钮变「完成」。测试设备验完已删掉 |
| 收件箱真实数据 | 装好的 App 里看 | 等你处理 7、运行中 0、刚完成 17；Agent 页 5 个 agent（codex / claude / workbuddy / opencode / zcode），其中 WorkBuddy 按配置文件里的钩子判成「已接入」 |
| x64 版 | Rosetta 下直接跑 `/tmp/Homing-x64/Homing.app/Contents/MacOS/homing-desktop` | 日志四行正常、复用已有服务；x64 sidecar 独立起服务 `/health`、`/console/` 均 200 |
| 自定义接入清单 | 控制台里填「演练接入」→ 测试读取 → 保存并接入 → 删除 | 读到 10 个文件 182.2 KB；落盘 `~/.homing/agents/adapter-ejew27.yaml`（`reply.mode: none`）；已安装页出现「自定义」行；`DELETE /api/adapters/adapter-ejew27` 删掉文件。测试清单验完已清理 |

没有验证 / 没有做的：

- **托盘菜单没逐项点过**：点菜单项要走辅助功能授权，没开。验证到的是图标出现与消失、菜单构建不 panic、8 秒轮询在刷。
- **没有点过「安装钩子」**：这台机器上是真实在用的 agent，`~/.claude/settings.json`、`~/.codex/hooks.json` 一律没动，保持你原来的钩子和信任状态。
- **Windows 侧没有实机**：见下一节第 1 条。

---

## 9. 已知限制 / 这一版没做

1. **Windows 安装包还没产出**：NSIS 安装器只能在 Windows 上构建，`desktop/scripts/build-windows.ps1` 已经写好，
   `dist/` 里先放了交叉编译出来的 `homingd-win-x64.exe`（就是 sidecar 同款单文件，Windows 上可以直接双击跑命令行版）。
   Windows 侧的托盘、自启、WSL 探测都只做了代码路径，未实机验证。
2. **端到端加密是 M4**：这一版是局域网明文 + 设备令牌。局域网内抓包能看到会话内容，别在公共 Wi-Fi 下用。
3. **签名/公证没做**：dmg 只做了 ad-hoc 签名，第一次打开要右键「打开」。自动更新（`检查更新`）也只弹提示，没有接更新通道。
4. **Codex 的钩子不代装**：`~/.codex/hooks.json` 牵涉它自己的信任状态，控制台只给出片段和手工步骤。
5. **WSL 里只探测不读取**：Windows 上能发现 WSL 里的 agent 目录（`/api/agent-paths` 的 `wsl` 字段），
   但扫描器仍然读 `%USERPROFILE%` 那一份；要真正读 WSL 的会话文件，需要把 scanner 的家目录参数化，
   那是 relay 的改动，不在这一版范围内。
6. **托盘菜单的「等宽字」做不到**：原生菜单项只有一种系统字体，`agent · 项目` 只能靠文案对齐，没法真用 JetBrains Mono。
7. **单实例没做**：重复启动会开出第二个窗口（但不会起第二个 homingd，端口探测会拦住）。
8. **App 被强杀会留下孤儿 homingd**：正常退出（托盘「退出 Homing」）会带走自己拉起的 sidecar，
   但 `kill -9` 之类不会，那个 homingd 会一直占着 18787。下次启动会直接复用，不影响使用，只是它不会自己退出。
9. **开机自启只写文件不立刻生效**：Mac 上写的是 `~/Library/LaunchAgents/dev.homing.homingd.plist`，
   不去调 `launchctl`（避免在用户没预期的时候改动启动项），所以要到下次登录才生效；Windows 同理写 `HKCU\...\Run`。
10. **端口写死 18787**：桌面壳里 `PORT` 是常量，改端口要重新编译。命令行版可以用 `--port`。
