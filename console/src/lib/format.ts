// 文案与格式化：所有「人话」都在这里，页面只负责摆位。

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
  if (signal === "hook") return "钩子推送";
  if (signal === "http_events") return "本地事件";
  if (signal === "watch") return "监听文件";
  if (signal === "poll") return "轮询读取";
  return "未知";
};

export const signalHint = (signal?: string): string => {
  if (signal === "hook") return "agent 主动推送，最及时";
  if (signal === "http_events") return "本地服务推送";
  if (signal === "watch") return "盯住会话目录，比轮询快";
  if (signal === "poll") return "60 秒轮询一次，略有延迟";
  return "";
};

export const statusLabel = (status?: string): { text: string; tone: "ok" | "warn" | "bad" | "mute" } => {
  if (status === "connected") return { text: "已连接", tone: "ok" };
  if (status === "pending") return { text: "待连接", tone: "warn" };
  if (status === "missing") return { text: "未检测到", tone: "mute" };
  return { text: "未知", tone: "mute" };
};

export const kindLabel = (kind?: string): { text: string; tone: "ok" | "warn" | "bad" | "mute" } => {
  switch (kind) {
    case "approval":
    case "permission":
      return { text: "请求授权", tone: "warn" };
    case "needs_reply":
    case "needs_input":
      return { text: "等你回复", tone: "warn" };
    case "error":
      return { text: "出错", tone: "bad" };
    case "reply":
      return { text: "回复", tone: "ok" };
    case "done":
    case "stop":
    case "turn_complete":
    case "completed":
      return { text: "完成", tone: "ok" };
    default:
      return { text: kind || "事件", tone: "mute" };
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
  if (diff < 0) return "刚刚";
  const min = Math.floor(diff / 60_000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} 小时前`;
  const day = Math.floor(hour / 24);
  if (day === 1) return "昨天";
  if (day < 30) return `${day} 天前`;
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
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)} 小时`;
  return `${Math.floor(seconds / 86_400)} 天`;
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
  return version ? `App ${version}` : "App 版本未知";
}
