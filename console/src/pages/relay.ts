// 连接 › 中转与局域网：三种连接方式、本机地址、安全说明

import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { uptime } from "../lib/format";
import { api, endpoints, poll, type ConnectionInfo } from "../lib/api";
import { isContainerBusy } from "../lib/guard";
import { button, groupHead, kv, listCard, openExternalUrl, reportError, segmented, tag, toast } from "../lib/ui";
import { loc, t } from "../i18n";
import type { PageContext, PageResult } from "../main";

interface SavedRelayNode {
  id: string;
  name: string;
  server: string;
  note?: string;
}

interface RelayProbeInfo {
  server: string;
  auth_mode?: string;
  registration_mode?: string;
  email_enabled?: boolean;
  github_enabled?: boolean;
  legacy_open?: boolean;
  error?: string;
}

interface AdminAccountRow {
  id: string;
  email?: string;
  github_login?: string;
  display_name?: string;
  tier: string;
  status: string;
  sponsor_badge?: boolean;
  host_count?: number;
  client_count?: number;
  today_traffic?: { msg_count?: number; bytes_relayed?: number };
  weekly?: { active_minutes?: number; limit_minutes?: number; resets_at?: number };
}

interface AdminOverviewData {
  ok?: boolean;
  auth_mode?: string;
  registration_mode?: string;
  online_devices?: number;
  online_sockets?: number;
  free_weekly_minutes?: number;
  stats?: {
    total_accounts?: number;
    sponsor_accounts?: number;
    banned_accounts?: number;
    total_devices?: number;
    queued_envelopes?: number;
    today_messages?: number;
    today_bytes?: number;
    week_active_accounts?: number;
    week_active_minutes?: number;
  };
  error?: string;
}

type RegistrationMode = "open" | "invite" | "closed";

function formatQuotaMinutes(minutes?: number): string {
  const m = Math.max(0, Math.round(minutes || 0));
  if (m < 60) return `${m} 分钟`;
  const hours = Math.floor(m / 60);
  const rest = m % 60;
  return rest ? `${hours} 小时 ${rest} 分` : `${hours} 小时`;
}

const SAVED_RELAYS_SLOT = "homing.savedRelays.v1";
const ADMIN_AUTH_SESSION_SLOT = "homing.relayAdmin.session";
const ADMIN_AUTH_LOCAL_SLOT = "homing.relayAdmin.local";

function loadSavedRelays(): SavedRelayNode[] {
  try {
    const raw = localStorage.getItem(SAVED_RELAYS_SLOT);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((x) => x && typeof x.server === "string" && x.server.trim());
  } catch {
    return [];
  }
}

function storeSavedRelays(nodes: SavedRelayNode[]) {
  try {
    localStorage.setItem(SAVED_RELAYS_SLOT, JSON.stringify(nodes));
  } catch {
    /* ignore */
  }
}

function loadAdminToken(): { token: string; remembered: boolean } {
  try {
    const persisted = localStorage.getItem(ADMIN_AUTH_LOCAL_SLOT) || "";
    if (persisted) return { token: persisted, remembered: true };
    const sess = sessionStorage.getItem(ADMIN_AUTH_SESSION_SLOT) || "";
    return { token: sess, remembered: false };
  } catch {
    return { token: "", remembered: false };
  }
}

function saveAdminToken(token: string, remember: boolean) {
  try {
    if (!token) {
      sessionStorage.removeItem(ADMIN_AUTH_SESSION_SLOT);
      localStorage.removeItem(ADMIN_AUTH_LOCAL_SLOT);
      return;
    }
    if (remember) {
      localStorage.setItem(ADMIN_AUTH_LOCAL_SLOT, token);
      sessionStorage.removeItem(ADMIN_AUTH_SESSION_SLOT);
    } else {
      sessionStorage.setItem(ADMIN_AUTH_SESSION_SLOT, token);
      localStorage.removeItem(ADMIN_AUTH_LOCAL_SLOT);
    }
  } catch {
    /* ignore */
  }
}

