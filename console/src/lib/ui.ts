// 通用组件：卡片、数字卡、胶囊、标签、表格、提示条、toast。

import { append, h, icon, type Child } from "./dom";
import { icons } from "./icons";
import { AGENT_INITIALS } from "./format";
import { agentLogo } from "./agent-icons";
import { endpoints } from "./api";
import { localizeApiError, t } from "../i18n";

export function card(head: string | null, ...children: Child[]): HTMLElement {
  const el = h("div", { class: "card" });
  if (head) {
    el.append(h("div", { class: "card-head" }, h("h2", { text: head })));
  }
  append(el, children);
  return el;
}

export function cardWithActions(head: string, actions: Child[], ...children: Child[]): HTMLElement {
  const el = h("div", { class: "card" });
  el.append(
    h("div", { class: "card-head" }, h("h2", { text: head }), h("div", { class: "spacer" }), ...actions),
  );
  append(el, children);
  return el;
}

export function sectionTitle(text: string, ...right: Child[]): HTMLElement {
  return h("div", { class: "section-title" }, h("span", { text }), h("div", { class: "spacer" }), ...right);
}

export function statCard(options: {
  label: string;
  value: number | string;
  valueClass?: string;
  sub?: Child[];
  iconSvg?: string;
}): HTMLElement {
  return h(
    "div",
    { class: "stat" },
    h(
      "div",
      { class: "stat-label" },
      options.iconSvg ? icon(options.iconSvg, 14) : null,
      h("span", { text: options.label }),
    ),
    h("div", { class: `stat-value ${options.valueClass || ""}`, text: String(options.value) }),
    options.sub ? h("div", { class: "stat-sub" }, ...options.sub) : null,
  );
}

export function pill(text: string, tone: "ok" | "run" | "warn" | "bad" | "mute" = "mute", title?: string): HTMLElement {
  const dotClass =
    tone === "ok"
      ? "dot dot-ok"
      : tone === "run"
        ? "dot dot-run"
        : tone === "warn"
          ? "dot dot-warn"
          : tone === "bad"
            ? "dot dot-bad"
            : "dot";
  return h("span", { class: "pill", title: title || "" }, h("span", { class: dotClass }), h("span", { text }));
}

export function tag(text: string, tone: "ok" | "run" | "warn" | "bad" | "mute" | "plain" = "plain", title?: string): HTMLElement {
  const cls = tone === "plain" ? "tag" : `tag tag-${tone}`;
  return h("span", { class: cls, title: title || "" }, h("span", { text }));
}

export function agentSquare(source: string, small = false): HTMLElement {
  const logo = agentLogo(source);
  if (logo) {
    const cls = ["agent-square", "logo", small ? "sm" : "", logo.mono ? "mono" : ""]
      .filter(Boolean)
      .join(" ");
    const el = h("span", { class: cls, title: source });
    if (logo.color) el.style.color = logo.color;
    el.append(icon(logo.svg, small ? 18 : 20));
    return el;
  }
  let hash = 0;
  for (const ch of source || "agent") hash = (hash * 31 + ch.charCodeAt(0)) % 360;
  const el = h("span", {
    class: small ? "agent-square sm" : "agent-square",
    text: AGENT_INITIALS[source] || (source || "?").slice(0, 2).toUpperCase(),
    title: source,
  });
  el.style.background = `hsl(${hash} 42% 18%)`;
  el.style.color = `hsl(${hash} 78% 76%)`;
  return el;
}

export function button(
  label: string,
  options: {
    variant?: "primary" | "ghost" | "quiet" | "danger";
    iconSvg?: string;
    small?: boolean;
    disabled?: boolean;
    title?: string;
    onClick?: (event: MouseEvent) => void;
  } = {},
): HTMLButtonElement {
  const variant = options.variant || "ghost";
  const cls = ["btn", variant === "primary" ? "btn-primary" : "", variant === "quiet" ? "btn-quiet" : "", variant === "danger" ? "btn-danger" : "", options.small ? "btn-sm" : ""]
    .filter(Boolean)
    .join(" ");
  const el = h("button", { class: cls, type: "button", disabled: options.disabled, title: options.title || "" });
  if (options.iconSvg) el.append(icon(options.iconSvg, options.small ? 13 : 15));
  el.append(h("span", { text: label }));
  if (options.onClick) el.addEventListener("click", options.onClick);
  return el;
}

