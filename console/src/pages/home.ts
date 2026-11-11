// 使用 › 總覽：用 /api/home 的注意力模型排隊，而不是把所有事件攤成一張表。
// 寬屏三欄（等你處理 / 離開期間 / 運行中），窄屏按同一資料源疊成一列。

import { clear, h, icon } from "../lib/dom";
import {
  ApiError,
  endpoints,
  poll,
  type Home,
  type HomeAttention,
  type HomeReason,
  type SessionSummary,
} from "../lib/api";
import { AGENT_LABELS, duration, projectOf, timeAgo } from "../lib/format";
import { icons } from "../lib/icons";
import { approvalButtons } from "../lib/approval";
import {
  agentSquare,
  button,
  groupHead,
  pill,
  reportError,
  segmented,
  spinner,
  toast,
  toggle,
} from "../lib/ui";
import type { PageContext, PageResult } from "../main";
import { renderInbox } from "./inbox";

const REASON: Record<HomeReason, { label: string; tone: "ok" | "warn" | "bad" | "mute"; mark: string }> = {
  approval: { label: "請求授權", tone: "warn", mark: "盾" },
  needs_reply: { label: "等你回覆", tone: "warn", mark: "問" },
  error: { label: "運行出錯", tone: "bad", mark: "!" },
  collab_result: { label: "協作結果", tone: "ok", mark: "@" },
  done: { label: "已完成", tone: "ok", mark: "✓" },
  progress: { label: "有新進展", tone: "mute", mark: "·" },
};

