// 使用 › 協作：每條 @agent 子任務是一條可追蹤的線，而不是一條消失的通知。
// 上方泳道按父會話分組，下方保留完整表格，窄屏也能操作。

import { clear, h } from "../lib/dom";
import { endpoints, poll, type DispatchRecord, type SessionSummary } from "../lib/api";
import { AGENT_LABELS, duration, timeAgo, truncate } from "../lib/format";
import { isContainerBusy } from "../lib/guard";
import {
  agentSquare,
  button,
  emptyState,
  groupHead,
  reportError,
  segmented,
  spinner,
  tag,
  toast,
} from "../lib/ui";
import { loc, t } from "../i18n";
import type { PageContext, PageResult } from "../main";

function statusOf(record: DispatchRecord): { label: string; tone: "ok" | "run" | "warn" | "bad" | "mute" } {
  const map: Record<string, { label: string; tone: "ok" | "run" | "warn" | "bad" | "mute" }> = {
    starting: { label: loc("准备启动", "Starting"), tone: "run" },
    waiting_session: { label: loc("等待绑定", "Binding"), tone: "warn" },
    started: { label: loc("已启动", "Started"), tone: "run" },
    running: { label: loc("运行中", "Running"), tone: "run" },
    needs_you: { label: loc("等你处理", "Needs You"), tone: "warn" },
    completed: { label: loc("已完成", "Completed"), tone: "ok" },
    failed: { label: loc("失败", "Failed"), tone: "bad" },
    cancelled: { label: loc("已取消", "Cancelled"), tone: "mute" },
  };
  return map[record.status] || { label: record.status || t("common.unknown"), tone: "mute" };
}

function presetLabel(preset?: string): string {
  const map: Record<string, [string, string]> = {
    ask: ["咨询", "Ask"],
    do: ["动手", "Do"],
    review: ["审查", "Review"],
    handoff: ["交棒", "Handoff"],
  };
  const pair = map[preset || "ask"];
  return pair ? loc(pair[0], pair[1]) : preset || loc("咨询", "Ask");
}

function parentKey(record: DispatchRecord): string {
  return `${record.parent_source}:${record.parent_session_id}`;
}

