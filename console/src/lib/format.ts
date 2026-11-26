// 文案与格式化：所有「人话」都在这里，页面只负责摆位。
import { t, loc } from "../i18n";

export const AGENT_LABELS: Record<string, string> = {
  codex: "Codex",
  claude: "Claude Code",
  workbuddy: "WorkBuddy",
  kimi: "Kimi Code",
  opencode: "OpenCode",
  zcode: "ZCode",
  dsh: "DeepSeek Harness",
  openclaw: "OpenClaw",
  codebuddy: "CodeBuddy Code",
  "gemini-cli": "Gemini CLI",
  hermes: "Hermes Agent",
  cursor: "Cursor",
  goose: "Goose",
  zed: "Zed",
  aider: "Aider",
  continue: "Continue",
  "qwen-code": "Qwen Code",
  cline: "Cline",
  "roo-code": "Roo Code",
  crush: "Crush",
  amp: "Amp",
  droid: "Droid",
  "copilot-cli": "GitHub Copilot CLI",
  openhands: "OpenHands",
  antigravity: "Google Antigravity",
  warp: "Warp",
  trae: "Trae",
  qoder: "Qoder",
  windsurf: "Windsurf",
  kiro: "Kiro",
};

/** 未知 agent 的字母回退；已知 agent 使用 agent-icons 里的品牌图标。 */
export const AGENT_INITIALS: Record<string, string> = {
  codex: "C",
  claude: "Cl",
  workbuddy: "W",
  kimi: "K",
  opencode: "O",
  zcode: "Z",
  dsh: "DS",
  openclaw: "OC",
  codebuddy: "CB",
  "gemini-cli": "G",
  hermes: "H",
  cursor: "Cu",
  goose: "Go",
  zed: "Ze",
  aider: "Ai",
  continue: "Co",
  "qwen-code": "Qw",
  cline: "Cl",
  "roo-code": "Ro",
  crush: "Cr",
  amp: "Am",
  droid: "Dr",
  "copilot-cli": "GH",
  openhands: "OH",
  antigravity: "Ag",
  warp: "Wa",
  trae: "Tr",
  qoder: "Qo",
  windsurf: "Ws",
  kiro: "Ki",
  kilo: "Ki",
  nanobot: "NB",
  "reasonix-desktop": "Rx",
  pig: "PiG",
  bub: "Bu",
  type4me: "4",
  cowork: "协",
  opencodereview: "OCR",
  junie: "Ju",
  "kimi-desktop": "K",
  "minimax-design": "MM",
  pink: "Pi",
  wispterm: ">_",
  citebox: "[]",
  "wisp-science": "Ws",
  kaku: "K",
  eta: "η",
  "rhino9-ai": "Rh",
  "nowledge-mem": "N",
  "deveco-code": "DV",
  pi: "π",
  mothx: "M",
  penguin: "Pg",
  omo: "OMO",
  omp: "omp",
  atomcode: "At",
  snow: "Sn",
};

export const signalLabel = (signal?: string): string => {
  if (signal === "hook") return t("signal.hook");
  if (signal === "http_events") return t("signal.http_events");
  if (signal === "watch") return t("signal.watch");
  if (signal === "poll") return t("signal.poll");
  return t("common.unknown");
};

export const signalHint = (signal?: string): string => {
  if (signal === "hook") return t("signal.hook.hint");
  if (signal === "http_events") return t("signal.http_events.hint");
  if (signal === "watch") return t("signal.watch.hint");
  if (signal === "poll") return t("signal.poll.hint");
  return "";
};

export const statusLabel = (status?: string): { text: string; tone: "ok" | "warn" | "bad" | "mute" } => {
  if (status === "connected") return { text: t("status.connected"), tone: "ok" };
  if (status === "pending") return { text: t("status.pending"), tone: "warn" };
  if (status === "missing") return { text: t("status.missing"), tone: "mute" };
  return { text: t("common.unknown"), tone: "mute" };
};

export const kindLabel = (kind?: string): { text: string; tone: "ok" | "warn" | "bad" | "mute" } => {
  switch (kind) {
    case "approval":
    case "permission":
      return { text: t("kind.approval"), tone: "warn" };
    case "needs_reply":
    case "needs_input":
      return { text: t("kind.needs_reply"), tone: "warn" };
    case "error":
      return { text: t("kind.error"), tone: "bad" };
    case "reply":
      return { text: t("kind.reply"), tone: "ok" };
    case "done":
    case "stop":
    case "turn_complete":
    case "completed":
      return { text: t("kind.done"), tone: "ok" };
    default:
      return { text: kind || t("kind.event"), tone: "mute" };
  }
};

/** 通知分级：P0 立即 / P1 合并 120 秒 / P2 摘要 */
export const levelOf = (kind?: string): "P0" | "P1" | "P2" | null => {
  if (kind === "permission" || kind === "error" || kind === "approval") return "P0";
  if (kind === "needs_input" || kind === "question" || kind === "awaiting_input") return "P1";
  if (kind === "reply" || kind === "done" || kind === "stop" || kind === "turn_complete" || kind === "completed") return "P2";
  return null;
};

const pad = (n: number) => String(n).padStart(2, "0");

export function timeShort(iso?: string | null): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function dateTimeShort(iso?: string | null): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return `${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function timeAgo(iso?: string | null): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const diff = Date.now() - then;
  if (diff < 0) return t("time.justNow");
  const min = Math.floor(diff / 60_000);
  if (min < 1) return t("time.justNow");
  if (min < 60) return t("time.minutesAgo", { n: min });
  const hour = Math.floor(min / 60);
  if (hour < 24) return t("time.hoursAgo", { n: hour });
  const day = Math.floor(hour / 24);
  if (day === 1) return t("time.yesterday");
  if (day < 30) return t("time.daysAgo", { n: day });
  return dateTimeShort(iso);
}

/** 运行时长：03:41 / 1:02:33 */
export function duration(fromIso?: string | null): string {
  if (!fromIso) return "00:00";
  const start = new Date(fromIso).getTime();
  if (Number.isNaN(start)) return "00:00";
  const seconds = Math.max(0, Math.floor((Date.now() - start) / 1000));
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

export function uptime(seconds: number): string {
  if (seconds < 60) return t("time.seconds", { n: seconds });
  if (seconds < 3600) return t("time.minutes", { n: Math.floor(seconds / 60) });
  if (seconds < 86_400) return t("time.hours", { n: Math.floor(seconds / 3600) });
  return t("time.days", { n: Math.floor(seconds / 86_400) });
}

export function projectOf(session?: { project?: string; title?: string; cwd?: string } | null): string {
  if (!session) return "";
  if (session.project) return session.project;
  if (session.cwd) return session.cwd.split(/[\\/]/).filter(Boolean).pop() || session.cwd;
  return session.title || "";
}

/** 「Codex · 项目」这类等宽串 */
export function agentProject(source?: string, project?: string): string {
  const label = AGENT_LABELS[source || ""] || source || "agent";
  return project ? `${label} · ${project}` : label;
}

export function truncate(text: string, max: number): string {
  const clean = String(text || "").replace(/\s+/g, " ").trim();
  return clean.length > max ? `${clean.slice(0, max - 1)}…` : clean;
}

export function appVersionLabel(version?: string | null): string {
  return version ? `App ${version}` : loc("App 版本未知", "Unknown App version");
}