export function renderRelay(ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  let lastInfo: ConnectionInfo | null = null;
  let dirtyServerDraft: string | null = null;
  let dirtyKeyDraft: string | null = null;
  let savedRelays: SavedRelayNode[] = loadSavedRelays();
  let newNodeNameDraft = "";
  let newNodeUrlDraft = "";
  let probeState: RelayProbeInfo | null = null;
  let probing = false;

  const initialAdmin = loadAdminToken();
  let adminToken = initialAdmin.token;
  let rememberAdminToken = initialAdmin.remembered;
  let adminLoading = false;
  let adminError = "";
  let adminOverview: AdminOverviewData | null = null;
  let adminAccounts: AdminAccountRow[] = [];
  let adminSearch = "";
  let lastGeneratedCodes: string[] = [];
  const host = h("div");

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("已复制", "ok");
    } catch {
      toast("复制失败，请手动选中", "bad");
    }
  }

  async function saveCloud(server?: string, useSystemProxy?: boolean, relayKey?: string) {
    try {
      const result = await endpoints.configureCloud(server, useSystemProxy, relayKey);
      if (server !== undefined) {
        dirtyServerDraft = null;
      }
      if (relayKey !== undefined) {
        dirtyKeyDraft = null;
      }
      toast(
        server !== undefined && !server.trim()
          ? loc("已关闭云中转连接", "Cloud relay disconnected")
          : useSystemProxy !== undefined
            ? useSystemProxy
              ? loc("已切换为经本机系统代理连接中继", "Switched relay routing to system proxy")
              : loc("已切换为直连中继（自动绕过本机代理与 Fake-IP）", "Switched relay routing to direct dial (bypassing local proxy & Fake-IP)")
            : result.cloud?.connected
              ? loc("中继已连接", "Relay connected")
              : loc("中继地址已保存，正在后台连接", "Relay address saved, connecting in background"),
        "ok",
        5000,
      );
      await load(false);
    } catch (err) {
      reportError(err, loc("保存云中转设置失败", "Failed to save relay configuration"));
    }
  }

  function extractRelayHost(rawUrl: string): string {
    try {
      const parsed = new URL(rawUrl.trim());
      return parsed.hostname || "relay.example.cn";
    } catch {
      return "relay.example.cn";
    }
  }

  function deriveRelayPageUrl(wsRaw: string, pagePath: "/account" | "/admin"): string | null {
    const trimmed = wsRaw.trim();
    if (!trimmed) return null;
    try {
      const normalized = /^[a-z]+:\/\//i.test(trimmed) ? trimmed : `wss://${trimmed}`;
      const u = new URL(normalized);
      const proto = u.protocol === "wss:" || u.protocol === "https:" ? "https:" : u.protocol === "ws:" || u.protocol === "http:" ? "http:" : null;
      if (!proto || !u.host) return null;
      return `${proto}//${u.host}${pagePath}`;
    } catch {
      return null;
    }
  }

  function activeRelayServer(): string {
    return (dirtyServerDraft ?? lastInfo?.cloud?.server ?? "").trim();
  }

  function currentUseSystemProxy(): boolean {
    return lastInfo?.cloud?.direct_dial === false;
  }

  function formatTrafficBytes(bytes?: number): string {
    const n = Number(bytes || 0);
    if (!Number.isFinite(n) || n <= 0) return "0 B";
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  async function probeRelayServer(rawServer: string) {
    const server = rawServer.trim();
    if (!server) {
      toast("请先填写中继地址（例如 wss://relay.example.com/v1/ws）", "bad");
      return;
    }
    probing = true;
    if (lastInfo) paint(lastInfo);
    try {
      const res = await endpoints.relayProxy<{
        ok?: boolean;
        auth_mode?: string;
        registration_mode?: string;
        email_enabled?: boolean;
        github_enabled?: boolean;
        error?: string;
      }>({
        server,
        method: "GET",
        path: "/v1/account/config",
        use_system_proxy: currentUseSystemProxy(),
      });
      if (res.status >= 200 && res.status < 300 && res.data) {
        probeState = {
          server,
          auth_mode: res.data.auth_mode || "open",
          registration_mode: res.data.registration_mode || "open",
          email_enabled: Boolean(res.data.email_enabled),
          github_enabled: Boolean(res.data.github_enabled),
        };
        toast(
          probeState.auth_mode === "managed"
            ? "检测完成：该节点为官方受管模式（需要 Relay Key）"
            : "检测完成：该节点为开放免注册模式",
          "ok",
        );
      } else if (res.status === 404) {
        const health = await endpoints
          .relayProxy<{ ok?: boolean; service?: string }>({
            server,
            method: "GET",
            path: "/health",
            use_system_proxy: currentUseSystemProxy(),
          })
          .catch(() => null);
        if (health && health.status >= 200 && health.status < 300 && health.data?.ok) {
          probeState = {
            server,
            auth_mode: "open",
            legacy_open: true,
          };
          toast("中继在线（旧版免注册模式，服务器尚未部署 /account 与 /admin 账号系统）", "ok", 6000);
        } else {
          probeState = {
            server,
            error: res.data?.error || `HTTP ${res.status}`,
          };
          toast(`中继探测返回 HTTP ${res.status}`, "plain");
        }
      } else {
        probeState = {
          server,
          error: res.data?.error || `HTTP ${res.status}`,
        };
        toast(`中继探测返回 HTTP ${res.status}`, "plain");
      }
    } catch (err) {
      probeState = {
        server,
        error: (err as Error)?.message || "连接失败",
      };
      reportError(err, "探测中继失败");
    } finally {
      probing = false;
      if (!disposed && lastInfo) paint(lastInfo);
    }
  }

  function saveNodeBookmark(name: string, server: string) {
    const cleanServer = server.trim();
    if (!cleanServer) {
      toast("请先填写中继 WebSocket 地址", "bad");
      return;
    }
    const cleanName = name.trim() || extractRelayHost(cleanServer);
    const existingIdx = savedRelays.findIndex((item) => item.server.trim() === cleanServer);
    if (existingIdx >= 0) {
      savedRelays[existingIdx] = { ...savedRelays[existingIdx], name: cleanName, server: cleanServer };
    } else {
      savedRelays.unshift({
        id: `relay-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
        name: cleanName,
        server: cleanServer,
      });
    }
    storeSavedRelays(savedRelays);
    newNodeNameDraft = "";
    newNodeUrlDraft = "";
    toast(`已保存中继节点：${cleanName}`, "ok");
    if (lastInfo) paint(lastInfo);
  }

  function removeNodeBookmark(id: string) {
    savedRelays = savedRelays.filter((item) => item.id !== id);
    storeSavedRelays(savedRelays);
    toast("已从收藏列表中移除该中继节点", "plain");
    if (lastInfo) paint(lastInfo);
  }

  async function loadAdminData(silent = false) {
    const server = activeRelayServer();
    const token = adminToken.trim();
    if (!server) {
      adminError = "请先在上方填写要管理的中继节点地址";
      if (!silent) toast(adminError, "bad");
      if (lastInfo) paint(lastInfo);
      return;
    }
    if (!token) {
      adminError = "请先填写中继管理员令牌（HOMING_ADMIN_TOKEN）";
      if (!silent) toast(adminError, "bad");
      if (lastInfo) paint(lastInfo);
      return;
    }
    adminLoading = true;
    adminError = "";
    if (lastInfo) paint(lastInfo);
    try {
      const [ovRes, accRes] = await Promise.all([
        endpoints.relayProxy<AdminOverviewData>({
          server,
          method: "GET",
          path: "/v1/admin/overview",
          admin_token: token,
          use_system_proxy: currentUseSystemProxy(),
        }),
        endpoints.relayProxy<{ ok?: boolean; accounts?: AdminAccountRow[]; error?: string }>({
          server,
          method: "GET",
          path: adminSearch.trim()
            ? `/v1/admin/accounts?q=${encodeURIComponent(adminSearch.trim())}`
            : "/v1/admin/accounts",
          admin_token: token,
          use_system_proxy: currentUseSystemProxy(),
        }),
      ]);
      if (ovRes.status === 401 || ovRes.status === 403 || accRes.status === 401 || accRes.status === 403) {
        adminOverview = null;
        adminAccounts = [];
        adminError = "管理员令牌无效或该中继未启用管理后台（HTTP 401/403）";
        if (!silent) toast(adminError, "bad");
      } else if (ovRes.status >= 200 && ovRes.status < 300) {
        saveAdminToken(token, rememberAdminToken);
        adminOverview = ovRes.data || {};
        adminAccounts = Array.isArray(accRes.data?.accounts) ? accRes.data.accounts : [];
        adminError = "";
        if (!silent) toast("已连接中继管理后台", "ok");
      } else {
        adminOverview = null;
        adminAccounts = [];
        adminError = ovRes.data?.error || `管理接口返回 HTTP ${ovRes.status}`;
        if (!silent) toast(adminError, "bad");
      }
    } catch (err) {
      adminError = (err as Error)?.message || "连接管理后台失败";
      if (!silent) reportError(err, "连接中继管理后台失败");
    } finally {
      adminLoading = false;
      if (!disposed && lastInfo) paint(lastInfo);
    }
  }

  async function updateRegistrationMode(mode: RegistrationMode) {
    const server = activeRelayServer();
    const token = adminToken.trim();
    if (!server || !token) return;
    try {
      const res = await endpoints.relayProxy<{ ok?: boolean; error?: string }>({
        server,
        method: "PUT",
        path: "/v1/admin/settings",
        admin_token: token,
        payload: { registration_mode: mode },
        use_system_proxy: currentUseSystemProxy(),
      });
      if (res.status >= 200 && res.status < 300) {
        toast(`已切换注册策略：${mode === "open" ? "开放注册" : mode === "invite" ? "仅限邀请" : "关闭注册"}`, "ok");
        await loadAdminData(true);
      } else {
        toast(res.data?.error || `更新失败（HTTP ${res.status}）`, "bad");
      }
    } catch (err) {
      reportError(err, "更新注册策略失败");
    }
  }

  async function patchAdminAccount(account: AdminAccountRow, patch: { tier?: string; status?: string; sponsor_badge?: boolean }) {
    const server = activeRelayServer();
    const token = adminToken.trim();
    if (!server || !token) return;
    try {
      const res = await endpoints.relayProxy<{ ok?: boolean; error?: string }>({
        server,
        method: "PATCH",
        path: `/v1/admin/accounts/${encodeURIComponent(account.id)}`,
        admin_token: token,
        payload: patch,
        use_system_proxy: currentUseSystemProxy(),
      });
      if (res.status >= 200 && res.status < 300) {
        toast("账号状态已更新", "ok");
        await loadAdminData(true);
      } else {
        toast(res.data?.error || `操作失败（HTTP ${res.status}）`, "bad");
      }
    } catch (err) {
      reportError(err, "更新账号失败");
    }
  }

  async function generateSponsorCode() {
    const server = activeRelayServer();
    const token = adminToken.trim();
    if (!server || !token) return;
    try {
      const res = await endpoints.relayProxy<{ ok?: boolean; codes?: Array<{ code: string }>; error?: string }>({
        server,
        method: "POST",
        path: "/v1/admin/redeem-codes",
        admin_token: token,
        payload: { count: 1, tier: "sponsor", sponsor_badge: true, max_uses: 1, note: "desktop-admin" },
        use_system_proxy: currentUseSystemProxy(),
      });
      if (res.status >= 200 && res.status < 300 && Array.isArray(res.data?.codes) && res.data.codes.length > 0) {
        lastGeneratedCodes = res.data.codes.map((c) => c.code).filter(Boolean);
        toast(`已生成 Sponsor 兑换码：${lastGeneratedCodes[0]}`, "ok", 6000);
        if (lastInfo) paint(lastInfo);
      } else {
        toast(res.data?.error || `生成兑换码失败（HTTP ${res.status}）`, "bad");
      }
    } catch (err) {
      reportError(err, "生成兑换码失败");
    }
  }

  function methodRow(options: {
    title: string;
    desc: string;
    state: "on" | "ready" | "soon" | "off";
    badge?: { text: string; tone: "ok" | "warn" | "mute" | "plain" };
    note?: string;
  }): HTMLElement {
    const badge =
      options.badge
        ? tag(options.badge.text, options.badge.tone)
        : options.state === "on"
          ? tag("当前使用", "ok")
          : options.state === "ready"
            ? tag("可用", "plain")
            : options.state === "soon"
              ? tag("即将推出", "warn")
              : tag("这一版不可选", "mute");
    const dotClass =
      options.state === "on" ? "dot dot-ok" : options.state === "ready" ? "dot dot-warn" : "dot";
    return h(
      "div",
      { class: "lrow", style: "align-items:flex-start" },
      h("span", { class: dotClass, style: "margin-top:8px" }),
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: options.title }), badge),
        h("div", { class: "lrow-sub", style: "white-space:normal", text: options.desc }),
        options.note ? h("div", { class: "note", style: "margin-top:4px", text: options.note }) : null,
      ),
    );
  }

  function paint(info: ConnectionInfo) {
    clear(host);
    const cloud = info.cloud || { configured: false, connected: false, server: "", peers: 0 };
    const cloudConfigured = Boolean(cloud.configured);
    const cloudAuthRequired = Boolean(cloud.auth_required);
    const cloudConnected = cloudConfigured && Boolean(cloud.connected) && !cloudAuthRequired;
    const cloudBadge = !cloudConfigured
      ? { text: "可配置", tone: "plain" as const }
      : cloudAuthRequired
        ? { text: "需要 Relay Key", tone: "warn" as const }
        : cloudConnected
          ? { text: "已连接", tone: "ok" as const }
          : { text: "已配置 · 重连中", tone: "warn" as const };
    const cloudNote = !cloudConfigured
      ? "尚未配置。填写 Homing 云中转或自建 homingd 的 WebSocket 地址后，手机可在离开局域网时继续使用。"
      : cloudAuthRequired
        ? `尚未绑定 Relay Key：${cloud.server} · 请前往 /account 获取并保存密钥`
        : `${cloudConnected ? "已连接" : "正在后台重连"}：${cloud.server}${cloud.peers > 0 ? ` · ${cloud.peers} 台已配对设备` : ""}`;

    // 连接方式
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("连接方式"),
        listCard(
          methodRow({
            title: "局域网直连",
            desc: "手机和这台电脑在同一个 Wi-Fi 下，直接连，不经过任何服务器。",
            state: "on",
            note: `配对时用局域网地址：${info.lan_urls[0] || `http://127.0.0.1:${info.port}`}`,
          }),
          methodRow({
            title: "Homing 云中转",
            desc: "不在同一个网络时，通过 Homing 的服务器转发。",
            state: "ready",
            badge: cloudBadge,
            note: cloudNote,
          }),
          methodRow({
            title: "自建服务器",
            desc: "把 homingd 跑在自己的服务器上，手机连那台机器。",
            state: "ready",
            badge: { text: "可配置", tone: "plain" },
            note: "与云中转使用同一套端到端加密 RPC；地址可指向自建 homingd。",
          }),
        ),
      ),
    );

    // 云中转地址：与自建服务器共用一条 connector。
    const serverInput = h("input", {
      class: "input mono",
      type: "text",
      value: dirtyServerDraft ?? (cloud.server || ""),
      placeholder: "wss://relay.example.com/v1/ws",
      spellcheck: "false",
      autocomplete: "off",
      style: "min-width:240px;flex:1",
    }) as HTMLInputElement;
    serverInput.addEventListener("input", () => {
      dirtyServerDraft = serverInput.value;
    });
    const keyInput = h("input", {
      class: "input mono",
      type: "password",
      value: dirtyKeyDraft ?? "",
      placeholder: cloud.relay_key_configured
        ? loc(`已配置密钥（${cloud.relay_key_hint || "hrk_••••"}），留空保持不变`, `Key configured (${cloud.relay_key_hint || "hrk_••••"}), leave blank to keep`)
        : loc("官方中继密钥 hrk_...（自建免注册中继无需填写）", "Official Relay Key hrk_... (optional for self-hosted)"),
      spellcheck: "false",
      autocomplete: "off",
      style: "min-width:240px;flex:1",
    }) as HTMLInputElement;
    keyInput.addEventListener("input", () => {
      dirtyKeyDraft = keyInput.value;
    });

    const currentTargetServer = (serverInput.value || cloud.server || "").trim();
    const accountCenterUrl = deriveRelayPageUrl(currentTargetServer, "/account");
    const adminPortalUrl = deriveRelayPageUrl(currentTargetServer, "/admin");
    const matchedProbe = probeState && probeState.server === currentTargetServer ? probeState : null;

    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          "云中转设置",
          undefined,
          button("前往配对手机 ›", {
            small: true,
            variant: "quiet",
            onClick: () => ctx.navigate("/console/hub"),
          }),
        ),
        listCard(
          h(
            "div",
            { class: "lrow", style: "align-items:flex-start" },
            h("span", { class: cloudConnected ? "dot dot-ok" : cloudAuthRequired ? "dot dot-warn" : "dot", style: "margin-top:8px" }),
            h(
              "div",
              { class: "lrow-main" },
              h(
                "div",
                { class: "lrow-title" },
                h("span", { class: "txt", text: cloudConfigured ? "已配置中继" : "连接云中转或自建服务器" }),
                tag(
                  cloudAuthRequired ? "需要 Relay Key（未绑定账号）" : cloudConnected ? "已连接" : cloudConfigured ? "重连中" : "未配置",
                  cloudConnected ? "ok" : cloudConfigured ? "warn" : "mute",
                ),
                cloud.relay_key_configured ? tag(cloud.relay_key_hint || "已绑密钥", "plain") : null,
                matchedProbe && !matchedProbe.error
                  ? tag(
                      matchedProbe.auth_mode === "managed" ? "受管官方模式（需密钥）" : "开放免注册模式",
                      matchedProbe.auth_mode === "managed" ? "warn" : "ok",
                    )
                  : null,
              ),
              h("div", {
                class: "lrow-sub",
                style: "white-space:normal",
                text: "填写 WebSocket 地址；若连接官方公共中继，请先在系统浏览器打开 /account 注册账号并生成 hrk_... 密钥，粘贴后保存重连即可（手机端扫码即用，无需注册）。",
              }),
              h(
                "div",
                {
                  style:
                    "margin-top:10px;padding:10px 12px;border-radius:10px;background:var(--surface-2, rgba(255,255,255,0.03));border:1px solid var(--border);font-size:12.5px;line-height:1.65;color:var(--text-2)",
                },
                h("div", { style: "font-weight:600;color:var(--text);margin-bottom:4px", text: "新用户 4 步快速接入官方中继：" }),
                h("div", { text: "① 在下方填写官方或自建中继地址（例如 wss://homing.douqu.online/v1/ws）" }),
                h("div", { text: "② 点击「打开中继账号中心 (/account) ↗」，将在系统默认浏览器打开注册/登录页并生成 hrk_... 密钥" }),
                h("div", { text: "③ 复制密钥后点「粘贴密钥」，再点「保存并重连」看到绿色「已连接」" }),
                h("div", { text: "④ 点右上角「前往配对手机 ›」展示二维码，手机扫码即可零注册端到端加密直连" }),
              ),
              matchedProbe
                ? h(
                    "div",
                    {
                      style: "margin-top:8px;display:flex;gap:8px;flex-wrap:wrap;align-items:center",
                    },
                    matchedProbe.error
                      ? tag(`探测失败：${matchedProbe.error}`, "bad")
                      : matchedProbe.legacy_open
                        ? tag("中继在线 · 旧版免注册服务（尚未部署 /account 与 /admin）", "warn")
                        : tag(
                            `注册策略：${
                              matchedProbe.registration_mode === "open"
                                ? "开放注册"
                                : matchedProbe.registration_mode === "invite" || matchedProbe.registration_mode === "invite_only"
                                  ? "仅限邀请"
                                  : "关闭注册"
                            }`,
                            matchedProbe.registration_mode === "open" ? "ok" : "warn",
                          ),
                    !matchedProbe.error && matchedProbe.email_enabled ? tag("支持邮箱登录", "plain") : null,
                    !matchedProbe.error && matchedProbe.github_enabled ? tag("支持 GitHub 登录", "plain") : null,
                  )
                : null,
              cloud.relay_notice
                ? h(
                    "div",
                    {
                      style: "margin-top:8px;padding:8px 10px;border-radius:8px;background:rgba(251,191,36,0.12);color:var(--amber, #fbbf24);font-size:12.5px",
                      text: `中继提示：${cloud.relay_notice}`,
                    },
                  )
                : null,
              cloud.last_error && !cloudConnected
                ? h(
                    "div",
                    {
                      style: "margin-top:8px;padding:8px 10px;border-radius:8px;background:rgba(248,113,113,0.12);color:var(--red, #f87171);font-size:12.5px",
                      text: `最近连接提示：${cloud.last_error}`,
                    },
                  )
                : null,
              h(
                "div",
                { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:10px;align-items:center" },
                serverInput,
                button(probing ? "探测中…" : "检测中继模式", {
                  small: true,
                  variant: "quiet",
                  disabled: probing,
                  onClick: () => void probeRelayServer(serverInput.value || cloud.server),
                }),
              ),
              h(
                "div",
                { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:8px;align-items:center" },
                keyInput,
                button("粘贴密钥", {
                  small: true,
                  variant: "quiet",
                  onClick: async () => {
                    try {
                      const clip = (await navigator.clipboard.readText()).trim();
                      if (!clip) {
                        toast("剪贴板为空", "plain");
                        return;
                      }
                      keyInput.value = clip;
                      dirtyKeyDraft = clip;
                      toast("已粘贴密钥，点「保存并重连」生效", "ok");
                    } catch {
                      toast("未能读取剪贴板，请直接按 ⌘V / Ctrl+V 粘贴", "plain");
                    }
                  },
                }),
                button("保存并重连", {
                  small: true,
                  variant: "primary",
                  iconSvg: icons.refresh,
                  onClick: () => {
                    const nextKey = keyInput.value.trim();
                    void saveCloud(serverInput.value, undefined, nextKey ? nextKey : undefined);
                  },
                }),
              ),
              h(
                "div",
                { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:8px;align-items:center" },
                button("打开中继账号中心 (/account) ↗", {
                  small: true,
                  variant: "ghost",
                  onClick: () => {
                    const target = deriveRelayPageUrl(serverInput.value || cloud.server, "/account");
                    if (!target) {
                      toast("请先填写有效的中继地址（如 wss://homing.douqu.online/v1/ws）", "bad");
                      return;
                    }
                    void openExternalUrl(target);
                  },
                }),
                accountCenterUrl
                  ? button("复制账号中心链接", {
                      small: true,
                      variant: "quiet",
                      iconSvg: icons.copy,
                      onClick: () => void copy(accountCenterUrl),
                    })
                  : null,
                cloud.relay_key_configured
                  ? button("清除密钥", {
                      small: true,
                      variant: "quiet",
                      onClick: () => void saveCloud(undefined, undefined, ""),
                    })
                  : null,
                cloudConfigured
                  ? button("断开", {
                      small: true,
                      variant: "quiet",
                      onClick: () => void saveCloud(""),
                    })
                  : null,
              ),
            ),
          ),
        ),
        h("div", { class: "note", style: "margin-top:8px" }, "中继地址与密钥仅以 0600 权限保存在本机 peers.json；点击外部链接会直接拉起你的系统默认浏览器（兼容 macOS 与 Windows）。"),
      ),
    );

    // 已保存的中继节点（多节点切换与书签）
    const nodeNameInput = h("input", {
      class: "input",
      type: "text",
      value: newNodeNameDraft,
      placeholder: "节点名称（如：官方国内中继 / 公司自建）",
      autocomplete: "off",
      style: "min-width:170px;flex:1",
    }) as HTMLInputElement;
    nodeNameInput.addEventListener("input", () => {
      newNodeNameDraft = nodeNameInput.value;
    });
    const nodeUrlInput = h("input", {
      class: "input mono",
      type: "text",
      value: newNodeUrlDraft,
      placeholder: "wss://relay.example.com/v1/ws",
      spellcheck: "false",
      autocomplete: "off",
      style: "min-width:220px;flex:1.4",
    }) as HTMLInputElement;
    nodeUrlInput.addEventListener("input", () => {
      newNodeUrlDraft = nodeUrlInput.value;
    });

    const nodeRows: HTMLElement[] = [];
    for (const node of savedRelays) {
      const isCurrent = Boolean(cloud.server) && cloud.server.trim() === node.server.trim();
      const nodeAccount = deriveRelayPageUrl(node.server, "/account");
      const nodeAdmin = deriveRelayPageUrl(node.server, "/admin");
      nodeRows.push(
        h(
          "div",
          { class: "lrow", style: "align-items:center" },
          h("span", { class: isCurrent ? (cloudConnected ? "dot dot-ok" : "dot dot-warn") : "dot" }),
          h(
            "div",
            { class: "lrow-main" },
            h(
              "div",
              { class: "lrow-title" },
              h("span", { class: "txt", text: node.name }),
              isCurrent ? tag(cloudConnected ? "当前已连" : "当前配置", cloudConnected ? "ok" : "warn") : null,
            ),
            h("div", { class: "lrow-sub mono", text: node.server }),
          ),
          h(
            "div",
            { style: "display:flex;gap:6px;flex-wrap:wrap" },
            !isCurrent
              ? button("切换到此节点", {
                  small: true,
                  variant: "primary",
                  onClick: () => {
                    dirtyServerDraft = node.server;
                    void saveCloud(node.server);
                  },
                })
              : null,
            nodeAccount
              ? button("账号中心 ↗", {
                  small: true,
                  variant: "quiet",
                  onClick: () => void openExternalUrl(nodeAccount),
                })
              : null,
            nodeAdmin
              ? button("管理后台 ↗", {
                  small: true,
                  variant: "quiet",
                  onClick: () => void openExternalUrl(nodeAdmin),
                })
              : null,
            button("删除", {
              small: true,
              variant: "quiet",
              onClick: () => removeNodeBookmark(node.id),
            }),
          ),
        ),
      );
    }

    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          "常用中继节点（多节点管理）",
          undefined,
          currentTargetServer
            ? button("收藏当前节点", {
                small: true,
                variant: "quiet",
                onClick: () => saveNodeBookmark(extractRelayHost(currentTargetServer), currentTargetServer),
              })
            : null,
        ),
        listCard(
          ...(nodeRows.length > 0
            ? nodeRows
            : [
                h(
                  "div",
                  { class: "lrow" },
                  h("div", {
                    class: "note",
                    text: "还没有收藏的中继节点。你可以将官方中继、个人阿里云/腾讯云自建中继或团队内网中继都保存在这里，一键切换与打开对应管理后台。",
                  }),
                ),
              ]),
          h(
            "div",
            { class: "lrow", style: "flex-wrap:wrap;gap:8px" },
            nodeNameInput,
            nodeUrlInput,
            button("添加节点", {
              small: true,
              variant: "ghost",
              onClick: () => saveNodeBookmark(nodeNameInput.value, nodeUrlInput.value),
            }),
          ),
        ),
      ),
    );

    // 管理员：中继节点与账号管理台（服务运维者 / 自建管理员专用）
    const adminTokenInput = h("input", {
      class: "input mono",
      type: "password",
      value: adminToken,
      placeholder: "输入中继服务环境变量 HOMING_ADMIN_TOKEN",
      spellcheck: "false",
      autocomplete: "off",
      style: "min-width:240px;flex:1",
    }) as HTMLInputElement;
    adminTokenInput.addEventListener("input", () => {
      adminToken = adminTokenInput.value;
    });

    const rememberCb = h("input", {
      type: "checkbox",
      ...(rememberAdminToken ? { checked: "checked" } : {}),
    }) as HTMLInputElement;
    rememberCb.addEventListener("change", () => {
      rememberAdminToken = rememberCb.checked;
      if (adminOverview && adminToken.trim()) {
        saveAdminToken(adminToken.trim(), rememberAdminToken);
      }
    });

    const adminSearchInput = h("input", {
      class: "input",
      type: "search",
      value: adminSearch,
      placeholder: "按邮箱 / GitHub 用户名 / 昵称筛选账号…",
      autocomplete: "off",
      style: "min-width:200px;flex:1",
    }) as HTMLInputElement;
    adminSearchInput.addEventListener("input", () => {
      adminSearch = adminSearchInput.value;
    });
    adminSearchInput.addEventListener("keydown", (e: KeyboardEvent) => {
      if (e.key === "Enter") {
        e.preventDefault();
        void loadAdminData(false);
      }
    });

    const adminCardChildren: Array<HTMLElement | null> = [
      h(
        "div",
        { class: "lrow", style: "align-items:flex-start" },
        h("span", { class: adminOverview ? "dot dot-ok" : "dot", style: "margin-top:8px" }),
        h(
          "div",
          { class: "lrow-main" },
          h(
            "div",
            { class: "lrow-title" },
            h("span", { class: "txt", text: "中继站长管理台（账号 / 策略 / 兑换码）" }),
            tag(adminOverview ? "管理员已连接" : "仅站长可用", adminOverview ? "ok" : "mute"),
            currentTargetServer ? tag(extractRelayHost(currentTargetServer), "plain") : null,
          ),
          h("div", {
            class: "lrow-sub",
            style: "white-space:normal",
            text: "如果你是该中继节点的部署者或管理员，可直接在这里输入 HOMING_ADMIN_TOKEN 实时查看在线设备、切换开放/邀请注册、升级 Sponsor、封禁踢下线或生成兑换码；也可点击右上角打开完整 Web 管理后台。",
          }),
          adminError
            ? h("div", {
                style: "margin-top:8px;padding:8px 10px;border-radius:8px;background:rgba(248,113,113,0.12);color:var(--red, #f87171);font-size:12.5px",
                text: adminError,
              })
            : null,
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:10px;align-items:center" },
            adminTokenInput,
            button(adminLoading ? "连接中…" : adminOverview ? "刷新管理数据" : "连接管理后台", {
              small: true,
              variant: "primary",
              disabled: adminLoading,
              onClick: () => void loadAdminData(false),
            }),
            adminOverview
              ? button("退出管理员", {
                  small: true,
                  variant: "quiet",
                  onClick: () => {
                    adminOverview = null;
                    adminAccounts = [];
                    adminError = "";
                    adminToken = "";
                    lastGeneratedCodes = [];
                    saveAdminToken("", false);
                    toast("已清除本地管理员会话", "plain");
                    if (lastInfo) paint(lastInfo);
                  },
                })
              : null,
          ),
          h(
            "label",
            { style: "display:inline-flex;align-items:center;gap:6px;margin-top:8px;font-size:12px;color:var(--text-3);cursor:pointer" },
            rememberCb,
            h("span", { text: "在本机控制台持久化保存管理员令牌（默认仅当前会话有效）" }),
          ),
        ),
      ),
    ];

    if (adminOverview) {
      const stats = adminOverview.stats || {};
      const regMode = ((adminOverview.registration_mode === "invite_only" ? "invite" : adminOverview.registration_mode) || "open") as RegistrationMode;
      adminCardChildren.push(
        h(
          "div",
          { class: "lrow", style: "flex-direction:column;align-items:stretch;gap:12px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap" },
            tag(`在线设备 ${adminOverview.online_devices ?? 0}`, "ok"),
            tag(`在线连接 ${adminOverview.online_sockets ?? 0}`, "plain"),
            tag(`注册账号 ${stats.total_accounts ?? 0}`, "plain"),
            tag(`Sponsor ${stats.sponsor_accounts ?? 0}`, "ok"),
            tag(`已封禁 ${stats.banned_accounts ?? 0}`, (stats.banned_accounts ?? 0) > 0 ? "bad" : "mute"),
            tag(`今日流量 ${formatTrafficBytes(stats.today_bytes)}`, "plain"),
            tag(`离线队列 ${stats.queued_envelopes ?? 0} 条`, "plain"),
            tag(`本周活跃 ${stats.week_active_accounts ?? 0} 人 · ${formatQuotaMinutes(stats.week_active_minutes)}`, "plain"),
            tag(
              `Free 每周上限 ${(adminOverview.free_weekly_minutes ?? 0) > 0 ? formatQuotaMinutes(adminOverview.free_weekly_minutes) : "不限"}`,
              "plain",
            ),
          ),
          h(
            "div",
            { style: "display:flex;gap:10px;flex-wrap:wrap;align-items:center;justify-content:space-between" },
            h(
              "div",
              { style: "display:flex;gap:8px;align-items:center;flex-wrap:wrap" },
              h("span", { class: "note", text: "新用户注册策略：" }),
              segmented(
                [
                  { value: "open", label: "开放注册" },
                  { value: "invite", label: "仅限邀请" },
                  { value: "closed", label: "关闭注册" },
                ],
                regMode,
                (nextMode) => void updateRegistrationMode(nextMode),
              ),
            ),
            button("生成 Sponsor 兑换码", {
              small: true,
              variant: "ghost",
              onClick: () => void generateSponsorCode(),
            }),
          ),
          lastGeneratedCodes.length > 0
            ? h(
                "div",
                {
                  style:
                    "display:flex;gap:8px;flex-wrap:wrap;align-items:center;padding:8px 10px;border-radius:8px;background:rgba(16,185,129,0.1);border:1px solid rgba(16,185,129,0.28)",
                },
                h("span", { class: "note", style: "color:var(--text)", text: "新生成的兑换码：" }),
                ...lastGeneratedCodes.map((code) => h("code", { class: "mono", style: "font-weight:600;color:var(--green, #10b981)", text: code })),
                button("复制兑换码", {
                  small: true,
                  variant: "quiet",
                  iconSvg: icons.copy,
                  onClick: () => void copy(lastGeneratedCodes.join("\n")),
                }),
              )
            : null,
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;align-items:center" },
            adminSearchInput,
            button("搜索账号", {
              small: true,
              variant: "quiet",
              onClick: () => void loadAdminData(false),
            }),
          ),
        ),
      );

      if (adminAccounts.length === 0) {
        adminCardChildren.push(
          h("div", { class: "lrow" }, h("div", { class: "note", text: "没有匹配的中继账号记录。" })),
        );
      } else {
        for (const acc of adminAccounts.slice(0, 30)) {
          const isSponsor = acc.tier === "sponsor";
          const isBanned = acc.status === "banned";
          const identity = acc.email || (acc.github_login ? `@${acc.github_login}` : acc.display_name || acc.id);
          adminCardChildren.push(
            h(
              "div",
              { class: "lrow", style: "align-items:center" },
              h("span", { class: isBanned ? "dot dot-bad" : isSponsor ? "dot dot-ok" : "dot" }),
              h(
                "div",
                { class: "lrow-main" },
                h(
                  "div",
                  { class: "lrow-title" },
                  h("span", { class: "txt", text: identity }),
                  tag(isSponsor ? "Sponsor" : "Free", isSponsor ? "ok" : "plain"),
                  tag(isBanned ? "已封禁" : "正常", isBanned ? "bad" : "mute"),
                ),
                h("div", {
                  class: "lrow-sub",
                  text: `电脑 ${acc.host_count ?? 0} 台 · 手机 ${acc.client_count ?? 0} 台 · 今日流量 ${formatTrafficBytes(acc.today_traffic?.bytes_relayed)} · 本周远程 ${formatQuotaMinutes(acc.weekly?.active_minutes)} / ${(acc.weekly?.limit_minutes ?? 0) > 0 ? formatQuotaMinutes(acc.weekly?.limit_minutes) : "不限"}`,
                }),
              ),
              h(
                "div",
                { style: "display:flex;gap:6px;flex-wrap:wrap" },
                button(isSponsor ? "降为 Free" : "升为 Sponsor", {
                  small: true,
                  variant: "quiet",
                  onClick: () =>
                    void patchAdminAccount(acc, {
                      tier: isSponsor ? "free" : "sponsor",
                      sponsor_badge: !isSponsor,
                    }),
                }),
                button(isBanned ? "解封" : "封禁并踢下线", {
                  small: true,
                  variant: "quiet",
                  onClick: () =>
                    void patchAdminAccount(acc, {
                      status: isBanned ? "active" : "banned",
                    }),
                }),
              ),
            ),
          );
        }
      }
    }

    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          "管理员：中继节点与账号管理台",
          undefined,
          button("打开 Web 管理后台 (/admin) ↗", {
            small: true,
            variant: "quiet",
            onClick: () => {
              if (!adminPortalUrl) {
                toast("请先填写有效的中继地址（如 wss://homing.douqu.online/v1/ws）", "bad");
                return;
              }
              void openExternalUrl(adminPortalUrl);
            },
          }),
        ),
        listCard(...adminCardChildren),
      ),
    );

    // 双栈网络与代理分流（中国大陆中继 × 代理网络工作站）
    const directDial = cloud.direct_dial !== false;
    const relayHost = extractRelayHost(cloud.server || "wss://relay.example.cn/v1/ws");
    const clashRules = [
      "# Homing 双栈分流直连规则（Clash / Mihomo）",
      "  - PROCESS-NAME,homing,DIRECT",
      `  - DOMAIN,${relayHost},DIRECT`,
    ].join("\n");
    const surgeRules = [
      "# Homing 双栈分流直连规则（Surge / Loon）",
      "PROCESS-NAME,homing,DIRECT",
      `DOMAIN,${relayHost},DIRECT`,
    ].join("\n");

    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          loc("代理网络与双栈分流（中国大陆中继推荐）", "Split-Horizon & Proxy Routing"),
          undefined,
          button(
            directDial
              ? loc("切换为走系统代理（海外中继）", "Route via System Proxy")
              : loc("切换为直连绕过代理（推荐）", "Switch to Direct Dial (Recommended)"),
            {
              small: true,
              variant: directDial ? "quiet" : "primary",
              onClick: () => void saveCloud(undefined, directDial ? true : false),
            },
          ),
        ),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px" },
            tag(
              directDial
                ? loc("直连模式 · 已绕过本机代理", "Direct Dial · Local Proxy Bypassed")
                : loc("代理模式 · 经系统代理连接中继", "Proxy Mode · Using System Proxy"),
              directDial ? "ok" : "warn",
            ),
            cloudConnected && cloud.latency_ms
              ? tag(`RTT ${cloud.latency_ms} ms`, cloud.latency_ms < 120 ? "ok" : "warn")
              : null,
            cloud.proxy_env_detected && directDial
              ? tag(loc("已隔离 HTTP_PROXY 环境变量", "HTTP_PROXY Isolated"), "ok")
              : null,
            cloud.tun_fake_ip_bypassed
              ? tag(loc("已自动绕过 TUN Fake-IP", "TUN Fake-IP Bypassed"), "ok")
              : cloud.tun_fake_ip_detected
                ? tag(loc("检测到 TUN Fake-IP，建议配置下方直连规则", "TUN Fake-IP Detected"), "warn")
                : null,
            cloud.resolved_ip
              ? tag(loc(`解析 IP: ${cloud.resolved_ip}`, `IP: ${cloud.resolved_ip}`), "plain")
              : null,
          ),
          h(
            "div",
            { class: "note", style: "white-space:normal;line-height:1.6" },
            loc(
              "双栈分流原理：这台设备上的 Claude Code、Codex、Gemini 等 Agent 照常走本机代理访问海外大模型；而 Homing 默认绕过 HTTP_PROXY 并通过公共 DNS 自动穿透 Clash/Surge 的 198.18.0.0/15 Fake-IP 直连中国大陆公网中继，手机端使用国内 5G/Wi-Fi 无需开启任何 VPN 或代理即可秒连。若你的设备开启了严格全局 L3 TUN 网卡路由，可一键复制下方直连规则：",
              "Split-horizon routing: local coding agents keep using your workstation proxy for overseas LLM APIs, while Homing bypasses HTTP_PROXY and 198.18.0.0/15 TUN Fake-IP to dial your Mainland China relay directly—so your phone needs zero VPN on cellular data.",
            ),
          ),
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:10px" },
            button(loc("复制 Clash / Mihomo 规则", "Copy Clash / Mihomo Rules"), {
              small: true,
              variant: "ghost",
              iconSvg: icons.copy,
              onClick: () => void copy(clashRules),
            }),
            button(loc("复制 Surge 规则", "Copy Surge Rules"), {
              small: true,
              variant: "ghost",
              iconSvg: icons.copy,
              onClick: () => void copy(surgeRules),
            }),
          ),
        ),
      ),
    );

    // 本机地址
    const addresses = [
      { who: "本机", url: `http://127.0.0.1:${info.port}`, hint: "控制台" },
      ...info.lan_urls.map((url, index) => ({ who: index === 0 ? "局域网" : "局域网", url, hint: "手机配对" })),
    ];
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("这台电脑的地址"),
        h(
          "div",
          { class: "list-card", style: "padding:10px 15px" },
          h(
            "div",
            null,
            ...addresses.map((item) =>
              h(
                "div",
                { class: "addr-row" },
                h("span", { class: "who", text: item.who }),
                h("span", { style: "flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis", text: item.url }),
                h("span", { class: "note", text: item.hint }),
                button("复制", {
                  small: true,
                  variant: "quiet",
                  iconSvg: icons.copy,
                  onClick: () => void copy(item.hint === "控制台" ? `${item.url}/console` : item.url),
                }),
              ),
            ),
          ),
          h("div", { class: "note", style: "margin-top:8px" }, "手机配对用局域网地址；控制台只在本机 127.0.0.1 打开。"),
        ),
      ),
    );

    // 安全
    const e2eTag =
      info.security.e2e === "connected"
        ? tag("端到端加密 · 已连接", "ok")
        : info.security.e2e === "configured"
          ? tag("端到端加密 · 待重连", "warn")
          : info.security.e2e === "planned_m4"
            ? tag("端到端加密 M4 接入", "warn")
            : tag("端到端加密 · 可配置", "plain");
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("安全"),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px" },
            tag(info.security.tls ? "HTTPS" : "局域网明文传输", info.security.tls ? "ok" : "warn"),
            tag("设备令牌鉴权", "ok"),
            e2eTag,
          ),
          h(
            "div",
            { class: "note" },
            "本机控制台用 HttpOnly cookie 鉴权，页面里不出现 token；手机用一次性配对令牌换长期设备令牌，可以随时在「手机 › 设备」里移除。局域网直连是明文 HTTP；云中转与自建服务器走端到端加密 RPC。",
          ),
        ),
      ),
    );

    // 本机服务
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("本机服务"),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          kv([
            ["版本", info.version],
            ["平台", `${info.platform} · ${info.arch}`],
            ["运行方式", info.runtime === "go" ? "单文件 homing（Go）" : info.packaged ? "单文件 homingd" : "node relay/server.mjs"],
            [info.runtime === "go" ? "Go" : "Node", info.runtime === "go" ? info.runtime_version || "-" : info.node],
            ["端口", String(info.port)],
            ["已运行", uptime(info.uptime_s)],
            ["数据目录", info.data_dir],
            ["控制台目录", info.console_dir],
          ]),
        ),
      ),
    );

    // 开机自启（纯命令行版）
    const enabled = info.autostart.enabled;
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          "开机自启（命令行版）",
          undefined,
          button(enabled ? "关闭" : "开启", {
            small: true,
            variant: enabled ? "quiet" : "ghost",
            iconSvg: icons.power,
            onClick: async () => {
              try {
                const result = await api.post<{ ok: boolean; note?: string; error?: string }>("/api/autostart", { enabled: !enabled });
                if (!result.ok) {
                  toast(result.error || "设置失败", "bad");
                  return;
                }
                toast(result.note || "已更新", "ok");
                await load();
              } catch (err) {
                reportError(err, "设置开机自启失败");
              }
            },
          }),
        ),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-bottom:8px" },
            tag(enabled ? "已开启" : "未开启", enabled ? "ok" : "mute"),
            tag(info.autostart.backend === "run-key" ? "当前用户登录启动项" : "用户级 LaunchAgent", "plain"),
          ),
          info.autostart.legacy
            ? h("div", { class: "note", style: "color:var(--amber)", text: `检测到已有 launchd 服务：${info.autostart.legacy.split("/").pop()}，桌面端不会重复拉起 homingd。` })
            : null,
          h("div", { class: "note", text: "桌面 App 用系统自带的登录项；这里管的是纯命令行版 homingd。" }),
        ),
      ),
    );
  }

  async function load(fromPoll = false) {
    try {
      const info = await endpoints.connection();
      if (disposed) return;
      lastInfo = info;
      if (fromPoll && (isContainerBusy(host) || dirtyServerDraft !== null || dirtyKeyDraft !== null)) return;
      paint(info);
    } catch (err) {
      if (disposed) return;
      if (lastInfo) return;
      clear(host);
      host.append(h("div", { class: "list-card" }, h("div", { class: "empty", text: (err as Error).message })));
    }
  }

  clear(host);
  host.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: t("common.loading") })));
  root.append(host);
  void load(false);
  const stop = poll(() => load(true), 10_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