export function renderCollab(ctx: PageContext, query: URLSearchParams): PageResult {
  const root = h("div", { class: "collab-page" });
  let disposed = false;
  let records: DispatchRecord[] = [];
  let sessions = new Map<string, SessionSummary>();
  let filter: "all" | "running" | "needs_you" | "completed" | "failed" = "all";
  let parentFilter = query.get("parent") || "";
  let reply: { id: string; kind: "reply" | "forward"; text: string; needsFocus: boolean } | null = null;
  let loading = false;
  let titlesLoading = false;

  const head = h("div", { class: "collab-page-head" });
  const filterHost = h("div", { class: "collab-filter" });
  const lanesHost = h("div", { class: "collab-lanes" });
  const tableHost = h("div", { class: "collab-table-host" });
  root.append(head, filterHost, lanesHost, tableHost);

  function titleFor(sessionID: string): string {
    return sessions.get(sessionID)?.title || sessionID || "未知父會話";
  }

  function closeReply() {
    reply = null;
    paint();
  }

  async function startReply(record: DispatchRecord, kind: "reply" | "forward") {
    reply = { id: record.dispatch_id, kind, text: "", needsFocus: true };
    paint();
  }

  async function sendReply(record: DispatchRecord) {
    if (!reply || reply.id !== record.dispatch_id) return;
    const text = reply.text.trim();
    if (!text) {
      toast("內容是空的", "bad");
      return;
    }
    try {
      const kind = reply.kind;
      if (kind === "forward") await endpoints.forwardDispatch(record.dispatch_id, text);
      else await endpoints.replyDispatch(record.dispatch_id, text);
      reply = null;
      toast(kind === "reply" ? "已追問子任務" : "已轉交給父會話", "ok");
      await load();
    } catch (err) {
      reportError(err, "送出失敗");
    }
  }

  async function cancel(record: DispatchRecord) {
    try {
      await endpoints.cancelDispatch(record.dispatch_id);
      toast("已取消協作任務", "ok");
      await load();
    } catch (err) {
      reportError(err, "取消失敗");
    }
  }

  function openSession(sessionID?: string) {
    if (!sessionID) return;
    ctx.navigate(`/console/sessions?session=${encodeURIComponent(sessionID)}`);
  }

  function replyEditor(record: DispatchRecord): HTMLElement {
    if (!reply || reply.id !== record.dispatch_id) return h("div");
    const input = h("textarea", {
      class: "input collab-reply-input",
      rows: "3",
      placeholder:
        reply.kind === "reply"
          ? loc(`追问 ${AGENT_LABELS[record.target_agent] || record.target_agent}（⌘Enter 发送）…`, `Follow up with ${AGENT_LABELS[record.target_agent] || record.target_agent} (⌘Enter)…`)
          : loc("编辑要转交给父会话的文字（⌘Enter 发送）…", "Edit text to forward to parent session (⌘Enter)…"),
    }) as HTMLTextAreaElement;
    input.value =
      reply.text ||
      (reply.kind === "forward" && record.result?.text
        ? `【@${record.target_agent}】\n${record.result.text}`
        : "");
    input.addEventListener("input", () => {
      if (reply) reply.text = input.value;
    });
    input.addEventListener("keydown", (event) => {
      const ke = event as KeyboardEvent;
      if (ke.isComposing) return;
      if (ke.key === "Enter" && (ke.metaKey || ke.ctrlKey)) {
        ke.preventDefault();
        void sendReply(record);
      }
    });
    if (reply.needsFocus) {
      reply.needsFocus = false;
      window.setTimeout(() => input.focus(), 20);
    }
    return h(
      "div",
      { class: "collab-reply" },
      input,
      h(
        "div",
        { class: "collab-reply-actions" },
        button(t("common.cancel"), { small: true, variant: "quiet", onClick: closeReply }),
        button(reply.kind === "reply" ? loc("送出追问", "Send Follow-up") : loc("确认转交", "Forward"), {
          small: true,
          variant: "primary",
          onClick: () => void sendReply(record),
        }),
      ),
    );
  }

  function laneAction(record: DispatchRecord): HTMLElement {
    const actions = h("div", { class: "collab-lane-actions" });
    if (record.status === "needs_you") {
      actions.append(button("處理", { small: true, variant: "primary", onClick: () => openSession(record.target_session_id) }));
    } else if (record.status === "completed") {
      if (record.result?.text) {
        actions.append(button("轉交", { small: true, variant: "primary", onClick: () => void startReply(record, "forward") }));
      }
      actions.append(button("追問", { small: true, onClick: () => void startReply(record, "reply") }));
      actions.append(button("打開", { small: true, variant: "quiet", onClick: () => openSession(record.target_session_id) }));
    } else if (record.status === "failed" || record.status === "cancelled") {
      actions.append(button("查看父會話", { small: true, variant: "quiet", onClick: () => openSession(record.parent_session_id) }));
    } else {
      actions.append(button("打開會話", { small: true, variant: "quiet", onClick: () => openSession(record.target_session_id) }));
      actions.append(button("取消", { small: true, variant: "danger", onClick: () => void cancel(record) }));
    }
    return actions;
  }

  function laneBar(record: DispatchRecord, base: number, span: number): HTMLElement {
    const start = new Date(record.created_at).getTime();
    const end = new Date(record.updated_at || record.created_at).getTime();
    const left = Math.max(0, Math.min(94, ((start - base) / span) * 100));
    const width = Math.max(3, Math.min(100 - left, ((end - start) / span) * 100));
    const status = statusOf(record);
    return h(
      "div",
      { class: `collab-lane-bar status-${record.status}`, title: `${record.target_agent} · ${status.label} · ${timeAgo(record.created_at)}` },
      h("span", { class: "collab-lane-bar-fill", style: `left:${left}%;width:${width}%` }),
      h("span", { class: "collab-lane-bar-label", text: `${AGENT_LABELS[record.target_agent] || record.target_agent} · ${status.label}` }),
    );
  }

  function lane(recordsForParent: DispatchRecord[]): HTMLElement {
    const sample = recordsForParent[0];
    const times = recordsForParent.flatMap((record) => [
      new Date(record.created_at).getTime(),
      new Date(record.updated_at || record.created_at).getTime(),
    ]);
    const base = Math.min(...times);
    const span = Math.max(60_000, Math.max(...times) - base);
    const children = h(
      "div",
      { class: "collab-lane-children" },
      ...recordsForParent.map((record) =>
        h(
          "div",
          { class: "collab-lane-row" },
          agentSquare(record.target_agent, true),
          h(
            "div",
            { class: "collab-lane-name" },
            h("strong", { text: AGENT_LABELS[record.target_agent] || record.target_agent }),
            h("span", { text: `${presetLabel(record.preset)} · ${truncate(record.text, 44)}` }),
          ),
          laneBar(record, base, span),
          tag(statusOf(record).label, statusOf(record).tone),
          laneAction(record),
          replyEditor(record),
        ),
      ),
    );
    return h(
      "section",
      { class: "collab-lane" },
      h(
        "div",
        { class: "collab-lane-head" },
        agentSquare(sample.parent_source, true),
        h(
          "div",
          { class: "collab-lane-parent" },
          h("strong", { text: titleFor(sample.parent_session_id) }),
          h("span", { text: `${AGENT_LABELS[sample.parent_source] || sample.parent_source} · ${recordsForParent.length} 條協作線` }),
        ),
        h("span", { class: "spacer" }),
        button("父會話", { small: true, variant: "quiet", onClick: () => openSession(sample.parent_session_id) }),
      ),
      children,
    );
  }

  function tableRow(record: DispatchRecord): HTMLElement {
    const status = statusOf(record);
    return h(
      "tr",
      null,
      h(
        "td",
        null,
        h(
          "button",
          {
            class: "link-btn",
            type: "button",
            text: titleFor(record.parent_session_id),
            onClick: () => openSession(record.parent_session_id),
          },
        ),
      ),
      h(
        "td",
        null,
        h("span", { class: "collab-agent-cell" }, agentSquare(record.target_agent, true), AGENT_LABELS[record.target_agent] || record.target_agent),
      ),
      h("td", null, tag(presetLabel(record.preset), "plain")),
      h("td", null, tag(status.label, status.tone)),
      h("td", { class: "mono", text: timeAgo(record.created_at) }),
      h("td", { class: "mono", text: duration(record.updated_at || record.created_at) }),
      h(
        "td",
        null,
        record.result?.files_changed?.length
          ? h("span", { text: `${record.result.files_changed.length} 個文件` })
          : record.message
            ? h("span", { class: "muted", text: truncate(record.message, 32) })
            : h("span", { class: "muted", text: "—" }),
      ),
      h("td", null, laneAction(record)),
    );
  }

  function filtered(): DispatchRecord[] {
    return records.filter((record) => {
      if (parentFilter && record.parent_session_id !== parentFilter) return false;
      if (filter === "running") {
        return ["starting", "waiting_session", "started", "running"].includes(record.status);
      }
      if (filter === "needs_you") return record.status === "needs_you";
      if (filter === "completed") return record.status === "completed";
      if (filter === "failed") return record.status === "failed" || record.status === "cancelled";
      return true;
    });
  }

  function paintHead() {
    clear(head);
    const running = records.filter((record) => ["starting", "waiting_session", "started", "running"].includes(record.status)).length;
    const needs = records.filter((record) => record.status === "needs_you").length;
    const completed = records.filter((record) => record.status === "completed").length;
    const metrics = h(
      "div",
      { class: "collab-metrics" },
      h("div", null, h("strong", { text: String(running) }), h("span", { text: "運行中" })),
      h("div", null, h("strong", { text: String(needs) }), h("span", { text: "等你" })),
      h("div", null, h("strong", { text: String(completed) }), h("span", { text: "已完成" })),
    );
    metrics.hidden = records.length === 0;
    head.append(
      h(
        "div",
        { class: "collab-copy" },
        h("h1", { text: "跨 Agent 協作" }),
        h("p", {
          text: "用 @agent 派工；子任務的狀態、結果與授權都會回到父會話。",
        }),
      ),
      metrics,
    );
  }

  function paintFilters() {
    clear(filterHost);
    filterHost.hidden = records.length === 0;
    if (!records.length) return;
    const options: Array<{ value: typeof filter; label: string }> = [
      { value: "all", label: "全部" },
      { value: "running", label: "運行中" },
      { value: "needs_you", label: "等你處理" },
      { value: "completed", label: "已完成" },
      { value: "failed", label: "失敗 / 取消" },
    ];
    filterHost.append(
      segmented(options, filter, (next) => {
        filter = next;
        paint();
      }),
    );
    if (parentFilter) {
      filterHost.append(
        h("button", {
          class: "link-btn",
          type: "button",
          text: `只看父會話 ${titleFor(parentFilter)} ×`,
          onClick: () => {
            parentFilter = "";
            ctx.navigate("/console/collab");
          },
        }),
      );
    }
  }

  function paintLanes(items: DispatchRecord[]) {
    clear(lanesHost);
    const groups = new Map<string, DispatchRecord[]>();
    for (const record of items) {
      const key = parentKey(record);
      const list = groups.get(key) || [];
      list.push(record);
      groups.set(key, list);
    }
    if (!groups.size) {
      const empty = emptyState("還沒有協作線", "在會話輸入框用 @agent 派一個任務；完成後會回到父會話。");
      empty.append(
        button("打開會話", {
          small: true,
          variant: "primary",
          onClick: () => ctx.navigate("/console/sessions"),
        }),
      );
      lanesHost.append(empty);
      return;
    }
    for (const group of groups.values()) {
      group.sort((a, b) => a.created_at.localeCompare(b.created_at));
      lanesHost.append(lane(group));
    }
  }

  function paintTable(items: DispatchRecord[]) {
    clear(tableHost);
    if (!items.length) return;
    const table = h(
      "table",
      { class: "table collab-table" },
      h(
        "thead",
        null,
        h(
          "tr",
          null,
          h("th", { text: "父會話" }),
          h("th", { text: "目標 Agent" }),
          h("th", { text: "預設" }),
          h("th", { text: "狀態" }),
          h("th", { text: "開始" }),
          h("th", { text: "耗時" }),
          h("th", { text: "結果" }),
          h("th", { text: "操作" }),
        ),
      ),
      h("tbody", null, ...items.map(tableRow)),
    );
    tableHost.append(h("div", { class: "group" }, groupHead("完整記錄", items.length), h("div", { class: "list-card collab-table-card" }, table)));
  }

  function paint() {
    paintHead();
    paintFilters();
    const items = filtered();
    paintLanes(items);
    paintTable(items);
  }

  async function loadTitles() {
    if (titlesLoading || records.length === 0) return;
    titlesLoading = true;
    try {
      const sessionPayload = await endpoints.sessions(500);
      if (disposed) return;
      sessions = new Map(sessionPayload.sessions.map((session) => [session.session_id, session]));
      paint();
    } catch {
      // 父會話標題是漸進增強；掃描失敗時仍用既有標題或 id 顯示協作線。
    } finally {
      titlesLoading = false;
    }
  }

  async function load(fromPoll = false) {
    if (loading) return;
    if (fromPoll && (isContainerBusy(root) || reply !== null)) return;
    loading = true;
    try {
      const dispatchPayload = await endpoints.dispatches();
      if (disposed) return;
      records = dispatchPayload.dispatches;
      paint();
      void loadTitles();
    } catch (err) {
      if (disposed) return;
      if (records.length > 0) return;
      clear(lanesHost);
      clear(tableHost);
      lanesHost.append(
        h(
          "div",
          { class: "list-card" },
          h("div", { class: "empty" }, h("div", { text: loc("读取协作记录失败", "Failed to load dispatches") }), h("div", { class: "note", text: (err as Error).message })),
        ),
      );
    } finally {
      loading = false;
    }
  }

  lanesHost.append(
    h(
      "div",
      { class: "list-card", style: "padding:18px" },
      h("div", { style: "display:flex;gap:9px;align-items:center" }, spinner(), h("span", { class: "muted", text: t("common.loading") })),
    ),
  );
  void load(false);
  const stop = poll(() => load(true), 6000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
