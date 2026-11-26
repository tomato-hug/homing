// 托盘面板：Wails 的窄版窗口，只保留立即决策与状态，不套用主控制台侧栏。

import { clear, h, icon } from "../lib/dom";
import {
  ApiError,
  endpoints,
  poll,
  type ConnectionInfo,
  type Home,
  type HomeAttention,
  type StatsPayload,
} from "../lib/api";
import { AGENT_LABELS, timeAgo } from "../lib/format";
import { icons } from "../lib/icons";
import { approvalButtons } from "../lib/approval";
import { isContainerBusy, isImeComposing } from "../lib/guard";
import { agentSquare, button, reportError, spinner, toast, toggle } from "../lib/ui";
import { loc, t } from "../i18n";
import type { PageContext, PageResult } from "../main";

type WailsWindow = {
  Show?: () => Promise<void>;
  Focus?: () => Promise<void>;
  SetURL?: (url: string) => Promise<void>;
};

type WailsRuntime = {
  Window?: {
    Get?: (name: string) => WailsWindow;
  };
};

async function openMainWindow(path = "/console/") {
  try {
    const res = await endpoints.openDesktopWindow(path);
    if (res?.handled_by_shell) {
      return;
    }
  } catch {
    // Fall back to direct runtime or browser navigation when daemon is older or unreachable.
  }
  try {
    const runtime = (window as unknown as { wails?: WailsRuntime }).wails;
    const main = runtime?.Window?.Get?.("main");
    if (main?.Show) {
      if (path && path !== "/console/" && main.SetURL) {
        await main.SetURL(path);
      }
      await main.Show();
      await main.Focus?.();
      return;
    }
  } catch {
    // Older Wails runtimes or a regular browser tab fall back to a new tab.
  }
  const opened = window.open(path, "_blank", "noopener,noreferrer");
  if (!opened) {
    window.location.assign(path);
  }
}

