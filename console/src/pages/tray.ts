// 托盤面板：Wails 的窄版窗口，只保留立即決策與狀態，不套用主控制台側欄。

import QRCode from "qrcode";
import { clear, h, icon } from "../lib/dom";
import {
  ApiError,
  endpoints,
  poll,
  type ConnectionInfo,
  type Home,
  type HomeAttention,
  type PairOffer,
  type StatsPayload,
} from "../lib/api";
import { AGENT_LABELS, timeAgo } from "../lib/format";
import { icons } from "../lib/icons";
import { approvalButtons } from "../lib/approval";
import { agentSquare, button, reportError, spinner, toast, toggle } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

function openConsole(path: string) {
  window.open(path, "_blank", "noopener,noreferrer");
}

type WailsWindow = {
  Show?: () => Promise<void>;
  Focus?: () => Promise<void>;
};

type WailsRuntime = {
  Window?: {
    Get?: (name: string) => WailsWindow;
  };
};

async function openMainWindow() {
  try {
    const runtime = (window as unknown as { wails?: WailsRuntime }).wails;
    const main = runtime?.Window?.Get?.("main");
    if (main?.Show) {
      await main.Show();
      await main.Focus?.();
      return;
    }
  } catch {
    // Older Wails runtimes or a regular browser tab fall back to a new tab.
  }
  window.open("/console/", "_blank", "noopener,noreferrer");
}

const PAIR_IN_MAIN_WINDOW: boolean = true;

