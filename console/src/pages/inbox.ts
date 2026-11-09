// 收件箱：等你处理 / 运行中 / 刚完成。首页就是这一页，不再放数字卡。

import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { endpoints, poll, type Inbox, type InboxItem } from "../lib/api";
import { AGENT_LABELS, AGENT_INITIALS, kindLabel, timeAgo, projectOf } from "../lib/format";
import { button, groupHead, listCard, reportError, toast, emptyState, spinner } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

function agentChip(source: string): HTMLElement {
  return h("span", {
    class: "agent-square sm",
    text: AGENT_INITIALS[source] || (source || "?").slice(0, 1).toUpperCase(),
    title: AGENT_LABELS[source] || source,
  });
}

export function renderInbox(ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  let inbox: Inbox | null = null;
  const expanded = new Set<string>();

  function key(item: InboxItem): string {
    return `${item.session?.session_id || ""}:${item.event_id}`;
  }

  async function allow(item: InboxItem) {
    try {
      await endpoints.approve(item.event_id);
      toast("已允许", "ok");
      expanded.delete(key(item));
      await load();
    } catch (err) {
      reportError(err, "允许失败");
    }
  }

  async function deny(item: InboxItem) {
    try {
      await endpoints.deny(item.event_id);
      toast("已拒绝", "ok");
      expanded.delete(key(item));
      await load();
    } catch (err) {
      reportError(err, "拒绝失败");
    }
  }

  async function sendReply(item: InboxItem, text: string) {
    const clean = text.trim();
    if (!clean) {
      toast("回复内容是空的", "bad");
      return;
    }
    try {
      await endpoints.reply({
        event_id: item.event_id || null,
        session_id: item.session.session_id,
        source: item.session.source,
        text: clean,
      });
      toast("已发出", "ok");
      expanded.delete(key(item));
      await load();
    } catch (err) {
      reportError(err, "发送失败");
    }
  }

  async function retry(item: InboxItem) {
    try {
      await endpoints.reply({
        event_id: item.event_id || null,
        session_id: item.session.session_id,
        source: item.session.source,
        text: "上一步失败了，请再试一次。",
      });
      toast("已让它重试", "ok");
      expanded.delete(key(item));
      await load();
    } catch (err) {
      reportError(err, "重试失败");
    }
  }

  function replyBox(item: InboxItem, placeholder: string): HTMLElement {
    const input = h("textarea", {
      class: "input",
      rows: "3",
      placeholder,
      style: "height:auto;padding:9px 11px;resize:vertical;line-height:1.6",
    });
    const send = button("发送", {
      variant: "primary",
      small: true,
      onClick: () => void sendReply(item, (input as HTMLTextAreaElement).value),
    });
    const cancel = button("取消", {
      variant: "quiet",
      small: true,
      onClick: () => {
        expanded.delete(key(item));
        paint();
      },
    });
    window.setTimeout(() => (input as HTMLTextAreaElement).focus(), 30);
    return h(
      "div",
      { style: "padding:0 15px 13px 15px;display:flex;flex-direction:column;gap:8px" },
      input,
      h("div", { style: "display:flex;gap:8px;justify-content:flex-end" }, cancel, send),
    );
  }

  function waitingRow(item: InboxItem): HTMLElement[] {
    const label = kindLabel(item.kind);
    const isApproval = item.kind === "approval" || item.kind === "permission";
    const isError = item.kind === "error";
    const open = expanded.has(key(item));

    const actions = h("div", { class: "lrow-actions" });
    if (isApproval) {
      actions.append(
        button("允许", { variant: "primary", small: true, onClick: () => void allow(item) }),
        button("拒绝", { small: true, onClick: () => void deny(item) }),
      );
    }
    if (isError) {
      actions.append(button("重试", { variant: "primary", small: true, iconSvg: icons.refresh, onClick: () => void retry(item) }));
    }
    actions.append(
      button(open ? "收起" : "回复", {
        variant: isApproval || isError ? "quiet" : "ghost",
        small: true,
        onClick: () => {
          if (open) expanded.delete(key(item));
          else expanded.add(key(item));
          paint();
        },
      }),
    );

    const row = h(
      "div",
      { class: `lrow kind-${item.kind === "permission" ? "approval" : item.kind}` },
      agentChip(item.session.source),
      h(
        "div",
        { class: "lrow-main" },
        h(
          "div",
          { class: "lrow-title" },
          h("span", { class: "txt", text: item.title || kindLabel(item.kind).text }),
          label.tone === "bad" ? h("span", { class: "tag tag-bad", text: label.text }) : h("span", { class: "tag tag-warn", text: label.text }),
        ),
        h(
          "div",
          { class: "lrow-sub" },
          h("span", { class: "mono", text: `${AGENT_LABELS[item.session.source] || item.session.source} · ${projectOf(item.session)}` }),
          item.summary ? ` · ${item.summary}` : "",
        ),
      ),
      h("div", { class: "lrow-actions" }, actions),
      h("div", { class: "lrow-time", text: timeAgo(item.at) }),
    );

    if (!open) return [row];
    return [row, replyBox(item, isApproval ? "写一句说明，会把你的话当成回复发过去…" : "回复这个会话…")];
  }

  function plainRow(item: InboxItem, actionLabel: string | null): HTMLElement[] {
    const label = kindLabel(item.kind);
    const open = expanded.has(key(item));
    const actions = h("div", { class: "lrow-actions" });
    if (actionLabel) {
      actions.append(
        button(open ? "收起" : actionLabel, {
          variant: "quiet",
          small: true,
          onClick: () => {
            if (open) expanded.delete(key(item));
            else expanded.add(key(item));
            paint();
          },
        }),
      );
    }

    const row = h(
      "div",
      { class: `lrow kind-${item.kind === "permission" ? "approval" : item.kind}` },
      agentChip(item.session.source),
      h(
        "div",
        { class: "lrow-main" },
        h(
          "div",
          { class: "lrow-title" },
          h("span", { class: "txt", text: item.title || label.text }),
          h("span", { class: `tag ${label.tone === "bad" ? "tag-bad" : label.tone === "ok" ? "tag-ok" : ""}`.trim(), text: label.text }),
        ),
        h(
          "div",
          { class: "lrow-sub" },
          h("span", { class: "mono", text: `${AGENT_LABELS[item.session.source] || item.session.source} · ${projectOf(item.session)}` }),
          item.summary ? ` · ${item.summary}` : "",
        ),
      ),
      h("div", { class: "lrow-actions" }, actions),
      h("div", { class: "lrow-time", text: timeAgo(item.at) }),
    );

    if (!open) return [row];
    return [row, replyBox(item, "回复这个会话…")];
  }

  function paint() {
    if (!inbox) return;
    clear(root);

    const nothingYet =
      inbox.counts.waiting === 0 && inbox.counts.running === 0 && inbox.recent_done.length === 0;
    if (nothingYet) {
      clear(root);
      root.append(
        emptyState("还没有事件", "装好钩子或让 agent 跑一轮，这里就会出现需要你决定的事。"),
        h(
          "div",
          { style: "display:flex;justify-content:center;margin-top:12px" },
          button("去看看怎么接入", { variant: "primary", onClick: () => ctx.navigate("/console/agents") }),
        ),
      );
      return;
    }

    root.append(
      h(
        "div",
        { class: "group" },
        groupHead("等你处理", inbox.waiting.length),
        inbox.waiting.length
          ? listCard(...inbox.waiting.flatMap((item) => waitingRow(item)))
          : h("div", { class: "list-card" }, emptyState("没有等你处理的事")),
      ),
      h(
        "div",
        { class: "group" },
        groupHead("运行中", inbox.running.length),
        inbox.running.length
          ? listCard(...inbox.running.flatMap((item) => plainRow(item, null)))
          : h("div", { class: "list-card" }, emptyState("现在没有在跑的会话")),
      ),
      h(
        "div",
        { class: "group" },
        groupHead("刚完成", inbox.recent_done.length),
        inbox.recent_done.length
          ? listCard(...inbox.recent_done.flatMap((item) => plainRow(item, "回复")))
          : h("div", { class: "list-card" }, emptyState("最近还没有完成的事件")),
      ),
    );
  }

  async function load() {
    try {
      inbox = await endpoints.inbox();
      if (disposed) return;
      paint();
      ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      clear(root);
      root.append(
        h(
          "div",
          { class: "list-card" },
          h("div", { class: "empty" }, h("div", { text: "读取收件箱失败" }), h("div", { class: "note", text: (err as Error).message })),
        ),
      );
    }
  }

  clear(root);
  root.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { style: "display:flex;gap:9px;align-items:center" }, spinner(), h("span", { class: "muted", text: "正在读收件箱…" }))));
  void load();
  const stop = poll(load, 5000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
