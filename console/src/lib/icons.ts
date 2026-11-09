// 单文件线性图标（stroke: currentColor），16 格对齐。

const wrap = (body: string) =>
  `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">${body}</svg>`;

export const icons = {
  overview: wrap('<rect x="2" y="2" width="5.2" height="5.2" rx="1.4"/><rect x="8.8" y="2" width="5.2" height="5.2" rx="1.4"/><rect x="2" y="8.8" width="5.2" height="5.2" rx="1.4"/><rect x="8.8" y="8.8" width="5.2" height="5.2" rx="1.4"/>'),
  agents: wrap('<path d="M8 1.9 13.4 4.8v6.4L8 14.1 2.6 11.2V4.8z"/><path d="M5.4 6.4 8 7.9l2.6-1.5M8 7.9v3.5"/>'),
  phone: wrap('<rect x="4.2" y="1.8" width="7.6" height="12.4" rx="1.8"/><path d="M6.9 11.9h2.2"/>'),
  bell: wrap('<path d="M4.2 6.6a3.8 3.8 0 1 1 7.6 0c0 2.1.7 3.2 1.2 3.9H3c.5-.7 1.2-1.8 1.2-3.9Z"/><path d="M6.6 12.6a1.5 1.5 0 0 0 2.8 0"/>'),
  plug: wrap('<path d="M6 2.4v2.2M10 2.4v2.2"/><path d="M4 4.6h8v3a4 4 0 0 1-8 0z"/><path d="M8 11.6v2"/>'),
  logs: wrap('<path d="M3 3.2h10v9.6H3z"/><path d="M5.4 6h5.2M5.4 8.4h5.2M5.4 10.8h3"/>'),
  info: wrap('<circle cx="8" cy="8" r="5.6"/><path d="M8 7.2v4M8 5.2v.1"/>'),
  plus: wrap('<path d="M8 3.4v9.2M3.4 8h9.2"/>'),
  refresh: wrap('<path d="M13 8a5 5 0 1 1-1.6-3.7"/><path d="M13.2 2.6v2.9H10.3"/>'),
  check: wrap('<path d="M3.2 8.4 6.4 11.6 12.8 4.8"/>'),
  warn: wrap('<path d="M8 2.6 14 13H2z"/><path d="M8 6.4v3M8 11v.1"/>'),
  close: wrap('<path d="M4 4l8 8M12 4l-8 8"/>'),
  download: wrap('<path d="M8 2.8v7.4"/><path d="M5.2 7.6 8 10.4l2.8-2.8"/><path d="M3 12.8h10"/>'),
  external: wrap('<path d="M9.2 3.2h3.6v3.6"/><path d="M12.8 3.2 7.6 8.4"/><path d="M12 9.6v3.2H3.2V4h3.2"/>'),
  copy: wrap('<rect x="5.4" y="5.4" width="8" height="8" rx="1.6"/><path d="M10.6 3.4a1.6 1.6 0 0 0-1.6-1.4H4.2a1.6 1.6 0 0 0-1.6 1.6v4.8c0 .8.6 1.5 1.4 1.6"/>'),
  trash: wrap('<path d="M3.2 4.6h9.6"/><path d="M6.4 4.6V3.2h3.2v1.4"/><path d="M4.4 4.6l.6 8.2h6l.6-8.2"/>'),
  power: wrap('<path d="M8 2.4v5"/><path d="M5 4.2a4.4 4.4 0 1 0 6 0"/>'),
  clock: wrap('<circle cx="8" cy="8" r="5.6"/><path d="M8 5.2V8l2 1.2"/>'),
  pause: wrap('<path d="M6 3.6v8.8M10 3.6v8.8"/>'),
  arrowRight: wrap('<path d="M3 8h9.6"/><path d="M9.4 4.6 12.8 8l-3.4 3.4"/>'),
  arrowLeft: wrap('<path d="M13 8H3.4"/><path d="M6.6 4.6 3.2 8l3.4 3.4"/>'),
  shield: wrap('<path d="M8 2 13 3.8v4.1c0 3-2.1 4.9-5 6.1-2.9-1.2-5-3.1-5-6.1V3.8z"/>'),
  terminal: wrap('<rect x="2.4" y="3" width="11.2" height="10" rx="1.6"/><path d="M5 6.4 6.8 8 5 9.6M8.4 10h3"/>'),
  key: wrap('<circle cx="5.8" cy="10.2" r="2.4"/><path d="M7.6 8.4 12 4M10.4 5.6l1.4 1.4M11.8 4.2l1.4 1.4"/>'),
  wifi: wrap('<path d="M2.4 6.2a8 8 0 0 1 11.2 0"/><path d="M4.6 8.6a4.8 4.8 0 0 1 6.8 0"/><path d="M6.8 11a1.7 1.7 0 0 1 2.4 0"/><path d="M8 13.2v.1"/>'),
  // 新版导航与卡片用得到的
  inbox: wrap('<path d="M2.4 9.4 4 3.4h8l1.6 6"/><path d="M2.4 9.4h3.2l.8 1.6h3.2l.8-1.6h3.2v3.4H2.4z"/>'),
  chat: wrap('<path d="M13.4 8.6c0 2.3-2.4 4.2-5.4 4.2-.7 0-1.4-.1-2-.3L3 13.6l.8-2.2A4 4 0 0 1 2.6 8.6c0-2.3 2.4-4.2 5.4-4.2s5.4 1.9 5.4 4.2Z"/>'),
  puzzle: wrap('<path d="M6.2 2.6h1.8a1.2 1.2 0 0 1 0 2.4H6.2v1.8a1.2 1.2 0 1 1-2.4 0V5H2.6v3.2H1.4v-3.2a1.2 1.2 0 0 1 1.2-1.2h1.2V3.8a1.2 1.2 0 0 1 1.2-1.2Z"/><path d="M9.4 6.8h2.4v2.4h1.2a1.2 1.2 0 0 1 0 2.4h-1.2v1.6a1.2 1.2 0 0 1-1.2 1.2H8.2v-1.6a1.2 1.2 0 1 0-2.4 0v1.6H4.4a1.2 1.2 0 0 1-1.2-1.2v-2.4h2.4a1.2 1.2 0 1 0 0-2.4H3.2V6.8"/>'),
  gear: wrap('<circle cx="8" cy="8" r="2.1"/><path d="M8 1.9v1.5M8 12.6v1.5M2.6 8H1.1M14.9 8h-1.5M4.2 4.2 3.1 3.1M12.9 12.9l-1.1-1.1M11.8 4.2l1.1-1.1M3.1 12.9l1.1-1.1"/>'),
  server: wrap('<rect x="2.6" y="2.8" width="10.8" height="4.2" rx="1.2"/><rect x="2.6" y="9" width="10.8" height="4.2" rx="1.2"/><path d="M5 4.9h.1M5 11.1h.1"/>'),
  grip: wrap('<path d="M6 4h.1M10 4h.1M6 8h.1M10 8h.1M6 12h.1M10 12h.1"/>'),
  chevronRight: wrap('<path d="M6.4 4 10 8l-3.6 4"/>'),
  chevronDown: wrap('<path d="M4 6.4 8 10l4-3.6"/>'),
  bolt: wrap('<path d="M8.6 2 4.2 8.6h3.2L7.4 14l4.4-6.6H8.6z"/>'),
  fileText: wrap('<path d="M4 2.4h5l3 3v8.2H4z"/><path d="M9 2.4v3h3"/><path d="M6 8h4M6 10.4h4"/>'),
  cornerDown: wrap('<path d="M5 3v6.4c0 .8.6 1.4 1.4 1.4H12"/><path d="M10 8.6 12.4 11 10 13.2"/>'),
};

/** 托盘 / 侧栏用的鸽子（单色轮廓，静态） */
export const pigeonSilhouette =
  '<svg viewBox="0 0 120 120" xmlns="http://www.w3.org/2000/svg"><g transform="translate(4,6)"><path d="M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z" fill="currentColor"/></g></svg>';

export function pigeon(size = 26, color = "var(--body)"): HTMLElement {
  const span = document.createElement("span");
  span.style.color = color;
  span.style.display = "inline-flex";
  span.style.width = `${size}px`;
  span.style.height = `${size}px`;
  span.innerHTML = pigeonSilhouette;
  const svg = span.querySelector("svg");
  svg?.setAttribute("width", String(size));
  svg?.setAttribute("height", String(size));
  return span;
}
