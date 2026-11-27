// 离席守护中心（Mac 默认首页）：
// 聚焦用户离开电脑时的三大核心职责：
//   ① Agent 接入与手机推送开关（含一键试推）
//   ② 手机扫码配对与中继直连状态（含绕过本机代理直连开关与实时延迟）
//   ③ 离席模式、防止 Mac 休眠（caffeinate -i）与免打扰推送控制
// 同时在存在待决策事项（授权 / 追问 / 报错）时，在顶部显著置顶一键处理横条。

import { clear, h, icon } from "../lib/dom";
import {
  ApiError,
  endpoints,
  poll,
  type AgentCatalogEntry,
  type AgentSettings,
  type ConnectionInfo,
  type Device,
  type Home,
  type HomeAttention,
  type KeepAwakeState,
  type StatsPayload,
} from "../lib/api";
import { approvalButtons } from "../lib/approval";
import { AGENT_LABELS, projectOf, timeAgo } from "../lib/format";
import { isContainerBusy } from "../lib/guard";
import { icons } from "../lib/icons";
import { createPairCard, type PairCard } from "../lib/pair-card";
import { agentSquare, button, pill, reportError, segmented, spinner, toast, toggle } from "../lib/ui";
import { loc, t } from "../i18n";
import { getDesktopAppVersion, type PageContext, type PageResult } from "../main";
import { renderInbox } from "./inbox";

const PRIMARY_AGENTS = ["claude", "codex", "workbuddy", "opencode", "kimi", "cursor"];