function shortDuration(startValue: string, endValue?: string | null): string {
  const started = new Date(startValue).getTime();
  if (Number.isNaN(started)) return "—";
  const ended = endValue ? new Date(endValue).getTime() : Date.now();
  if (Number.isNaN(ended)) return "—";
  const seconds = Math.max(0, Math.floor((ended - started) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return hours > 0 ? `${hours} 小時 ${minutes} 分` : `${Math.max(1, minutes)} 分`;
}

export function renderHome(ctx: PageContext): PageResult {
  const root = h("div", { class: "home-page" });
  let disposed = false;
  let loading = false;
  let home: Home | null = null;
  let fallback: PageResult | null = null;
  let composer: { key: string; text: string } | null = null;

  const awayHost = h("div", { class: "home-away-host" });
  const headlineHost = h("div", { class: "home-headline" });
  const columnsHost = h("div", { class: "home-columns" });
  const lowerHost = h("div", { class: "home-lower" });
  root.append(awayHost, headlineHost, columnsHost, lowerHost);

  function openSession(sessionID: string) {
    ctx.navigate(`/console/sessions?session=${encodeURIComponent(sessionID)}`);
  }

  async function toggleAway(enabled: boolean) {
    try {
      await endpoints.setAway(enabled);
      toast(enabled ? "已進入離開模式" : "已結束離開模式", "ok");
      await load();
      ctx.refreshShell();
    } catch (err) {
      reportError(err, enabled ? "開啟離開模式失敗" : "結束離開模式失敗");
    }
  }

  async function clearAway(id: string) {
    try {
      await endpoints.clearAway(id);
      toast("已把這段離開期間標為看過", "ok");
      await load();
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "清除失敗");
    }
  }

  async function setNewActivity(scope: "away" | "unread") {
    try {
      await endpoints.setHomeNewActivity(scope);
      toast(scope === "unread" ? "新動態範圍：所有未讀" : "新動態範圍：離開期間", "ok");
      await load();
    } catch (err) {
      reportError(err, "保存設定失敗");
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
      ctx.refreshShell();
    } catch (err) {
      reportError(err, decision === "allow" ? "允許失敗" : "拒絕失敗");
    }
  }

  async function markSeen(item: HomeAttention) {
    try {
      await endpoints.patchSession(item.session.session_id, { read: true });
      await load();
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "標為已看失敗");
    }
  }

  async function send(item: HomeAttention, text: string) {
    const clean = text.trim();
    if (!clean) {
      toast("回覆內容是空的", "bad");
      return;
    }
    try {
      await endpoints.compose({
        session_id: item.session.session_id,
        source: item.session.source,
        cwd: item.session.cwd,
        text: clean,
      });
      composer = null;
      toast("已發出", "ok");
      await load();
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "發送失敗");
    }
  }

  function replyComposer(item: HomeAttention): HTMLElement {
    const key = `${item.session.session_id}:${item.event_id}`;
    const open = composer?.key === key;
    if (!open) return h("div");
    const input = h("textarea", {
      class: "input home-reply-input",
      rows: "3",
      placeholder: item.reason === "approval" ? "先寫一句說明，再允許或拒絕…" : "回覆這個會話，支援 @agent…",
    }) as HTMLTextAreaElement;
    input.value = composer?.text || "";
    input.addEventListener("input", () => {
      if (composer) composer.text = input.value;
    });
    window.setTimeout(() => input.focus(), 20);
    return h(
      "div",
      { class: "home-reply" },
      input,
      h(
        "div",
        { class: "home-reply-actions" },
        button("取消", {
          small: true,
          variant: "quiet",
          onClick: () => {
            composer = null;
            paint();
          },
        }),
        button("發送", {
          small: true,
          variant: "primary",
          iconSvg: icons.arrowRight,
          onClick: () => void send(item, input.value),
        }),
      ),
    );
  }

  function attentionCard(item: HomeAttention): HTMLElement {
    const meta = REASON[item.reason] || REASON.progress;
    const key = `${item.session.session_id}:${item.event_id}`;
    const actions = h("div", { class: "home-card-actions" });
    if (item.reason === "approval" && item.approval) {
      actions.append(
        approvalButtons(item.approval, (decision, optionId) =>
          void decideApproval(item, decision, optionId),
        ),
      );
    }
    if (item.reason !== "approval") {
      actions.append(
        button(composer?.key === key ? "收起" : "回覆", {
          small: true,
          variant: "ghost",
          onClick: () => {
            composer = composer?.key === key ? null : { key, text: "" };
            paint();
          },
        }),
      );
    }
    actions.append(
      button("查看", {
        small: true,
        variant: "quiet",
        iconSvg: icons.arrowRight,
        onClick: () => openSession(item.session.session_id),
      }),
    );

    return h(
      "article",
      { class: `home-attention-card reason-${item.reason}` },
      h(
        "div",
        { class: "home-card-head" },
        h("span", { class: `home-reason-mark ${meta.tone}`, text: meta.mark }),
        h(
          "div",
          { class: "home-card-title" },
          h("div", { class: "home-card-kicker" }, h("span", { text: meta.label }), item.since_away ? pill("離開期間", "warn") : null),
          h("h3", { text: item.headline || item.session.title || "未命名會話" }),
          h(
            "div",
            { class: "home-card-meta" },
            agentSquare(item.session.source, true),
            h("span", { text: AGENT_LABELS[item.session.source] || item.session.source }),
            h("span", { class: "sep", text: "·" }),
            h("span", { text: projectOf(item.session) || "未分組" }),
          ),
        ),
        h("span", { class: "home-card-time", text: timeAgo(item.at) }),
      ),
      item.summary ? h("p", { class: "home-card-summary", text: item.summary }) : null,
      item.approval ? h("pre", { class: "home-approval-command" }, h("code", { text: item.approval.command })) : null,
      item.collab
        ? h(
            "div",
            { class: "home-collab-line" },
            icon(icons.arrowRight, 13),
            h("span", {
              text: `${item.collab.children} 條協作線 · ${item.collab.running} 運行中${
                item.collab.needs_you ? ` · ${item.collab.needs_you} 等你` : ""
              }`,
            }),
            h("button", {
              class: "link-btn",
              type: "button",
              text: "查看協作",
              onClick: () => ctx.navigate(`/console/collab?parent=${encodeURIComponent(item.session.session_id)}`),
            }),
          )
        : null,
      h("div", { class: "home-card-foot" }, actions, h("span", { class: "spacer" }), h("button", {
        class: "link-btn",
        type: "button",
        text: "標為已看",
        onClick: () => void markSeen(item),
      })),
      replyComposer(item),
    );
  }

  function runningCard(session: SessionSummary): HTMLElement {
    return h(
      "button",
      {
        class: "home-running-row",
        type: "button",
        onClick: () => openSession(session.session_id),
      },
      agentSquare(session.source, true),
      h(
        "div",
        { class: "home-running-main" },
        h("strong", { text: session.title || "未命名會話" }),
        h("span", { text: `${AGENT_LABELS[session.source] || session.source} · ${projectOf(session) || "未分組"}` }),
      ),
      h("span", { class: "mono home-running-time", text: duration(session.running_since || session.last_ts) }),
      icon(icons.chevronRight, 14),
    );
  }

  function resumeRow(session: SessionSummary): HTMLElement {
    return h(
      "button",
      {
        class: "home-resume-row",
        type: "button",
        onClick: () => ctx.navigate(`/console/sessions?session=${encodeURIComponent(session.session_id)}`),
      },
      agentSquare(session.source, true),
      h("span", { class: "home-resume-title", text: session.title || "未命名會話" }),
      h("span", { class: "home-resume-project", text: projectOf(session) || "未分組" }),
      h("span", { class: "home-resume-time", text: timeAgo(session.last_ts) }),
      icon(icons.chevronRight, 13),
    );
  }

  function paintAway() {
    if (!home) return;
    clear(awayHost);
    const current = home.away.current || (home.away.enabled ? home.away.period : undefined);
    const period = home.away.period;
    if (home.away.enabled && current) {
      awayHost.append(
        h(
          "div",
          { class: "home-away-bar active" },
          h("span", { class: "home-away-dot" }),
          h(
            "div",
            { class: "home-away-copy" },
            h("strong", { text: `離開中 · 已 ${shortDuration(current.started_at)}` }),
            h("span", {
              text: `${home.counts.needs_you} 件事等你 · ${home.counts.away_new} 個會話有新動態`,
            }),
          ),
          h("span", { class: "spacer" }),
          toggle({ on: true, title: "結束離開模式", onChange: (next) => void toggleAway(next) }),
          button("結束", { small: true, variant: "primary", onClick: () => void toggleAway(false) }),
        ),
      );
      return;
    }
    if (period && !period.cleared_at) {
      awayHost.append(
        h(
          "div",
          { class: "home-away-bar returned" },
          icon(icons.clock, 16),
          h(
            "div",
            { class: "home-away-copy" },
            h("strong", {
              text: `你離開了 ${
                period.ended_at ? shortDuration(period.started_at, period.ended_at) : "一段時間"
              }：${home.counts.away_new} 個會話有新動態`,
            }),
            h("span", { text: "看完後可一次清掉這段期間的提醒" }),
          ),
          h("span", { class: "spacer" }),
          button("全部已看", { small: true, variant: "quiet", onClick: () => void clearAway(period.id) }),
        ),
      );
      return;
    }
    awayHost.append(
      h(
        "div",
        { class: "home-away-bar idle" },
        icon(icons.clock, 15),
        h("div", { class: "home-away-copy" }, h("span", { text: "離開模式關閉；授權、錯誤與協作結果仍會留在最上面。" })),
        h("span", { class: "spacer" }),
        button("進入離開模式", { small: true, onClick: () => void toggleAway(true) }),
      ),
    );
  }

  function paintHeadline() {
    const counts = home?.counts;
    clear(headlineHost);
    headlineHost.append(
      h(
        "div",
        { class: "home-title" },
        h("div", { class: "eyebrow", text: "注意力佇列" }),
        h("h1", { text: counts?.needs_you ? `${counts.needs_you} 件事等你` : "都處理完了" }),
        h("p", {
          text:
            home?.settings.new_activity === "unread"
              ? "目前顯示所有未讀；你可以在設定裡改回只看離開期間。"
              : "只把需要你決定的、離開期間的新動態留在最上面。",
        }),
      ),
      h(
        "div",
        { class: "home-summary-metrics" },
        h("div", null, h("strong", { text: String(counts?.needs_you || 0) }), h("span", { text: "等你處理" })),
        h("div", null, h("strong", { text: String(counts?.running || 0) }), h("span", { text: "運行中" })),
        h("div", null, h("strong", { text: String(counts?.away_new || 0) }), h("span", { text: "新動態" })),
        h("div", null, h("strong", { text: String(counts?.earlier || 0) }), h("span", { text: "較早會話" })),
      ),
    );
  }

  function paintColumns() {
    if (!home) return;
    clear(columnsHost);
    const needs = home.attention.filter((item) => ["approval", "needs_reply", "error"].includes(item.reason));
    const away = home.attention.filter((item) => !["approval", "needs_reply", "error"].includes(item.reason));

    const needsCol = h(
      "section",
      { class: "home-column home-column-needs" },
      groupHead("等你處理", needs.length),
      needs.length
        ? h("div", { class: "home-card-list" }, ...needs.map(attentionCard))
        : h("div", { class: "home-column-empty" }, h("strong", { text: "沒有待決事項" }), h("span", { text: "授權、提問和錯誤會出現在這裡。" })),
    );
    const awayCol = h(
      "section",
      { class: "home-column home-column-away" },
      groupHead(
        "離開期間的新動態",
        away.length,
        h("div", { class: "home-scope" }, segmented(
          [
            { value: "away", label: "離開期間" },
            { value: "unread", label: "所有未讀" },
          ],
          home.settings.new_activity,
          (next) => void setNewActivity(next),
        )),
      ),
      away.length
        ? h("div", { class: "home-card-list" }, ...away.map(attentionCard))
        : h("div", { class: "home-column-empty" }, h("strong", { text: "這段期間沒有新動態" }), h("span", { text: "開啟離開模式後，完成與協作結果會集中在這裡。" })),
    );
    const runningCol = h(
      "section",
      { class: "home-column home-column-running" },
      groupHead("運行中", home.running.length),
      home.running.length
        ? h("div", { class: "home-running-list" }, ...home.running.map(runningCard))
        : h("div", { class: "home-column-empty" }, h("strong", { text: "現在沒有任務在跑" }), h("span", { text: "從會話頁新建，或回覆一個舊會話。" })),
      button("開啟新會話", {
        variant: "quiet",
        small: true,
        iconSvg: icons.plus,
        onClick: () => ctx.navigate("/console/sessions?new=1"),
      }),
    );
    columnsHost.append(needsCol, awayCol, runningCol);
  }

  function paintLower() {
    if (!home) return;
    clear(lowerHost);
    const resume = h(
      "section",
      { class: "home-resume-panel" },
      groupHead("繼續工作", home.resume.length, button("全部會話", { small: true, variant: "quiet", onClick: () => ctx.navigate("/console/sessions") })),
      home.resume.length
        ? h("div", { class: "home-resume-list" }, ...home.resume.map(resumeRow))
        : h("div", { class: "home-column-empty" }, h("span", { text: "還沒有可繼續的會話。" })),
    );
    const earlier = h(
      "button",
      {
        class: "home-earlier-row",
        type: "button",
        onClick: () => ctx.navigate("/console/sessions"),
        disabled: !home.earlier.count,
      },
      h("span", { text: `較早的 ${home.earlier.count} 個會話` }),
      icon(icons.chevronRight, 14),
    );
    lowerHost.append(resume, earlier);
  }

  function paint() {
    if (!home) return;
    clear(root);
    root.append(awayHost, headlineHost, columnsHost, lowerHost);
    paintAway();
    paintHeadline();
    paintColumns();
    paintLower();
  }

  async function load() {
    if (loading) return;
    loading = true;
    try {
      const next = await endpoints.home();
      if (disposed) return;
      home = next;
      paint();
      ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      if (err instanceof ApiError && err.status === 404) {
        const next = renderInbox(ctx);
        fallback = next;
        clear(root);
        root.append(next.el);
        return;
      }
      clear(root);
      root.append(
        h(
          "div",
          { class: "list-card" },
          h("div", { class: "empty" }, h("div", { text: "讀取總覽失敗" }), h("div", { class: "note", text: (err as Error).message })),
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
      h("div", { style: "display:flex;gap:9px;align-items:center" }, spinner(), h("span", { class: "muted", text: "正在整理需要你處理的事…" })),
    ),
  );
  void load();
  const stop = poll(load, 8000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
      fallback?.dispose?.();
    },
  };
}
