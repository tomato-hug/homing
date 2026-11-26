# Changelog

## 0.11.6-beta.12 — Public Open-Source Release (Cross-Platform Desktop, Managed Relay & Away Mode)

- **Native Windows 10/11 & macOS Desktop Parity**: Added native Windows system-tray application (`Homing.exe`, `PerMonitorV2` HiDPI, `SetThreadExecutionState` Keep-Awake, `\\wsl$\` session discovery, and PowerShell one-line installer `scripts/install.ps1`), alongside unified desktop Back/Forward navigation, clickable breadcrumbs, OS browser handoff, and reliable macOS menu-bar window reopening.
- **Managed Public Relay (`HOMING_AUTH_MODE=managed`) & Weekly Fair-Use Quota**: Built-in `/account` user portal (Email verification code & GitHub OAuth login, `hrk_...` host keys, sponsor code redemption), `/admin` web & desktop admin console, brute-force IP rate limiting, and a default **20-hour/week (`1,200` active phone minutes)** fair-use limit for free accounts (desktop standby and push notifications never consume weekly minutes; self-hosted `open` mode remains registration-free).
- **Synchronized Away-Listening Mode & Multi-OEM Android Keep-Alive**: Added 1-click synchronized **Away Listening** toggles across PC topbar, system tray, and Android header/drawer; strict historical session noise filtering; WorkBuddy/Claude `idle_prompt` status & brand normalization; clean conversation header without sibling-chip clutter; and OEM battery-optimization guidance for ColorOS, HyperOS/MIUI, OriginOS, HarmonyOS, One UI, and stock Android.

## 0.11.6-beta.3 — Away-Guardian, Conversation-First Drawer & Split-Horizon Relay

- **Split-Horizon Proxy & Mainland China Relay Support**: `homing` isolates `HTTP_PROXY` / `HTTPS_PROXY` by default (`direct_dial: true`) and automatically detects and bypasses `198.18.0.0/15` / `fc00::/18` TUN Fake-IP hijacking via direct public DNS while preserving TLS SNI—enabling proxy-equipped workstations to connect directly to a Mainland China `homing-server` while Android phones connect over plain domestic 5G with zero VPN. Added live diagnostics and 1-click Clash / Mihomo / Surge rule export in the Console.
- **Conversation-First Android Left Drawer**: Replaced multi-tab switching with a gesture-safe Left Drawer featuring active-only Agent capsules, pinned cross-agent urgent approvals with inline Allow/Deny, and a header urgent-jump pill.
- **Remote Agent Wake & Active Process Probe**: Added 3s-cached OS process detection (`process_running`, `active_online`, `can_wake`) and whitelisted `POST /api/agents/{id}/wake` (`drive` scope) to launch closed macOS desktop apps in the background (`open -g -a`) or activate local daemon/CLI agents from mobile.
- **Away Lifecycle, Dual Quick-Clear & Restore Latest 10 Records**: Added foreground keep-alive while Homing remains open, automatic clean-slate reset when Homing is swiped closed (preserving running and urgent tasks on the computer), dual quick-clear buttons with 5-second undo, and selective session restore loading the latest 10 conversation records.
- **0-RTT Previews, 1-RTT Bootstrap & Mac Keep-Awake**: Embedded 8 KB markdown snippets in encrypted push payloads, consolidated mobile startup sync into `GET /api/mobile/bootstrap`, streamlined the Mac Away-Guardian Console into 3 status cards with `caffeinate` idle-sleep protection, and fixed vector fill-rule (`NonZero` vs `EvenOdd`) rendering across 27+ official Agent brand logos.

## 0.11.5-beta.1 — release candidate

- Android groups notifications into phone-local conversations, starts context at the first received message, and loads earlier context in ten-round pages on request.
- Inbox clearing and conversation removal affect only phone display, with undo support. Added the in-app link browser.
- Event synchronization uses bounded pages and negotiated compression over encrypted RPC. Older clients retain the unpaged response format.
- WebSocket control frames remain responsive during slower API calls. Android resumes synchronization when a connection returns and refreshes the conversation left open.
- Failed explicit permission options now report failure instead of sending an ordinary chat reply.
- Fixed Android JSON-null history notices and switching the active computer after selection was already persisted.
- Retains meeting room chat, Agent mentions, pause / resume, minutes and confirmed action dispatch from the preceding development builds.

See the Beta guide and acceptance record for the distinction between tested behavior, runtime prerequisites and pending checks. This entry does not imply that a public GitHub release has already been published.