export function banner(options: {
  tone?: "warn" | "bad";
  text: Child;
  actions?: Child[];
}): HTMLElement {
  return h(
    "div",
    { class: `banner ${options.tone === "bad" ? "bad" : ""}`.trim() },
    icon(options.tone === "bad" ? icons.warn : icons.warn, 18),
    h("div", { class: "body", style: "flex:1;min-width:0" }, options.text),
    ...(options.actions || []),
  );
}

export function emptyState(text: string, hint?: string): HTMLElement {
  return h("div", { class: "empty" }, h("div", { text }), hint ? h("div", { class: "note", text: hint }) : null);
}

export function table(headers: string[], rows: Child[][], widths?: string[]): HTMLElement {
  const thead = h(
    "thead",
    null,
    h("tr", null, ...headers.map((t, i) => h("th", { text: t, style: widths?.[i] ? `width:${widths[i]}` : "" }))),
  );
  const tbody = h(
    "tbody",
    null,
    ...rows.map((cells) => h("tr", null, ...cells.map((cell) => h("td", null, cell)))),
  );
  return h("table", { class: "table" }, thead, tbody);
}

export function kv(entries: Array<[string, Child]>): HTMLElement {
  const dl = h("dl", { class: "kv" });
  for (const [key, value] of entries) {
    dl.append(h("dt", { text: key }), h("dd", null, value));
  }
  return dl;
}

export function mono(text: string, title?: string): HTMLElement {
  return h("span", { class: "mono", text, title: title || text });
}

// ---------------------------------------------------------------- 新版控件

/** 分组标题：左边标题 + 计数，右边可以放一个弱动作 */
export function groupHead(title: string, count?: number | string, ...right: Child[]): HTMLElement {
  return h(
    "div",
    { class: "group-head" },
    h("h2", { text: title }),
    count === undefined || count === "" ? null : h("span", { class: "count", text: String(count) }),
    h("div", { class: "spacer" }),
    ...right,
  );
}

export function listCard(...rows: Child[]): HTMLElement {
  return h("div", { class: "list-card" }, ...rows);
}

export function iconButton(svg: string, title: string, onClick: (event: MouseEvent) => void): HTMLButtonElement {
  const el = h("button", { class: "icon-btn", type: "button", title, "aria-label": title });
  el.append(icon(svg, 15));
  el.addEventListener("click", onClick);
  return el;
}

export function toggle(options: {
  on: boolean;
  title?: string;
  disabled?: boolean;
  onChange: (next: boolean) => void;
}): HTMLButtonElement {
  const el = h("button", {
    class: options.on ? "switch on" : "switch",
    type: "button",
    role: "switch",
    "aria-checked": String(options.on),
    title: options.title || "",
    disabled: options.disabled,
  });
  el.addEventListener("click", () => {
    if (options.disabled) return;
    const next = !el.classList.contains("on");
    el.classList.toggle("on", next);
    el.setAttribute("aria-checked", String(next));
    options.onChange(next);
  });
  return el;
}

export function segmented<T extends string>(
  options: Array<{ value: T; label: string; disabled?: boolean; title?: string }>,
  value: T,
  onChange: (next: T) => void,
): HTMLElement {
  const wrap = h("div", { class: "segmented", role: "group" });
  for (const option of options) {
    const btn = h("button", {
      class: option.value === value ? "on" : "",
      type: "button",
      text: option.label,
      title: option.title || "",
      disabled: option.disabled,
    });
    btn.addEventListener("click", () => {
      if (option.disabled) return;
      for (const child of Array.from(wrap.children)) child.classList.remove("on");
      btn.classList.add("on");
      onChange(option.value);
    });
    wrap.append(btn);
  }
  return wrap;
}

