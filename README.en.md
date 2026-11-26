# Homing

[简体中文](README.md) | English

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-0D9488.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](go.mod)
[![Android](https://img.shields.io/badge/Android-8.0%2B%20(Compose)-3DDC84.svg)](android/)
[![E2E Encrypted](https://img.shields.io/badge/E2E-X25519%20%2B%20XChaCha20--Poly1305-6366F1.svg)](docs/protocol.md)
[![Agents](https://img.shields.io/badge/Agents-27%2B%20Supported-F59E0B.svg)](docs/adapters.md)
[![QQ Group](https://img.shields.io/badge/QQ_Group-512365124-12B7F5.svg)](#community--feedback)

**Every coding agent, one pocket workspace — zero VPN on your phone, zero plaintext on the relay.**

**Homing** is an open-source, self-hostable **multi-agent away-from-desk command center**. When you run **Claude Code, Codex, WorkBuddy, Cursor, Gemini CLI, OpenCode**, and other AI coding agents across your Mac, Linux workstation, or remote devbox, Homing lets you step away from your computer and seamlessly read live transcripts, approve or deny tool calls, send replies, remotely wake closed desktop agents, and run multi-agent meeting rooms from a single Android app—protected end-to-end by `X25519 + XChaCha20-Poly1305` encryption.

> Homing is currently in public **Beta**. See the [Beta Guide](docs/beta-release.md) for package types, signing channels, and verified scope.

---

## Why Homing When Official Mobile Apps Already Exist?

Individual coding agents increasingly ship mobile companions, yet real-world workflows break down as soon as you combine **multiple agents** or **split-horizon / proxy-restricted networks**:

| Away-From-Desk Pain Point | Separate Vendor Mobile Apps | Raw SSH / Termux | **Homing (Open Source)** |
| :--- | :--- | :--- | :--- |
| **Running 3+ Agents at Once** | Constant app-switching between Claude, Codex, Cursor, etc. | Juggling multiple `tmux` panes on a tiny keyboard | **One Conversation-First Left Drawer** unifying 27+ agents with pinned cross-agent approvals |
| **Proxy Workstations × Regional Networks (e.g. Mainland China)** | Requires **24/7 VPN/proxy on your phone** as well—draining battery and hurting local apps | Exposing SSH ports or suffering frequent cellular drops | **Split-Horizon Dual-Plane Routing**: workstation agents keep their proxy for overseas LLMs; Homing bypasses `HTTP_PROXY` & TUN Fake-IP to dial a **domestic blind relay** directly—**zero VPN on your phone** |
| **Closed Desktop Agents** | Unreachable if you forgot to launch the app before leaving | Manual CLI launch commands | **1-Tap Remote Wake** (`open -g -a` background macOS launch, service start, or CLI activation) |
| **Privacy & Data Sovereignty** | Prompts & code hosted on third-party clouds | Manual tunnel management | **Zero-Knowledge Blind Relay** (`X25519 + XChaCha20-Poly1305`), self-hosted on any 1-vCPU VM with zero plaintext |
| **Mobile Session Clutter** | Endless historical logs mixed with live tasks | Raw scrolling terminal buffers | **Background Keep-Alive while open · Auto-Clean on close** (retaining running/urgent tasks) + **Restore latest 10 items** on demand |

---

## 6 Core Highlights

### 1. Split-Horizon Dual-Plane Architecture (Manage Proxy-Equipped Workstations via a Domestic Relay)

Developers in Mainland China (and corporate split-tunnel environments) frequently run local proxies (`Clash`, `Mihomo`, `Surge`, `Sing-box`, `HTTP_PROXY`) on their workstations so overseas coding agents can reach Anthropic, OpenAI, and Google APIs. Conventional remote tools inherit those proxy settings—causing U-turn hairpin routing overseas or forcing the mobile phone to run a VPN as well.

Homing decouples the **Model Plane** from the **Control & Relay Plane**:

```text
                        [Model Plane · Keeps Workstation Proxy]
                   ┌──────────────────────────────────────────────┐
                   │ Mac / Linux / Remote Devbox (Behind Proxy)   │
                   │   ├─ Claude Code / Codex / Gemini CLI ───────┼──► Local Proxy / TUN ──► Overseas LLM APIs
                   │   ├─ WorkBuddy / OpenCode / DSH / Qwen ──────┼──► Domestic / Custom LLM APIs
                   │   │                                          │
                   │   └─ Homing Daemon (Direct-Dial Engine)      │
                   └──────────────────────┬───────────────────────┘
                                          │ 1. Isolates HTTP_PROXY / HTTPS_PROXY env vars
                                          │ 2. Detects & bypasses 198.18.0.0/15 TUN Fake-IP via direct DNS (preserving TLS SNI)
                                          │ 3. Outbound-only E2E encrypted WSS (zero inbound ports on workstation)
                                          ▼
                           ┌──────────────────────────────┐
                           │ Mainland China / Regional VM │  ◄── Self-host homing-server on a 1C1G VM
                           │  (Zero-Knowledge Blind Relay)│      Forwards ciphertext envelopes only
                           └──────────────┬───────────────┘
                                          │ Low-latency domestic WSS (~20–40 ms)
                                          ▼
                           ┌──────────────────────────────┐
                           │     Android Mobile Client    │  ◄── Native 5G / Wi-Fi direct connection
                           │   (Zero VPN · Zero Proxy)    │      Instant cellular handoff & resync
                           └──────────────────────────────┘
```

- **Zero Interference with Agent Proxies**: `homing` isolates `HTTP_PROXY` / `HTTPS_PROXY` by default (`direct_dial: true`) so your coding agents keep full overseas connectivity while relay traffic stays on low-latency domestic links.
- **Automatic TUN Fake-IP Penetration**: Even when Clash / Surge / Sing-box runs in **TUN + Fake-IP (`198.18.0.0/15` / `fc00::/18`)** mode, Homing detects Fake-IP responses and queries public DNS directly to dial the real relay IP while preserving full TLS SNI verification—plus 1-click Clash / Mihomo / Surge `DIRECT` rule export in the Console.
- **Reversible for Overseas Relays**: Hosting `homing-server` overseas instead? Flip **Route via System Proxy** (`use_system_proxy: true`) in one click in the Console.
- Deep-dive guide: **[Mainland China Public Relay & Proxy Workstation Guide](docs/china-relay-and-proxy.md)**.

### 2. Conversation-First Mobile Workspace & 1-Tap Remote Agent Wake

- **Boot Straight Into Your Active Thread**: Inspired by Claude and Codex mobile ergonomics, Homing opens directly into the conversation with a gesture-safe Left Drawer (respecting 24dp Android edge-back zones and yielding to horizontal code/table scrolling).
- **Active-Only Agent Filtering + Remote Wake**: The drawer's horizontal capsules show **only agents actively open/running on your computer** (probed via a 3s cached single-snapshot process check). Tap **`Wake Agent`** (`POST /api/agents/{id}/wake`) to launch closed macOS desktop apps (`WorkBuddy`, `Cursor`, `Windsurf`, `Trae`, `Zed`) in the background without stealing focus (`open -g -a`), start local agent servers (`OpenCode`), or bring CLI agents online.
- **Pinned Cross-Agent Emergencies**: Pending tool approvals and input requests pin to the top of the drawer with inline **Allow / Deny** buttons and a live **`Urgent ›` jump pill** in the conversation header.

### 3. End-to-End Encrypted Zero-Knowledge Blind Relay

- **Cryptographic Envelope**: Every host and phone generates its own `Ed25519` signing identity and `X25519` key-agreement pair; all RPC calls, streaming items, notifications, and file/image attachments are sealed end-to-end with **`XChaCha20-Poly1305`**.
- **4-Word SAS Verification**: QR and 6-digit pairing codes display deterministic 4-word Short Authentication Strings on both screens to defeat MITM attacks.
- **Tiny Footprint**: `homing-server` stores only public keys, pairing links, and bounded ciphertext queues (500 envelopes / 7 days). A minimal 1-vCPU / 1 GB RAM instance effortlessly serves all your devices.

### 4. Purpose-Built Away Lifecycle: Keep-Alive, Clean-on-Close & 0-RTT Open

- **Mac Away-Guardian Console**: Streamlined 3-card dashboard covering live Agent probes, Relay RTT & Split-Horizon status, and Paired Phones + a built-in **Prevent Mac Idle Sleep (`caffeinate`)** switch.
- **Background Keep-Alive While Open · Clean Slate When Closed**: Locking your screen or switching apps keeps the Android foreground service connected in real time. Swiping Homing closed resets the mobile view on next launch (**automatically preserving sessions currently running or awaiting approval on your computer**).
- **Dual Quick-Clear & Restore Latest 10 Records**: One-tap cleanup buttons (with 5-second Undo) in both the drawer and conversation header keep your phone clutter-free without touching desktop files; use **Restore History** anytime to re-attach specific sessions with their **latest 10 conversation records**.
- **0-RTT Notification Previews & 1-RTT Bootstrap**: Encrypted push envelopes carry an inline markdown snippet so tapping a notification renders the reply in **0 ms** before any network round-trip; startup sync consolidates 9 endpoints into one `GET /api/mobile/bootstrap` call.

### 5. Cross-Agent Orchestration (`@agent` Dispatch & Multi-Agent Rooms)

- **Inline `@agent` Subtasks**: Mention another agent right inside an active thread (e.g. `@codex review the race condition in connector.go`) to spawn an isolated Git-worktree subtask and forward conclusions back to the parent session.
- **Multi-Agent Rooms**: Bring multiple agents into a structured discussion room with speaker call-outs, pause/resume controls, automated meeting minutes, and confirmed action-item dispatch.

### 6. Single Go Binary Across macOS, Windows, Linux, WSL2 & Docker

- Both `homing` (native macOS menu-bar `.app` + **Windows 10/11 system-tray `Homing.exe`** + daemon + embedded bilingual TypeScript console) and `homing-server` compile into self-contained Go binaries with zero Node.js or Python runtime dependencies—equally at home on a MacBook, a Windows workstation (with native `\\wsl$\` session discovery & `SetThreadExecutionState` Keep-Awake), a headless Linux GPU server behind NAT, or Docker.

---

## Supported Agents (27+)

| Tier | Agents | Capabilities |
| :--- | :--- | :--- |
| **Core Deep Integrations** | **Claude Code**, **Codex**, **WorkBuddy**, **Kimi CLI**, **OpenCode**, **ZCode** | Continuous transcript streaming, lifecycle/permission hooks, mobile reply injection, model & reasoning-effort switching, remote wake, start/stop sessions |
| **ACP Drivers & Built-in Catalog** | **Cursor**, **Windsurf**, **Trae**, **Antigravity**, **Gemini CLI**, **Qwen Code**, **GitHub Copilot CLI**, **DSH**, **CodeBuddy**, **Zed**, **Kiro**, **Cline**, **Roo Code**, **Goose**, **OpenHands**, **OpenClaw**, **Aider**, **Amp**, **Kilo Code**, **Junie**, **Warp**, **Droid**, and custom YAML/script adapters | Auto-detection, official vector brand icons, ACP session driving & `@agent` dispatch, extensible via `~/.homing/agents/*.yaml` |

---

## 5-Minute Quick Start

### 1. Start `homing` on Your Workstation (macOS / Windows / Linux)

Requires **Go 1.26+** and at least one locally installed coding agent (prebuilt Web Console assets are already embedded):

```sh
git clone https://github.com/tomato-hug/homing.git
cd homing

# macOS Desktop app (menu-bar tray + management window):
go build -o homing ./cmd/homing
./homing

# Windows 10/11 (PowerShell one-liner or local native bundle):
#   irm https://raw.githubusercontent.com/tomato-hug/homing/main/scripts/install.ps1 | iex
python3 scripts/package-windows.py --version 0.11.6 --arch amd64 --commit $(git rev-parse HEAD) --output dist/win

# Linux / WSL2 / Remote headless devbox:
CGO_ENABLED=0 go build -tags headless -trimpath -ldflags="-s -w" -o homing ./cmd/homing
./homing serve --addr 127.0.0.1:18787
```

Open `http://127.0.0.1:18787/console` to inspect your detected agents and Away-Guardian status.

### 2. Deploy the Self-Hosted Blind Relay (`homing-server`)

On the same Wi-Fi network, you can pair directly over LAN (`--addr 0.0.0.0:18787`). For cellular access from anywhere, deploy `homing-server` behind HTTPS/WSS (Caddy or Nginx):

```sh
docker build -f server/Dockerfile -t homing-server .
docker run -d --name homing-server --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v homing-data:/data homing-server
```

Point Caddy at `127.0.0.1:8080` (`wss://homing.example.com/v1/ws`), then paste that URL into the Console under **Relay & LAN**. Keep **Direct Dial (Bypass Local Proxy)** enabled if your workstation runs a proxy for overseas LLMs while your relay is domestic.

### 3. Install the Android App & Pair

```sh
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Scan the pairing QR code from the Console (or run `./homing pair --serve http://127.0.0.1:18787`), verify the **4 SAS words** match on both devices, confirm on your computer, and switch your phone to cellular data to verify!

---

## Documentation & Governance

- **[Mainland China Public Relay & Proxy Workstation Guide (Split-Horizon Routing)](docs/china-relay-and-proxy.md)**
- **[Self-Hosting Relay, TLS, FCM Push & Public Smoke Test](docs/self-host.md)**
- **[macOS Native Menu-Bar App Guide](docs/macos-native.md)**
- **[Protocol v1 Specification](docs/protocol.md)** · **[Go Architecture](docs/go-architecture.md)** · **[Custom Agent Adapters](docs/adapters.md)**
- **[Beta Installation & Known Limits](docs/beta-release.md)** · **[Contributing](CONTRIBUTING.md)** · **[Security Policy](SECURITY.md)** · **[Changelog](CHANGELOG.md)**

---

## 💬 Community & Feedback

- **QQ Discussion & Feedback Group**: **`512365124`** (setup troubleshooting, Mainland China relay & split-horizon proxy help, new Agent adapter requests, and bug reports)
- **GitHub Issues**: [Submit bug reports & feature requests](https://github.com/tomato-hug/homing/issues)

---

## ☕ Support & Sponsor

Homing is **Apache-2.0 open source, ad-free, and zero-knowledge E2E encrypted**. Self-hosting (`AUTH_MODE=open`) requires zero registration; for the official public relay (`AUTH_MODE=managed`), voluntary tips directly help cover server and Mainland BGP bandwidth costs. Backers are credited in [SPONSORS.md](SPONSORS.md) (anonymous optional) and eligible for official relay **Sponsor** quota codes.

<details>
<summary><b>👉 Expand WeChat Reward / Alipay Tip QR Codes</b></summary>
<br>

| WeChat Reward | Alipay Tip |
| :---: | :---: |
| <img src="docs/assets/sponsor/wechat-reward.png" width="220" alt="WeChat Reward QR"> | <img src="docs/assets/sponsor/alipay-reward.png" width="220" alt="Alipay Tip QR"> |

</details>

---

## 🙏 Acknowledgments

- **Architecture Inspiration**: Thanks to [Magpie](https://github.com/yetone/magpie) for pioneering the single-binary Go + Wails desktop pattern.
- **Agent & Icon Ecosystem**: Thanks to the open CLI / Hook / ACP ecosystems around Claude Code, OpenAI Codex, WorkBuddy, OpenCode, Gemini CLI, and Kimi CLI, and [@lobehub/icons](https://github.com/lobehub/lobe-icons) for brand vector assets.
- **Core Open-Source Libraries**: Built with Go, `Wails`, `modernc.org/sqlite`, `coder/websocket`, `golang.org/x/crypto`, `Jetpack Compose`, `Lazysodium`, `ZXing`, and `JetBrains Mono` (SIL OFL 1.1). See [SPONSORS.md](SPONSORS.md) for the full list.

---

## License

Licensed under the [Apache License 2.0](LICENSE). Third-party fonts and dependencies retain their respective licenses; see [NOTICE](NOTICE).
