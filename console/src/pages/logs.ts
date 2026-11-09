// 设置 › 运行日志：只读最近几百行，本机落盘，token 已抹掉。

import { h } from "../lib/dom";
import { icons } from "../lib/icons";
import { endpoints, poll, type LogLine } from "../lib/api";
import { button, emptyState, groupHead, listCard, reportError, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

export function renderLogs(_ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  let autoScroll = true;
  let showAccess = false;

  const listHost = h("div", { class: "logs" });
  const sourceText = h("span", { class: "note mono", text: "" });

  const autoBtn = button("自动刷新 开", {
    small: true,
    variant: "quiet",
    iconSvg: icons.refresh,
    onClick: () => {
      autoScroll = !autoScroll;
      autoBtn.querySelector("span")!.textContent = autoScroll ? "自动刷新 开" : "自动刷新 关";
      if (autoScroll) void refresh();
    },
  });

  const accessBtn = button("显示访问日志", {
    small: true,
    variant: "quiet",
    iconSvg: icons.terminal,
    onClick: () => {
      showAccess = !showAccess;
      accessBtn.querySelector("span")!.textContent = showAccess ? "隐藏访问日志" : "显示访问日志";
      void refresh();
    },
  });

  const copyBtn = button("复制", {
    small: true,
    variant: "quiet",
    iconSvg: icons.copy,
    onClick: () => {
      const text = (listHost.textContent || "").trim();
      void navigator.clipboard
        ?.writeText(text)
        .then(() => toast("已复制当前日志", "ok"))
        .catch(() => toast("复制失败", "bad"));
    },
  });

  const toolbar = h(
    "div",
    { class: "lrow", style: "align-items:center" },
    h(
      "div",
      { class: "lrow-main" },
      h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "homingd 日志" })),
      h("div", { class: "lrow-sub" }, sourceText),
    ),
    h("div", { class: "lrow-actions" }, autoBtn, accessBtn, copyBtn),
  );

  root.append(
    h(
      "div",
      { class: "group" },
      groupHead("运行日志"),
      listCard(toolbar, h("div", { style: "padding:12px 14px 14px" }, listHost)),
      h("div", { class: "note", style: "padding:8px 2px 0" }, "日志只在本机落盘，不上传；token 与配对令牌不会写进日志。"),
    ),
  );

  function paint(lines: LogLine[], source: string | null) {
    sourceText.textContent = source ? source.replace(/^\/Users\/[^/]+/, "~") : "没有找到日志文件";
    sourceText.title = source || "";
    const filtered = lines.filter((line) => (showAccess ? true : line.level !== "access"));
    if (filtered.length === 0) {
      listHost.replaceChildren(emptyState("暂时没有日志", "服务刚启动或者日志还在写"));
      return;
    }
    listHost.replaceChildren(
      ...filtered.map((line) => {
        const time = line.at ? line.at.slice(11, 19) : "";
        const level = line.level === "access" ? "" : line.level;
        return h(
          "div",
          { class: `log-line ${level === "error" ? "error" : level === "warn" ? "warn" : ""}`.trim() },
          h("span", { class: "log-time", text: time }),
          h("span", { class: "log-text", text: line.text }),
        );
      }),
    );
    if (autoScroll) listHost.scrollTop = listHost.scrollHeight;
  }

  async function refresh() {
    try {
      const payload = await endpoints.logs(400);
      if (disposed) return;
      paint(payload.lines, payload.source);
    } catch (err) {
      reportError(err, "读取日志失败");
    }
  }

  const stop = poll(() => (autoScroll ? refresh() : undefined), 5000);
  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