export function dashedAdd(text: string, hint: string | null, onClick: () => void): HTMLButtonElement {
  const el = h("button", { class: "dashed", type: "button" });
  el.append(icon(icons.plus, 16), h("div", null, h("div", { class: "t", text }), hint ? h("div", { class: "note", text: hint }) : null));
  el.addEventListener("click", onClick);
  return el;
}

export function spinner(): HTMLElement {
  return h("span", { class: "spinner" });
}

// ---------------------------------------------------------------- toast

let toastWrap: HTMLElement | null = null;

export function toast(message: string, tone: "ok" | "bad" | "plain" = "plain", ttl = 4200) {
  if (!toastWrap) {
    toastWrap = h("div", { class: "toast-wrap", role: "region", "aria-live": "polite" });
    document.body.append(toastWrap);
  }
  const el = h("div", {
    class: `toast ${tone === "ok" ? "ok" : tone === "bad" ? "bad" : ""}`.trim(),
    role: tone === "bad" ? "alert" : "status",
    text: message,
  });
  toastWrap.append(el);
  window.setTimeout(() => {
    el.style.opacity = "0";
    el.style.transition = "opacity .2s ease";
    window.setTimeout(() => el.remove(), 220);
  }, ttl);
}

/** 带操作按钮的轻量提示（例如 5 秒撤销归档） */
export function toastWithAction(
  message: string,
  actionLabel: string,
  onAction: () => void | Promise<void>,
  tone: "ok" | "bad" | "plain" = "plain",
  ttl = 5500,
) {
  if (!toastWrap) {
    toastWrap = h("div", { class: "toast-wrap", role: "region", "aria-live": "polite" });
    document.body.append(toastWrap);
  }
  const actBtn = h("button", { class: "toast-action", type: "button", text: actionLabel });
  const el = h(
    "div",
    {
      class: `toast toast-with-action ${tone === "ok" ? "ok" : tone === "bad" ? "bad" : ""}`.trim(),
      role: "status",
    },
    h("span", { text: message }),
    actBtn,
  );
  let dismissed = false;
  const dismiss = () => {
    if (dismissed) return;
    dismissed = true;
    el.style.opacity = "0";
    el.style.transition = "opacity .2s ease";
    window.setTimeout(() => el.remove(), 220);
  };
  actBtn.addEventListener("click", () => {
    dismiss();
    void onAction();
  });
  toastWrap.append(el);
  window.setTimeout(dismiss, ttl);
}

/** 统一把接口错误变成一句可本地化的人话 */
export function reportError(err: unknown, prefix?: string) {
  const message = localizeApiError(err);
  const resolvedPrefix = prefix || t("common.actionFailed");
  const status = (err as { status?: number } | null)?.status;
  if (status === 401 || /401|Unauthorized/i.test(message)) {
    toast(t("err.unauthorized"), "bad", 6000);
    return;
  }
  toast(`${resolvedPrefix}: ${message}`, "bad", 6000);
}

/**
 * 在系统默认浏览器中打开外部链接（兼容 macOS WKWebView 与 Windows WebView2）。
 * Wails 原生窗口内直接调用 window.open("_blank") 会被 macOS WKWebView 静默拦截，
 * 因此优先走本机回环 /api/desktop/open-external 调起系统真实浏览器。
 */
export async function openExternalUrl(rawUrl: string): Promise<void> {
  const trimmed = rawUrl.trim();
  if (!trimmed) return;
  let target = trimmed;
  try {
    target = new URL(trimmed, location.origin).toString();
  } catch {
    toast("链接格式无效", "bad");
    return;
  }
  try {
    const res = await endpoints.openExternal(target);
    if (res?.opened) {
      return;
    }
  } catch {
    // 浏览器访问旧版或远程服务时降级到 window.open
  }
  const opened = window.open(target, "_blank", "noopener,noreferrer");
  if (!opened) {
    try {
      await navigator.clipboard.writeText(target);
      toast(`已复制链接，请在浏览器打开：${target}`, "plain", 5500);
    } catch {
      toast(`请在浏览器中打开：${target}`, "plain", 5500);
    }
  }
}
