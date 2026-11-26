# Homing

简体中文 | [English](README.en.md)

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-0D9488.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](go.mod)
[![Android](https://img.shields.io/badge/Android-8.0%2B%20(Compose)-3DDC84.svg)](android/)
[![E2E Encrypted](https://img.shields.io/badge/E2E-X25519%20%2B%20XChaCha20--Poly1305-6366F1.svg)](docs/protocol.md)
[![Agents](https://img.shields.io/badge/Agents-27%2B%20Supported-F59E0B.svg)](docs/adapters.md)
[![QQ 交流群](https://img.shields.io/badge/QQ交流群-512365124-12B7F5.svg)](#社区交流与问题反馈)

**Every coding agent, one pocket workspace — zero VPN on your phone, zero plaintext on the relay.**

**Homing** 是一个开源的跨平台 **多 AI 编程 Agent 离席接管与协同中枢**。当你在电脑或远程开发机上同时运行 **Claude Code、Codex、WorkBuddy、Cursor、Gemini CLI、OpenCode** 等多个编程 Agent 时，Homing 让你在离开工位后，仅凭一部手机（**国内 5G / Wi-Fi 直连，手机无需安装或开启任何代理**）即可在一个原生 App 内实时接收通知、审批工具调用、回复连续对话、远程唤醒电脑上未打开的 Agent，甚至拉起多 Agent 协同会议室。

> 当前版本处于公开 **Beta** 阶段。安装包差异、签名说明与验收边界详见 [Beta 指南](docs/beta-release.md)。

---

## 为什么已有官方移动端，依然需要 Homing？

大多数主流编程 Agent 都推出了各自的云端或移动端入口，但在真实的开发工作流（尤其是**多 Agent 并行**与**跨境代理网络环境**）中，依然存在明显的断层：

| 真实离席痛点 | 各家官方移动端方案 | 传统 SSH / Termux 终端方案 | **Homing 开源方案** |
| :--- | :--- | :--- | :--- |
| **同时使用多个 Agent** | 需在 Claude、Codex、Cursor 等多个 App 间反复切换 | 需开多个 tmux 窗口逐个翻日志 | **单 App 左侧抽屉统一接管 27+ Agent**，待审批会话跨 Agent 置顶秒审 |
| **中国大陆网络 × 代理开发机** | 海外 Agent 移动端要求**手机也常开代理/VPN**，耗电且影响国内应用与弱网切换 | 公网 SSH 暴露端口风险高，移动网络切基站易断连 | **独创「双栈分流」架构**：开发机 Agent 照常挂代理调海外模型；Homing 绕过代理直连**中国大陆公网盲中继**，**手机纯国内 5G 零代理秒连** |
| **远程打开未启动的 Agent** | 桌面端没开就彻底失联 | 需手敲命令行拉起图形应用或守护进程 | **手机一键远程唤醒**（`open -g -a` 后台无感拉起 Mac 桌面端或激活 CLI） |
| **代码与对话隐私安全** | 会话托管在各厂商云端 | 依赖通道自身配置 | **端到端加密零知识盲中继**（`X25519 + XChaCha20-Poly1305`），1C1G 轻量云自建，中继零明文 |
| **手机端会话噪音治理** | 历史列表无限堆积，真假活跃难分 | 纯文本滚屏，无结构化交互 | **未关闭后台保活 · 关闭重开自动净空**（保留后台运行任务），支持一键清理与**挑会话恢复最近 10 条记录** |

---

## 六大核心亮点

### 1. 中国大陆公网盲中继 × 代理网络设备「双栈分流」架构（手机零代理直连）

针对中国大陆开发者最常见的**「开发机挂代理（Clash / Mihomo / Surge / Sing-box / v2ray）访问海外大模型，但希望手机出门只用国内普通 5G 网络」**场景，Homing 从网络栈底层设计了**控制面与模型面彻底解耦的双栈分流机制**：

```text
                        【模型流量面 · 保持原样走代理】
                   ┌───────────────────────────────────────────┐
                   │ Mac / Linux / 远程开发机（挂有代理 / TUN） │
                   │   ├─ Claude Code / Codex / Gemini CLI ────┼──► 本机代理 (7890 / TUN) ──► 海外大模型 API
                   │   ├─ WorkBuddy / OpenCode / DSH / Qwen ───┼──► 国内或自定义模型 API
                   │   │                                       │
                   │   └─ Homing 守护进程 (Direct-Dial 直连引擎)│
                   └─────────────────────┬─────────────────────┘
                                         │ ① 自动隔离 HTTP_PROXY / HTTPS_PROXY 环境变量
                                         │ ② 自动检测并穿透 198.18.0.0/15 (TUN Fake-IP) 解析真实国内 IP
                                         │ ③ 出站 WSS 端到端加密长连接（开发机零公网入站端口）
                                         ▼
                           ┌───────────────────────────┐
                           │  中国大陆公网轻量云服务器  │  ◄── 1C1G 即可自建 homing-server
                           │   （零知识加密盲中继）     │      仅转发密文信封，不跑模型、不落明文
                           └─────────────┬─────────────┘
                                         │ 国内骨干网低延迟 WSS (~20–40ms)
                                         ▼
                           ┌───────────────────────────┐
                           │     Android 手机客户端     │  ◄── 纯净国内 5G / Wi-Fi 直连
                           │  （零 VPN · 零代理配置）   │      电梯/高铁切网毫秒级自动重连补同步
                           └───────────────────────────┘
```

- **电脑端代理零干扰**：本机 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量会被连接器自动隔离（`direct_dial: true`），绝不让国内中继流量绕道海外代理节点产生「U 型回程高延迟」。
- **TUN Fake-IP 透明代理自动穿透**：即使开发机开启了 Clash / Surge / Sing-box 的 **TUN 增强模式（`198.18.0.0/15` 或 `fc00::/18` Fake-IP）**，Homing 也会自动通过国内公共 DNS 解析真实公网 IP 直连并保留完整 TLS SNI 证书校验；控制台同时提供**一键复制 Clash / Mihomo / Surge 直连分流规则**。
- **双向兼容海外中继**：若你的自建中继部署在海外（如 Fly.io / 海外 VPS）且开发机需要借道本机代理才能连上，可在控制台一键切换为「经本机系统代理连接中继」。
- 完整部署与分流指南见：**[《中国大陆公网中继与代理网络设备部署指南》](docs/china-relay-and-proxy.md)**。

### 2. 对话优先（Conversation-First）左侧抽屉 ＋ 27+ Agent 远程唤醒

- **打开即对话，右滑切全局**：参考 Claude / Codex 移动端的极简心智，启动直接进入当前活跃对话（带底部防边缘返回手势冲突的 24dp 安全区与代码块横向滚动礼让）。
- **只看本机真正打开的活跃 Agent**：左侧抽屉胶囊列实时映射电脑端活跃进程状态（3 秒低开销快照探针），未打开的 Agent 自动收起不添乱；需要时点击 **`⚡ 唤醒 Agent`** 即可远程在电脑后台无干扰启动桌面端应用（如 `WorkBuddy`、`Cursor`、`Trae`、`Windsurf`）或激活 CLI 在线。
- **跨 Agent 紧急审批置顶**：当一个会话正在跑长测试、另一个 Agent 突然请求危险命令授权时，抽屉顶部置顶 **`⚡ 待你处理`** 卡片（支持免进会话一键允许/拒绝），会话顶栏同步浮现 **`待审 ›` 瞬移胶囊**，处理完一键返回原会话。

### 3. 端到端加密「零知识盲中继」（Zero-Knowledge Blind Relay）

- **密码学身份与信封**：每台电脑与手机在本地生成独立的 `Ed25519` 签名密钥与 `X25519` 协商密钥，所有 RPC 请求、流式事件、通知摘要乃至图片附件均使用 **`XChaCha20-Poly1305`** 在端侧完成加密。
- **4 词 SAS 防中间人核验**：扫码或输入 6 位配对码时，双端基于公钥指纹生成一致的 4 个验证词，杜绝中继伪造身份。
- **1 核 1G 即可承载**：`homing-server` 仅负责转发密文信封与离线队列缓存（默认上限 500 条 / 7 天），不解密任何代码、Prompt 或附件，极低内存与带宽占用即可在任意入门级云主机长期稳定运行。

### 4. 专为移动离席打磨的「保活 · 净空 · 0-RTT 秒开」体验

- **Mac 离席守护中心**：桌面端精简为三张核心状态卡（本机 Agent 探针、中继链路与 `RTT`、配对手机与 **防止 Mac 休眠 `caffeinate` 守护开关**），合上窗口收起至菜单栏托盘即可安心离开电脑。
- **未关闭时后台保活，关闭重开默认清空**：锁屏或切去微信等应用时，Android 前景守护服务保持加密长连接实时收信；一旦在系统多任务中**划掉关闭 Homing，下次打开默认恢复清爽空视图（电脑后台仍在运行或待审批的任务自动保留）**。
- **双入口一键快速清理 ＋ 挑会话恢复最近 10 条记录**：抽屉工具栏与会话右上角均配备一键清理（带 5 秒撤销，绝不误删电脑原始记录）；需要回看旧工作时，随时从「恢复历史」勾选必要会话，精准拉取**最新 10 条对话记录**。
- **0-RTT 通知正文携带与单次 Bootstrap 同步**：通知密文内直接内联最新回复正文片段，蜂窝高延迟网络下点开通知 **0 毫秒直出内容**；启动与断线重连将 9 个接口合并为单次 `GET /api/mobile/bootstrap` 加密调用。

### 5. 跨 Agent 协同调度（`@agent` 子任务派发 ＋ 多 Agent 会议室）

- **在任意对话里 `@agent` 派活**：例如在 Claude 会话中直接输入 `@codex 审查刚才的并发锁实现`，Homing 自动规划上下文并在独立 Git Worktree 中拉起子任务，完成后一键把结论交回主会话。
- **多 Agent 圆桌会议室（Rooms）**：拉起多个不同特长的 Agent 围绕同一议题讨论，支持主持人点名发言、暂停/恢复、自动汇总会议纪要，并将经你确认的行动项直接派发为可执行任务。

### 6. 单一 Go 二进制全栈架构（macOS / Windows / Linux / 远程开发机 / Docker）

- **零外部运行时依赖**：电脑端守护进程、Wails 原生菜单栏/系统托盘应用（支持 **macOS `.app` 与 Windows 10/11 原生托盘端 `Homing.exe`**）与内嵌零框架 TypeScript 控制台编译为**单个轻量化 Go 可执行文件**；服务器端 `homing-server` 同样为纯静态编译单文件（`CGO_ENABLED=0`）。
- **天然适配 Windows + WSL2 与远程 Linux 开发机**：在 Windows 上不仅原生探测桌面 Agent（Cursor、WorkBuddy、Trae、Windsurf 等）与防休眠，还自动穿透 `\\wsl$\` 读取 WSL2 内的 Claude Code / Codex 会话；在 Linux 工作站或 Docker 容器中运行 `homing serve` 连上公网中继即可手机直连管控。

---

## 支持的 Agent 生态（27+）

| 接入层级 | 代表 Agent | 支持能力概览 |
| :--- | :--- | :--- |
| **内置核心深度集成** | **Claude Code**、**Codex**、**WorkBuddy**、**Kimi CLI**、**OpenCode**、**ZCode** | 连续会话读取、实时消息/权限钩子（Hooks）、移动端回复注入、模型与推理强度切换、远程唤醒、新建/停止会话 |
| **ACP 驱动与内置清单生态** | **Cursor**、**Windsurf**、**Trae**、**Antigravity**、**Gemini CLI**、**Qwen Code**、**GitHub Copilot CLI**、**DSH**、**CodeBuddy**、**Zed**、**Kiro**、**Cline**、**Roo Code**、**Goose**、**OpenHands**、**OpenClaw**、**Aider**、**Amp**、**Kilo Code**、**Junie**、**Warp**、**Droid** 等 | 自动安装探测、官方矢量品牌图标、ACP 会话驱动与跨 Agent 调度、自定义 YAML 清单（`~/.homing/agents/*.yaml`）与脚本适配器扩展 |

> 各 Agent 的具体可用能力取决于本机安装版本、登录态与钩子配置，可在桌面控制台「Agent 管理」页面实时查看与一键配置。

---

## 5 分钟快速开始

### 第一步：在开发机（macOS / Windows / Linux）启动 Homing

准备 **Go 1.26+** 与至少一个已登录的本地编程 Agent（仓库已内嵌构建好的 Web 控制台静态产物，直接 `go build` 即可）：

```sh
git clone https://github.com/tomato-hug/homing.git
cd homing

# macOS 桌面模式（带菜单栏托盘与管理窗口）或直接运行后台服务：
go build -o homing ./cmd/homing
./homing
```

若在 **Windows 10 / 11** 上一键安装或构建桌面管理端（带右下角系统托盘、PerMonitorV2 高分屏主窗口、防休眠与 WSL2 会话穿透）：

```powershell
# 方式 A：PowerShell 一键下载校验安装（创建桌面/开始菜单快捷方式并加入 PATH）
irm https://raw.githubusercontent.com/tomato-hug/homing/main/scripts/install.ps1 | iex

# 方式 B：本地源码打包 Windows 桌面版（生成 Homing.exe 无黑框托盘端 + homing-cli.exe）
python scripts/package-windows.py --version 0.11.6 --arch amd64 --commit $(git rev-parse HEAD) --output dist/win
```

若在 **Linux 远程开发机 / WSL2 / 无图形界面服务器** 上编译：

```sh
CGO_ENABLED=0 go build -tags headless -trimpath -ldflags="-s -w" -o homing ./cmd/homing
./homing serve --addr 127.0.0.1:18787
```

浏览器打开本机 `http://127.0.0.1:18787/console`，即可进入 **Mac / 开发机离席守护中心**。

### 第二步：（可选但强烈推荐）部署公网盲中继 `homing-server`

同一 Wi-Fi 下手机可直接局域网配对（将启动参数改为 `--addr 0.0.0.0:18787`）。若要在离开工位后通过移动蜂窝网络使用，可在任意一台 **1C1G 中国大陆轻量云服务器**（需完成域名备案并配置 HTTPS/WSS 反向代理）或海外云主机上部署 `homing-server`：

```sh
# 方式 A：Docker 一键启动（监听本机 127.0.0.1:8080，前接 Caddy / Nginx TLS）
docker build -f server/Dockerfile -t homing-server .
docker run -d --name homing-server --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v homing-data:/data homing-server

# 方式 B：交叉编译纯静态 Linux 二进制 + systemd 原子部署脚本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o homing-server ./cmd/homing-server
# 上传至服务器后配合 deploy/homing-relay.service 与 scripts/install-relay.sh 安装
```

随后在 Caddy（自动签发证书）或 Nginx 中转发 WebSocket：

```caddyfile
homing.example.cn {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

在开发机控制台 **「连接 › 中转与局域网」** 填入 `wss://homing.example.cn/v1/ws` 保存即可秒连（若开发机挂有 Clash/Surge 代理，保持默认的 **「直连模式 · 绕过本机代理」** 即可）。

### 第三步：构建或安装 Android 客户端并扫码配对

```sh
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

1. 在电脑离席守护中心点击 **「扫码配对新手机」**（或在终端执行 `./homing pair --serve http://127.0.0.1:18787`）。
2. 打开手机端 **Homing** 扫描二维码（或输入 6 位配对码），核对双端显示的 **4 个验证词** 一致后在电脑端确认。
3. 关闭手机 Wi-Fi 切换至 5G 移动网络（无需开任何 VPN），即可开始离席管控！

---

## 仓库结构与文档导航

```text
cmd/homing/          开发机守护进程、CLI 与 macOS 原生菜单栏入口 (Go + Wails)
cmd/homing-server/   轻量端到端加密公网盲中继服务端 (纯静态 Go + SQLite)
internal/
  ├─ protocol/       Ed25519 / X25519 / XChaCha20-Poly1305 协议实现与测试向量对齐
  ├─ cloud/          公网中继连接器、双栈代理隔离、TUN Fake-IP 穿透、并发加密 RPC 与离线补发
  ├─ agent/          内置 Agent 扫描器、钩子审批与回复注入实现
  ├─ adapter/        27+ Agent 目录清单引擎与自定义脚本适配器
  ├─ driver/         ACP 协议驱动与会话生命周期控制
  ├─ orchestrator/   @agent 跨 Agent 子任务调度与 Git Worktree 隔离
  ├─ platform/       跨平台路径、开机自启与 macOS caffeinate 防休眠守护
  └─ api/ relayapi/  本地 REST/SSE 接口、移动端 1-RTT Bootstrap 与远程唤醒接口
console/             桌面离席守护中心与全功能 Web 控制台 (零框架 TypeScript + Vite，中英双语)
android/             Android 原生移动端 (Kotlin + Jetpack Compose，对话优先左侧抽屉架构)
deploy/ scripts/     systemd 服务单元、Nginx 配置片段、macOS 打包与公网中继安装脚本
```

### 延伸文档

- **[中国大陆公网中继与代理网络设备部署指南（双栈分流详解）](docs/china-relay-and-proxy.md)**
- **[自托管中继服务、TLS 反向代理、FCM 唤醒与冒烟验收](docs/self-host.md)**
- **[macOS 原生菜单栏应用构建与常驻服务说明](docs/macos-native.md)**
- **[端到端加密协议规范 (Protocol v1)](docs/protocol.md)** · **[Go 架构说明](docs/go-architecture.md)** · **[自定义 Agent 适配器规范](docs/adapters.md)**
- **[Beta 安装升级与已知限制](docs/beta-release.md)** · **[致谢与赞助者鸣谢榜](SPONSORS.md)** · **[贡献指南](CONTRIBUTING.md)** · **[安全边界与漏洞报告](SECURITY.md)** · **[更新日志](CHANGELOG.md)**

---

## 💬 社区交流与问题反馈

- **问题反馈与开发交流 QQ 群**：**`512365124`**（安装部署答疑、中国大陆公网中继与代理分流配置、新 Agent 适配需求及实测 Bug 反馈）
- **GitHub Issues**：[提交可复现 Bug 与功能建议](https://github.com/tomato-hug/homing/issues)

---

## ☕ 支持与打赏（Support & Sponsor）

Homing 坚持 **Apache-2.0 开源、无广告、端到端零知识加密**。自建中继默认免注册开箱即用；同时，为了方便不想购置服务器的开发者，官方公共中继内置了轻量账号配额与 `/admin` 管控台。

如果 Homing 帮你在离开电脑时省下了切 App 与配网络的麻烦，欢迎通过**微信赞赏码**或**支付宝收款码**请作者喝杯咖啡——所有赞赏将直接用于分担官方公网盲中继服务器的带宽成本，支持者可列入 [SPONSORS.md](SPONSORS.md) 鸣谢榜（支持匿名）并获取官方中继 **Sponsor 提额兑换码**。

<details>
<summary><b>👉 点击展开：微信赞赏码 / 支付宝收款码</b></summary>
<br>

| 微信赞赏码（微信扫一扫） | 支付宝收款码（支付宝扫一扫） |
| :---: | :---: |
| <img src="docs/assets/sponsor/wechat-reward.png" width="220" alt="微信赞赏码"> | <img src="docs/assets/sponsor/alipay-reward.png" width="220" alt="支付宝收款码"> |

</details>

---

## 🙏 致谢（Acknowledgments）

- **架构灵感**：感谢 [Magpie](https://github.com/yetone/magpie) 在单文件 Go + Wails 跨平台桌面形态上的先行探索。
- **Agent 与图标生态**：感谢 Claude Code、OpenAI Codex、WorkBuddy、OpenCode、Gemini CLI、Kimi CLI 等开放 CLI / Hook / ACP 生态，以及 [@lobehub/icons](https://github.com/lobehub/lobe-icons) 开源品牌图标库。
- **核心开源组件**：感谢 Go、`Wails`、`modernc.org/sqlite`、`coder/websocket`、`golang.org/x/crypto`、`Jetpack Compose`、`Lazysodium`、`ZXing` 以及 `JetBrains Mono`（SIL OFL 1.1）。完整致谢与赞助鸣谢榜见 [SPONSORS.md](SPONSORS.md)。

---

## 开源许可

Homing 基于 [Apache License 2.0](LICENSE) 开源发布。第三方字体与开源组件保留各自原始许可证，详见 [NOTICE](NOTICE)。