export function renderTray(ctx: PageContext): PageResult {
  const root = h("div", { class: "tray-panel" });
  let home: Home | null = null;
  let connection: ConnectionInfo | null = null;
  let stats: StatsPayload | null = null;
  let disposed = false;
  let loading = false;
  let reply: { eventId: number; text: string; sending: boolean } | null = null;
  let pairing: {
    open: boolean;
    offer: PairOffer | null;
    qr: string;
    loading: boolean;
    error: string;
  } = { open: false, offer: null, qr: "", loading: false, error: "" };
  let pairingPoll: number | null = null;

  async function load() {
    if (loading) return;
    loading = true;
    try {
      const [nextHome, nextConnection, nextStats] = await Promise.all([
        endpoints.home(),
        endpoints.connection(),
        endpoints.stats(),
      ]);
      if (disposed) return;
      home = nextHome;
      connection = nextConnection;
      stats = nextStats;
      paint();
      ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      clear(root);
      root.append(
        h(
          "div",
          { class: "tray-error" },
          icon(icons.warn, 22),
          h("strong", { text: "本機服務未回應" }),
          h("span", { text: err instanceof ApiError ? err.message : String(err) }),
          button("重試", { small: true, onClick: () => void load() }),
        ),
      );
    } finally {
      loading = false;
    }
  }

  async function setAway(enabled: boolean) {
    try {
      await endpoints.setAway(enabled);
      toast(enabled ? "已進入離開模式" : "已結束離開模式", "ok");
      await load();
    } catch (err) {
      reportError(err, "切換離開模式失敗");
    }
  }

  async function decideApproval(
    item: HomeAttention,
    decision: "allow" | "deny",
    optionId?: string,
  ) {
    try {
      await endpoints.resolveApproval(item.event_id, decision, optionId);
      toast(decision === "allow" ? "已允許" : "已拒絕", "ok");
      await load();
    } catch (err) {
      reportError(err, decision === "allow" ? "允許失敗" : "拒絕失敗");
    }
  }

  async function sendReply(item: HomeAttention) {
    if (!reply || reply.eventId !== item.event_id || reply.sending) return;
    const text = reply.text.trim();
    if (!text) {
      toast("回覆內容是空的", "bad");
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
      toast("已送出", "ok");
      await load();
    } catch (err) {
      if (reply) reply.sending = false;
      reportError(err, "回覆失敗");
      paint();
    }
  }

  async function togglePause() {
    try {
      if (stats?.pause.paused) await endpoints.resumePush();
      else await endpoints.pausePush(60);
      toast(stats?.pause.paused ? "已恢復推送" : "已暫停推送 60 分鐘", "ok");
      await load();
    } catch (err) {
      reportError(err, "切換推送失敗");
    }
  }

  function stopPairingPoll() {
    if (pairingPoll !== null) {
      window.clearInterval(pairingPoll);
      pairingPoll = null;
    }
  }

  function closePairing() {
    stopPairingPoll();
    pairing = { open: false, offer: null, qr: "", loading: false, error: "" };
    paint();
  }

  async function paintPairQr(offer: PairOffer): Promise<string> {
    if (!offer.qr_payload) return "";
    try {
      return await QRCode.toDataURL(offer.qr_payload, {
        errorCorrectionLevel: "M",
        margin: 1,
        width: 360,
        color: { dark: "#0f172a", light: "#f8fafc" },
      });
    } catch {
      return "";
    }
  }

  function startPairingPoll() {
    stopPairingPoll();
    pairingPoll = window.setInterval(() => void pollPairing(), 2000);
  }

  async function usePairOffer(offer: PairOffer, startPolling: boolean) {
    const qr = await paintPairQr(offer);
    if (disposed || !pairing.open) return;
    pairing = {
      ...pairing,
      offer,
      qr,
      loading: false,
      error: "",
    };
    paint();
    if (startPolling && offer.status === "waiting") startPairingPoll();
  }

  async function newPairOffer() {
    pairing = { ...pairing, loading: true, error: "", qr: "" };
    paint();
    try {
      const offer = await endpoints.newOffer();
      await usePairOffer(offer, true);
    } catch (err) {
      if (disposed) return;
      pairing = {
        ...pairing,
        loading: false,
        offer: null,
        qr: "",
        error: err instanceof ApiError ? err.message : String(err),
      };
      paint();
    }
  }

  async function openPairing() {
    // 托盘面板太窄：配对（二维码 + 核对四个校验词）在主窗口首页的手机卡上完成。
    // 旧的 /api/pair/offer 二维码 App 不认，下面的旧流程保留给没有主窗口的场景。
    if (PAIR_IN_MAIN_WINDOW) {
      await openMainWindow();
      return;
    }
    pairing = { open: true, offer: null, qr: "", loading: true, error: "" };
    paint();
    try {
      const latest = await endpoints.latestOffer();
      if (disposed || !pairing.open) return;
      if (latest.offer && latest.offer.status === "waiting") {
        await usePairOffer(latest.offer, true);
        return;
      }
    } catch {
      // A fresh offer below gives a clearer result than showing a stale error.
    }
    if (!disposed && pairing.open) await newPairOffer();
  }

  async function pollPairing() {
    const current = pairing.offer;
    if (!pairing.open || !current || current.status !== "waiting") {
      stopPairingPoll();
      return;
    }
    try {
      const fresh = await endpoints.offer(current.offer_id);
      if (disposed || !pairing.open || pairing.offer?.offer_id !== current.offer_id) return;
      const justClaimed = fresh.status === "claimed";
      pairing = { ...pairing, offer: fresh, error: "" };
      if (fresh.status === "claimed" || fresh.status === "expired") stopPairingPoll();
      paint();
      if (justClaimed) {
        toast("手机已配对", "ok");
        void load();
      }
    } catch (err) {
      if (disposed || !pairing.open) return;
      pairing = {
        ...pairing,
        error: err instanceof ApiError ? err.message : String(err),
      };
      paint();
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
              ? `${connection?.host_name || "本機"} · 連接埠 ${connection?.port || 18787}`
              : "正在連線…",
          }),
        ),
        h("span", { class: online ? "tray-live on" : "tray-live", title: online ? "已連線" : "離線" }),
      ),
      h(
        "div",
        { class: "tray-away" },
        h("span", { text: away ? "離開模式已開啟" : "離開模式" }),
        toggle({ on: away, title: away ? "結束離開模式" : "進入離開模式", onChange: (next) => void setAway(next) }),
      ),
    );
  }

  function replyBox(item: HomeAttention): HTMLElement {
    if (!reply || reply.eventId !== item.event_id) return h("div");
    const input = h("textarea", {
      class: "input tray-reply-input",
      rows: "2",
      placeholder: "回覆這個會話…",
    }) as HTMLTextAreaElement;
    input.value = reply.text;
    input.addEventListener("input", () => {
      if (reply) reply.text = input.value;
    });
    window.setTimeout(() => input.focus(), 20);
    return h(
      "div",
      { class: "tray-reply" },
      input,
      h(
        "div",
        { class: "tray-reply-actions" },
        button("取消", {
          small: true,
          variant: "quiet",
          onClick: () => {
            reply = null;
            paint();
          },
        }),
        button(reply.sending ? "送出中…" : "送出", {
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
          h("strong", { text: item.headline || item.session.title || "未命名會話" }),
          h("span", {
            text: `${AGENT_LABELS[item.session.source] || item.session.source} · ${timeAgo(item.at)}`,
          }),
        ),
        item.since_away ? h("span", { class: "tag tag-warn", text: "新" }) : null,
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
          ? button(reply?.eventId === item.event_id ? "收起" : "回覆", {
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
        button("打開", {
          small: true,
          variant: "quiet",
          iconSvg: icons.arrowRight,
          onClick: () =>
            openConsole(`/console/sessions?session=${encodeURIComponent(item.session.session_id)}`),
        }),
      ),
      replyBox(item),
    );
  }

  function pairingModal(): HTMLElement {
    const offer = pairing.offer;
    const waiting = offer?.status === "waiting";
    const claimed = offer?.status === "claimed";
    const expired = offer?.status === "expired";
    let expiry = "";
    if (waiting && offer) {
      const left = Math.max(0, Math.round((new Date(offer.expires_at).getTime() - Date.now()) / 1000));
      expiry = left > 0
        ? `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} 后失效`
        : "配对码已失效";
    }
    return h(
      "div",
      {
        class: "tray-pair-backdrop",
        role: "dialog",
        "aria-modal": "true",
        "aria-label": "配对手机",
      },
      h(
        "section",
        { class: "tray-pair-card" },
        h(
          "header",
          { class: "tray-pair-head" },
          h(
            "div",
            null,
            h("span", { class: "eyebrow", text: "PAIR PHONE" }),
            h("h2", { text: "配对手机" }),
          ),
          button("关闭", {
            small: true,
            variant: "quiet",
            onClick: closePairing,
          }),
        ),
        h("p", {
          class: "tray-pair-copy",
          text: "打开 Homing App，进入「我的 › 添加电脑」，扫描二维码或输入 6 位配对码。",
        }),
        h(
          "div",
          { class: "tray-pair-qr" },
          pairing.loading
            ? h("div", { class: "tray-pair-qr-empty" }, spinner(), h("span", { text: "正在生成二维码…" }))
            : pairing.qr
              ? h("img", { src: pairing.qr, alt: "配对二维码", width: "192", height: "192" })
              : h("div", { class: "tray-pair-qr-empty", text: "二维码不可用，可直接输入下方配对码" }),
        ),
        offer
          ? h(
              "div",
              { class: "tray-pair-code-block" },
              h("div", { class: "tray-pair-code mono", text: offer.code.split("").join(" ") }),
              h("div", {
                class: waiting ? "tray-pair-status" : "tray-pair-status settled",
                text: claimed
                  ? "手机已配对"
                  : expired
                    ? "配对码已失效，请换一个"
                    : `等待手机扫码 · ${expiry}`,
              }),
            )
          : null,
        pairing.error ? h("div", { class: "tray-pair-error", text: pairing.error }) : null,
        h(
          "div",
          { class: "tray-pair-actions" },
          button(claimed ? "再配一台" : "换一个配对码", {
            small: true,
            iconSvg: icons.refresh,
            disabled: pairing.loading,
            onClick: () => void newPairOffer(),
          }),
          button("完整配对页", {
            small: true,
            variant: "quiet",
            iconSvg: icons.arrowRight,
            onClick: () => openConsole("/console/devices?pair=1"),
          }),
        ),
      ),
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
        h("div", null, h("strong", { text: String(counts?.needs_you || 0) }), h("span", { text: "等你" })),
        h("div", null, h("strong", { text: String(counts?.away_new || 0) }), h("span", { text: "新動態" })),
        h("div", null, h("strong", { text: String(counts?.running || 0) }), h("span", { text: "運行中" })),
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
          h("h2", { text: "等你處理" }),
          h("span", { text: String(counts?.needs_you || 0) }),
          h("span", { class: "spacer" }),
          h("button", {
            class: "link-btn",
            type: "button",
            text: "全部",
            onClick: () => openConsole("/console/"),
          }),
        ),
        attention.length
          ? h("div", { class: "tray-list" }, ...attention.map(attentionRow))
          : h("div", { class: "tray-empty" }, icon(icons.check, 17), h("span", { text: "現在沒有需要你決定的事" })),
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
            h("h2", { text: "運行中" }),
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
                    openConsole(`/console/sessions?session=${encodeURIComponent(session.session_id)}`),
                },
                agentSquare(session.source, true),
                h("span", { text: session.title || "未命名會話" }),
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
        button("打開主視窗", {
          small: true,
          variant: "primary",
          iconSvg: icons.overview,
          onClick: () => void openMainWindow(),
        }),
        button("配對手機", {
          small: true,
          iconSvg: icons.phone,
          onClick: () => void openPairing(),
        }),
        button(stats?.pause.paused ? "恢復推送" : "暫停推送", {
          small: true,
          iconSvg: stats?.pause.paused ? icons.bell : icons.pause,
          onClick: () => void togglePause(),
        }),
      ),
    );
    if (pairing.open) root.append(pairingModal());
  }

  clear(root);
  root.append(
    h(
      "div",
      { class: "tray-loading" },
      spinner(),
      h("span", { text: "正在讀取本機狀態…" }),
    ),
  );
  void load();
  const stopPoll = poll(() => {
    if (!disposed && !reply) void load();
  }, 5000);
  return {
    el: root,
    dispose: () => {
      disposed = true;
      stopPairingPoll();
      stopPoll();
    },
  };
}