export function renderTray(ctx: PageContext): PageResult {
  const root = h("div", { class: "tray-panel" });
  let home: Home | null = null;
  let connection: ConnectionInfo | null = null;
  let stats: StatsPayload | null = null;
  let keepAwakeActive = false;
  let disposed = false;
  let loading = false;
  let reply: { eventId: number; text: string; sending: boolean } | null = null;

  async function load() {
    if (loading) return;
    loading = true;
    try {
      const [nextHome, nextConnection, nextStats, nextKeepAwake] = await Promise.all([
        endpoints.home(),
        endpoints.connection(),
        endpoints.stats(),
        endpoints.keepAwake().catch(() => null),
      ]);
      if (disposed) return;
      home = nextHome;
      connection = nextConnection;
      stats = nextStats;
      if (nextKeepAwake) keepAwakeActive = nextKeepAwake.active;
      paint();
      ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      if (home) return;
      clear(root);
      root.append(
        h(
          "div",
          { class: "tray-error" },
          icon(icons.warn, 22),
          h("strong", { text: loc("本机服务未响应", "Local service unreachable") }),
          h("span", { text: err instanceof ApiError ? err.message : String(err) }),
          button(t("common.retry"), { small: true, onClick: () => void load() }),
        ),
      );
    } finally {
      loading = false;
    }
  }

  async function setAway(enabled: boolean) {
    try {
      await endpoints.setAway(enabled);
      toast(
        enabled
          ? loc("已开启离席监听：新消息将推送至手机", "Away monitoring enabled")
          : loc("已关闭离席监听：回到电脑前工作", "Away monitoring disabled"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(err, loc("切换离开模式失败", "Failed to toggle away mode"));
    }
  }

  async function decideApproval(
    item: HomeAttention,
    decision: "allow" | "deny",
    optionId?: string,
  ) {
    try {
      await endpoints.resolveApproval(item.event_id, decision, optionId);
      toast(
        decision === "allow" ? loc("已允许", "Allowed") : loc("已拒绝", "Denied"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(
        err,
        decision === "allow" ? loc("允许失败", "Allow failed") : loc("拒绝失败", "Deny failed"),
      );
    }
  }

  async function sendReply(item: HomeAttention) {
    if (!reply || reply.eventId !== item.event_id || reply.sending) return;
    const text = reply.text.trim();
    if (!text) {
      toast(loc("回复内容是空的", "Reply cannot be empty"), "bad");
      return;
    }
    reply.sending = true;
    paint();
    try {
      await endpoints.reply({
        event_id: item.event_id || null,
        session_id: item.session.session_id,
        source: item.session.source,
        text,
      });
      reply = null;
      toast(loc("已发送", "Sent"), "ok");
      await load();
    } catch (err) {
      if (reply) reply.sending = false;
      reportError(err, loc("回复失败", "Reply failed"));
      paint();
    }
  }

  async function togglePause() {
    try {
      if (stats?.pause.paused) await endpoints.resumePush();
      else await endpoints.pausePush(60);
      toast(
        stats?.pause.paused
          ? loc("已恢复推送", "Push notifications resumed")
          : loc("已暂停推送 60 分钟", "Push paused for 60 minutes"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(err, loc("切换推送失败", "Failed to toggle push"));
    }
  }

  async function toggleKeepAwake() {
    try {
      const next = await endpoints.updateKeepAwake({ enabled: !keepAwakeActive });
      keepAwakeActive = next.active;
      toast(
        next.active
          ? loc("已开启防止电脑休眠", "Keep-Awake enabled")
          : loc("已关闭手动防休眠", "Manual Keep-Awake disabled"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(err, loc("切换防休眠失败", "Failed to toggle Keep-Awake"));
    }
  }

  function header(): HTMLElement {
    const online = Boolean(home && connection);
    const away = Boolean(home?.away.enabled);
    return h(
      "header",
      { class: "tray-head" },
      h(
        "div",
        { class: "tray-brand" },
        h("div", { class: "tray-mark" }, icon(icons.overview, 17)),
        h(
          "div",
          null,
          h("strong", { text: "Homing" }),
          h("span", {
            text: online
              ? loc(
                  `${connection?.host_name || "本机"} · 端口 ${connection?.port || 18787}`,
                  `${connection?.host_name || "Local"} · Port ${connection?.port || 18787}`,
                )
              : loc("正在连接…", "Connecting…"),
          }),
        ),
        h("span", {
          class: online ? "tray-live on" : "tray-live",
          title: online ? loc("已连接", "Connected") : loc("离线", "Offline"),
        }),
      ),
      h(
        "div",
        { class: "tray-away" },
        h("span", {
          text: away ? loc("离席监听中（推送手机）", "Monitoring On") : loc("离席监听（已关闭）", "Monitoring Off"),
        }),
        toggle({
          on: away,
          title: away ? loc("关闭离席监听（回到电脑）", "Turn off away monitoring") : loc("开启离席监听（离开电脑）", "Turn on away monitoring"),
          onChange: (next) => void setAway(next),
        }),
      ),
    );
  }

  function replyBox(item: HomeAttention): HTMLElement {
    if (!reply || reply.eventId !== item.event_id) return h("div");
    const input = h("textarea", {
      class: "input tray-reply-input",
      rows: "2",
      placeholder: loc("回复这个会话…（⌘Enter 发送）", "Reply to this session… (⌘Enter to send)"),
    }) as HTMLTextAreaElement;
    input.value = reply.text;
    input.addEventListener("input", () => {
      if (reply) reply.text = input.value;
    });
    input.addEventListener("keydown", (event) => {
      if (event.isComposing || isImeComposing()) return;
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
        event.preventDefault();
        void sendReply(item);
      } else if (event.key === "Escape") {
        event.preventDefault();
        reply = null;
        paint();
      }
    });
    window.setTimeout(() => input.focus(), 20);
    return h(
      "div",
      { class: "tray-reply" },
      input,
      h(
        "div",
        { class: "tray-reply-actions" },
        button(t("common.cancel"), {
          small: true,
          variant: "quiet",
          onClick: () => {
            reply = null;
            paint();
          },
        }),
        button(reply.sending ? loc("发送中…", "Sending…") : t("common.send"), {
          small: true,
          variant: "primary",
          disabled: reply.sending,
          onClick: () => void sendReply(item),
        }),
      ),
    );
  }

  function attentionRow(item: HomeAttention): HTMLElement {
    const approval = item.reason === "approval";
    const canReply = item.reason === "needs_reply" || item.reason === "error";
    return h(
      "article",
      { class: `tray-attention reason-${item.reason}` },
      h(
        "div",
        { class: "tray-item-head" },
        agentSquare(item.session.source, true),
        h(
          "div",
          { class: "tray-item-title" },
          h("strong", { text: item.headline || item.session.title || loc("未命名会话", "Untitled session") }),
          h("span", {
            text: `${AGENT_LABELS[item.session.source] || item.session.source} · ${timeAgo(item.at)}`,
          }),
        ),
        item.since_away ? h("span", { class: "tag tag-warn", text: loc("新", "NEW") }) : null,
      ),
      item.summary ? h("p", { class: "tray-item-summary", text: item.summary }) : null,
      approval && item.approval
        ? h("pre", { class: "tray-approval" }, h("code", { text: item.approval.command }))
        : null,
      h(
        "div",
        { class: "tray-item-actions" },
        approval
          ? approvalButtons(item.approval, (decision, optionId) =>
              void decideApproval(item, decision, optionId),
            )
          : null,
        canReply
          ? button(reply?.eventId === item.event_id ? loc("收起", "Hide") : loc("回复", "Reply"), {
              small: true,
              variant: "ghost",
              onClick: () => {
                reply =
                  reply?.eventId === item.event_id
                    ? null
                    : { eventId: item.event_id, text: "", sending: false };
                paint();
              },
            })
          : null,
        h("span", { class: "spacer" }),
        button(t("common.open"), {
          small: true,
          variant: "quiet",
          iconSvg: icons.arrowRight,
          onClick: () =>
            void openMainWindow(`/console/sessions?session=${encodeURIComponent(item.session.session_id)}`),
        }),
      ),
      replyBox(item),
    );
  }

  function paint() {
    clear(root);
    root.append(header());
    const counts = home?.counts;
    root.append(
      h(
        "div",
        { class: "tray-counts" },
        h("div", null, h("strong", { text: String(counts?.needs_you || 0) }), h("span", { text: loc("等你", "Waiting") })),
        h("div", null, h("strong", { text: String(counts?.away_new || 0) }), h("span", { text: loc("新动态", "Away new") })),
        h("div", null, h("strong", { text: String(counts?.running || 0) }), h("span", { text: loc("运行中", "Running") })),
      ),
    );

    const attention = home?.attention.slice(0, 5) || [];
    root.append(
      h(
        "section",
        { class: "tray-section" },
        h(
          "div",
          { class: "tray-section-head" },
          h("h2", { text: loc("等你处理", "Needs you") }),
          h("span", { text: String(counts?.needs_you || 0) }),
          h("span", { class: "spacer" }),
          h("button", {
            class: "link-btn",
            type: "button",
            text: loc("全部", "All"),
            onClick: () => void openMainWindow("/console/"),
          }),
        ),
        attention.length
          ? h("div", { class: "tray-list" }, ...attention.map(attentionRow))
          : h("div", { class: "tray-empty" }, icon(icons.check, 17), h("span", { text: loc("现在没有需要你决定的事", "Nothing waiting for your decision") })),
      ),
    );

    const running = home?.running.slice(0, 3) || [];
    if (running.length) {
      root.append(
        h(
          "section",
          { class: "tray-section tray-running" },
          h(
            "div",
            { class: "tray-section-head" },
            h("h2", { text: loc("运行中", "Running") }),
            h("span", { text: String(counts?.running || 0) }),
          ),
          h(
            "div",
            { class: "tray-running-list" },
            ...running.map((session) =>
              h(
                "button",
                {
                  class: "tray-running-row",
                  type: "button",
                  onClick: () =>
                    void openMainWindow(`/console/sessions?session=${encodeURIComponent(session.session_id)}`),
                },
                agentSquare(session.source, true),
                h("span", { text: session.title || loc("未命名会话", "Untitled session") }),
                icon(icons.chevronRight, 12),
              ),
            ),
          ),
        ),
      );
    }

    root.append(
      h(
        "footer",
        { class: "tray-foot" },
        button(loc("打开主窗口", "Open Console"), {
          small: true,
          variant: "primary",
          iconSvg: icons.overview,
          onClick: () => void openMainWindow("/console/"),
        }),
        button(loc("配对手机", "Pair Phone"), {
          small: true,
          iconSvg: icons.phone,
          onClick: () => void openMainWindow("/console/hub"),
        }),
        button(keepAwakeActive ? loc("防休眠中", "Awake On") : loc("防休眠", "Keep Awake"), {
          small: true,
          iconSvg: icons.bolt,
          onClick: () => void toggleKeepAwake(),
        }),
        button(stats?.pause.paused ? loc("恢复推送", "Resume") : loc("暂停推送", "Pause"), {
          small: true,
          iconSvg: stats?.pause.paused ? icons.bell : icons.pause,
          onClick: () => void togglePause(),
        }),
      ),
    );
  }

  clear(root);
  root.append(
    h(
      "div",
      { class: "tray-loading" },
      spinner(),
      h("span", { text: loc("正在读取本机状态…", "Loading local status…") }),
    ),
  );
  void load();
  const stopPoll = poll(() => {
    if (!disposed && !reply && !isContainerBusy(root)) void load();
  }, 5000);
  return {
    el: root,
    dispose: () => {
      disposed = true;
      stopPoll();
    },
  };
}
