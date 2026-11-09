// 极简 DOM 构造器：不引框架，页面全部由这里拼出来。

export type Child = Node | string | number | null | undefined | false;

type Props = {
  class?: string;
  style?: Partial<CSSStyleDeclaration> | string;
  dataset?: Record<string, string>;
  html?: string;
  text?: string;
  onClick?: (event: MouseEvent) => void;
  onInput?: (event: Event) => void;
  onChange?: (event: Event) => void;
  onSubmit?: (event: Event) => void;
  [key: string]: unknown;
};

function applyProp(el: HTMLElement, key: string, value: unknown) {
  if (value === null || value === undefined || value === false) return;
  if (key === "class") {
    el.className = String(value);
    return;
  }
  if (key === "html") {
    el.innerHTML = String(value);
    return;
  }
  if (key === "text") {
    el.textContent = String(value);
    return;
  }
  if (key === "style") {
    if (typeof value === "string") el.setAttribute("style", value);
    else Object.assign(el.style, value);
    return;
  }
  if (key === "dataset") {
    for (const [k, v] of Object.entries(value as Record<string, string>)) el.dataset[k] = v;
    return;
  }
  if (key.startsWith("on") && typeof value === "function") {
    el.addEventListener(key.slice(2).toLowerCase(), value as EventListener);
    return;
  }
  if (key === "value" && el instanceof HTMLInputElement) {
    el.value = String(value);
    return;
  }
  if (value === true) {
    el.setAttribute(key, "");
    return;
  }
  el.setAttribute(key, String(value));
}

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props?: Props | null,
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  if (props) {
    for (const [key, value] of Object.entries(props)) applyProp(el, key, value);
  }
  append(el, children);
  return el;
}

export function append(parent: HTMLElement, children: Child[]) {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    parent.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(el: Element) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

export function frag(...children: Child[]): DocumentFragment {
  const f = document.createDocumentFragment();
  append(f as unknown as HTMLElement, children);
  return f;
}

/** 内联 SVG（图标全部是单色线性图标，不走 emoji） */
export function icon(svg: string, size = 16): SVGSVGElement {
  const wrap = document.createElement("span");
  wrap.innerHTML = svg;
  const el = wrap.firstElementChild as SVGSVGElement;
  el.setAttribute("width", String(size));
  el.setAttribute("height", String(size));
  el.setAttribute("aria-hidden", "true");
  return el;
}
