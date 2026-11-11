// 使用 › 会话：Console 的三栏工作台。左栏按项目分组，中栏是会话行，
// 右栏直接读正文、处理授权、回复和 @ 派发。

import { append, clear, h, icon, type Child } from "../lib/dom";
import {
  ApiError,
  endpoints,
  type Agent,
  type ComposePlan,
  type DispatchRecord,
  type InboxItem,
  type SearchHit,
  type SessionCommand,
  type SessionItem,
  type SessionSummary,
  type UploadedAttachment,
} from "../lib/api";
import { AGENT_INITIALS, AGENT_LABELS, timeAgo } from "../lib/format";
import { agentLogo } from "../lib/agent-icons";
import { icons } from "../lib/icons";
import { approvalButtons } from "../lib/approval";
import { parseMentions } from "../lib/mentions";
import {
  button,
  emptyState,
  pill,
  reportError,
  tag,
  toast,
} from "../lib/ui";
import type { PageContext, PageResult } from "../main";

const PRESET_LABELS: Record<string, string> = {
  ask: "咨询",
  do: "动手",
  review: "审查",
  handoff: "交棒",
};

const DISPATCH_STATUS: Record<string, [string, "ok" | "warn" | "bad" | "mute"]> = {
  starting: ["准备启动", "warn"],
  waiting_session: ["等待会话绑定", "warn"],
  started: ["已启动", "ok"],
  running: ["运行中", "warn"],
  needs_you: ["等你处理", "warn"],
  completed: ["已完成", "ok"],
  failed: ["失败", "bad"],
  cancelled: ["已取消", "mute"],
};

const SESSION_RENDER_BATCH = 100;
const TRANSCRIPT_RENDER_BATCH = 200;
const SESSION_LIST_LIMIT = 1000;
const TRANSCRIPT_ITEM_LIMIT = 2000;

interface MentionEdit {
  target: "auto" | "new" | "continue";
  context: string[];
  workspace: "auto" | "shared" | "readonly" | "worktree";
}

function agentName(source: string): string {
  return AGENT_LABELS[source] || source || "未知 Agent";
}

function agentInitial(source: string): string {
  return AGENT_INITIALS[source] || (source || "?").slice(0, 1).toUpperCase();
}

function agentBadge(source: string, small = true): HTMLElement {
  const logo = agentLogo(source);
  const badge = h("span", {
    class: `agent-square${small ? " sm" : ""}${logo ? " logo" : ""}`,
    title: agentName(source),
  });
  if (logo) {
    if (logo.mono) badge.classList.add("mono");
    if (logo.color) badge.style.color = logo.color;
    badge.append(icon(logo.svg, small ? 18 : 20));
  } else {
    badge.textContent = agentInitial(source);
  }
  return badge;
}

function formatClock(value?: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return "0 B";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function formatCount(value: number | null | undefined): string {
  if (!value || value <= 0) return "0";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 10_000) return `${Math.round(value / 1000)}k`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`;
  return String(value);
}

function formatDateTime(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function projectKey(session: SessionSummary): string {
  if (session.is_temp) return "临时目录";
  return session.project || session.workspace || session.cwd || "未分组";
}

// A deliberately small, safe Markdown subset. Everything is created as text
// nodes; agent output is never interpreted as HTML.
function inlineMarkdown(text: string): Node[] {
  const nodes: Node[] = [];
  const pattern = /(`[^`\n]+`|\*\*[^*\n]+\*\*|https?:\/\/[^\s)]+)/g;
  let cursor = 0;
  for (const match of text.matchAll(pattern)) {
    const index = match.index ?? 0;
    if (index > cursor) nodes.push(document.createTextNode(text.slice(cursor, index)));
    const token = match[0];
    if (token.startsWith("`")) {
      nodes.push(h("code", { text: token.slice(1, -1) }));
    } else if (token.startsWith("**")) {
      nodes.push(h("strong", { text: token.slice(2, -2) }));
    } else {
      nodes.push(h("a", { href: token, target: "_blank", rel: "noopener noreferrer", text: token }));
    }
    cursor = index + token.length;
  }
  if (cursor < text.length) nodes.push(document.createTextNode(text.slice(cursor)));
  return nodes;
}

function renderMarkdown(text: string): HTMLElement {
  const root = h("div", { class: "md" });
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (line.startsWith("```")) {
      const language = line.slice(3).trim();
      const code: string[] = [];
      index++;
      while (index < lines.length && !lines[index].startsWith("```")) {
        code.push(lines[index]);
        index++;
      }
      if (index < lines.length) index++;
      root.append(
        h(
          "div",
          { class: "md-code" },
          language ? h("span", { class: "md-code-lang", text: language }) : null,
          h("pre", null, h("code", { text: code.join("\n") })),
        ),
      );
      continue;
    }
    if (!line.trim()) {
      index++;
      continue;
    }
    const heading = /^(#{1,3})\s+(.+)$/.exec(line);
    if (heading) {
      const level = Math.min(3, heading[1].length);
      const element = h(level === 1 ? "h2" : level === 2 ? "h3" : "h4");
      element.append(...inlineMarkdown(heading[2]));
      root.append(element);
      index++;
      continue;
    }
    if (/^\s*[-*]\s+/.test(line)) {
      const list = h("ul");
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index])) {
        const item = h("li");
        item.append(...inlineMarkdown(lines[index].replace(/^\s*[-*]\s+/, "")));
        list.append(item);
        index++;
      }
      root.append(list);
      continue;
    }
    if (/^\s*\d+[.)]\s+/.test(line)) {
      const list = h("ol");
      while (index < lines.length && /^\s*\d+[.)]\s+/.test(lines[index])) {
        const item = h("li");
        item.append(...inlineMarkdown(lines[index].replace(/^\s*\d+[.)]\s+/, "")));
        list.append(item);
        index++;
      }
      root.append(list);
      continue;
    }
    if (line.startsWith(">")) {
      const quote = h("blockquote");
      while (index < lines.length && lines[index].startsWith(">")) {
        const part = h("p");
        part.append(...inlineMarkdown(lines[index].replace(/^>\s?/, "")));
        quote.append(part);
        index++;
      }
      root.append(quote);
      continue;
    }
    const paragraph: string[] = [line];
    index++;
    while (
      index < lines.length &&
      lines[index].trim() &&
      !lines[index].startsWith("```") &&
      !/^(#{1,3})\s+/.test(lines[index]) &&
      !/^\s*[-*]\s+/.test(lines[index]) &&
      !/^\s*\d+[.)]\s+/.test(lines[index]) &&
      !lines[index].startsWith(">")
    ) {
      paragraph.push(lines[index]);
      index++;
    }
    const paragraphEl = h("p");
    paragraph.forEach((part, partIndex) => {
      if (partIndex > 0) paragraphEl.append(document.createElement("br"));
      paragraphEl.append(...inlineMarkdown(part));
    });
    root.append(paragraphEl);
  }
  return root;
}

function itemStatus(item: SessionItem): string {
  return item.tool?.status || (item.exit_code === 0 ? "ok" : item.exit_code ? "error" : "");
}

function transcriptItem(item: SessionItem, source = ""): HTMLElement {
  if (item.type === "message") {
    const user = item.role === "user";
    return h(
      "article",
      { class: user ? "transcript-message user" : "transcript-message assistant" },
      h(
        "div",
        { class: "transcript-who" },
        user
          ? h("span", { class: "agent-square sm me", text: "你" })
          : agentBadge(source || "agent"),
        h("strong", { text: user ? "你" : "Agent" }),
        item.model ? h("span", { class: "mono", text: item.model }) : null,
        h("div", { class: "spacer", style: "flex:1" }),
        h("span", { class: "mono", text: formatClock(item.ts) }),
      ),
      renderMarkdown(item.text || ""),
    );
  }
  if (item.type === "reasoning") {
    return h(
      "details",
      { class: "transcript-detail reasoning" },
      h("summary", null, h("span", { class: "dot dot-warn" }), h("span", { text: "思考过程" }), h("span", { class: "mono", text: `#${item.seq}` })),
      renderMarkdown(item.text || ""),
    );
  }
  if (item.type === "tool_call") {
    const status = itemStatus(item);
    return h(
      "details",
      { class: "transcript-detail tool" },
      h(
        "summary",
        null,
        icon(icons.terminal, 14),
        h("strong", { text: item.tool?.name || "工具调用" }),
        status
          ? tag(
              status === "error" || status === "failed" ? "失败" : status === "running" ? "运行中" : "完成",
              status === "error" || status === "failed" ? "bad" : status === "running" ? "warn" : "ok",
            )
          : null,
        h("span", { class: "mono", text: formatClock(item.ts) }),
      ),
      item.command ? h("pre", { class: "command" }, h("code", { text: item.command })) : null,
      item.tool?.input_summary ? h("div", { class: "tool-summary mono", text: item.tool.input_summary }) : null,
      item.tool?.output_tail ? h("pre", { class: "output" }, h("code", { text: item.tool.output_tail })) : null,
      item.tool?.exit_code !== null && item.tool?.exit_code !== undefined
        ? h("div", { class: "tool-exit mono", text: `exit ${item.tool.exit_code}` })
        : null,
    );
  }
  if (item.type === "file_change") {
    return h(
      "details",
      { class: "transcript-detail files" },
      h(
        "summary",
        null,
        icon(icons.fileText, 14),
        h("strong", { text: `文件改动 · ${item.files?.length || 0}` }),
        h("span", { class: "mono", text: formatClock(item.ts) }),
      ),
      ...(item.files || []).map((file) =>
        h(
          "section",
          { class: "file-change" },
          h(
            "div",
            { class: "file-change-head" },
            h("span", { class: "mono", text: file.path }),
            h("span", { class: "added", text: `+${file.added}` }),
            h("span", { class: "removed", text: `−${file.removed}` }),
          ),
          file.diff
            ? h(
                "pre",
                { class: "diff" },
                h(
                  "code",
                  null,
                  ...file.diff.split("\n").map((line) =>
                    h("span", {
                      class: line.startsWith("+") ? "diff-add" : line.startsWith("-") ? "diff-del" : "",
                      text: line,
                    }),
                  ),
                ),
              )
            : null,
        ),
      ),
    );
  }
  if (item.type === "plan") {
    return h(
      "details",
      { class: "transcript-detail plan", open: true },
      h("summary", null, icon(icons.check, 14), h("strong", { text: "计划" })),
      ...(item.plan || []).map((step) =>
        h(
          "div",
          { class: "plan-step" },
          h("span", { class: step.status === "completed" ? "plan-check done" : "plan-check" }, icon(icons.check, 10)),
          h("span", { text: step.step }),
        ),
      ),
    );
  }
  if (item.type === "error") {
    return h(
      "div",
      { class: "transcript-error" },
      icon(icons.warn, 15),
      h("div", null, h("strong", { text: "Agent 报错" }), h("div", { text: item.error || item.text || "未知错误" })),
    );
  }
  return h(
    "div",
    { class: "transcript-unknown" },
    h("span", { class: "mono", text: item.type }),
    h("span", { text: item.text || "" }),
  );
}

function itemKey(item: SessionItem, index = 0): string {
  return item.id || `${item.type}:${item.seq}:${index}`;
}

function compareSessionItems(a: SessionItem, b: SessionItem): number {
  if (a.seq > 0 && b.seq > 0 && a.seq !== b.seq) return a.seq - b.seq;
  if (!a.seq && b.seq) return 1;
  if (a.seq && !b.seq) return -1;
  return 0;
}

function mergeLiveItem(existing: SessionItem, incoming: SessionItem): SessionItem {
  const merged: SessionItem = { ...existing, ...incoming };
  if (!incoming.truncated) {
    if (!incoming.partial || (incoming.text || "").length >= (existing.text || "").length) {
      return incoming;
    }
    merged.text = existing.text;
    return merged;
  }
  merged.text =
    (existing.text || "").length > (incoming.text || "").length ? existing.text : incoming.text;
  merged.error = incoming.error || existing.error;
  merged.plan = incoming.plan?.length ? incoming.plan : existing.plan;
  merged.files = incoming.files?.length ? incoming.files : existing.files;
  if (existing.tool) {
    merged.tool = {
      ...existing.tool,
      ...(incoming.tool || {}),
      input_summary:
        (existing.tool.input_summary || "").length > (incoming.tool?.input_summary || "").length
          ? existing.tool.input_summary
          : incoming.tool?.input_summary || null,
      output_tail:
        (existing.tool.output_tail || "").length > (incoming.tool?.output_tail || "").length
          ? existing.tool.output_tail
          : incoming.tool?.output_tail || null,
    };
  }
  merged.truncated = existing.truncated;
  return merged;
}

