// 本机接口封装。页面与 homingd 同源，鉴权靠 /console 下发的 HttpOnly cookie，
// 因此这里不带任何 token，也不往 localStorage 里塞东西。

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
}

async function request<T>(path: string, init?: RequestOptions): Promise<T> {
  const options: RequestInit = {
    method: init?.method || "GET",
    cache: "no-store",
    credentials: "same-origin",
    headers: { ...(init?.headers || {}) },
  };
  if (init?.body !== undefined) {
    (options.headers as Record<string, string>)["Content-Type"] = "application/json";
    options.body = JSON.stringify(init.body) as BodyInit;
  }
  let response: Response;
  try {
    response = await fetch(path, options);
  } catch (err) {
    throw new ApiError(0, `连不上本机服务：${String((err as Error)?.message || err)}`);
  }
  const text = await response.text();
  let payload: unknown = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { error: text.slice(0, 200) };
    }
  }
  if (!response.ok) {
    const message =
      (payload as { error?: string } | null)?.error || `${response.status} ${response.statusText}`;
    throw new ApiError(response.status, message);
  }
  return payload as T;
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: "POST", body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: "PATCH", body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: "PUT", body }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

// ---------------------------------------------------------------- 类型

export type AgentStatus = "connected" | "pending" | "missing";
export type PushSignal = "hook" | "http_events" | "poll";

export interface Agent {
  source: string;
  display_name: string;
  available: boolean;
  capabilities: Record<string, boolean>;
  models: Array<{ id: string; label: string; provider?: string; default?: boolean }>;
  default_model: string | null;
  icon?: string;
  installed?: boolean;
  version?: string | null;
  push_signal?: PushSignal;
  status?: AgentStatus;
  last_event_at?: string | null;
}

export interface SessionSummary {
  session_id: string;
  source: string;
  title: string;
  title_source?: string;
  project?: string;
  workspace?: string;
  branch?: string | null;
  model?: string | null;
  last_ts?: string;
  status?: string;
  cwd?: string;
  read?: boolean;
  pinned?: boolean;
  muted?: boolean;
  archived?: boolean;
  is_temp?: boolean;
  is_empty?: boolean;
}

export interface InboxItem {
  session: SessionSummary;
  event_id: number;
  kind: "approval" | "needs_reply" | "error" | string;
  title: string;
  summary: string;
  at: string | null;
  pinned: boolean;
  read: boolean;
}

export interface Inbox {
  waiting: InboxItem[];
  running: InboxItem[];
  recent_done: InboxItem[];
  counts: { waiting: number; running: number };
  generated_at: string;
}

export interface HomingEvent {
  id: number;
  source: string;
  kind: string;
  session_id: string;
  cwd?: string;
  title?: string;
  text?: string;
  ts?: string;
  read?: boolean;
}

export interface Device {
  device_id: string;
  name: string;
  platform: string;
  app_version: string | null;
  created_at: string | null;
  last_seen_at: string | null;
  online: boolean;
  paired_via: string;
  last_ip: string | null;
}

export interface PairOffer {
  offer_id: string;
  code: string;
  status: "waiting" | "claimed" | "expired";
  created_at: string;
  expires_at: string;
  ttl_s: number;
  lan_url: string | null;
  lan_urls: string[];
  qr_payload: string | null;
  claimed_device_id: string | null;
  token?: string;
}

export interface ConnectionInfo {
  version: string;
  product: string;
  mode: string;
  port: number;
  local_url: string;
  lan_urls: string[];
  host_name: string;
  platform: string;
  platform_raw: string;
  arch: string;
  node: string;
  data_dir: string;
  console_dir: string;
  packaged: boolean;
  uptime_s: number;
  autostart: { enabled: boolean; backend: string; target: string | null; legacy: string | null };
  security: { tls: boolean; e2e: string; local_auth: string; remote_auth: string };
  options: Record<string, { available: boolean; label: string; note?: string; recommended?: boolean }>;
}

export interface NotifyStats {
  day: string;
  pushed: number;
  merged: number;
  by_level: Record<string, number>;
}

export interface PauseState {
  paused: boolean;
  until: string | null;
  minutes: number;
  reason: string | null;
  set_at: string | null;
}

export interface StatsPayload {
  notify: NotifyStats;
  pause: PauseState;
  devices: number;
}

export interface LogLine {
  at: string | null;
  level: string;
  text: string;
}

export interface AgentPathInfo {
  path: string | null;
  wsl: Array<{ distro: string; user: string; path: string }>;
  error?: string;
}

export interface AgentPaths {
  paths: Record<string, AgentPathInfo>;
  homedir: string;
  wsl_supported: boolean;
}

export interface HookInstallResult {
  ok: boolean;
  changed?: boolean;
  file?: string;
  backup?: string;
  message?: string;
  error?: string;
  manual?: boolean;
  unsupported?: boolean;
  snippet?: unknown;
}

export interface SetupState {
  completed: boolean;
  completed_at: string | null;
  chosen_agents: string[];
  method: string;
  devices: number;
}

export type PushSignalChoice = "hook" | "watch" | "poll";
export type ReplyMode = "auto" | "queue" | "off";

export interface AgentSettings {
  id: string;
  connected: boolean;
  push_to_phone: boolean;
  read_sessions: boolean;
  push_signal: PushSignalChoice;
  reply_mode: ReplyMode;
  model: string | null;
  order: number | null;
  updated_at: string | null;
}

export interface ReplyRecord {
  reply_id: number;
  event_id: number | null;
  session_id: string;
  status: string;
  message: string;
  ts: string;
  text: string;
}