export function renderHome(ctx: PageContext): PageResult {
  const root = h("div", { class: "guardian-page" });
  let disposed = false;
  let loading = false;

  let home: Home | null = null;
  let connection: ConnectionInfo | null = null;
  let catalog: AgentCatalogEntry[] = [];
  let agentSettings: Record<string, AgentSettings> = {};
  let devices: Device[] = [];
  let keepAwake: KeepAwakeState | null = null;
  let stats: StatsPayload | null = null;
  let fallback: PageResult | null = null;

  let pairOpen = false;
  let pairAutoInitialized = false;
  let pairCard: PairCard | null = null;
  const busyAgents = new Set<string>();

  const versionBannerHost = h("div", { class: "guardian-banner-host" });
  const headerHost = h("div", { class: "guardian-header" });
  const urgentHost = h("div", { class: "guardian-urgent-host" });
  const gridHost = h("div", { class: "guardian-grid" });
  const footHost = h("div", { class: "guardian-foot" });
  root.append(versionBannerHost, headerHost, urgentHost, gridHost, footHost);

  function openSession(sessionId: string) {
    ctx.navigate(`/console/sessions?session=${encodeURIComponent(sessionId)}`);
  }

  function ensurePairCard(): PairCard {
    if (!pairCard) {
      pairCard = createPairCard({
        qrSize: 176,
        onPaired: () => {
          pairOpen = false;
          void load(false);
          ctx.refreshShell();
        },
      });
      void pairCard.start();
    }
    return pairCard;
  }

  async function toggleAgentPush(entry: AgentCatalogEntry, nextPush: boolean) {
    busyAgents.add(entry.id);
    paint();
    try {
      const updated = await endpoints.patchAgent(entry.id, {
        connected: true,
        push_to_phone: nextPush,
      });
      agentSettings[entry.id] = updated;
      toast(
        nextPush
          ? loc(`已开启 ${entry.name} 手机推送`, `${entry.name} phone push enabled`)
          : loc(`已关闭 ${entry.name} 手机推送`, `${entry.name} phone push muted`),
        "ok",
      );
      await load(false);
    } catch (err) {
      reportError(err, loc("切换 Agent 推送失败", "Failed to update agent push"));
    } finally {
      busyAgents.delete(entry.id);
      paint();
    }
  }

  async function connectAgentQuick(entry: AgentCatalogEntry) {
    busyAgents.add(entry.id);
    paint();
    try {
      let hookChanged = false;
      if (entry.readiness === "builtin") {
        await endpoints.patchAgent(entry.id, { connected: true, push_to_phone: true });
        if (entry.hook_install === "auto" && !entry.hook_installed) {
          const res = await endpoints.installHooks(entry.id).catch(() => null);
          if (res?.ok && res.changed) hookChanged = true;
        }
      } else {
        await endpoints.quickConnectCatalogAgent(entry.id);
      }
      toast(
        hookChanged
          ? loc(`${entry.name} 已接入并安装钩子，重启该 Agent 后推送生效`, `${entry.name} connected with hooks; restart agent to activate`)
          : loc(`${entry.name} 已接入并开启手机推送`, `${entry.name} connected`),
        "ok",
      );
      await load(false);
      ctx.refreshShell();
    } catch (err) {
      reportError(err, loc("接入 Agent 失败", "Failed to connect agent"));
    } finally {
      busyAgents.delete(entry.id);
      paint();
    }
  }

  async function testAgentPush(entry: AgentCatalogEntry) {
    busyAgents.add(`test:${entry.id}`);
    paint();
    try {
      const res = await endpoints.testPush(entry.id);
      if (res.ok) {
        toast(res.message || loc(`已向手机发送 ${entry.name} 测试通知`, `Test push sent for ${entry.name}`), "ok");
      } else {
        toast(res.message || loc("试发未送达，请确认手机在线", "Test push was not delivered"), "bad");
      }
    } catch (err) {
      reportError(err, loc("试发通知失败", "Test push failed"));
    } finally {
      busyAgents.delete(`test:${entry.id}`);
      paint();
    }
  }

  async function toggleDirectDial(direct: boolean) {
    try {
      const res = await endpoints.configureCloud(undefined, !direct);
      if (connection) connection.cloud = res.cloud;
      toast(
        direct
          ? loc("已启用中继直连（绕过本机 HTTP/Clash 代理）", "Direct relay dial enabled (bypassing system proxy)")
          : loc("已改为跟随系统代理连接中继", "Using system proxy for relay"),
        "ok",
      );
      await load(false);
    } catch (err) {
      reportError(err, loc("更新中继连接策略失败", "Failed to update relay proxy setting"));
    }
  }

  async function toggleAway(enabled: boolean) {
    try {
      await endpoints.setAway(enabled);
      toast(enabled ? t("home.awayOn") : t("home.awayOff"), "ok");
      await load(false);
      ctx.refreshShell();
    } catch (err) {
      reportError(err, loc("切换离开模式失败", "Failed to toggle away mode"));
    }
  }

  async function patchKeepAwake(patch: { enabled?: boolean; auto_on_away?: boolean; max_minutes?: number }) {
    try {
      keepAwake = await endpoints.updateKeepAwake(patch);
      toast(
        keepAwake.active
          ? loc("Mac 防休眠已生效（合盖除外）", "Mac sleep prevention active")
          : loc("防休眠设置已更新", "Sleep prevention updated"),
        "ok",
      );
      paint();
    } catch (err) {
      reportError(err, loc("更新防休眠失败", "Failed to update keep-awake"));
    }
  }

  async function togglePausePush(paused: boolean) {
    try {
      if (paused) await endpoints.pausePush(60);
      else await endpoints.resumePush();
      toast(
        paused
          ? loc("已暂停手机推送 60 分钟", "Phone push paused for 60m")
          : loc("已恢复手机推送", "Phone push resumed"),
        "ok",
      );
      await load(false);
    } catch (err) {
      reportError(err, loc("切换免打扰失败", "Failed to toggle Do Not Disturb"));
    }
  }

  async function decideApproval(item: HomeAttention, decision: "allow" | "deny", optionId?: string) {
    try {
      await endpoints.resolveApproval(item.event_id, decision, optionId);
      toast(decision === "allow" ? loc("已允许", "Allowed") : loc("已拒绝", "Denied"), "ok");
      await load(false);
      ctx.refreshShell();
    } catch (err) {
      reportError(err, loc("审批操作失败", "Approval failed"));
    }
  }

  function paintVersionBanner() {
    clear(versionBannerHost);
    const desktopVer = getDesktopAppVersion();
    const daemonVer = connection?.version || "";
    if (!desktopVer || !daemonVer || desktopVer === daemonVer) return;
    const baseDesktop = desktopVer.split("+")[0];
    const baseDaemon = daemonVer.split("+")[0];
    if (baseDesktop === baseDaemon) return;

    const cmd = "launchctl kickstart -k gui/$(id -u)/dev.homing.homingd";
    versionBannerHost.append(
      h(
        "div",
        { class: "guardian-version-banner" },
        icon(icons.info, 16),
        h(
          "div",
          { class: "guardian-version-copy" },
          h("strong", {
            text: loc(
              `检测到桌面端（${desktopVer}）与后台常驻服务（${daemonVer}）版本不一致`,
              `Desktop app (${desktopVer}) differs from background daemon (${daemonVer})`,
            ),
          }),
          h("span", {
            class: "note",
            text: loc("更新 ~/.homing/bin/homing 后重启常驻服务即可保持两端能力一致。", "Sync ~/.homing/bin/homing and restart launchd service to align versions."),
          }),
        ),
        button(loc("复制重启命令", "Copy Restart Cmd"), {
          small: true,
          variant: "quiet",
          iconSvg: icons.copy,
          onClick: () => {
            void navigator.clipboard?.writeText(cmd).then(
              () => toast(loc("已复制重启命令", "Restart command copied"), "ok"),
              () => toast(cmd, "plain", 6000),
            );
          },
        }),
      ),
    );
  }

  function paintHeader() {
    clear(headerHost);
    const connectedCount = catalog.filter((a) => a.connected).length;
    const onlinePhones = devices.filter((d) => d.online).length;
    const awayOn = Boolean(home?.away.enabled);
    const awakeOn = Boolean(keepAwake?.active);

    headerHost.append(
      h(
        "div",
        { class: "guardian-title-wrap" },
        h("div", { class: "eyebrow", text: loc("Mac 离席守护台", "Mac Away Guardian") }),
        h("h1", {
          text: awayOn
            ? loc("正在离席守护你的桌面 Agent", "Guarding your desktop agents while away")
            : loc("把电脑里的多个 Agent 稳稳交到手机上", "Keep every desktop agent reachable from your phone"),
        }),
        h("p", {
          class: "note",
          text: loc(
            "无需在手机切多个客户端或配置海外网络——在这里管好接入开关、手机配对与防休眠即可安心离开电脑。",
            "Manage agent push switches, phone pairing, and Mac sleep prevention in one place.",
          ),
        }),
      ),
      h(
        "div",
        { class: "guardian-pills" },
        pill(
          loc(`${connectedCount} 个 Agent 已接入`, `${connectedCount} Agent(s)`),
          connectedCount > 0 ? "ok" : "warn",
        ),
        pill(
          devices.length > 0
            ? loc(`${devices.length} 台手机（${onlinePhones} 在线）`, `${devices.length} Phone(s) (${onlinePhones} online)`)
            : loc("尚未配对手机", "No phone paired"),
          devices.length > 0 ? "ok" : "warn",
        ),
        awakeOn
          ? pill(loc("防休眠生效中", "Keep-Awake On"), "run")
          : pill(loc("防休眠待命", "Sleep Normal"), "mute"),
      ),
    );
  }

  function paintUrgent() {
    clear(urgentHost);
    const urgent = (home?.attention || []).filter((item) =>
      ["approval", "needs_reply", "error"].includes(item.reason),
    );
    if (urgent.length === 0) return;

    urgentHost.append(
      h(
        "section",
        { class: "guardian-urgent-card" },
        h(
          "div",
          { class: "guardian-urgent-head" },
          h("span", { class: "guardian-urgent-badge", text: `⚡ ${urgent.length}` }),
          h("strong", {
            text: loc(
              `${urgent.length} 个会话正在等你拍板或回复`,
              `${urgent.length} session(s) waiting for your approval or reply`,
            ),
          }),
          h("span", { class: "spacer" }),
          button(loc("打开会话工作台", "Open Workbench"), {
            small: true,
            variant: "quiet",
            iconSvg: icons.arrowRight,
            onClick: () => ctx.navigate("/console/sessions"),
          }),
        ),
        h(
          "div",
          { class: "guardian-urgent-list" },
          ...urgent.slice(0, 4).map((item) =>
            h(
              "div",
              { class: "guardian-urgent-row" },
              agentSquare(item.session.source, true),
              h(
                "div",
                { class: "guardian-urgent-main" },
                h(
                  "div",
                  { class: "guardian-urgent-title" },
                  h("strong", { text: item.headline || item.session.title || loc("未命名会话", "Untitled") }),
                  h("span", {
                    class: "note",
                    text: `${AGENT_LABELS[item.session.source] || item.session.source} · ${projectOf(item.session) || loc("未分组", "Ungrouped")} · ${timeAgo(item.at)}`,
                  }),
                ),
                item.approval
                  ? h("code", { class: "guardian-urgent-cmd", text: item.approval.command })
                  : item.summary
                    ? h("div", { class: "note", text: item.summary })
                    : null,
              ),
              h(
                "div",
                { class: "guardian-urgent-actions" },
                item.reason === "approval" && item.approval
                  ? approvalButtons(item.approval, (decision, optionId) =>
                      void decideApproval(item, decision, optionId),
                    )
                  : null,
                button(t("common.open"), {
                  small: true,
                  variant: "quiet",
                  onClick: () => openSession(item.session.session_id),
                }),
              ),
            ),
          ),
        ),
      ),
    );
  }

  function buildAgentAccessCard(): HTMLElement {
    const relevant = catalog
      .filter((entry) => entry.connected || entry.detected)
      .sort((a, b) => {
        if (a.connected !== b.connected) return a.connected ? -1 : 1;
        const ia = PRIMARY_AGENTS.indexOf(a.id);
        const ib = PRIMARY_AGENTS.indexOf(b.id);
        return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib);
      });

    const list = h("div", { class: "guardian-agent-list" });
    if (relevant.length === 0) {
      list.append(
        h(
          "div",
          { class: "guardian-empty" },
          h("strong", { text: loc("尚未检测到已接入的 Agent", "No desktop agents connected yet") }),
          h("span", { class: "note", text: loc("前往向导可一键检测或安装 Claude Code / Codex / WorkBuddy。", "Open Setup Wizard to connect or install agents.") }),
          button(loc("打开安装与接入向导", "Open Setup Wizard"), {
            small: true,
            variant: "primary",
            onClick: () => ctx.navigate("/console/hub"),
          }),
        ),
      );
    } else {
      for (const entry of relevant.slice(0, 7)) {
        const setting = agentSettings[entry.id];
        const pushOn = setting ? setting.push_to_phone !== false : entry.connected;
        const isBusy = busyAgents.has(entry.id);
        const isTesting = busyAgents.has(`test:${entry.id}`);
        const statusText = entry.connected
          ? entry.hook_support === "native" && !entry.hook_installed
            ? loc("已接入 · 建议装钩子", "Connected · Hook missing")
            : pushOn
              ? loc("已接入 · 推手机开启", "Connected · Push On")
              : loc("已接入 · 推手机静音", "Connected · Push Off")
          : loc("本机已检测到 · 未接入", "Detected · Not connected");

        list.append(
          h(
            "div",
            { class: "guardian-agent-row" },
            agentSquare(entry.id, false),
            h(
              "div",
              { class: "guardian-agent-info" },
              h("strong", { text: entry.name }),
              h("span", { class: "note", text: statusText }),
            ),
            entry.connected
              ? h(
                  "div",
                  { class: "guardian-agent-controls" },
                  button(isTesting ? "…" : loc("试推", "Test"), {
                    small: true,
                    variant: "quiet",
                    disabled: isTesting || isBusy,
                    onClick: () => void testAgentPush(entry),
                  }),
                  toggle({
                    on: pushOn,
                    disabled: isBusy,
                    title: loc("推送该 Agent 消息到手机", "Push this agent to phone"),
                    onChange: (next) => void toggleAgentPush(entry, next),
                  }),
                )
              : button(isBusy ? loc("接入中…", "Connecting…") : loc("一键接入", "Connect"), {
                  small: true,
                  variant: "primary",
                  disabled: isBusy,
                  onClick: () => void connectAgentQuick(entry),
                }),
          ),
        );
      }
    }

    return h(
      "section",
      { class: "card guardian-card" },
      h(
        "div",
        { class: "guardian-card-head" },
        h("div", { class: "guardian-card-icon" }, icon(icons.agents, 17)),
        h(
          "div",
          null,
          h("h2", { text: loc("① Agent 接入与推送开关", "1. Agent Access & Push") }),
          h("span", { class: "note", text: loc("勾选要转发到手机的桌面 Agent", "Choose which desktop agents push to your phone") }),
        ),
      ),
      list,
      h(
        "div",
        { class: "guardian-card-foot" },
        button(loc("安装新 Agent / 配对向导", "Install / Setup Wizard"), {
          small: true,
          variant: "quiet",
          onClick: () => ctx.navigate("/console/hub"),
        }),
        button(loc("全部 57+ 目录", "All Agents"), {
          small: true,
          variant: "quiet",
          iconSvg: icons.chevronRight,
          onClick: () => ctx.navigate("/console/agents"),
        }),
      ),
    );
  }

  function buildPairingNetworkCard(): HTMLElement {
    const cloud = connection?.cloud;
    const directDial = cloud?.direct_dial !== false;
    const authRequired = Boolean(cloud?.auth_required);
    const latencyLabel = authRequired
      ? loc("未绑定 Relay Key", "Relay Key required")
      : cloud?.connected
        ? cloud.latency_ms
          ? `${cloud.latency_ms} ms`
          : loc("已连通", "Connected")
        : cloud?.configured
          ? loc("重连中…", "Reconnecting…")
          : loc("仅局域网", "LAN only");

    const relayBox = h(
      "div",
      { class: "guardian-subbox" },
      h(
        "div",
        { class: "guardian-kv-row" },
        h(
          "div",
          null,
          h("strong", {
            text: cloud?.configured
              ? loc("跨网端到端加密中继", "E2E Encrypted Cloud Relay")
              : loc("局域网直连模式", "LAN Direct Mode"),
          }),
          h("div", {
            class: "note mono",
            text: cloud?.configured ? `${cloud.server} · ${latencyLabel}` : `127.0.0.1:${connection?.port || 18787}`,
          }),
        ),
        pill(
          authRequired
            ? loc("需要 Relay Key", "Relay Key Required")
            : cloud?.connected
              ? latencyLabel
              : cloud?.configured
                ? loc("重连中", "Retrying")
                : "LAN",
          authRequired ? "warn" : cloud?.connected ? "ok" : cloud?.configured ? "warn" : "mute",
        ),
      ),
      authRequired
        ? h(
            "div",
            {
              style:
                "margin-top:8px;padding:8px 10px;border-radius:8px;background:rgba(251,191,36,0.12);color:var(--amber, #fbbf24);font-size:12.5px;display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap",
            },
            h("span", {
              style: "flex:1;min-width:180px",
              text:
                cloud?.relay_notice ||
                loc("当前官方中继需要绑定 Relay Key，否则手机端无法通过中继连接", "Official relay requires a Relay Key before phones can connect"),
            }),
            button(loc("前往配置 Relay Key ›", "Configure Relay Key ›"), {
              small: true,
              variant: "quiet",
              onClick: () => ctx.navigate("/console/relay"),
            }),
          )
        : cloud?.relay_notice
          ? h("div", {
              style:
                "margin-top:8px;padding:6px 10px;border-radius:8px;background:rgba(251,191,36,0.10);color:var(--amber, #fbbf24);font-size:12px",
              text: cloud.relay_notice,
            })
          : null,
      cloud?.configured
        ? h(
            "div",
            { class: "guardian-switch-row" },
            h(
              "div",
              null,
              h("span", { text: loc("中继直连（绕过本机海外代理）", "Direct Relay Dial (Bypass Proxy)") }),
              h("small", {
                class: "note",
                text: loc("避免 Clash/HTTP_PROXY 拖慢国内中继往返", "Prevents local VPN/proxy from adding extra overseas hops"),
              }),
            ),
            toggle({
              on: directDial,
              title: loc("绕过系统代理直连中继", "Bypass system proxy for relay"),
              onChange: (next) => void toggleDirectDial(next),
            }),
          )
        : null,
    );

    const deviceList = h("div", { class: "guardian-device-list" });
    if (devices.length > 0) {
      for (const dev of devices.slice(0, 4)) {
        deviceList.append(
          h(
            "div",
            { class: "guardian-device-row" },
            h("span", { class: dev.online ? "dot dot-ok" : "dot" }),
            h(
              "div",
              { class: "guardian-device-info" },
              h("strong", { text: dev.name || loc("未命名手机", "Phone") }),
              h("span", {
                class: "note",
                text: `${dev.transport === "e2e" ? loc("端到端加密", "E2E") : "LAN"} · ${dev.online ? loc("当前在线", "Online") : dev.last_seen_at ? timeAgo(dev.last_seen_at) : loc("离线", "Offline")}`,
              }),
            ),
          ),
        );
      }
    }

    const pairSection = h("div", { class: "guardian-pair-wrap" });
    if (pairOpen) {
      pairSection.append(ensurePairCard().el);
    }

    return h(
      "section",
      { class: "card guardian-card" },
      h(
        "div",
        { class: "guardian-card-head" },
        h("div", { class: "guardian-card-icon" }, icon(icons.phone, 17)),
        h(
          "div",
          null,
          h("h2", { text: loc("② 手机配对与移动网络", "2. Phone Pairing & Relay") }),
          h("span", { class: "note", text: loc("中国大陆蜂窝网络零配网直连", "Zero-VPN mobile connectivity") }),
        ),
      ),
      relayBox,
      devices.length > 0 ? deviceList : null,
      pairSection,
      h(
        "div",
        { class: "guardian-card-foot" },
        button(
          pairOpen
            ? loc("收起配对二维码", "Hide Pairing QR")
            : devices.length > 0
              ? loc("＋ 配对新手机", "+ Pair Another Phone")
              : loc("显示配对二维码", "Show Pairing QR"),
          {
            small: true,
            variant: devices.length === 0 && !pairOpen ? "primary" : "quiet",
            iconSvg: icons.phone,
            onClick: () => {
              pairOpen = !pairOpen;
              paint();
            },
          },
        ),
        button(loc("设备与中继设置", "Devices & Relay"), {
          small: true,
          variant: "quiet",
          iconSvg: icons.chevronRight,
          onClick: () => ctx.navigate(devices.length > 0 ? "/console/devices" : "/console/relay"),
        }),
      ),
    );
  }

  function buildGuardianSleepCard(): HTMLElement {
    const awayOn = Boolean(home?.away.enabled);
    const paused = Boolean(stats?.pause.paused);
    const ka = keepAwake;
    const maxMins = ka?.max_minutes || 240;
    const durationValue = maxMins <= 60 ? "60" : maxMins <= 240 ? "240" : "480";
    const osName = connection?.platform === "Windows" ? "Windows" : connection?.platform === "Mac" ? "Mac" : loc("电脑", "Computer");
    const osNameEn = connection?.platform === "Windows" ? "Windows" : connection?.platform === "Mac" ? "Mac" : "Computer";

    return h(
      "section",
      { class: "card guardian-card" },
      h(
        "div",
        { class: "guardian-card-head" },
        h("div", { class: "guardian-card-icon" }, icon(icons.shield, 17)),
        h(
          "div",
          null,
          h("h2", { text: loc("③ 离席防休眠与消息策略", "3. Keep-Awake & Alerts") }),
          h("span", { class: "note", text: loc("防止离开电脑后休眠断联", `Prevent ${osNameEn} idle sleep from dropping sessions`) }),
        ),
      ),
      h(
        "div",
        { class: "guardian-switches" },
        h(
          "div",
          { class: "guardian-switch-row" },
          h(
            "div",
            null,
            h("strong", { text: loc("离开模式（Away Mode）", "Away Mode") }),
            h("small", {
              class: "note",
              text: loc("开启后，任务完成也会主动推送提醒到手机", "Upgrade task completions to active phone alerts"),
            }),
          ),
          toggle({
            on: awayOn,
            title: loc("切换离开模式", "Toggle Away Mode"),
            onChange: (next) => void toggleAway(next),
          }),
        ),
        h(
          "div",
          { class: "guardian-switch-row" },
          h(
            "div",
            null,
            h("strong", { text: loc("离开模式自动防休眠", "Auto Keep-Awake While Away") }),
            h("small", {
              class: "note",
              text: loc("进入离开模式时自动持有系统防空闲睡眠锁（Mac caffeinate / Windows ExecutionState）", "Hold OS idle-sleep assertion automatically while away"),
            }),
          ),
          toggle({
            on: ka ? ka.auto_on_away : true,
            title: loc("离开模式自动防休眠", "Auto Keep-Awake on Away"),
            onChange: (next) => void patchKeepAwake({ auto_on_away: next }),
          }),
        ),
        h(
          "div",
          { class: "guardian-switch-row" },
          h(
            "div",
            null,
            h("strong", {
              text: ka?.active
                ? loc(`立即防止 ${osName} 休眠（生效中）`, `Keep ${osNameEn} Awake Now (Active)`)
                : loc(`立即防止 ${osName} 休眠`, `Keep ${osNameEn} Awake Now`),
            }),
            h("small", {
              class: "note",
              text: ka?.active && ka.until
                ? loc(`屏幕可正常熄灭省电，系统保持唤醒至 ${new Date(ka.until).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`, `System stays awake until ${new Date(ka.until).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`)
                : loc("允许显示器熄屏，仅阻止后台与网络休眠（注意：笔记本合盖仍需外接电源）", "Allows display sleep while keeping agents & network alive"),
            }),
          ),
          toggle({
            on: Boolean(ka?.enabled || (ka?.active && ka?.reason === "manual")),
            title: loc(`立即防止 ${osName} 休眠`, `Keep ${osNameEn} Awake Now`),
            onChange: (next) => void patchKeepAwake({ enabled: next }),
          }),
        ),
        h(
          "div",
          { class: "guardian-switch-row" },
          h("span", { class: "note", text: loc("单次防休眠上限", "Max Keep-Awake Duration") }),
          segmented(
            [
              { value: "60", label: loc("1 小时", "1h") },
              { value: "240", label: loc("4 小时", "4h") },
              { value: "480", label: loc("8 小时", "8h") },
            ],
            durationValue,
            (val) => void patchKeepAwake({ max_minutes: Number(val) }),
          ),
        ),
        h(
          "div",
          { class: "guardian-switch-row" },
          h(
            "div",
            null,
            h("strong", { text: loc("暂停手机推送（免打扰 60 分钟）", "Pause Phone Push (60m DND)") }),
            h("small", {
              class: "note",
              text: paused
                ? loc("当前已静音，手机仍可主动打开查看消息", "Push muted; messages remain readable in the app")
                : loc("坐在电脑前专注工作时可临时静音手机通知", "Silence phone alerts while working at your desk"),
            }),
          ),
          toggle({
            on: paused,
            title: loc("暂停手机推送", "Pause Phone Push"),
            onChange: (next) => void togglePausePush(next),
          }),
        ),
      ),
      h(
        "div",
        { class: "guardian-card-foot" },
        button(loc("分级通知细则", "Notification Rules"), {
          small: true,
          variant: "quiet",
          onClick: () => ctx.navigate("/console/notifications"),
        }),
        button(
          loc(
            `进阶会话工作台（${home?.counts.running || 0} 运行中）`,
            `Sessions Workbench (${home?.counts.running || 0} running)`,
          ),
          {
            small: true,
            variant: "quiet",
            iconSvg: icons.chevronRight,
            onClick: () => ctx.navigate("/console/sessions"),
          },
        ),
      ),
    );
  }

  function paint() {
    if (!home && !connection) return;
    paintVersionBanner();
    paintHeader();
    paintUrgent();
    clear(gridHost);
    gridHost.append(
      buildAgentAccessCard(),
      buildPairingNetworkCard(),
      buildGuardianSleepCard(),
    );
    clear(footHost);
    const runningCount = home?.counts.running || 0;
    const resumeCount = home?.resume.length || 0;
    if (runningCount > 0 || resumeCount > 0) {
      footHost.append(
        h(
          "div",
          { class: "guardian-workbench-strip" },
          icon(icons.chat, 15),
          h("span", {
            text: loc(
              `当前有 ${runningCount} 个会话运行中、${resumeCount} 个近期会话可继续。日常回复可在手机完成，需要大屏操作可打开进阶工作台。`,
              `${runningCount} running, ${resumeCount} recent session(s). Open the desktop workbench anytime for full-screen control.`,
            ),
          }),
          h("span", { class: "spacer" }),
          button(loc("打开会话工作台", "Open Sessions"), {
            small: true,
            variant: "quiet",
            onClick: () => ctx.navigate("/console/sessions"),
          }),
          button(loc("多 Agent 会议室", "Rooms"), {
            small: true,
            variant: "quiet",
            onClick: () => ctx.navigate("/console/rooms"),
          }),
        ),
      );
    }
  }

  async function load(fromPoll = false) {
    if (loading) return;
    if (fromPoll && (isContainerBusy(root) || pairOpen)) return;
    loading = true;
    try {
      const [
        nextHome,
        nextConn,
        nextCat,
        nextSettings,
        nextDevices,
        nextKeepAwake,
        nextStats,
      ] = await Promise.all([
        endpoints.home(),
        endpoints.connection().catch(() => null),
        endpoints.agentCatalog().catch(() => null),
        endpoints.agentSettings().catch(() => null),
        endpoints.devices().catch(() => null),
        endpoints.keepAwake().catch(() => null),
        endpoints.stats().catch(() => null),
      ]);
      if (disposed) return;
      home = nextHome;
      if (nextConn) connection = nextConn;
      if (nextCat) catalog = nextCat.agents || [];
      if (nextSettings) agentSettings = nextSettings.settings || {};
      if (nextDevices) devices = nextDevices.devices || [];
      if (nextKeepAwake) keepAwake = nextKeepAwake;
      if (nextStats) stats = nextStats;

      if (!pairAutoInitialized) {
        pairAutoInitialized = true;
        if (devices.length === 0) {
          pairOpen = true;
        }
      }

      clear(root);
      root.append(versionBannerHost, headerHost, urgentHost, gridHost, footHost);
      paint();
      if (!fromPoll) ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      if (err instanceof ApiError && err.status === 404) {
        const next = renderInbox(ctx);
        fallback = next;
        clear(root);
        root.append(next.el);
        return;
      }
      if (home) return;
      clear(root);
      root.append(
        h(
          "div",
          { class: "list-card" },
          h(
            "div",
            { class: "empty" },
            h("div", { text: loc("读取守护中心状态失败", "Failed to load guardian status") }),
            h("div", { class: "note", text: (err as Error).message }),
            button(t("common.retry"), { small: true, onClick: () => void load(false) }),
          ),
        ),
      );
    } finally {
      loading = false;
    }
  }

  clear(root);
  root.append(
    h(
      "div",
      { class: "list-card", style: "padding:18px" },
      h("div", { style: "display:flex;gap:9px;align-items:center" }, spinner(), h("span", { class: "muted", text: t("common.loading") })),
    ),
  );
  void load(false);
  const stop = poll(() => load(true), 8000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
      pairCard?.dispose();
      fallback?.dispose?.();
    },
  };
}
