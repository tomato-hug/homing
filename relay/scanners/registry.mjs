// Homing relay · scanner 注册表
// 新增一家 agent：写 relay/scanners/<id>.mjs，导出统一接口，在这里登记一行。
import codex from "./codex.mjs";
import claude from "./claude.mjs";
import workbuddy from "./workbuddy.mjs";
import opencode from "./opencode.mjs";
import zcode from "./zcode.mjs";

// 单色图标（16×16，currentColor）：列表里跟随文字色，不引入品牌色
const ICONS = {
  codex: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M8 1.8 13.4 4.9v6.2L8 14.2 2.6 11.1V4.9z"/><path d="M5.2 6.4 8 8l2.8-1.6M8 8v3.6"/></svg>`,
  claude: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M8 1.6v3.2M8 11.2v3.2M1.6 8h3.2M11.2 8h3.2M3.5 3.5l2.3 2.3M12.5 3.5 10.2 5.8M3.5 12.5l2.3-2.3M12.5 12.5 10.2 10.2"/></svg>`,
  workbuddy: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><rect x="2.2" y="3.4" width="11.6" height="9.2" rx="2"/><path d="M5 7h6M5 9.6h3.4"/></svg>`,
  opencode: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M2.4 3.2h6.2l5 5v4.6H7.4l-5-5z"/><path d="M6.4 7.6 9 10.2"/></svg>`,
  zcode: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M3 3.4h10L5.2 12.6H13"/></svg>`,
  generic: `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M8 2.2 13.2 5.1v5.8L8 13.8 2.8 10.9V5.1z"/></svg>`,
};

const MODULES = [codex, claude, workbuddy, opencode, zcode];

export const scanners = new Map();
for (const mod of MODULES) {
  const def = mod.default || mod;
  if (!def?.id) continue;
  // 保留 getter（pushSignal / capabilities 随 serve 状态变化），不能用展开
  const entry = {};
  for (const key of Reflect.ownKeys(def)) {
    const d = Object.getOwnPropertyDescriptor(def, key);
    if (d) Object.defineProperty(entry, key, d);
  }
  Object.defineProperty(entry, "iconSvg", { value: ICONS[def.icon] || ICONS.generic, enumerable: true });
  Object.defineProperty(entry, "module", { value: mod, enumerable: false });
  scanners.set(def.id, entry);
}

export function get(id) {
  return scanners.get(id) || null;
}

export function ids() {
  return [...scanners.keys()];
}

export function iconFor(id) {
  return scanners.get(id)?.iconSvg || ICONS.generic;
}

export function initAll(ctx) {
  for (const def of scanners.values()) {
    try { def.init?.(ctx); } catch { /* 单个 scanner 初始化失败不影响启动 */ }
  }
}

export { ICONS };
