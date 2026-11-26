import { zhCN, type MessageKey } from "./zh-CN";
import { en } from "./en";

export type Locale = "zh-CN" | "en";
export type { MessageKey };

const STORAGE_KEY = "homing.locale";
const listeners = new Set<(locale: Locale) => void>();

function detectInitialLocale(): Locale {
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved === "zh-CN" || saved === "en") return saved;
  } catch {
    /* ignore private mode / SSR */
  }
  if (typeof navigator !== "undefined" && navigator.language) {
    return /^zh/i.test(navigator.language) ? "zh-CN" : "en";
  }
  return "zh-CN";
}

let currentLocale: Locale = detectInitialLocale();
if (typeof document !== "undefined") {
  document.documentElement.lang = currentLocale;
}

export function getLocale(): Locale {
  return currentLocale;
}

export function isEn(): boolean {
  return currentLocale === "en";
}

export function setLocale(next: Locale): void {
  if (next === currentLocale) return;
  currentLocale = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, next);
  } catch {
    /* ignore */
  }
  if (typeof document !== "undefined") {
    document.documentElement.lang = next;
  }
  for (const listener of listeners) {
    try {
      listener(next);
    } catch {
      /* ignore listener errors */
    }
  }
}

export function toggleLocale(): Locale {
  const next: Locale = currentLocale === "zh-CN" ? "en" : "zh-CN";
  setLocale(next);
  return next;
}

export function onLocaleChange(listener: (locale: Locale) => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function t(key: MessageKey, params?: Record<string, string | number>): string {
  const dict = currentLocale === "en" ? en : zhCN;
  let text: string = dict[key] ?? zhCN[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      text = text.replaceAll(`{${k}}`, String(v));
    }
  }
  return text;
}

/** Inline bilingual helper for page-specific copy without bloating global keys. */
export function loc(zh: string, english: string): string {
  return currentLocale === "en" ? english : zh;
}

const CODE_TO_KEY: Record<string, MessageKey> = {
  unauthorized: "err.unauthorized",
  scope_denied: "err.scope_denied",
  loopback_only: "err.loopback_only",
  method_not_allowed: "err.method_not_allowed",
  auto_away_unavailable: "err.auto_away_unavailable",
  invalid_idle_minutes: "err.invalid_idle_minutes",
  upload_interrupted: "err.upload_interrupted",
  file_too_large: "err.file_too_large",
  save_failed: "err.save_failed",
  lan_only: "err.lan_only",
  apk_not_built: "err.apk_not_built",
  agent_not_driveable: "err.agent_not_driveable",
  new_session_not_ready: "err.new_session_not_ready",
};

export function localizeApiError(err: unknown): string {
  const anyErr = err as { code?: string; status?: number; message?: string } | null;
  if (anyErr?.code && CODE_TO_KEY[anyErr.code]) {
    return t(CODE_TO_KEY[anyErr.code]);
  }
  if (anyErr?.status === 401) {
    return t("err.unauthorized");
  }
  if (anyErr?.status === 0) {
    return t("err.network");
  }
  const message = err instanceof Error ? err.message : String(err ?? "");
  if (/401|Unauthorized/i.test(message)) {
    return t("err.unauthorized");
  }
  return message;
}
