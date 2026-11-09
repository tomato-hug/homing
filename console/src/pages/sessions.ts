// 使用 › 会话：agent 会话一览，按最近活动排序。

import { h, clear } from "../lib/dom";
import { endpoints, poll, type SessionSummary } from "../lib/api";
import { AGENT_LABELS, AGENT_INITIALS, timeAgo } from "../lib/format";
import { emptyState, listCard, reportError, tag } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

function statusTag(session: SessionSummary): HTMLElement | null {
  if (session.archived) return tag("已归档", "mute");
  if (session.muted) return tag("静音", "mute");
  if (session.read === false) return tag("未读", "ok");
  return null;
}

export function renderSessions(_ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  let sessions: SessionSummary[] = [];
  let filter = "";
  const searchInput = h("input", {
    class: "input",
    type: "search",
    placeholder: "搜标题、项目或路径…",
    style: "height:34px;margin-bottom:12px",
  });
  searchInput.addEventListener("input", () => {
    filter = (searchInput as HTMLInputElement).value.trim().toLowerCase();
    paint();
  });

  const host = h("div");

  function matches(session: SessionSummary): boolean {
    if (!filter) return true;
    const haystack = [session.title, session.project, session.cwd, session.session_id, session.model]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return haystack.includes(filter);
  }

  function paint() {
    clear(host);
    const list = sessions.filter(matches);
    if (sessions.length === 0) {
      host.append(h("div", { class: "list-card" }, emptyState("还没有会话", "agent 跑过一次之后，会话会出现在这里")));
      return;
    }
    if (list.length === 0) {
      host.append(h("div", { class: "list-card" }, emptyState("没有匹配的会话", "换个关键词试试")));
      return;
    }
    host.append(
      listCard(
        ...list.map((session) =>
          h(
            "div",
            { class: "lrow" },
            h("span", {
              class: "agent-square sm",
              text: AGENT_INITIALS[session.source] || session.source.slice(0, 1).toUpperCase(),
              title: AGENT_LABELS[session.source] || session.source,
              style: "margin-top:2px",
            }),
            h(
              "div",
              { class: "lrow-main" },
              h(
                "div",
                { class: "lrow-title" },
                h("span", { class: "txt", text: session.title || session.session_id.slice(0, 12) }),
                statusTag(session),
              ),
              h(
                "div",
                { class: "lrow-sub" },
                h("span", { class: "mono", text: `${AGENT_LABELS[session.source] || session.source} · ${session.project || session.cwd || "—"}` }),
                session.model ? ` · ${session.model}` : "",
                session.branch ? ` · ${session.branch}` : "",
              ),
            ),
            h("div", { class: "lrow-time", text: timeAgo(session.last_ts) }),
          ),
        ),
      ),
    );
  }

  async function load() {
    try {
      const payload = await endpoints.sessions(300);
      if (disposed) return;
      sessions = payload.sessions;
      paint();
    } catch (err) {
      if (disposed) return;
      clear(host);
      host.append(h("div", { class: "list-card" }, h("div", { class: "empty", text: (err as Error).message })));
      reportError(err, "读取会话失败");
    }
  }

  root.append(searchInput, host);
  clear(host);
  host.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取会话…" })));
  void load();
  const stop = poll(load, 15_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