export interface BuiltinAdapter {
  id: string;
  name: string;
  push: string[];
  reply: string;
  default_signal: string;
  kind: "builtin";
  home: string | null;
  config_file: string | null;
  connected: boolean;
}

export interface CustomAdapter {
  id: string;
  name: string;
  source: { kind: string; path: string; format: string };
  push: { method: string };
  reply: { mode: string; command: string };
  file: string;
  yaml?: string;
  created_at?: string;
  broken?: boolean;
}

export interface CommunityAdapter {
  id: string;
  name: string;
  owner: string;
  note: string;
}

export interface AdaptersPayload {
  builtin: BuiltinAdapter[];
  installed: Array<BuiltinAdapter | CustomAdapter>;
  custom: CustomAdapter[];
  community: CommunityAdapter[];
  formats: string[];
  push_methods: string[];
  root: string;
  placeholder: boolean;
  note: string;
}

export interface AdapterTestResult {
  ok: boolean;
  format?: string;
  checked?: string;
  is_dir?: boolean;
  file_count?: number;
  total_size?: number;
  samples?: Array<{ file: string; size: number; preview: string }>;
  error?: string | null;
  note?: string;
}

// ---------------------------------------------------------------- 便捷方法

export const endpoints = {
  agents: () => api.get<{ agents: Agent[]; generated_at: string }>("/api/agents"),
  agentPaths: () => api.get<AgentPaths>("/api/agent-paths"),
  inbox: () => api.get<Inbox>("/api/inbox"),
  sessions: (limit = 300) => api.get<{ sessions: SessionSummary[]; count: number }>(`/api/sessions?limit=${limit}`),
  events: () => api.get<HomingEvent[]>("/api/events"),
  devices: () => api.get<{ devices: Device[]; count: number; online: number }>("/api/devices"),
  connection: () => api.get<ConnectionInfo>("/api/connection"),
  stats: () => api.get<StatsPayload>("/api/stats"),
  logs: (limit = 200) => api.get<{ source: string | null; count: number; lines: LogLine[] }>(`/api/logs?limit=${limit}`),
  setup: () => api.get<SetupState>("/api/setup"),
  saveSetup: (body: { agents: string[]; method: string }) => api.post<SetupState>("/api/setup", body),
  newOffer: () => api.post<PairOffer>("/api/pair/offer", {}),
  offer: (id: string) => api.get<PairOffer>(`/api/pair/offer/${id}`),
  latestOffer: () => api.get<{ offer: PairOffer | null }>("/api/pair/latest"),
  revokeDevice: (id: string) => api.del<{ ok: boolean }>(`/api/devices/${id}`),
  renameDevice: (id: string, name: string) => api.patch<{ ok: boolean }>(`/api/devices/${id}`, { name }),
  installHooks: (id: string) => api.post<HookInstallResult>(`/api/agents/${id}/hooks`),
  pausePush: (minutes: number) => api.post<PauseState>("/api/push/pause", { minutes }),
  resumePush: () => api.del<PauseState>("/api/push/pause"),

  // 收件箱动作
  approve: (eventId: number) => api.post<{ ok: boolean }>(`/api/approvals/${eventId}`, { decision: "allow" }),
  deny: (eventId: number) => api.post<{ ok: boolean }>(`/api/approvals/${eventId}`, { decision: "deny" }),
  reply: (body: { event_id?: number | null; session_id?: string; source?: string; text: string; model?: string | null }) =>
    api.post<{ ok: boolean }>("/api/reply", body),
  replies: (sessionId?: string) =>
    api.get<{ replies: ReplyRecord[] }>(`/api/replies${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ""}`),

  // 接入设置
  agentSettings: () => api.get<{ settings: Record<string, AgentSettings>; order: string[] }>("/api/agents/settings"),
  patchAgent: (id: string, changes: Partial<AgentSettings>) =>
    api.patch<AgentSettings>(`/api/agents/${id}/settings`, changes),
  saveAgentOrder: (order: string[]) => api.put<{ ok: boolean; order: string[] }>("/api/agents/order", { order }),
  testPush: (id: string) => api.post<{ ok: boolean; placeholder?: boolean; message?: string; devices?: number }>(`/api/agents/${id}/test-push`),
  disconnectAgent: (id: string) =>
    api.post<{ ok: boolean; settings: AgentSettings; file: string | null; note: string }>(`/api/agents/${id}/disconnect`),

  // 自定义接入（清单）
  adapters: () => api.get<AdaptersPayload>("/api/adapters"),
  saveAdapter: (adapter: Record<string, unknown>) => api.post<{ ok: boolean; adapter?: CustomAdapter; errors?: string[]; file?: string }>("/api/adapters", { adapter }),
  testAdapter: (body: { path: string; format: string }) => api.post<AdapterTestResult>("/api/adapters/test", body),
  removeAdapter: (id: string) => api.del<{ ok: boolean }>(`/api/adapters/${id}`),
};

/** 轮询：页面卸载时一定要 dispose，避免离开页面还在打接口 */
export function poll(fn: () => void | Promise<void>, intervalMs: number): () => void {
  let disposed = false;
  const tick = async () => {
    if (disposed) return;
    if (document.visibilityState === "visible") {
      try {
        await fn();
      } catch (err) {
        // 单次失败不打断轮询，但要在控制台留个痕迹，方便排查
        console.warn("[poll]", (err as Error)?.message || err);
      }
    }
  };
  void tick();
  const timer = window.setInterval(tick, intervalMs);
  const onVisible = () => {
    if (document.visibilityState === "visible") void tick();
  };
  document.addEventListener("visibilitychange", onVisible);
  return () => {
    disposed = true;
    window.clearInterval(timer);
    document.removeEventListener("visibilitychange", onVisible);
  };
}