export function renderSessions(_ctx: PageContext): PageResult {
  const root = h("div", { class: "workbench" });
  const listScrim = h("div", { class: "session-list-scrim", hidden: true });
  const left = h("aside", {
    id: "session-list-panel",
    class: "workbench-sidebar",
    "aria-label": "会话列表",
  });
  const center = h("section", { class: "workbench-list" });
  const main = h("section", { class: "workbench-main" });
  const drawerScrim = h("div", { class: "session-drawer-scrim", hidden: true });
  const drawerBody = h("div", { class: "session-drawer-body" });
  const drawerFoot = h("div", { class: "session-drawer-foot" });
  const drawerClose = h("button", {
    class: "icon-btn",
    type: "button",
    title: "收起会话信息",
    "aria-label": "收起会话信息",
  });
  drawerClose.append(icon(icons.close, 14));
  const drawer = h(
    "aside",
    {
      id: "session-info-drawer",
      class: "session-drawer",
      "aria-label": "会话信息",
      hidden: true,
    },
    h(
      "header",
      { class: "session-drawer-head" },
      h(
        "div",
        null,
        h("div", { class: "eyebrow", text: "SESSION" }),
        h("h2", { text: "会话信息" }),
      ),
      h("div", { class: "spacer", style: "flex:1" }),
      drawerClose,
    ),
    drawerBody,
    drawerFoot,
  );
  const newSessionOverlay = h("div", { class: "session-new-overlay", hidden: true });
  const newSessionModal = h("section", {
    class: "session-new-modal",
    role: "dialog",
    "aria-modal": "true",
    "aria-label": "新建会话",
    hidden: true,
  });
  root.append(listScrim, left, center, main, drawerScrim, drawer, newSessionOverlay, newSessionModal);

  let disposed = false;
  let sessions: SessionSummary[] = [];
  let agents: Agent[] = [];
  let selected: SessionSummary | null = null;
  let selectedItems: SessionItem[] = [];
  let selectedCommands: SessionCommand[] = [];
  let selectedDispatches: DispatchRecord[] = [];
  let selectedInbox: InboxItem[] = [];
  let search = "";
  let sourceFilter = "";
  let contentSearchQuery = "";
  let contentSearchHits: SearchHit[] = [];
  let contentSearchNotice = "";
  let contentSearching = false;
  let contentSearchRequest = 0;
  let pendingTranscriptItemId = "";
  let groupCollapsed = new Set<string>();
  let expandedOnce = false;
  let draft = "";
  let pendingAttachments: UploadedAttachment[] = [];
  let uploadingAttachments = false;
  let pendingPlan: ComposePlan | null = null;
  let mentionEdits: MentionEdit[] = [];
  let sending = false;
  let stream: EventSource | null = null;
  let itemTimer = 0;
  let loadToken = 0;
  let listPaintToken = 0;
  let transcriptPaintToken = 0;
  let transcriptPainted = false;
  const transcriptItemNodes = new Map<string, HTMLElement>();
  let headerHost: HTMLElement | null = null;
  let transcriptHost: HTMLElement | null = null;
  let collabHost: HTMLElement | null = null;
  let approvalHost: HTMLElement | null = null;
  let composerTextarea: HTMLTextAreaElement | null = null;
  let sendRowHost: HTMLElement | null = null;
  let previewHost: HTMLElement | null = null;
  let palettesHost: HTMLElement | null = null;
  let attachmentHost: HTMLElement | null = null;
  let attachmentInput: HTMLInputElement | null = null;
  let searchInputEl: HTMLInputElement | null = null;
  let newSessionOpen = false;
  let newSessionBusy = false;
  let newSessionUploading = false;
  let newSessionAgent = "";
  let newSessionCwd = "";
  let newSessionText = "";
  let newSessionModel = "";
  let newSessionEffort = "";
  let newSessionPermission = "";
  let newSessionError = "";
  let newSessionAttachments: UploadedAttachment[] = [];
  let newSessionReturnFocus: HTMLElement | null = null;
  let pendingNewSessionFocus = false;
  let replyModelOverride = "";
  let replyEffortOverride = "";
  let replyPermissionOverride = "";
  let drawerOpen = false;
  let listOpen = false;

  function currentFromURL(): string {
    return new URLSearchParams(location.search).get("session") || "";
  }

  function setURLSession(id: string) {
    const url = new URL(location.href);
    if (id) url.searchParams.set("session", id);
    else url.searchParams.delete("session");
    history.replaceState({}, "", url.toString());
  }

  function autoExpandFor(session: SessionSummary | null) {
    if (!session || expandedOnce) return;
    expandedOnce = true;
    const selectedKey = projectKey(session);
    for (const item of sessions) {
      if (projectKey(item) !== selectedKey) groupCollapsed.add(projectKey(item));
    }
    groupCollapsed.delete(selectedKey);
  }

  function visibleSessions(): SessionSummary[] {
    const needle = search.trim().toLowerCase();
    return sessions.filter((session) => {
      if (session.archived) return false;
      if (sourceFilter && session.source !== sourceFilter) return false;
      if (!needle) return true;
      return [session.title, session.session_id, session.project, session.cwd, session.model, session.branch]
        .filter(Boolean)
        .join(" ")
        .toLowerCase()
        .includes(needle);
    });
  }

  function clearContentSearch() {
    contentSearchQuery = "";
    contentSearchHits = [];
    contentSearchNotice = "";
    contentSearching = false;
    contentSearchRequest++;
  }

  async function searchSessionContent() {
    const query = search.trim();
    if (!query) {
      clearContentSearch();
      paintList();
      return;
    }
    const request = ++contentSearchRequest;
    contentSearchQuery = query;
    contentSearchHits = [];
    contentSearchNotice = "";
    contentSearching = true;
    if (drawerOpen) setDrawerOpen(false, false);
    paintList();
    try {
      const result = await endpoints.searchSessions(query, "content", sourceFilter, 50);
      if (disposed || request !== contentSearchRequest || search.trim() !== query) return;
      contentSearchHits = result.hits || [];
      if (result.degraded) {
        contentSearchNotice = result.degraded_reason
          ? `索引不可用，已降级为逐会话搜索：${result.degraded_reason}`
          : "索引不可用，已降级为逐会话搜索。";
      } else if (result.fallback_like) {
        contentSearchNotice = "查詢短於 3 個字元，已使用較慢的字串比對。";
      } else if (result.truncated) {
        contentSearchNotice = "命中較多，目前只顯示前 50 條。";
      }
    } catch (err) {
      if (disposed || request !== contentSearchRequest) return;
      contentSearchNotice =
        err instanceof ApiError && err.status === 409
          ? "正文索引已关闭，请到「设置 → 搜索与隐私」重新开启。"
          : `正文搜索失败：${(err as Error).message}`;
      toast(contentSearchNotice, "bad");
    } finally {
      if (!disposed && request === contentSearchRequest) {
        contentSearching = false;
        paintList();
      }
    }
  }

  function openContentSearchHit(hit: SearchHit) {
    if (!sessions.some((session) =>
      session.session_id === hit.session.session_id && session.source === hit.session.source
    )) {
      sessions = [hit.session, ...sessions];
    }
    pendingTranscriptItemId = hit.item_id || "";
    clearContentSearch();
    selectSession(hit.session);
  }

  function contentSearchPanel(): HTMLElement {
    const panel = h("section", { class: "content-search-panel" });
    panel.append(
      h(
        "div",
        { class: "content-search-head" },
        h("strong", { text: "正文搜索" }),
        h("span", { class: "mono content-search-query", text: contentSearchQuery }),
        contentSearching
          ? h("span", { class: "note", text: "搜索中…" })
          : h("span", { class: "count", text: String(contentSearchHits.length) }),
        h("div", { class: "spacer", style: "flex:1" }),
        button("清除", {
          variant: "quiet",
          small: true,
          onClick: () => {
            search = "";
            clearContentSearch();
            paintList();
          },
        }),
      ),
    );
    if (contentSearchNotice) {
      panel.append(h("div", { class: "content-search-note", text: contentSearchNotice }));
    }
    if (contentSearching) {
      panel.append(h("div", { class: "content-search-empty", text: "正在搜索会话正文…" }));
      return panel;
    }
    if (!contentSearchHits.length) {
      panel.append(
        h("div", {
          class: "content-search-empty",
          text: contentSearchNotice ? "没有可显示的正文命中。" : "没有正文命中。",
        }),
      );
      return panel;
    }
    const list = h("div", { class: "content-search-hits" });
    for (const hit of contentSearchHits) {
      const session = hit.session;
      const row = h(
        "button",
        { class: "content-search-hit", type: "button" },
        h(
          "span",
          { class: "content-search-hit-head" },
          agentBadge(session.source),
          h("strong", { text: session.title || session.session_id.slice(0, 16) }),
          h("span", { class: "content-search-hit-agent", text: agentName(session.source) }),
          h("div", { class: "spacer", style: "flex:1" }),
          h("span", { class: "mono content-search-hit-time", text: timeAgo(session.last_ts) }),
        ),
        h(
          "span",
          { class: "content-search-snippet" },
          h("span", { text: hit.snippet.before }),
          h("mark", { text: hit.snippet.match }),
          h("span", { text: hit.snippet.after }),
        ),
        h(
          "span",
          { class: "content-search-hit-meta" },
          h("span", { text: session.project || session.cwd || "未分组" }),
          hit.item_id ? h("span", { class: "mono", text: hit.item_id }) : null,
        ),
      );
      row.addEventListener("click", () => openContentSearchHit(hit));
      list.append(row);
    }
    panel.append(list);
    return panel;
  }

  function orderedVisibleSessions(): SessionSummary[] {
    return Array.from(grouped(visibleSessions()).values()).flat();
  }

  function grouped(list: SessionSummary[]): Map<string, SessionSummary[]> {
    const map = new Map<string, SessionSummary[]>();
    for (const session of list) {
      const key = projectKey(session);
      const group = map.get(key) || [];
      group.push(session);
      map.set(key, group);
    }
    for (const group of map.values()) {
      group.sort((a, b) => {
        if (a.collab?.role === "parent" && b.collab?.parent?.session_id === a.session_id) return -1;
        if (b.collab?.role === "parent" && a.collab?.parent?.session_id === b.session_id) return 1;
        return (b.last_ts || "").localeCompare(a.last_ts || "");
      });
    }
    return map;
  }

  function scheduleBatch<T>(
    items: readonly T[],
    render: (item: T) => void,
    isCurrent: () => boolean,
    batchSize: number,
  ) {
    let index = 0;
    const pump = () => {
      if (!isCurrent()) return;
      const end = Math.min(items.length, index + batchSize);
      for (; index < end; index++) render(items[index]);
      if (index < items.length) window.requestAnimationFrame(pump);
    };
    pump();
  }

  function paintSidebar(paintToken: number, visibleRows: SessionSummary[]) {
    const active = document.activeElement as HTMLInputElement | null;
    const keepSearchFocus = active?.getAttribute("aria-label") === "搜索会话";
    const keepNewButtonFocus =
      pendingNewSessionFocus || Boolean(active?.matches("[data-session-new-trigger]"));
    const caret = keepSearchFocus ? active?.selectionStart ?? search.length : search.length;
    clear(left);
    const sources = Array.from(new Set(sessions.map((session) => session.source))).sort();
    const searchInput = h("input", {
      class: "input",
      type: "search",
      placeholder: "搜索标题、路径；Enter 搜正文…",
      value: search,
      "aria-label": "搜索会话",
    });
    searchInput.addEventListener("input", () => {
      search = (searchInput as HTMLInputElement).value;
      clearContentSearch();
      paintList();
    });
    searchInput.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" || event.isComposing) return;
      event.preventDefault();
      void searchSessionContent();
    });
    searchInputEl = searchInput as HTMLInputElement;
    const sourceSelect = h("select", { class: "input compact", "aria-label": "按 Agent 筛选" }) as HTMLSelectElement;
    sourceSelect.append(h("option", { value: "", text: "全部 Agent" }));
    for (const source of sources) sourceSelect.append(h("option", { value: source, text: agentName(source) }));
    sourceSelect.value = sourceFilter;
    sourceSelect.addEventListener("change", () => {
      sourceFilter = sourceSelect.value;
      clearContentSearch();
      paintList();
    });
    const newButton = h("button", {
      class: "icon-btn",
      type: "button",
      title: "新会话",
      "aria-label": "新会话",
      "data-session-new-trigger": "",
    });
    newButton.append(icon(icons.plus, 15));
    newButton.addEventListener("click", () => void openNewSession());
    const listClose = h("button", {
      class: "icon-btn session-list-close",
      type: "button",
      title: "收起会话列表",
      "aria-label": "收起会话列表",
    });
    listClose.append(icon(icons.close, 14));
    listClose.addEventListener("click", () => setListOpen(false));
    left.append(
      h(
        "div",
        { class: "workbench-sidebar-head" },
        h("div", null, h("h2", { text: "会话" })),
        h("div", { class: "spacer", style: "flex:1" }),
        h("span", { class: "count", text: String(visibleRows.length) }),
        newButton,
        listClose,
      ),
      h("div", { class: "workbench-filters" }, searchInput, sourceSelect),
    );
    const tree = h("div", { class: "project-tree" });
    const pendingRows: Array<{ host: HTMLElement; session: SessionSummary }> = [];
    for (const [project, rows] of grouped(visibleRows)) {
      const collapsed = groupCollapsed.has(project);
      const groupHead = h(
        "button",
        { class: collapsed ? "project-head collapsed" : "project-head", type: "button" },
        icon(collapsed ? icons.chevronRight : icons.chevronDown, 12),
        h("span", { class: "project-name", text: project }),
        h("span", { class: "count", text: String(rows.length) }),
      );
      groupHead.addEventListener("click", () => {
        if (collapsed) groupCollapsed.delete(project);
        else groupCollapsed.add(project);
        paintList();
      });
      tree.append(groupHead);
      if (!collapsed) {
        const rowsHost = h("div", { class: "project-rows" });
        for (const session of rows) pendingRows.push({ host: rowsHost, session });
        tree.append(rowsHost);
      }
    }
    if (!visibleRows.length) {
      tree.append(
        emptyState(
          search ? "没有匹配的会话" : "还没有会话",
          search ? "换一个关键词。" : "Agent 跑过一轮后会出现在这里。",
        ),
      );
    }
    left.append(tree);
    scheduleBatch(
      pendingRows,
      ({ host, session }) => host.append(sessionRow(session)),
      () => !disposed && paintToken === listPaintToken,
      SESSION_RENDER_BATCH,
    );
    if (keepSearchFocus) {
      searchInput.focus();
      try {
        (searchInput as HTMLInputElement).setSelectionRange(caret, caret);
      } catch {
        // Search inputs that don't expose selection still keep their value.
      }
    } else if (keepNewButtonFocus) {
      newButton.focus();
      pendingNewSessionFocus = false;
    }
  }

  function sessionRow(session: SessionSummary): HTMLElement {
    const active = selected?.session_id === session.session_id && selected?.source === session.source;
    const child = session.collab?.role === "child";
    const row = h(
      "button",
      { class: `session-row${active ? " active" : ""}${child ? " child" : ""}`, type: "button" },
      agentBadge(child ? session.collab?.parent?.source || session.source : session.source),
      h(
        "span",
        { class: "session-row-main" },
        h(
          "span",
          { class: "session-row-title" },
          child ? h("span", { class: "session-child-mark", text: "↳" }) : null,
          h("span", { class: "txt", text: session.title || session.session_id.slice(0, 14) }),
          session.running ? h("span", { class: "dot dot-warn" }) : null,
          session.read === false ? h("span", { class: "unread-dot", title: "未读" }) : null,
        ),
        h(
          "span",
          { class: "session-row-sub" },
          h("span", { text: agentName(session.source) }),
          session.running ? " · 运行中" : "",
          session.branch ? ` · ${session.branch}` : "",
          session.collab?.role === "parent" && session.collab.children?.length
            ? ` · ${session.collab.children.length} 个协作`
            : "",
        ),
      ),
      h("span", { class: "session-row-time", text: timeAgo(session.last_ts) }),
    );
    row.addEventListener("click", () => selectSession(session));
    return row;
  }

  function sessionCard(session: SessionSummary): HTMLElement {
    const active = selected?.session_id === session.session_id && selected?.source === session.source;
    const card = h(
      "button",
      { class: `session-card${active ? " active" : ""}`, type: "button" },
      h(
        "span",
        { class: "session-card-head" },
        agentBadge(session.source),
        h("span", { class: "session-card-name", text: agentName(session.source) }),
        h("div", { class: "spacer", style: "flex:1" }),
        session.running ? tag("运行中", "warn") : null,
        session.read === false ? tag("未读", "ok") : null,
        h("span", { class: "mono session-card-time", text: timeAgo(session.last_ts) }),
      ),
      h("strong", { class: "session-card-title", text: session.title || session.session_id.slice(0, 14) }),
      h("span", { class: "session-card-preview", text: session.last_preview || session.cwd || "—" }),
      h(
        "span",
        { class: "session-card-meta" },
        h("span", { text: session.project || "未分组" }),
        session.model ? h("span", { text: session.model }) : null,
        session.collab?.role === "child" ? h("span", { class: "collab-mark", text: "协作子会话" }) : null,
        session.collab?.role === "parent" && session.collab.children?.length
          ? h("span", { class: "collab-mark", text: `${session.collab.children.length} 协作` })
          : null,
      ),
    );
    card.addEventListener("click", () => selectSession(session));
    return card;
  }

  function paintList() {
    const paintToken = ++listPaintToken;
    const rows = visibleSessions();
    paintSidebar(paintToken, rows);
    clear(center);
    root.classList.toggle("search-open", Boolean(contentSearchQuery));
    const cards = h("div", { class: "session-card-list" });
    if (contentSearchQuery) cards.append(contentSearchPanel());
    center.append(cards);
    if (rows.length) {
      scheduleBatch(
        rows,
        (session) => cards.append(sessionCard(session)),
        () => !disposed && paintToken === listPaintToken,
        SESSION_RENDER_BATCH,
      );
    } else {
      cards.append(emptyState("没有会话", "调整搜索或 Agent 筛选，或先让一个 Agent 跑一轮。"));
    }
  }

  async function loadSessions(initial = false) {
    try {
      const [sessionPayload, agentPayload] = await Promise.all([
        endpoints.sessions(SESSION_LIST_LIMIT),
        endpoints.agents().catch(() => ({ agents: [] as Agent[], generated_at: "" })),
      ]);
      if (disposed) return;
      sessions = sessionPayload.sessions.filter((session) => !session.archived);
      agents = agentPayload.agents;
      const requested = selected?.session_id || currentFromURL();
      const next =
        sessions.find((session) => session.session_id === requested) ||
        selected ||
        sessions.find((session) => !session.is_empty) ||
        sessions[0] ||
        null;
      if (initial || !selected || next?.session_id !== selected.session_id) {
        if (next) {
          selected = next;
          autoExpandFor(next);
          setURLSession(next.session_id);
          paintMain();
          void loadSelected(true);
        }
      } else if (next) {
        const changed =
          next.title !== selected.title ||
          next.running !== selected.running ||
          next.last_ts !== selected.last_ts ||
          next.read !== selected.read;
        selected = next;
        if (changed) paintHeaderOnly();
      }
      paintList();
      if (initial && !selected) paintMain();
    } catch (err) {
      if (disposed) return;
      clear(center);
      center.append(h("div", { class: "list-card" }, emptyState("读取会话失败", (err as Error).message)));
      reportError(err, "读取会话失败");
    }
  }

  function selectSession(session: SessionSummary) {
    if (selected && selected.session_id === session.session_id && selected.source === session.source) {
      if (window.matchMedia("(max-width: 820px)").matches) setListOpen(false);
      return;
    }
    selected = session;
    draft = "";
    replyModelOverride = "";
    replyEffortOverride = "";
    replyPermissionOverride = "";
    pendingAttachments = [];
    uploadingAttachments = false;
    pendingPlan = null;
    mentionEdits = [];
    selectedItems = [];
    selectedCommands = [];
    selectedDispatches = [];
    selectedInbox = [];
    autoExpandFor(session);
    setURLSession(session.session_id);
    paintList();
    paintMain();
    if (window.matchMedia("(max-width: 820px)").matches) setListOpen(false);
    void loadSelected(true);
  }

  function setListOpen(next: boolean, restoreFocus = true) {
    listOpen = next;
    root.classList.toggle("list-open", next);
    listScrim.hidden = !next;
    const toggle = root.querySelector<HTMLElement>("[data-session-list-toggle]");
    toggle?.classList.toggle("active", next);
    toggle?.setAttribute("aria-expanded", String(next));
    toggle?.setAttribute("aria-label", next ? "收起会话列表" : "打开会话列表");
    toggle?.setAttribute("title", next ? "收起会话列表" : "会话列表");
    if (next) {
      window.requestAnimationFrame(() => searchInputEl?.focus());
    } else if (restoreFocus) {
      window.requestAnimationFrame(() => {
        root.querySelector<HTMLElement>("[data-session-list-toggle]")?.focus();
      });
    }
  }
  listScrim.addEventListener("click", () => setListOpen(false));

  function trapTabWithin(event: KeyboardEvent, container: HTMLElement) {
    const focusable = Array.from(
      container.querySelectorAll<HTMLElement>(
        "button:not([disabled]), input:not([disabled]):not([type='hidden']), " +
          "select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex='-1'])",
      ),
    ).filter((element) => element.getClientRects().length > 0);
    if (!focusable.length) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement as HTMLElement | null;
    if (!active || !container.contains(active)) {
      event.preventDefault();
      first.focus();
    } else if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function moveSelection(delta: number) {
    const rows = orderedVisibleSessions();
    if (!rows.length) return;
    const current = selected
      ? rows.findIndex(
          (row) => row.session_id === selected?.session_id && row.source === selected?.source,
        )
      : -1;
    const nextIndex =
      current < 0
        ? delta > 0
          ? 0
          : rows.length - 1
        : Math.max(0, Math.min(rows.length - 1, current + delta));
    const next = rows[nextIndex];
    if (!next) return;
    selectSession(next);
    window.setTimeout(() => {
      document.querySelector(".session-row.active")?.scrollIntoView({ block: "nearest" });
    }, 0);
  }

  function creatableAgents(): Agent[] {
    return agents.filter(
      (agent) => agent.available && Boolean(agent.capabilities?.new_session),
    );
  }

  function newSessionAgentProfile(): Agent | null {
    return agents.find((agent) => agent.source === newSessionAgent) || null;
  }

  function defaultPermissionMode(agent: Agent | null): string {
    const modes = agent?.permission_modes || [];
    if (!modes.length) return "";
    return modes.includes("default") ? "default" : modes[0];
  }

  function projectCandidates(): Array<{ cwd: string; label: string }> {
    const seen = new Set<string>();
    const out: Array<{ cwd: string; label: string }> = [];
    for (const session of sessions) {
      const cwd = (session.cwd || "").trim();
      if (!cwd || seen.has(cwd)) continue;
      seen.add(cwd);
      out.push({
        cwd,
        label: session.project || cwd.split("/").filter(Boolean).pop() || cwd,
      });
    }
    return out;
  }

  function newSessionField(label: string, control: HTMLElement, hint?: string): HTMLElement {
    return h(
      "label",
      { class: "session-new-field" },
      h("span", { class: "session-new-label", text: label }),
      control,
      hint ? h("span", { class: "session-new-hint", text: hint }) : null,
    );
  }

  function resetNewSessionForm() {
    newSessionAgent = "";
    newSessionCwd = "";
    newSessionText = "";
    newSessionModel = "";
    newSessionEffort = "";
    newSessionPermission = "";
    newSessionError = "";
    newSessionAttachments = [];
  }

  function removeNewSessionQuery() {
    const url = new URL(location.href);
    if (!url.searchParams.has("new")) return;
    url.searchParams.delete("new");
    history.replaceState({}, "", url.toString());
  }

  function closeNewSession() {
    if (newSessionBusy) return;
    newSessionOpen = false;
    newSessionUploading = false;
    newSessionOverlay.hidden = true;
    newSessionModal.hidden = true;
    resetNewSessionForm();
    removeNewSessionQuery();
    const capturedFocus = newSessionReturnFocus;
    newSessionReturnFocus = null;
    pendingNewSessionFocus = true;
    window.setTimeout(() => {
      const returnFocus = capturedFocus?.isConnected
        ? capturedFocus
        : root.querySelector<HTMLElement>("[data-session-new-trigger]");
      returnFocus?.focus();
      if (returnFocus && document.activeElement === returnFocus) {
        pendingNewSessionFocus = false;
      }
    }, 0);
  }

  async function openNewSession() {
    newSessionError = "";
    if (!agents.length) {
      try {
        const payload = await endpoints.agents();
        if (disposed) return;
        agents = payload.agents;
      } catch (err) {
        reportError(err, "读取 Agent 失败");
        return;
      }
    }
    const available = creatableAgents();
    if (!available.length) {
      toast("还没有可新建会话的 Agent；请先在「已接入」启用驱动", "bad");
      return;
    }
    if (!available.some((agent) => agent.source === newSessionAgent)) {
      const preferred = available.find((agent) => agent.source === selected?.source);
      newSessionAgent = preferred?.source || available[0].source;
    }
    if (!newSessionCwd) {
      newSessionCwd = selected?.cwd || projectCandidates()[0]?.cwd || "";
    }
    const profile = newSessionAgentProfile();
    if (!newSessionPermission) newSessionPermission = defaultPermissionMode(profile);
    newSessionReturnFocus =
      document.activeElement instanceof HTMLElement && document.activeElement !== document.body
        ? document.activeElement
        : null;
    newSessionOpen = true;
    removeNewSessionQuery();
    paintNewSession();
    window.setTimeout(() => {
      newSessionModal.querySelector<HTMLTextAreaElement>("textarea")?.focus();
    }, 0);
  }

  async function uploadNewSessionFiles(files: FileList | File[]) {
    const cwd = newSessionCwd.trim();
    if (!cwd) {
      toast("先填写项目目录，附件需要上传到该目录", "bad");
      return;
    }
    newSessionUploading = true;
    paintNewSession();
    try {
      for (const file of Array.from(files)) {
        const uploaded = await endpoints.uploadAttachment(cwd, file);
        if (!newSessionAttachments.some((item) => item.path === uploaded.path)) {
          newSessionAttachments = [...newSessionAttachments, uploaded];
        }
      }
    } catch (err) {
      reportError(err, "附件上传失败");
    } finally {
      newSessionUploading = false;
      if (newSessionOpen) paintNewSession();
    }
  }

  async function submitNewSession() {
    if (newSessionBusy) return;
    const profile = newSessionAgentProfile();
    if (!profile) {
      newSessionError = "请选择一个可新建会话的 Agent。";
      paintNewSession();
      return;
    }
    if (!newSessionCwd.trim()) {
      newSessionError = "项目目录不能为空。";
      paintNewSession();
      return;
    }
    if (!newSessionText.trim()) {
      newSessionError = "任务内容不能为空。";
      paintNewSession();
      return;
    }
    newSessionBusy = true;
    newSessionError = "";
    paintNewSession();
    try {
      const result = await endpoints.startSession({
        source: profile.source,
        cwd: newSessionCwd.trim(),
        text: newSessionText.trim(),
        files: newSessionAttachments.map((attachment) => attachment.path),
        model: newSessionModel || null,
        effort: newSessionEffort || null,
        permission_mode: newSessionPermission || null,
      });
      if (!result.ok) {
        throw new Error(result.message || result.error || "Agent 没有启动会话");
      }
      const sessionID = result.session_id || "";
      const requestID = result.request_id || "";
      newSessionBusy = false;
      closeNewSession();
      toast(
        sessionID
          ? `${agentName(profile.source)} 已启动新会话`
          : `已交给 ${agentName(profile.source)}，等待会话绑定`,
        "ok",
      );
      if (result.warning) toast(result.warning, "plain", 6000);
      await loadSessions(false);
      if (sessionID) {
        const created = sessions.find((session) => session.session_id === sessionID);
        if (created) selectSession(created);
      } else if (requestID) {
        window.setTimeout(() => void loadSessions(false), 1800);
      }
    } catch (err) {
      newSessionBusy = false;
      newSessionError = err instanceof Error ? err.message : String(err);
      if (newSessionOpen) paintNewSession();
      else reportError(err, "新建会话失败");
    }
  }

  function paintNewSession() {
    newSessionOverlay.hidden = !newSessionOpen;
    newSessionModal.hidden = !newSessionOpen;
    clear(newSessionModal);
    if (!newSessionOpen) return;

    const available = creatableAgents();
    const profile = newSessionAgentProfile() || available[0] || null;
    if (profile && newSessionAgent !== profile.source) {
      newSessionAgent = profile.source;
      newSessionModel = "";
      newSessionEffort = "";
      newSessionPermission = defaultPermissionMode(profile);
    }

    const closeButton = h("button", {
      class: "icon-btn",
      type: "button",
      title: "关闭",
      "aria-label": "关闭",
      disabled: newSessionBusy,
    });
    closeButton.append(icon(icons.close, 15));
    closeButton.addEventListener("click", closeNewSession);

    const head = h(
      "header",
      { class: "session-new-head" },
      h(
        "div",
        null,
        h("div", { class: "eyebrow", text: "NEW SESSION" }),
        h("h2", { text: "让一个 Agent 开始工作" }),
        h("p", {
          text: "选择 Agent、项目目录和权限模式。手机与电脑使用同一套 /api/sessions/new 能力。",
        }),
      ),
      h("span", { class: "spacer", style: "flex:1" }),
      closeButton,
    );

    const form = h("div", { class: "session-new-form" });
    const agentSelect = h("select", { class: "input" }) as HTMLSelectElement;
    for (const agent of available) {
      agentSelect.append(
        h("option", {
          value: agent.source,
          text: `${agent.display_name || agentName(agent.source)}${
            agent.version ? ` · ${agent.version}` : ""
          }`,
        }),
      );
    }
    agentSelect.value = profile?.source || "";
    agentSelect.disabled = newSessionBusy || available.length === 0;
    agentSelect.addEventListener("change", () => {
      newSessionAgent = agentSelect.value;
      const next = newSessionAgentProfile();
      newSessionModel = "";
      newSessionEffort = "";
      newSessionPermission = defaultPermissionMode(next);
      newSessionError = "";
      paintNewSession();
    });
    form.append(
      newSessionField(
        "Agent",
        agentSelect,
        available.length
          ? "只列出当前真的能新建会话的 Agent。"
          : "还没有可用的新会话驱动。",
      ),
    );

    const candidates = projectCandidates();
    const datalistId = "homing-new-session-projects";
    const cwdInput = h("input", {
      class: "input mono",
      type: "text",
      value: newSessionCwd,
      placeholder: "/Users/you/Projects/example",
      list: datalistId,
      spellcheck: "false",
      autocomplete: "off",
      disabled: newSessionBusy,
    }) as HTMLInputElement;
    cwdInput.addEventListener("input", () => {
      newSessionCwd = cwdInput.value;
    });
    const datalist = h(
      "datalist",
      { id: datalistId },
      ...candidates.map((candidate) =>
        h("option", { value: candidate.cwd, label: candidate.label }),
      ),
    );
    form.append(
      newSessionField(
        "项目目录",
        cwdInput,
        candidates.length ? "可从最近项目中选择，也可以直接输入绝对路径。" : "请输入 Agent 所在电脑上的绝对路径。",
      ),
      datalist,
    );

    const modelSelect = h("select", { class: "input" }) as HTMLSelectElement;
    modelSelect.append(h("option", { value: "", text: "Agent 默认模型" }));
    for (const model of profile?.models || []) {
      modelSelect.append(
        h("option", {
          value: model.id,
          text: `${model.label || model.id}${model.provider ? ` · ${model.provider}` : ""}`,
        }),
      );
    }
    if (!profile?.models.some((model) => model.id === newSessionModel)) {
      newSessionModel = "";
    }
    modelSelect.value = newSessionModel;
    modelSelect.disabled =
      newSessionBusy || !profile?.capabilities?.switch_model || !(profile?.models.length);
    modelSelect.addEventListener("change", () => {
      newSessionModel = modelSelect.value;
      newSessionEffort = "";
      paintNewSession();
    });

    const selectedModel = profile?.models.find((model) => model.id === newSessionModel);
    const efforts = Array.from(
      new Set(
        (selectedModel?.efforts?.length
          ? selectedModel.efforts
          : (profile?.models || []).flatMap((model) => model.efforts || [])
        ).filter(Boolean),
      ),
    );
    const effortSelect = h("select", { class: "input" }) as HTMLSelectElement;
    effortSelect.append(h("option", { value: "", text: "Agent 默认强度" }));
    for (const effort of efforts) {
      effortSelect.append(h("option", { value: effort, text: effort }));
    }
    if (!efforts.includes(newSessionEffort)) newSessionEffort = "";
    effortSelect.value = newSessionEffort;
    effortSelect.disabled =
      newSessionBusy || !profile?.capabilities?.effort || efforts.length === 0;
    effortSelect.addEventListener("change", () => {
      newSessionEffort = effortSelect.value;
    });

    const permissionModes = profile?.permission_modes || [];
    const permissionSelect = h("select", { class: "input" }) as HTMLSelectElement;
    permissionSelect.append(h("option", { value: "", text: "默认（不指定）" }));
    for (const mode of permissionModes) {
      permissionSelect.append(h("option", { value: mode, text: mode }));
    }
    if (newSessionPermission && !permissionModes.includes(newSessionPermission)) {
      newSessionPermission = "";
    }
    permissionSelect.value = newSessionPermission;
    permissionSelect.disabled = newSessionBusy || permissionModes.length === 0;
    permissionSelect.addEventListener("change", () => {
      newSessionPermission = permissionSelect.value;
    });

    form.append(
      h(
        "div",
        { class: "session-new-grid" },
        newSessionField(
          "模型",
          modelSelect,
          profile?.capabilities?.switch_model ? "" : "该 Agent 不支持指定模型。",
        ),
        newSessionField(
          "推理强度",
          effortSelect,
          profile?.capabilities?.effort ? "" : "该 Agent 未声明可调强度。",
        ),
        newSessionField(
          "权限模式",
          permissionSelect,
          permissionModes.length
            ? "不会提供绕过审批的选项。"
            : "该 Agent 未声明权限模式。",
        ),
      ),
    );

    const textarea = h("textarea", {
      class: "input session-new-task",
      rows: "5",
      placeholder: "描述你希望 Agent 完成的工作…",
      disabled: newSessionBusy,
    }) as HTMLTextAreaElement;
    textarea.value = newSessionText;
    textarea.addEventListener("input", () => {
      newSessionText = textarea.value;
    });
    form.append(newSessionField("任务内容", textarea));

    const fileInput = h("input", {
      type: "file",
      multiple: "multiple",
      style: "display:none",
    }) as HTMLInputElement;
    fileInput.addEventListener("change", () => {
      if (fileInput.files?.length) void uploadNewSessionFiles(fileInput.files);
      fileInput.value = "";
    });
    const addFiles = button(newSessionUploading ? "上传中…" : "添加附件", {
      variant: "quiet",
      small: true,
      iconSvg: icons.fileText,
      disabled: newSessionBusy || newSessionUploading,
      onClick: () => fileInput.click(),
    });
    const attachments = h("div", { class: "session-new-attachments" });
    if (newSessionAttachments.length) {
      for (const attachment of newSessionAttachments) {
        const remove = h("button", {
          class: "icon-btn",
          type: "button",
          title: "移除附件",
          "aria-label": "移除附件",
          disabled: newSessionBusy,
        });
        remove.append(icon(icons.close, 12));
        remove.addEventListener("click", () => {
          newSessionAttachments = newSessionAttachments.filter(
            (item) => item.path !== attachment.path,
          );
          paintNewSession();
        });
        attachments.append(
          h(
            "span",
            { class: "session-new-attachment" },
            h("span", { class: "name", text: attachment.name }),
            h("span", { class: "size", text: formatBytes(attachment.size) }),
            remove,
          ),
        );
      }
    }
    form.append(
      h(
        "div",
        { class: "session-new-attachment-row" },
        fileInput,
        addFiles,
        h("span", {
          class: "session-new-hint",
          text: "附件会先上传到项目目录，并把路径写进任务。",
        }),
        attachments,
      ),
    );

    if (newSessionError) {
      form.append(
        h(
          "div",
          { class: "session-new-error" },
          icon(icons.warn, 15),
          h("span", { text: newSessionError }),
        ),
      );
    }

    const cancel = button("取消", {
      variant: "quiet",
      disabled: newSessionBusy,
      onClick: closeNewSession,
    });
    const submit = button(newSessionBusy ? "正在启动…" : "开始会话", {
      variant: "primary",
      iconSvg: icons.arrowRight,
      disabled:
        newSessionBusy ||
        newSessionUploading ||
        !profile ||
        !newSessionCwd.trim() ||
        !newSessionText.trim(),
      onClick: () => void submitNewSession(),
    });
    newSessionModal.append(
      head,
      form,
      h("footer", { class: "session-new-foot" }, h("span", { class: "spacer" }), cancel, submit),
    );
  }

  async function loadSelected(markRead: boolean) {
    if (!selected) return;
    const id = selected.session_id;
    const token = ++loadToken;
    try {
      const [detail, items, commands, dispatches, inbox] = await Promise.all([
        endpoints.session(id).catch(() => selected!),
        endpoints.sessionItems(id, TRANSCRIPT_ITEM_LIMIT, undefined, true).catch(() => ({
          session_id: id,
          source: selected!.source,
          count: 0,
          items: [] as SessionItem[],
          turns: [] as unknown[],
          reason: "读取会话正文失败",
        })),
        endpoints.sessionCommands(id).catch(() => ({ session_id: id, source: selected!.source, commands: [] as SessionCommand[], count: 0 })),
        endpoints.dispatches(id).catch(() => ({ dispatches: [] as DispatchRecord[], count: 0 })),
        endpoints.inbox().catch(() => ({ waiting: [] as InboxItem[], running: [] as InboxItem[], recent_done: [] as InboxItem[], counts: { waiting: 0, running: 0 }, generated_at: "" })),
      ]);
      if (disposed || token !== loadToken || selected?.session_id !== id) return;
      selected = detail;
      selectedItems = [...items.items].sort(compareSessionItems);
      selectedCommands = commands.commands;
      selectedDispatches = dispatches.dispatches;
      selectedInbox = inbox.waiting.filter(
        (item) => item.session.session_id === id && item.session.source === detail.source,
      );
      paintHeaderOnly();
      paintTranscript(true);
      paintCollab();
      paintApproval();
      connectStream(id);
      if (markRead && detail.read === false) {
        const state = await endpoints.patchSession(id, { read: true }).catch(() => null);
        if (state?.session && selected?.session_id === id) {
          selected = state.session;
          paintHeaderOnly();
          paintList();
        }
      }
    } catch (err) {
      if (token !== loadToken || disposed) return;
      toast(`读取会话失败：${(err as Error).message}`, "bad");
    }
  }

  function mergeFetchedItems(incoming: SessionItem[]): boolean {
    if (!incoming.length) return false;
    const merged = new Map<string, SessionItem>();
    selectedItems.forEach((item, index) => merged.set(itemKey(item, index), item));
    let changed = false;
    incoming.forEach((item, index) => {
      const key = itemKey(item, index);
      const existing = merged.get(key);
      if (!existing || (existing.partial && !item.partial)) {
        merged.set(key, existing ? mergeLiveItem(existing, item) : item);
        changed = true;
      }
    });
    if (changed) selectedItems = Array.from(merged.values()).sort(compareSessionItems);
    return changed;
  }

  async function refreshSelectedData(fullItems = false) {
    if (!selected) return;
    const id = selected.session_id;
    const token = loadToken;
    const afterSeq = fullItems
      ? undefined
      : selectedItems.reduce((maximum, item) => Math.max(maximum, item.seq || 0), 0);
    const [items, dispatches, inbox, detail] = await Promise.all([
      endpoints.sessionItems(id, TRANSCRIPT_ITEM_LIMIT, afterSeq, true).catch(() => null),
      endpoints.dispatches(id).catch(() => null),
      endpoints.inbox().catch(() => null),
      endpoints.session(id).catch(() => null),
    ]);
    if (disposed || token !== loadToken || selected?.session_id !== id) return;
    let transcriptChanged = false;
    if (items) {
      if (fullItems) {
        selectedItems = [...items.items].sort(compareSessionItems);
        transcriptChanged = true;
      } else {
        transcriptChanged = mergeFetchedItems(items.items);
      }
    }
    if (dispatches) selectedDispatches = dispatches.dispatches;
    if (detail) selected = detail;
    if (inbox) {
      selectedInbox = inbox.waiting.filter(
        (item) => item.session.session_id === id && item.session.source === selected?.source,
      );
    }
    paintHeaderOnly();
    if (transcriptChanged) paintTranscript(false);
    paintCollab();
    paintApproval();
  }

  function connectStream(id: string) {
    stream?.close();
    stream = new EventSource(
      `/api/stream?session_id=${encodeURIComponent(id)}&include_reasoning=1`,
    );
    let openedOnce = false;
    const schedule = () => {
      if (disposed || selected?.session_id !== id) return;
      window.clearTimeout(itemTimer);
      itemTimer = window.setTimeout(() => void refreshSelectedData(), 260);
    };
    stream.onopen = () => {
      if (openedOnce) {
        void refreshSelectedData(true);
      } else {
        openedOnce = true;
      }
    };
    stream.addEventListener("item", (event) => {
      if (disposed || selected?.session_id !== id) return;
      try {
        const payload = JSON.parse((event as MessageEvent<string>).data) as {
          session_id?: string;
          item?: SessionItem;
        };
        if (payload.session_id && payload.session_id !== id) return;
        if (payload.item) applyStreamItem(payload.item);
      } catch {
        // A malformed live frame is ignored; the periodic resync will catch up.
      }
    });
    for (const eventName of ["turn", "message", "session", "collab_result", "dispatch"]) {
      stream.addEventListener(eventName, schedule);
    }
    stream.onerror = () => {
      // EventSource reconnects; the low-frequency poll below is the fallback.
    };
  }

  function paintMain() {
    clear(main);
    headerHost = null;
    transcriptHost = null;
    collabHost = null;
    approvalHost = null;
    composerTextarea = null;
    palettesHost = null;
    if (!selected) {
      setDrawerOpen(false, false);
      main.append(
        h(
          "div",
          { class: "workbench-empty" },
          h("div", { class: "big-pigeon", text: "◎" }),
          h("h2", { text: "选一个会话开始" }),
          h("p", { text: "左边是会话库；打开后可读正文、处理授权、回复，或输入 @ 交给另一个 Agent。" }),
        ),
      );
      return;
    }
    headerHost = h("header", { class: "workbench-main-head" });
    collabHost = h("div", { class: "collab-host" });
    approvalHost = h("div", { class: "approval-host" });
    transcriptHost = h("div", { class: "transcript" });
    main.append(headerHost, collabHost, approvalHost, transcriptHost, composer());
    paintHeaderOnly();
    paintCollab();
    paintApproval();
    paintTranscript(true);
  }

  function paintHeaderOnly() {
    if (!headerHost || !selected) return;
    clear(headerHost);
    const session = selected;
    const parent = session.collab?.role === "child" ? session.collab.parent : undefined;
    const subtitleParts = [agentName(session.source), session.project, session.branch].filter(Boolean) as string[];
    const drawerToggle = h("button", {
      class: drawerOpen ? "icon-btn drawer-toggle active" : "icon-btn drawer-toggle",
      type: "button",
      title: drawerOpen ? "收起会话信息" : "会话信息",
      "aria-label": drawerOpen ? "收起会话信息" : "打开会话信息",
      "aria-expanded": String(drawerOpen),
      "aria-controls": "session-info-drawer",
      "data-session-drawer-toggle": "",
    });
    drawerToggle.append(icon(icons.info, 15));
    drawerToggle.addEventListener("click", () => setDrawerOpen(!drawerOpen));
    const listToggle = h("button", {
      class: "icon-btn session-list-toggle",
      type: "button",
      title: "会话列表",
      "aria-label": listOpen ? "收起会话列表" : "打开会话列表",
      "aria-expanded": String(listOpen),
      "aria-controls": "session-list-panel",
      "data-session-list-toggle": "",
    });
    listToggle.append(icon(icons.sidebar, 15));
    listToggle.addEventListener("click", () => setListOpen(true));

    const more = h("details", { class: "session-more" });
    const moreSummary = h("summary", {
      class: "icon-btn",
      title: "更多会话操作",
      "aria-label": "更多会话操作",
    });
    moreSummary.append(icon(icons.grip, 15));
    const moreMenu = h("div", { class: "session-more-menu" });
    moreMenu.append(
      button("重命名", {
        variant: "quiet",
        small: true,
        iconSvg: icons.edit,
        onClick: () => {
          more.open = false;
          void renameSession();
        },
      }),
      h(
        "a",
        {
          class: "btn btn-quiet btn-sm",
          href: `/api/sessions/${encodeURIComponent(session.session_id)}/export?format=md&include_reasoning=1`,
          download: "",
        },
        icon(icons.download, 13),
        h("span", { text: "导出 Markdown" }),
      ),
      button("归档会话", {
        variant: "quiet",
        small: true,
        iconSvg: icons.trash,
        onClick: () => {
          more.open = false;
          void archiveSelected();
        },
      }),
    );
    more.append(moreSummary, moreMenu);
    let dismissMore: ((event: PointerEvent) => void) | null = null;
    more.addEventListener("toggle", () => {
      if (more.open) {
        dismissMore = (event) => {
          if (more.contains(event.target as Node)) return;
          more.open = false;
          if (dismissMore) document.removeEventListener("pointerdown", dismissMore);
          dismissMore = null;
        };
        window.setTimeout(() => {
          if (dismissMore) document.addEventListener("pointerdown", dismissMore);
        }, 0);
      } else if (dismissMore) {
        document.removeEventListener("pointerdown", dismissMore);
        dismissMore = null;
      }
    });
    more.addEventListener("keydown", (event) => {
      if ((event as KeyboardEvent).key === "Escape") {
        more.open = false;
        moreSummary.focus();
      }
    });

    const stop = session.running
      ? button("停止", {
          variant: "danger",
          small: true,
          title: "停止 Homing 启动的回合",
          onClick: () => void stopSession(),
        })
      : null;
    headerHost.append(
      h(
        "div",
        { class: "workbench-title" },
        h(
          "div",
          { class: "workbench-title-row" },
          agentBadge(session.source),
          h("h2", { text: session.title || session.session_id.slice(0, 16) }),
          session.running ? pill("运行中", "warn") : null,
          session.read === false ? tag("未读", "ok") : null,
        ),
        h(
          "div",
          { class: "workbench-subtitle" },
          ...subtitleParts.flatMap((part, index) => [
            index ? " · " : "",
            h("span", { class: index === 0 ? "mono" : "", text: part }),
          ]),
        ),
        parent
          ? h(
              "button",
              {
                class: "parent-link",
                type: "button",
                onClick: () => {
                  const candidate = sessions.find(
                    (row) => row.session_id === parent.session_id && row.source === parent.source,
                  );
                  if (candidate) selectSession(candidate);
                  else toast("父会话不在当前列表里，刷新后再试", "bad");
                },
              },
              icon(icons.arrowLeft, 12),
              h("span", { text: `来自 ${agentName(parent.source)} 的协作任务` }),
            )
          : null,
      ),
      h("div", { class: "workbench-actions" }, listToggle, stop, drawerToggle, more),
    );
    paintDrawer();
  }

  function setDrawerOpen(next: boolean, restoreFocus = true) {
    if (next && !selected) return;
    const toggle = root.querySelector<HTMLElement>("[data-session-drawer-toggle]");
    drawerOpen = next;
    root.classList.toggle("drawer-open", next);
    drawer.hidden = !next;
    drawerScrim.hidden = !next;
    toggle?.classList.toggle("active", next);
    toggle?.setAttribute("aria-expanded", String(next));
    toggle?.setAttribute("aria-label", next ? "收起会话信息" : "打开会话信息");
    toggle?.setAttribute("title", next ? "收起会话信息" : "会话信息");
    if (next) {
      paintDrawer();
      window.requestAnimationFrame(() => drawerClose.focus());
    } else if (restoreFocus) {
      window.requestAnimationFrame(() => {
        root.querySelector<HTMLElement>("[data-session-drawer-toggle]")?.focus();
      });
    }
  }

  function drawerSection(title: string, ...children: Child[]): HTMLElement {
    return h(
      "section",
      { class: "session-drawer-section" },
      h("h3", { text: title }),
      ...children,
    );
  }

  function drawerField(label: string, value: Child): HTMLElement {
    return h(
      "div",
      { class: "session-drawer-field" },
      h("span", { class: "session-drawer-label", text: label }),
      h("div", { class: "session-drawer-value" }, value),
    );
  }

  function drawerCopyButton(text: string, label: string): HTMLButtonElement {
    const copy = h("button", {
      class: "icon-btn",
      type: "button",
      title: `复制${label}`,
      "aria-label": `复制${label}`,
    });
    copy.append(icon(icons.copy, 12));
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(text);
        toast(`${label}已复制`, "ok");
      } catch {
        window.prompt(`复制${label}`, text);
      }
    });
    return copy;
  }

  function openRelatedSession(sessionId: string, source?: string, message = "会话不在当前列表里，刷新后再试") {
    const candidate = sessions.find(
      (row) => row.session_id === sessionId && (!source || row.source === source),
    );
    if (candidate) {
      selectSession(candidate);
      return;
    }
    toast(message, "plain");
  }

  function paintDrawer() {
    clear(drawerBody);
    clear(drawerFoot);
    if (!drawerOpen || !selected) return;
    const session = selected;
    const profile = agents.find((agent) => agent.source === session.source);
    const context = session.context_usage;
    const contextUsed = context?.used || 0;
    const contextLimit = context?.limit || 0;
    const contextPercent = contextLimit > 0 ? Math.min(100, Math.round((contextUsed / contextLimit) * 100)) : 0;
    const permission = session.permission_mode
      ? permissionModeLabel(session.permission_mode)
      : "未提供";
    const cwdValue = session.cwd
      ? h(
          "div",
          { class: "session-drawer-path" },
          h("code", { title: session.cwd, text: session.cwd }),
          drawerCopyButton(session.cwd, "路径"),
        )
      : h("span", { class: "muted", text: "未记录" });

    drawerBody.append(
      drawerSection(
        "运行",
        h(
          "div",
          { class: "session-drawer-agent" },
          agentBadge(session.source, false),
          h(
            "div",
            null,
            h("strong", { text: agentName(session.source) }),
            h("span", {
              text: session.running ? "正在运行" : profile?.available ? "已连接" : "未检测到可用实例",
            }),
          ),
          session.running ? pill("运行中", "warn") : null,
        ),
        drawerField("工作目录", cwdValue),
        drawerField(
          "分支",
          session.branch
            ? h("code", { class: "session-drawer-inline-code", text: session.branch })
            : h("span", { class: "muted", text: "未记录" }),
        ),
        drawerField(
          "模型",
          h("span", { class: "mono", text: session.model || profile?.default_model || "默认" }),
        ),
        drawerField("推理强度", h("span", { text: session.effort || "默认" })),
        drawerField("权限模式", h("span", { text: permission })),
        drawerField("最近活动", h("span", { text: formatDateTime(session.last_ts) })),
      ),
      drawerSection(
        "用量",
        h(
          "div",
          { class: "session-drawer-usage" },
          h(
            "div",
            { class: "session-drawer-usage-head" },
            h("span", { text: "上下文" }),
            h("span", {
              class: "mono",
              text: context
                ? `${formatCount(contextUsed)}${contextLimit ? ` / ${formatCount(contextLimit)}` : ""}`
                : "未提供",
            }),
          ),
          context
            ? h(
                "div",
                {
                  class: "session-drawer-meter",
                  role: "progressbar",
                  "aria-label": "上下文用量",
                  "aria-valuemin": "0",
                  "aria-valuemax": String(contextLimit || contextUsed || 100),
                  "aria-valuenow": String(contextUsed),
                },
                h("span", {
                  class: contextPercent >= 85 ? "warn" : contextPercent >= 70 ? "hot" : "",
                  style: contextLimit ? `width:${contextPercent}%` : "width:100%",
                }),
              )
            : null,
          context && contextLimit
            ? h("div", { class: "session-drawer-usage-foot", text: `已使用 ${contextPercent}%` })
            : null,
        ),
        h(
          "div",
          { class: "session-drawer-stats" },
          h(
            "div",
            null,
            h("span", { text: "事件数" }),
            h("strong", { text: formatCount(session.event_count ?? selectedItems.length) }),
          ),
          h(
            "div",
            null,
            h("span", { text: "已读位置" }),
            h("strong", {
              class: "mono",
              text: session.seen_event_id ? `#${formatCount(session.seen_event_id)}` : "—",
            }),
          ),
        ),
      ),
      drawerSection("协作线", ...drawerCollabRows()),
    );

    const exportAction = h(
      "a",
      {
        class: "btn btn-quiet btn-sm",
        href: `/api/sessions/${encodeURIComponent(session.session_id)}/export?format=md&include_reasoning=1`,
        title: "导出 Markdown",
        download: "",
      },
      icon(icons.download, 13),
      h("span", { text: "导出" }),
    );
    drawerFoot.append(
      exportAction,
      button("归档", {
        variant: "quiet",
        small: true,
        iconSvg: icons.trash,
        onClick: () => void archiveSelected(),
      }),
    );
  }

  function drawerCollabRows(): Child[] {
    if (!selected) return [h("p", { class: "session-drawer-note", text: "尚未选择会话。" })];
    const collab = selected.collab;
    if (!collab || collab.role === "none") {
      return [h("p", { class: "session-drawer-note", text: "这个会话还没有跨 Agent 协作线。" })];
    }
    const rows: Child[] = [];
    if (collab.role === "child" && collab.parent) {
      const parent = collab.parent;
      const dispatch = selectedDispatches.find((item) => item.dispatch_id === parent.dispatch_id);
      rows.push(
        h(
          "button",
          {
            class: "session-drawer-collab",
            type: "button",
            onClick: () => openRelatedSession(parent.session_id, parent.source),
          },
          agentBadge(parent.source),
          h(
            "span",
            { class: "session-drawer-collab-main" },
            h("strong", { text: `来自 ${agentName(parent.source)}` }),
            h("span", { text: dispatch ? PRESET_LABELS[dispatch.preset || ""] || "协作任务" : "父会话" }),
          ),
          icon(icons.chevronRight, 12),
        ),
      );
    }
    const children = collab.children || [];
    for (const child of children) {
      const dispatch = selectedDispatches.find((item) => item.dispatch_id === child.dispatch_id);
      const [statusLabel, tone] = DISPATCH_STATUS[child.status] || [child.status, "mute"];
      rows.push(
        h(
          "button",
          {
            class: "session-drawer-collab",
            type: "button",
            onClick: () => {
              const childSession = sessions.find(
                (session) =>
                  session.session_id === dispatch?.target_session_id && session.source === child.agent,
              );
              if (childSession) selectSession(childSession);
              else
                openRelatedSession(
                  dispatch?.target_session_id || "",
                  child.agent,
                  `@${agentName(child.agent)} 的子会话尚未出现在列表`,
                );
            },
          },
          agentBadge(child.agent),
          h(
            "span",
            { class: "session-drawer-collab-main" },
            h("strong", { text: `@${agentName(child.agent)}` }),
            h("span", { text: PRESET_LABELS[child.preset || ""] || dispatch?.text || "协作任务" }),
          ),
          tag(statusLabel, tone),
          icon(icons.chevronRight, 12),
        ),
      );
    }
    if (!rows.length) {
      rows.push(h("p", { class: "session-drawer-note", text: "协作关系存在，但暂无可显示的条目。" }));
    }
    return rows;
  }

  function paintTranscript(scroll: boolean) {
    if (!transcriptHost) return;
    const wasNearBottom = transcriptHost.scrollHeight - transcriptHost.scrollTop - transcriptHost.clientHeight < 180;
    const paintToken = ++transcriptPaintToken;
    transcriptPainted = false;
    transcriptItemNodes.clear();
    clear(transcriptHost);
    if (!selectedItems.length) {
      transcriptHost.append(
        h(
          "div",
          { class: "transcript-empty" },
          icon(icons.chat, 26),
          h("strong", { text: "暂时没有正文" }),
          h("p", { text: "部分 Agent 只提供事件，没有可解析的会话记录；仍可在下方回复并接收状态。" }),
        ),
      );
      transcriptPainted = true;
      return;
    }
    let index = 0;
    const pump = () => {
      if (disposed || paintToken !== transcriptPaintToken || !transcriptHost) return;
      const fragment = document.createDocumentFragment();
      let appended = 0;
      while (index < selectedItems.length && appended < TRANSCRIPT_RENDER_BATCH) {
        const item = selectedItems[index];
        const key = itemKey(item, index);
        index++;
        if (transcriptItemNodes.has(key)) continue;
        const node = transcriptItem(item, selected?.source || "");
        node.dataset.itemId = key;
        transcriptItemNodes.set(key, node);
        fragment.append(node);
        appended++;
      }
      transcriptHost.append(fragment);
      if (index < selectedItems.length) {
        requestAnimationFrame(pump);
        return;
      }
      if (transcriptItemNodes.size < selectedItems.length) {
        paintTranscript(false);
        return;
      }
      transcriptPainted = true;
      const focusNode = pendingTranscriptItemId
        ? transcriptItemNodes.get(pendingTranscriptItemId)
        : null;
      if (focusNode) {
        pendingTranscriptItemId = "";
        requestAnimationFrame(() => {
          focusNode.scrollIntoView({ block: "center" });
          focusNode.classList.add("search-focus");
          window.setTimeout(() => focusNode.classList.remove("search-focus"), 2200);
        });
      } else if (scroll || wasNearBottom) {
        requestAnimationFrame(() => {
          if (transcriptHost) transcriptHost.scrollTop = transcriptHost.scrollHeight;
        });
      }
    };
    pump();
  }

  function insertTranscriptItem(item: SessionItem, key: string) {
    if (!transcriptHost || !transcriptPainted) return;
    const existing = transcriptItemNodes.get(key);
    const wasNearBottom =
      transcriptHost.scrollHeight - transcriptHost.scrollTop - transcriptHost.clientHeight < 180;
    const node = transcriptItem(item, selected?.source || "");
    node.dataset.itemId = key;
    if (existing) {
      if (existing instanceof HTMLDetailsElement && node instanceof HTMLDetailsElement) {
        node.open = existing.open;
      }
      existing.replaceWith(node);
    } else {
      const index = selectedItems.findIndex((candidate) => itemKey(candidate) === key);
      let before: HTMLElement | null = null;
      for (let cursor = index + 1; cursor < selectedItems.length; cursor++) {
        const candidate = transcriptItemNodes.get(itemKey(selectedItems[cursor]));
        if (candidate) {
          before = candidate;
          break;
        }
      }
      transcriptHost.insertBefore(node, before);
    }
    transcriptItemNodes.set(key, node);
    if (wasNearBottom) {
      requestAnimationFrame(() => {
        if (transcriptHost) transcriptHost.scrollTop = transcriptHost.scrollHeight;
      });
    }
  }

  function applyStreamItem(item: SessionItem) {
    if (!selected) return;
    const incomingKey = itemKey(item);
    const index = selectedItems.findIndex((candidate, candidateIndex) => {
      return candidate.id ? candidate.id === item.id : itemKey(candidate, candidateIndex) === incomingKey;
    });
    let key = incomingKey;
    if (index >= 0) {
      key = selectedItems[index].id || incomingKey;
      const merged = mergeLiveItem(selectedItems[index], item);
      selectedItems[index] = merged;
      insertTranscriptItem(merged, key);
    } else {
      selectedItems.push(item);
      selectedItems.sort(compareSessionItems);
      insertTranscriptItem(item, key);
    }
  }

  function dispatchCard(dispatch: DispatchRecord): HTMLElement {
    const [label, tone] = DISPATCH_STATUS[dispatch.status] || [dispatch.status, "mute"];
    const child = sessions.find((session) => session.session_id === dispatch.target_session_id);
    const actions = h("div", { class: "collab-actions" });
    if (dispatch.status !== "completed" && dispatch.status !== "cancelled" && dispatch.status !== "failed") {
      actions.append(button("取消", { variant: "quiet", small: true, onClick: () => void cancelDispatch(dispatch) }));
    }
    if (dispatch.result?.text) {
      actions.append(
        button("转交给父 Agent", { variant: "quiet", small: true, onClick: () => void forwardDispatch(dispatch) }),
        button("追问", { variant: "quiet", small: true, onClick: () => void askDispatch(dispatch) }),
      );
      if (dispatch.result.branch) {
        actions.append(
          button("复制分支", {
            variant: "quiet",
            small: true,
            onClick: () => void copyDispatchBranch(dispatch),
          }),
        );
      }
    }
    if (
      dispatch.workspace === "worktree" &&
      dispatch.branch &&
      !dispatch.worktree_discarded_at &&
      ["completed", "cancelled", "failed"].includes(dispatch.status)
    ) {
      actions.append(
        button("丢弃 worktree", {
          variant: "quiet",
          small: true,
          onClick: () => void discardDispatchWorktree(dispatch),
        }),
      );
    }
    if (child) {
      actions.append(button("打开子会话", { variant: "quiet", small: true, onClick: () => selectSession(child) }));
    }
    return h(
      "article",
      { class: `collab-card status-${dispatch.status}` },
      h(
        "div",
        { class: "collab-card-head" },
        agentBadge(dispatch.target_agent),
        h("strong", { text: agentName(dispatch.target_agent) }),
        tag(PRESET_LABELS[dispatch.preset || ""] || dispatch.preset || "协作", "mute"),
        tag(label, tone),
        h("div", { class: "spacer", style: "flex:1" }),
        h("span", { class: "mono", text: timeAgo(dispatch.created_at) }),
      ),
      h("p", { class: "collab-task", text: dispatch.text }),
      dispatch.status === "waiting_session"
        ? h("div", { class: "collab-note", text: "目标 Agent 已启动，正在等待同目录的钩子绑定 conversation id。" })
        : null,
      dispatch.result?.text
        ? h(
            "details",
            { class: "collab-result", open: dispatch.status === "completed" },
            h(
              "summary",
              null,
              h("strong", { text: "结论" }),
              dispatch.result.files_changed?.length
                ? h("span", { text: ` · 改了 ${dispatch.result.files_changed.length} 个文件` })
                : null,
              dispatch.result.branch ? h("span", { class: "mono", text: ` · ${dispatch.result.branch}` }) : null,
            ),
            renderMarkdown(dispatch.result.text),
            dispatch.result.diff_stat
              ? h(
                  "pre",
                  { class: "collab-diff-stat" },
                  h("code", { text: dispatch.result.diff_stat }),
                )
              : null,
          )
        : null,
      actions,
    );
  }

  function paintCollab() {
    if (!collabHost || !selected) return;
    clear(collabHost);
    const children = selected.collab?.role === "parent" ? selected.collab.children || [] : [];
    const cards = selectedDispatches.filter((dispatch) => !dispatch.thread);
    if (!children.length && !cards.length) return;
    collabHost.append(
      h(
        "div",
        { class: "collab-strip" },
        h("span", { class: "collab-strip-label", text: "协作" }),
        h(
          "span",
          { class: "agent-chip active" },
          agentBadge(selected.source),
          h("span", { text: agentName(selected.source) }),
        ),
        ...children.map((child) =>
          h(
            "button",
            {
              class: "agent-chip",
              type: "button",
              onClick: () => {
                const dispatch = selectedDispatches.find((item) => item.dispatch_id === child.dispatch_id);
                const childSession = sessions.find(
                  (session) => dispatch?.target_session_id && session.session_id === dispatch.target_session_id,
                );
                if (childSession) selectSession(childSession);
                else toast(`@${agentName(child.agent)} 的子会话尚未出现在列表`, "plain");
              },
            },
            agentBadge(child.agent),
            h("span", { text: `@${agentName(child.agent)}` }),
            h("span", { class: `dot ${child.status === "completed" ? "dot-ok" : "dot-warn"}` }),
          ),
        ),
      ),
    );
    if (cards.length) {
      const list = h("div", { class: "collab-cards" });
      for (const dispatch of cards.slice(0, 6)) list.append(dispatchCard(dispatch));
      collabHost.append(list);
    }
  }

  function paintApproval() {
    if (!approvalHost) return;
    clear(approvalHost);
    const pending = selectedInbox[0];
    if (!pending) return;
    const approval = pending.kind === "approval";
    approvalHost.append(
      h(
        "div",
        { class: "approval-card" },
        h("div", { class: "approval-icon" }, icon(approval ? icons.shield : icons.warn, 18)),
        h(
          "div",
          { class: "approval-copy" },
          h("strong", {
            text: approval ? "Agent 请求授权" : pending.kind === "error" ? "Agent 出错" : "Agent 等你回复",
          }),
          h("p", { text: pending.summary || pending.title }),
        ),
        approval
          ? approvalButtons(pending.approval, (decision, optionId) =>
              void resolveApproval(pending, decision, optionId),
            )
          : null,
      ),
    );
  }

  async function resolveApproval(
    item: InboxItem,
    decision: "allow" | "deny",
    optionId?: string,
  ) {
    try {
      await endpoints.resolveApproval(item.event_id, decision, optionId);
      toast(decision === "allow" ? "已允许" : "已拒绝", "ok");
      await refreshSelectedData();
      await loadSessions(false);
    } catch (err) {
      reportError(err, "处理授权失败");
    }
  }

  function paintSendRow() {
    if (!sendRowHost) return;
    clear(sendRowHost);
    const dispatchable = agents
      .filter((agent) => agent.capabilities?.dispatch)
      .map((agent) => agent.source);
    const mentionCount = parseMentions(draft, dispatchable).mentions.length;
    append(sendRowHost, [
      button("@", { variant: "quiet", small: true, title: "交给另一个 Agent", onClick: () => showAgentPalette() }),
      button("/", { variant: "quiet", small: true, title: "会话命令", onClick: () => showCommandPalette() }),
      button("文件", {
        variant: "quiet",
        small: true,
        iconSvg: icons.fileText,
        onClick: () => void showFilePalette(),
      }),
      button(uploadingAttachments ? "上传中…" : "附件", {
        variant: "quiet",
        small: true,
        iconSvg: icons.plus,
        disabled: uploadingAttachments,
        title: selected?.cwd ? "添加附件，也可以直接拖进输入区" : "这个会话没有可用的工作目录",
        onClick: () => attachmentInput?.click(),
      }),
      h("div", { class: "spacer", style: "flex:1" }),
      mentionCount ? h("span", { class: "composer-dispatch-note", text: `将派发 ${mentionCount} 个协作任务` }) : null,
      button(sending ? "发送中…" : mentionCount ? "预览并发送" : "发送", {
        variant: "primary",
        small: true,
        disabled: sending || uploadingAttachments || !draft.trim(),
        onClick: () => void sendMessage(),
      }),
    ]);
  }

  function permissionModeLabel(mode: string): string {
    switch (mode) {
      case "":
      case "default":
        return "默认";
      case "plan":
        return "计划";
      case "accept_edits":
      case "acceptEdits":
        return "自动接受编辑";
      case "read_only":
      case "readonly":
        return "只读";
      case "never":
        return "不询问";
      default:
        return mode;
    }
  }

  function paintReplyConfig(host: HTMLElement) {
    if (!selected) return;
    clear(host);
    const profile = agents.find((agent) => agent.source === selected?.source);
    if (!profile) return;

    const currentModel = selected.model || profile.default_model || "";
    const currentModelOption = profile.models.find((model) => model.id === currentModel);
    const currentModelLabel = currentModelOption?.label || currentModel || "默认";

    const modelSelect = h("select", {
      class: "input compact composer-config-select",
      "aria-label": "回复模型",
      title: profile.capabilities.switch_model
        ? "只影响下一条回复；留空项表示沿用当前会话"
        : "该 Agent 没有声明可切模型",
    }) as HTMLSelectElement;
    modelSelect.append(
      h("option", {
        value: "",
        text: `模型 · ${currentModelLabel}`,
      }),
    );
    for (const model of profile.models) {
      modelSelect.append(h("option", { value: model.id, text: model.label || model.id }));
    }
    modelSelect.value = replyModelOverride;
    modelSelect.disabled = !profile.capabilities.switch_model || profile.models.length === 0;
    modelSelect.addEventListener("change", () => {
      replyModelOverride = modelSelect.value;
      replyEffortOverride = "";
      paintReplyConfig(host);
    });
    host.append(modelSelect);

    if (profile.capabilities.effort) {
      const effectiveModel = replyModelOverride || currentModel;
      const modelEfforts = profile.models.find((model) => model.id === effectiveModel)?.efforts || [];
      const efforts = Array.from(
        new Set(
          (modelEfforts.length ? modelEfforts : profile.models.flatMap((model) => model.efforts || [])).filter(Boolean),
        ),
      );
      const currentEffort = selected.effort || "";
      const effortSelect = h("select", {
        class: "input compact composer-config-select",
        "aria-label": "回复推理强度",
        title: "只影响下一条回复；留空项表示沿用当前会话",
      }) as HTMLSelectElement;
      effortSelect.append(
        h("option", {
          value: "",
          text: `强度 · ${currentEffort || "默认"}`,
        }),
      );
      for (const effort of efforts) {
        effortSelect.append(h("option", { value: effort, text: effort }));
      }
      effortSelect.value = replyEffortOverride;
      effortSelect.disabled = efforts.length === 0;
      effortSelect.addEventListener("change", () => {
        replyEffortOverride = effortSelect.value;
        paintReplyConfig(host);
      });
      host.append(effortSelect);
    }

    const permissionModes = profile.permission_modes || [];
    if (permissionModes.length) {
      const currentPermission = selected.permission_mode || "default";
      const permissionSelect = h("select", {
        class: "input compact composer-config-select",
        "aria-label": "回复权限模式",
        title: "只影响下一条回复；留空项表示沿用当前会话",
      }) as HTMLSelectElement;
      permissionSelect.append(
        h("option", {
          value: "",
          text: `权限 · ${permissionModeLabel(currentPermission)}`,
        }),
      );
      for (const mode of permissionModes) {
        permissionSelect.append(
          h("option", {
            value: mode,
            text: permissionModeLabel(mode),
          }),
        );
      }
      permissionSelect.value = replyPermissionOverride;
      permissionSelect.addEventListener("change", () => {
        replyPermissionOverride = permissionSelect.value;
        paintReplyConfig(host);
      });
      host.append(permissionSelect);
    }
  }

  function paintAttachments() {
    if (!attachmentHost) return;
    clear(attachmentHost);
    if (!pendingAttachments.length) return;
    for (const attachment of pendingAttachments) {
      attachmentHost.append(
        h(
          "span",
          { class: "composer-attachment", title: attachment.path },
          icon(icons.fileText, 12),
          h("span", { class: "name", text: attachment.name }),
          h("span", { class: "size", text: formatBytes(attachment.size) }),
          h("button", {
            class: "icon-btn",
            type: "button",
            title: "移除附件",
            "aria-label": `移除 ${attachment.name}`,
            onClick: () => {
              pendingAttachments = pendingAttachments.filter((item) => item.path !== attachment.path);
              paintAttachments();
            },
          }, icon(icons.close, 11)),
        ),
      );
    }
  }

  async function addAttachments(files: FileList | File[]) {
    if (!selected?.cwd) {
      toast("这个会话没有可用的工作目录，不能上传附件", "bad");
      return;
    }
    const cwd = selected.cwd;
    const queue = Array.from(files);
    if (!queue.length || uploadingAttachments) return;
    uploadingAttachments = true;
    paintSendRow();
    try {
      for (const file of queue) {
        if (file.size > 50 * 1024 * 1024) {
          toast(`${file.name} 超过 50 MB，已跳过`, "bad");
          continue;
        }
        const uploaded = await endpoints.uploadAttachment(cwd, file);
        if (!pendingAttachments.some((item) => item.path === uploaded.path)) {
          pendingAttachments.push(uploaded);
        }
      }
      paintAttachments();
    } catch (err) {
      reportError(err, "附件上传失败");
    } finally {
      uploadingAttachments = false;
      paintSendRow();
    }
  }

  function composer(): HTMLElement {
    const box = h("div", { class: "composer" });
    palettesHost = h("div", { class: "composer-palettes" });
    attachmentHost = h("div", { class: "composer-attachments" });
    attachmentInput = h("input", {
      type: "file",
      multiple: "multiple",
      style: "display:none",
    }) as HTMLInputElement;
    attachmentInput.addEventListener("change", () => {
      const input = attachmentInput;
      if (!input) return;
      const files = input.files;
      if (files?.length) void addAttachments(files);
      input.value = "";
    });
    const textarea = h("textarea", {
      class: "composer-input",
      rows: "3",
      placeholder: "回复…  输入 @ 指派 Agent，/ 查看命令",
      "aria-keyshortcuts": "Meta+Enter Control+Enter",
      title: "⌘Enter 发送 · Shift+Enter 换行",
    }) as HTMLTextAreaElement;
    textarea.value = draft;
    composerTextarea = textarea;
    textarea.addEventListener("input", () => {
      draft = textarea.value;
      pendingPlan = null;
      mentionEdits = [];
      repaintPreview();
      paintSendRow();
      if (draft.endsWith("@")) showAgentPalette();
    });
    textarea.addEventListener("keydown", (event) => {
      if (event.metaKey || event.ctrlKey) return;
      if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
        event.preventDefault();
        void sendMessage();
      }
    });
    sendRowHost = h("div", { class: "composer-actions" });
    paintSendRow();
    previewHost = h("div", { class: "compose-preview-host" });
    const configHost = h("div", { class: "composer-config" });
    paintReplyConfig(configHost);
    const settings = h(
      "details",
      { class: "composer-settings" },
      h("summary", null, icon(icons.gear, 12), h("span", { text: "回复设置" })),
      configHost,
    );
    box.append(attachmentInput, palettesHost, attachmentHost, settings, textarea, sendRowHost, previewHost);
    paintAttachments();
    box.addEventListener("dragover", (event) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      box.classList.add("drop-active");
    });
    box.addEventListener("dragleave", (event) => {
      if (!box.contains(event.relatedTarget as Node | null)) box.classList.remove("drop-active");
    });
    box.addEventListener("drop", (event) => {
      if (!event.dataTransfer?.files?.length) return;
      event.preventDefault();
      box.classList.remove("drop-active");
      void addAttachments(event.dataTransfer.files);
    });
    return box;
  }

  function insertAtCursor(text: string) {
    if (!composerTextarea) return;
    const start = composerTextarea.selectionStart;
    const end = composerTextarea.selectionEnd;
    composerTextarea.value =
      composerTextarea.value.slice(0, start) + text + composerTextarea.value.slice(end);
    composerTextarea.selectionStart = composerTextarea.selectionEnd = start + text.length;
    composerTextarea.focus();
    draft = composerTextarea.value;
    pendingPlan = null;
    mentionEdits = [];
    repaintPreview();
    paintSendRow();
  }

  function showAgentPalette() {
    if (!palettesHost) return;
    clear(palettesHost);
    const dispatchable = agents.filter((agent) => agent.capabilities?.dispatch);
    if (!dispatchable.length) {
      palettesHost.append(h("div", { class: "palette-empty", text: "当前没有可派发的 Agent。先到「接入」检查安装与能力。" }));
      return;
    }
    const palette = h("div", { class: "composer-palette" });
    for (const agent of dispatchable) {
      const item = h(
        "button",
        { class: "palette-item", type: "button" },
        agentBadge(agent.source),
        h("span", null, h("strong", { text: agent.display_name }), h("span", { text: `@${agent.source} · 可新建并派发` })),
      );
      item.addEventListener("click", () => {
        insertAtCursor(`@${agent.source} `);
        clear(palettesHost!);
      });
      palette.append(item);
    }
    palettesHost.append(palette);
  }

  function showCommandPalette() {
    if (!palettesHost) return;
    clear(palettesHost);
    if (!selectedCommands.length) {
      palettesHost.append(h("div", { class: "palette-empty", text: "这个 Agent 没有提供 / 命令。" }));
      return;
    }
    const palette = h("div", { class: "composer-palette" });
    for (const command of selectedCommands) {
      const item = h(
        "button",
        { class: "palette-item", type: "button" },
        h("span", { class: "palette-key", text: `/${command.name}` }),
        h(
          "span",
          null,
          h("strong", { text: command.description }),
          command.argument_hint ? h("span", { text: command.argument_hint }) : null,
        ),
      );
      item.addEventListener("click", () => {
        insertAtCursor(`/${command.name}${command.argument_hint ? " " : ""}`);
        clear(palettesHost!);
      });
      palette.append(item);
    }
    palettesHost.append(palette);
  }

  async function showFilePalette() {
    if (!palettesHost) return;
    clear(palettesHost);
    if (!selected?.cwd) {
      palettesHost.append(h("div", { class: "palette-empty", text: "该会话没有记录 cwd，不能列文件。" }));
      return;
    }
    palettesHost.append(h("div", { class: "palette-empty", text: "正在读取项目文件…" }));
    try {
      const payload = await endpoints.fsComplete(selected.cwd, "");
      if (!palettesHost) return;
      clear(palettesHost);
      if (!payload.items.length) {
        palettesHost.append(h("div", { class: "palette-empty", text: "没有可补全的文件。" }));
        return;
      }
      const palette = h("div", { class: "composer-palette file-palette" });
      for (const entry of payload.items) {
        const item = h(
          "button",
          { class: "palette-item", type: "button" },
          icon(entry.type === "dir" ? icons.library : icons.fileText, 13),
          h("span", { class: "mono", text: entry.path }),
        );
        item.addEventListener("click", () => {
          insertAtCursor(`@${entry.path} `);
          if (palettesHost) clear(palettesHost);
        });
        palette.append(item);
      }
      palettesHost.append(palette);
    } catch (err) {
      if (!palettesHost) return;
      clear(palettesHost);
      palettesHost.append(h("div", { class: "palette-empty", text: (err as Error).message }));
    }
  }

  function paintPreview() {
    if (!previewHost) return;
    clear(previewHost);
    if (!pendingPlan) return;
    previewHost.append(
      h(
        "div",
        { class: "compose-preview" },
        h(
          "div",
          { class: "compose-preview-head" },
          h("strong", { text: "发送前预览" }),
          h("span", { class: "note", text: "确认后才会写入父会话与派发任务" }),
        ),
        pendingPlan.message
          ? h("div", { class: "preview-line" }, tag("父会话", "mute"), h("span", { text: pendingPlan.message }))
          : null,
        ...pendingPlan.mentions.map((mention, index) =>
          h(
            "div",
            { class: "preview-line compose-mention-edit" },
            h(
              "div",
              { class: "preview-mention-main" },
              tag(`@${agentName(mention.agent)} · ${PRESET_LABELS[mention.preset] || mention.preset}`, "ok"),
              h("span", { text: mention.text }),
            ),
            mentionControl("协作线", mentionEditSelect(
              [
                ["auto", "自动"],
                ["new", "新开"],
                ["continue", "接续"],
              ],
              mentionEdits[index]?.target || "auto",
              (value) => updateMentionEdit(index, { target: value as MentionEdit["target"] }),
            )),
            mentionControl("工作区", mentionEditSelect(
              [
                ["auto", "自动"],
                ["readonly", "唯读"],
                ["shared", "共享"],
                ["worktree", "worktree"],
              ],
              mentionEdits[index]?.workspace || "auto",
              (value) => updateMentionEdit(index, { workspace: value as MentionEdit["workspace"] }),
            )),
            mentionContextControls(index),
            h(
              "span",
              { class: "mono preview-resolution", text: `${mention.target} · ${mention.workspace}` },
            ),
          ),
        ),
        h(
          "div",
          { class: "compose-preview-actions" },
          button("取消", {
            variant: "quiet",
            small: true,
            onClick: () => {
              pendingPlan = null;
              mentionEdits = [];
              paintPreview();
            },
          }),
          button("确认发送", {
            variant: "primary",
            small: true,
            disabled: sending,
            onClick: () => void sendCompose(true),
          }),
        ),
      ),
    );
  }

  function repaintPreview() {
    paintPreview();
  }

  function mentionEditSelect(
    options: Array<[string, string]>,
    value: string,
    onChange: (value: string) => void,
  ): HTMLSelectElement {
    const select = h("select", { class: "input compact preview-select" }) as HTMLSelectElement;
    for (const [optionValue, label] of options) {
      const option = document.createElement("option");
      option.value = optionValue;
      option.textContent = label;
      option.selected = optionValue === value;
      select.append(option);
    }
    select.addEventListener("change", () => onChange(select.value));
    return select;
  }

  function mentionControl(label: string, control: HTMLElement): HTMLElement {
    return h("label", { class: "preview-control" }, h("span", { text: label }), control);
  }

  function mentionContextControls(index: number): HTMLElement {
    const selected = new Set(mentionEdits[index]?.context || ["recent", "files"]);
    const host = h("div", { class: "preview-context-controls" });
    for (const [value, label] of [
      ["recent", "对话"],
      ["files", "文件"],
      ["diff", "diff"],
    ]) {
      const input = h("input", { type: "checkbox" }) as HTMLInputElement;
      input.checked = selected.has(value);
      input.addEventListener("change", () => {
        const next = new Set(mentionEdits[index]?.context || ["recent", "files"]);
        if (input.checked) next.add(value);
        else next.delete(value);
        updateMentionEdit(index, { context: [...next] });
      });
      host.append(h("label", { class: "preview-check" }, input, h("span", { text: label })));
    }
    return mentionControl("背景", host);
  }

  function updateMentionEdit(index: number, patch: Partial<MentionEdit>) {
    const current = mentionEdits[index] || {
      target: "auto" as const,
      context: ["recent", "files"],
      workspace: "auto" as const,
    };
    mentionEdits[index] = { ...current, ...patch };
    void previewCompose();
  }

  function mentionOverrideBody() {
    return mentionEdits.map((edit, index) => ({
      agent: pendingPlan?.mentions[index]?.agent,
      target: edit.target,
      context: edit.context,
      workspace: edit.workspace,
    }));
  }

  async function sendMessage() {
    if (!selected || !draft.trim() || sending || uploadingAttachments) return;
    const dispatchable = agents
      .filter((agent) => agent.capabilities?.dispatch)
      .map((agent) => agent.source);
    const mentions = parseMentions(draft, dispatchable).mentions.length > 0;
    if (mentions && !pendingPlan) {
      await previewCompose();
      return;
    }
    await sendCompose(!mentions || Boolean(pendingPlan));
  }

  async function previewCompose() {
    if (!selected) return;
    sending = true;
    paintSendRow();
    try {
      const result = await endpoints.compose({
        session_id: selected.session_id,
        source: selected.source,
        cwd: selected.cwd,
        text: draft.trim(),
        files: pendingAttachments.map((attachment) => attachment.path),
        model: replyModelOverride || null,
        effort: replyEffortOverride || null,
        permission_mode: replyPermissionOverride || null,
        collab: { context: ["recent", "files"], workspace: "auto", return: "card" },
        mentions: mentionEdits.length ? mentionOverrideBody() : undefined,
        dry_run: true,
      });
      pendingPlan = result.plan;
      if (mentionEdits.length !== result.plan.mentions.length) {
        mentionEdits = result.plan.mentions.map(() => ({
          target: "auto",
          context: ["recent", "files"],
          workspace: "auto",
        }));
      }
      paintPreview();
    } catch (err) {
      reportError(err, "生成派发预览失败");
    } finally {
      sending = false;
      paintSendRow();
    }
  }

  async function sendCompose(confirmed: boolean) {
    if (!selected || !draft.trim() || sending) return;
    if (!confirmed && !pendingPlan) {
      await previewCompose();
      return;
    }
    sending = true;
    paintSendRow();
    try {
      const result = await endpoints.compose({
        session_id: selected.session_id,
        source: selected.source,
        cwd: selected.cwd,
        text: draft.trim(),
        files: pendingAttachments.map((attachment) => attachment.path),
        model: replyModelOverride || null,
        effort: replyEffortOverride || null,
        permission_mode: replyPermissionOverride || null,
        collab: { context: ["recent", "files"], workspace: "auto", return: "card" },
        mentions: mentionEdits,
        dry_run: false,
      });
      if (!result.ok) throw new Error(result.error || "发送失败");
      const count = result.dispatches?.length || 0;
      toast(count ? `已发送，并派发 ${count} 个协作任务` : result.reply?.message || "已发送", "ok");
      draft = "";
      pendingAttachments = [];
      pendingPlan = null;
      mentionEdits = [];
      if (composerTextarea) composerTextarea.value = "";
      paintAttachments();
      paintPreview();
      await refreshSelectedData();
      await loadSessions(false);
    } catch (err) {
      reportError(err, "发送失败");
    } finally {
      sending = false;
      paintSendRow();
    }
  }

  async function renameSession() {
    if (!selected) return;
    const next = window.prompt("新的会话标题（只改 Homing 的显示名）", selected.title || "");
    if (next === null) return;
    try {
      const payload = await endpoints.patchSession(selected.session_id, { title: next.trim() });
      if (payload.session) selected = payload.session;
      toast("标题已更新", "ok");
      paintHeaderOnly();
      await loadSessions(false);
    } catch (err) {
      reportError(err, "重命名失败");
    }
  }

  async function archiveSelected() {
    if (!selected) return;
    const target = selected;
    try {
      await endpoints.patchSession(target.session_id, { archived: true });
      toast("会话已归档", "ok");
      sessions = sessions.filter(
        (session) =>
          session.session_id !== target.session_id || session.source !== target.source,
      );
      selected = null;
      await loadSessions(false);
      paintList();
      if (!selected) paintMain();
    } catch (err) {
      reportError(err, "归档失败");
    }
  }

  async function stopSession() {
    if (!selected) return;
    if (!window.confirm("停止当前会话正在运行的回合？只对 Homing 启动的进程或支持的 Agent 有效。")) return;
    try {
      const result = await endpoints.stopSession(selected.session_id);
      if (!result.ok) throw new Error(result.message || result.error || "Agent 不支持停止");
      toast("已发送停止请求", "ok");
      await refreshSelectedData();
    } catch (err) {
      reportError(err, "停止失败");
    }
  }

  async function cancelDispatch(dispatch: DispatchRecord) {
    if (!window.confirm(`取消 ${agentName(dispatch.target_agent)} 的协作任务？`)) return;
    try {
      await endpoints.cancelDispatch(dispatch.dispatch_id);
      toast("协作任务已取消", "ok");
      await refreshSelectedData();
    } catch (err) {
      reportError(err, "取消失败");
    }
  }

  async function forwardDispatch(dispatch: DispatchRecord) {
    if (!dispatch.result?.text) return;
    const text = window.prompt(
      "转交给父 Agent 的文本（可编辑）",
      `【@${dispatch.target_agent} 的结论】\n${dispatch.result.text}${
        dispatch.result.branch ? `\n\n请检查并合并分支 ${dispatch.result.branch}。` : ""
      }`,
    );
    if (text === null || !text.trim()) return;
    try {
      await endpoints.forwardDispatch(dispatch.dispatch_id, text.trim());
      toast("已转交给父 Agent", "ok");
      await refreshSelectedData();
    } catch (err) {
      reportError(err, "转交失败");
    }
  }

  async function askDispatch(dispatch: DispatchRecord) {
    const text = window.prompt(`追问 @${agentName(dispatch.target_agent)} 的协作线`, "");
    if (!text?.trim()) return;
    try {
      await endpoints.replyDispatch(dispatch.dispatch_id, text.trim());
      toast("已追问，协作任务回到运行中", "ok");
      await refreshSelectedData();
    } catch (err) {
      reportError(err, "追问失败");
    }
  }

  async function discardDispatchWorktree(dispatch: DispatchRecord) {
    if (
      !window.confirm(
        `丢弃 ${dispatch.branch || "协作 worktree"}？\n\n未合并的改动会一并删除，主工作区不会受影响。`,
      )
    ) {
      return;
    }
    try {
      const result = await endpoints.discardDispatchWorktree(dispatch.dispatch_id, true);
      if (!result.ok) throw new Error(result.error || "丢弃 worktree 失败");
      toast("已丢弃协作 worktree 与分支", "ok");
      await refreshSelectedData();
    } catch (err) {
      reportError(err, "丢弃 worktree 失败");
    }
  }

  async function copyDispatchBranch(dispatch: DispatchRecord) {
    const branch = dispatch.result?.branch || dispatch.branch;
    if (!branch) return;
    try {
      await navigator.clipboard.writeText(branch);
      toast("分支名已复制", "ok");
    } catch {
      window.prompt("复制分支名", branch);
    }
  }

  newSessionOverlay.addEventListener("click", closeNewSession);
  drawerClose.addEventListener("click", () => setDrawerOpen(false));
  drawerScrim.addEventListener("click", () => setDrawerOpen(false));
  newSessionModal.addEventListener("keydown", (event) => {
    if (!newSessionOpen || event.key !== "Tab") return;
    const focusable = Array.from(
      newSessionModal.querySelectorAll<HTMLElement>(
        "button:not([disabled]), input:not([disabled]):not([type='hidden']), " +
          "select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex='-1'])",
      ),
    ).filter((element) => element.getClientRects().length > 0);
    if (!focusable.length) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  const wantsNewSession = new URLSearchParams(location.search).get("new") === "1";
  paintList();
  paintMain();
  void loadSessions(true).then(() => {
    if (wantsNewSession && !disposed) void openNewSession();
  });
  const handleShortcut = (event: KeyboardEvent) => {
    if (disposed) return;
    if (newSessionOpen) {
      if (event.key === "Escape") {
        event.preventDefault();
        closeNewSession();
      }
      return;
    }
    if (
      event.key === "Tab" &&
      window.matchMedia("(max-width: 820px)").matches
    ) {
      if (drawerOpen) {
        trapTabWithin(event, drawer);
        return;
      }
      if (listOpen) {
        trapTabWithin(event, left);
        return;
      }
    }
    if (listOpen && event.key === "Escape") {
      event.preventDefault();
      setListOpen(false);
      return;
    }
    if (drawerOpen && event.key === "Escape") {
      event.preventDefault();
      setDrawerOpen(false);
      return;
    }
    const mod = event.metaKey || event.ctrlKey;
    const key = event.key.toLowerCase();
    if (mod && key === "k") {
      event.preventDefault();
      searchInputEl?.focus();
      searchInputEl?.select();
      return;
    }
    if (mod && event.key === "Enter") {
      if (!composerTextarea) return;
      event.preventDefault();
      void sendMessage();
      return;
    }
    const target = event.target as HTMLElement | null;
    if (
      target?.closest("input, textarea, select, [contenteditable='true'], [role='textbox']") ||
      event.altKey ||
      mod
    ) {
      return;
    }
    if (key === "j") {
      event.preventDefault();
      moveSelection(1);
    } else if (key === "k") {
      event.preventDefault();
      moveSelection(-1);
    } else if (key === "e") {
      event.preventDefault();
      void archiveSelected();
    }
  };
  document.addEventListener("keydown", handleShortcut);
  const stopSessionsPoll = window.setInterval(() => void loadSessions(false), 15_000);
  const stopDataPoll = window.setInterval(() => void refreshSelectedData(), 12_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stream?.close();
      window.clearTimeout(itemTimer);
      window.clearInterval(stopSessionsPoll);
      window.clearInterval(stopDataPoll);
      document.removeEventListener("keydown", handleShortcut);
    },
  };
}
