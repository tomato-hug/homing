// 通用组件：卡片、数字卡、胶囊、标签、表格、提示条、toast。

import { append, h, icon, type Child } from "./dom";
import { icons } from "./icons";
import { AGENT_INITIALS } from "./format";

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

export function pill(text: string, tone: "ok" | "warn" | "bad" | "mute" = "mute", title?: string): HTMLElement {
  const dotClass = tone === "ok" ? "dot dot-ok" : tone === "warn" ? "dot dot-warn" : tone === "bad" ? "dot dot-bad" : "dot";
  return h("span", { class: "pill", title: title || "" }, h("span", { class: dotClass }), h("span", { text }));
}

export function tag(text: string, tone: "ok" | "warn" | "bad" | "mute" | "plain" = "plain", title?: string): HTMLElement {
  const cls = tone === "plain" ? "tag" : `tag tag-${tone}`;
  return h("span", { class: cls, title: title || "" }, h("span", { text }));
}

export function agentSquare(source: string, small = false): HTMLElement {
  return h("span", {
    class: small ? "agent-square sm" : "agent-square",
    text: AGENT_INITIALS[source] || (source || "?").slice(0, 1).toUpperCase(),
    title: source,
  });
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
    toastWrap = h("div", { class: "toast-wrap" });
    document.body.append(toastWrap);
  }
  const el = h("div", { class: `toast ${tone === "ok" ? "ok" : tone === "bad" ? "bad" : ""}`.trim(), text: message });
  toastWrap.append(el);
  window.setTimeout(() => {
    el.style.opacity = "0";
    el.style.transition = "opacity .2s ease";
    window.setTimeout(() => el.remove(), 220);
  }, ttl);
}

/** 统一把接口错误变成一句人话 */
export function reportError(err: unknown, prefix = "操作失败") {
  const message = err instanceof Error ? err.message : String(err);
  if (/401|Unauthorized/i.test(message)) {
    toast("控制台会话已失效，请刷新页面重新载入", "bad", 6000);
    return;
  }
  toast(`${prefix}：${message}`, "bad", 6000);
}
