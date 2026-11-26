// 本机接口封装。页面与 homingd 同源，鉴权靠 /console 下发的 HttpOnly cookie，
// 因此这里不带任何 token，也不往 localStorage 里塞东西。

export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
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
    const obj = payload as { error?: string; code?: string } | null;
    const message = obj?.error || `${response.status} ${response.statusText}`;
    throw new ApiError(response.status, message, obj?.code);
  }
  return payload as T;
}

async function uploadFile(cwd: string, file: File) {
  let response: Response;
  try {
    response = await fetch(
      `/api/upload?cwd=${encodeURIComponent(cwd)}&name=${encodeURIComponent(file.name)}`,
      {
        method: "POST",
        cache: "no-store",
        credentials: "same-origin",
        headers: { "Content-Type": file.type || "application/octet-stream" },
        body: file,
      },
    );
  } catch (err) {
    throw new ApiError(0, `附件上传失败：${String((err as Error)?.message || err)}`);
  }
  const text = await response.text();
  let payload: { error?: string } & Record<string, unknown> = {};
  if (text) {
    try {
      payload = JSON.parse(text) as { error?: string } & Record<string, unknown>;
    } catch {
      payload = { error: text.slice(0, 200) };
    }
  }
  if (!response.ok) {
    throw new ApiError(response.status, payload.error || `${response.status} ${response.statusText}`);
  }
  return payload as unknown as UploadedAttachment;
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
  models: Array<{
    id: string;
    label: string;
    provider?: string;
    default?: boolean;
    efforts?: string[];
  }>;
  default_model: string | null;
  icon?: string;
  installed?: boolean;
  version?: string | null;
  push_signal?: PushSignal;
  status?: AgentStatus;
  last_event_at?: string | null;
  permission_modes?: string[];
  driver?: AgentDriver | null;
}

export interface AgentDriver {
  kind: "acp" | string;
  command?: string;
  installed: boolean;
  ready: boolean;
  detail?: string;
  experimental?: boolean;
  verified: boolean;
}

export type CatalogReadiness = "builtin" | "adapter" | "hook" | "unverified";

export interface CatalogCommand {
  label: string;
  command: string;
  /** 这台电脑上有对应程序（curl / brew / npm），控制台可以直接执行 */
  runnable?: boolean;
}

export interface SetupJob {
  id: string;
  agent_id: string;
  agent_name: string;
  label: string;
  command: string;
  status: "running" | "ok" | "failed";
  exit_code?: number;
  error?: string;
  log: string;
  detected: boolean;
  started_at: string;
  ended_at?: string;
}

export interface CloudPairInvite {
  mode: "lan" | "cloud";
  server: string;
  pair_token: string;
  code6: string;
  expires_in: number;
  host_id: string;
  host_fingerprint: string;
  uri: string;
}

export interface CloudPairRequest {
  client_id: string;
  name?: string;
  platform?: string;
  fingerprint: string;
  sas: string[];
  received_at: string;
}

export interface FlowHop {
  seq: number;
  at: string;
  kind: "event" | "notify" | "reply" | "dispatch" | "result" | "room" | "output" | "turn" | "action" | string;
  from: string;
  to: string;
  source?: string;
  session_id?: string;
  title: string;
  preview?: string;
  status?: "ok" | "wait" | "fail";
  via?: "lan" | "cloud" | "local";
  bytes?: number;
}

export interface FlowNodeInfo {
  id: string;
  kind: "phone" | "homing" | "console" | "agent" | "room";
  label: string;
  detail?: string;
}

export interface CatalogIntegration {
  kind: "scanner" | "manifest" | "hook" | "wrapper" | "plugin" | "ingest";
  label: string;
  description: string;
  state: "ready" | "available" | "draft";
  path?: string;
  command?: string;
}

export interface CatalogDrive {
  supported: boolean;
  enabled: boolean;
  installed: boolean;
  ready: boolean;
  kind: "acp" | string;
  command?: string;
  detail?: string;
  experimental?: boolean;
  verified: boolean;
}

export interface AgentCatalogEntry {
  id: string;
  name: string;
  vendor?: string;
  category?: string;
  icon?: string;
  readiness: CatalogReadiness;
  detected: boolean;
  connected: boolean;
  available: boolean;
  manifest_installed: boolean;
  can_install: boolean;
  can_connect: boolean;
  can_generate_kit: boolean;
  install_kind: "builtin" | "manifest" | "hook" | "none";
  sessions: boolean;
  reply: boolean;
  models: boolean;
  push_signals: string[];
  hook_support: "native" | "generic" | "none";
  hook_install: "auto" | "snippet" | "manual" | "generic" | "none";
  hook_installed: boolean;
  hook_file?: string;
  hook_hint?: string;
  detect_bins?: string[];
  detect_paths?: string[];
  version?: string | null;
  manifest_file?: string;
  notes?: string;
  tagline?: string;
  homepage?: string;
  docs_url?: string;
  config_file?: string;
  platforms?: string[];
  catalog_source?: "homing" | "magpie" | "magpie-community" | string;
  source_url?: string;
  source_package?: string;
  install_commands?: CatalogCommand[];
  integrations?: CatalogIntegration[];
  kit_installed: boolean;
  kit_dir?: string;
  drive?: CatalogDrive;
  download_url?: string;
  can_setup?: boolean;
}

export interface AgentCatalogSummary {
  total: number;
  connected: number;
  detected: number;
  installable: number;
  hook_only: number;
  builtin: number;
}

export interface AgentCatalogPayload {
  agents: AgentCatalogEntry[];
  summary: AgentCatalogSummary;
  generated_at: string;
}

export interface CatalogInstallResult {
  ok: boolean;
  id: string;
  file: string;
  backup?: string;
  settings: AgentSettings;
}

export interface CatalogKitResult {
  ok: boolean;
  id: string;
  dir: string;
  hook: string;
  wrapper: string;
  manifest_draft: string;
  adapter_draft: string;
  readme: string;
  native_files?: string[];
  existing: boolean;
}

export interface CatalogQuickResult {
  ok: boolean;
  id: string;
  manifest_installed: boolean;
  manifest_file?: string;
  kit_installed: boolean;
  kit_existing: boolean;
  kit_dir?: string;
  hook?: string;
  wrapper?: string;
  settings: AgentSettings;
}

export interface CatalogKitRemoveResult {
  ok: boolean;
  id: string;
  dir: string;
  trash: string;
}

export interface CatalogDriveResult {
  ok: boolean;
  id: string;
  file: string;
  backup?: string;
  drive: CatalogDrive;
  probe: {
    session_id: string;
    supports_load_session: boolean;
    handshake_duration_ms: number;
    protocol_version?: unknown;
    agent_info?: unknown;
  };
  settings: AgentSettings;
}

export interface CatalogDriveRemoveResult {
  ok: boolean;
  id: string;
  file: string;
  manifest_removed: boolean;
  trash?: string;
  drive: CatalogDrive;
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
  effort?: string | null;
  permission_mode?: string | null;
  last_ts?: string;
  status?: string;
  cwd?: string;
  read?: boolean;
  pinned?: boolean;
  muted?: boolean;
  archived?: boolean;
  is_temp?: boolean;
  is_empty?: boolean;
  running?: boolean;
  running_since?: string;
  last_preview?: string;
  unread?: number;
  event_count?: number;
  context_usage?: { used: number; limit: number | null } | null;
  seen_event_id?: number;
  collab?: {
    role: "parent" | "child" | "none";
    parent?: { session_id: string; source: string; dispatch_id: string };
    children?: Array<{ dispatch_id: string; agent: string; status: string; preset?: string }>;
  };
}

export interface SearchSnippet {
  before: string;
  match: string;
  after: string;
}

export interface SearchHit {
  session: SessionSummary;
  snippet: SearchSnippet;
  item_id?: string;
}

export interface SearchResult {
  query: string;
  scope: "title" | "content";
  hits: SearchHit[];
  searched: number;
  truncated: boolean;
  indexed?: number;
  fallback_like?: boolean;
  indexed_search?: boolean;
  degraded?: boolean;
  degraded_reason?: string;
  generated_at?: string;
}

export interface SessionItemTool {
  name: string;
  input_summary: string | null;
  output_tail: string | null;
  exit_code: number | null;
  status: string;
}

export interface SessionItemFile {
  path: string;
  added: number;
  removed: number;
  diff: string;
}

export interface SessionItem {
  ts: string;
  id: string;
  seq: number;
  type: string;
  role: string;
  partial?: boolean;
  truncated?: boolean;
  text?: string;
  model?: string;
  command?: string;
  exit_code?: number;
  tool?: SessionItemTool;
  files?: SessionItemFile[];
  plan?: Array<{ step: string; status: string }>;
  error?: string;
}

export interface SessionItemsPayload {
  session_id: string;
  source: string | null;
  count: number;
  items: SessionItem[];
  turns: unknown[];
  reason: string | null;
}

export interface SessionCommand {
  name: string;
  description: string;
  argument_hint?: string;
}

export interface UploadedAttachment {
  ok: boolean;
  path: string;
  name: string;
  size: number;
  mime: string;
}

export interface StartSessionResult {
  ok: boolean;
  status?: string;
  session_id?: string | null;
  request_id?: string;
  warning?: string;
  cwd?: string;
  code?: string;
  error?: string;
  message?: string;
}

export interface DispatchRecord {
  dispatch_id: string;
  parent_session_id: string;
  parent_source: string;
  parent_event_id: number | null;
  target_agent: string;
  target_session_id?: string;
  request_id?: string;
  text: string;
  cwd: string;
  mode: string;
  preset?: string;
  workspace?: string;
  thread?: string;
  branch?: string;
  repo_cwd?: string;
  worktree_discarded_at?: string;
  return?: string;
  status: string;
  message?: string;
  result?: {
    text: string;
    files_changed?: string[];
    finished_at: string;
    branch?: string;
    diff_stat?: string;
  };
  created_at: string;
  updated_at: string;
}

export interface ComposePlanMention {
  agent: string;
  preset: string;
  text: string;
  target: "new" | "continue" | string;
  thread_id?: string;
  workspace: string;
  context: string[];
  dispatchable: boolean;
  reason?: string;
}

export interface ComposePlan {
  message: string;
  mentions: ComposePlanMention[];
}

export interface ComposeResult {
  ok: boolean;
  plan: ComposePlan;
  dry_run?: boolean;
  reply?: { reply_id: number; status: string; message?: string } | null;
  dispatches?: DispatchRecord[];
  error?: string;
  code?: string;
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
  approval?: HomeApproval;
}

export interface Inbox {
  waiting: InboxItem[];
  running: InboxItem[];
  recent_done: InboxItem[];
  counts: { waiting: number; running: number };
  generated_at: string;
}

export interface AwayPeriod {
  id: string;
  started_at: string;
  ended_at?: string | null;
  source?: string;
  device_id?: string | null;
  cleared_at?: string | null;
}

export interface AwayState {
  enabled: boolean;
  current?: AwayPeriod;
  last?: AwayPeriod;
}

export interface AutoAwaySettings {
  enabled: boolean;
  idle_minutes: number;
  available: boolean;
}

export interface HomeApproval {
  command: string;
  options: Array<{
    id?: string;
    option_id?: string;
    label?: string;
    name?: string;
    kind?: string;
    decision?: string;
  }>;
}

export interface HomeCollab {
  children: number;
  running: number;
  needs_you: number;
}

export type HomeReason =
  | "approval"
  | "needs_reply"
  | "error"
  | "collab_result"
  | "done"
  | "progress";

export interface HomeAttention {
  session: SessionSummary;
  reason: HomeReason;
  since_away: boolean;
  event_id: number;
  unseen_count: number;
  headline: string;
  summary: string;
  at: string;
  actions: string[];
  approval?: HomeApproval;
  collab?: HomeCollab;
}

export interface HomeSettings {
  new_activity: "away" | "unread";
  collab_return: "card" | "inject";
}

export interface Home {
  away: {
    enabled: boolean;
    period?: AwayPeriod;
    current?: AwayPeriod;
  };
  attention: HomeAttention[];
  running: SessionSummary[];
  resume: SessionSummary[];
  earlier: { count: number; next_cursor?: string };
  counts: { needs_you: number; away_new: number; running: number; earlier: number };
  settings: HomeSettings;
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
  transport: "token" | "e2e";
  last_ip: string | null;
  scopes: string[];
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
  runtime?: "go" | "node";
  runtime_version?: string;
  data_dir: string;
  console_dir: string;
  packaged: boolean;
  uptime_s: number;
  autostart: { enabled: boolean; backend: string; target: string | null; legacy: string | null };
  security: { tls: boolean; e2e: string; local_auth: string; remote_auth: string };
  options: Record<string, { available: boolean; label: string; note?: string; recommended?: boolean }>;
  cloud?: CloudStatus;
}

export interface SearchIndexStats {
  path: string;
  exists: boolean;
  size_bytes: number;
  sessions: number;
  items: number;
  trigram: boolean;
  updated_at?: string;
  open_error?: string;
}

export interface SearchIndexStatus {
  enabled: boolean;
  stats?: SearchIndexStats;
  fts5?: boolean;
  error?: string;
}

export interface SearchIndexActionResult {
  ok: boolean;
  enabled?: boolean;
  indexed?: number;
}

export interface CloudStatus {
  configured: boolean;
  connected: boolean;
  server: string;
  peers: number;
  direct_dial?: boolean;
  latency_ms?: number;
  proxy_env_detected?: boolean;
  tun_fake_ip_detected?: boolean;
  tun_fake_ip_bypassed?: boolean;
  resolved_ip?: string;
  relay_key_configured?: boolean;
  relay_key_hint?: string;
  last_error?: string;
  relay_notice?: string;
}

export interface KeepAwakeState {
  available: boolean;
  active: boolean;
  enabled: boolean;
  auto_on_away: boolean;
  max_minutes: number;
  until?: string | null;
  until_ms?: number;
  reason?: string;
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
  dirs?: { manifests: string; scripts: string };
}

export interface IngestTokenResult {
  ok: boolean;
  agent: string;
  token: string;
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

export type LibraryKind =
  | "skill"
  | "instructions"
  | "mcp"
  | "hook"
  | "workflow"
  | "plugin"
  | "context"
  | "pack";

export interface LibraryTarget {
  agent: string;
  label: string;
  path: string;
  entry?: string;
  managed?: boolean;
}

export interface LibraryResource {
  id: string;
  kind: LibraryKind;
  name: string;
  description?: string;
  version?: string;
  source: string;
  trust: string;
  scope: string;
  state: string;
  targets: LibraryTarget[];
  file_count: number;
  size_bytes: number;
  modified_at: string;
  removable: boolean;
  warnings?: string[];
}

export interface LibrarySource {
  id: string;
  agent: string;
  label: string;
  path: string;
  kind: LibraryKind;
  managed: boolean;
  exists: boolean;
  count: number;
  note?: string;
}

export interface LibrarySnapshot {
  resources: LibraryResource[];
  sources: LibrarySource[];
  counts: Partial<Record<LibraryKind, number>>;
  total: number;
  managed: number;
  generated_at: string;
}

export interface LibraryFile {
  path: string;
  size: number;
  modified_at: string;
}

export interface LibraryDetail extends LibraryResource {
  files: LibraryFile[];
}

export interface LibraryInspection {
  path: string;
  name: string;
  kind: LibraryKind;
  description?: string;
  version?: string;
  file_count: number;
  size_bytes: number;
  checksum?: string;
  files: LibraryFile[];
  warnings?: string[];
  suggested_id: string;
}

export interface LibraryPublishCheck {
  ok: boolean;
  inspection: LibraryInspection;
  manifest?: {
    apiVersion: string;
    kind: string;
    metadata: {
      id: string;
      name: string;
      version: string;
      description?: string;
      license?: string;
      source: string;
      checksum: string;
      tags?: string[];
    };
    spec?: {
      targets?: Array<{ agent: string; mode?: string }>;
      permissions?: {
        filesystem?: { read?: string[]; write?: string[] };
        network?: boolean;
        commands?: string[];
      };
      install?: { strategy?: string; backup?: boolean };
      uninstall?: { strategy?: string };
    };
    path?: string;
  };
  manifest_path?: string;
  warnings?: string[];
  problems?: string[];
  note: string;
}

export interface LibraryPlanAction {
  action: string;
  from?: string;
  to?: string;
  file_count?: number;
}

export interface LibraryImportPlan {
  ok: boolean;
  source: string;
  destination: string;
  kind: LibraryKind;
  name: string;
  actions: LibraryPlanAction[];
  warnings?: string[];
  inspection: LibraryInspection;
}

export interface LibraryAuditEntry {
  at: string;
  action: string;
  device?: string;
  resource_id?: string;
  kind?: string;
}

export interface LibraryRemoteProvider {
  id: string;
  label: string;
  example: string;
  note: string;
  ready: boolean;
}

export interface LibraryRemotePlan {
  ok: boolean;
  fetch_id: string;
  provider: string;
  url: string;
  resolved_url: string;
  download_sha256: string;
  trust: string;
  warnings?: string[];
  inspection: LibraryInspection;
  plan: LibraryImportPlan;
  manifest?: {
    apiVersion: string;
    kind: string;
    metadata: {
      id: string;
      name: string;
      version: string;
      description?: string;
      license?: string;
      source: string;
      checksum: string;
      tags?: string[];
    };
    spec?: {
      targets?: Array<{ agent: string; mode?: string }>;
      permissions?: {
        filesystem?: { read?: string[]; write?: string[] };
        network?: boolean;
        commands?: string[];
      };
    };
  };
  fetched_at: string;
  expires_at: string;
}

export interface LibraryInstallation {
  id: string;
  resource_id: string;
  name: string;
  kind: LibraryKind;
  version?: string;
  destination: string;
  source_url?: string;
  resolved_url?: string;
  download_sha256?: string;
  trust: string;
  state: "installed" | "rolled_back" | string;
  installed_at: string;
  rolled_back_at?: string;
  trash_path?: string;
  backup_path?: string;
  previous_version?: string;
  previous_download_sha256?: string;
  previous_resolved_url?: string;
  previous_trust?: string;
  previous_installed_at?: string;
}

export interface LibraryRemoteUpdatePlan {
  ok: boolean;
  installation_id: string;
  resource_id: string;
  name: string;
  current_version?: string;
  available_version?: string;
  current_checksum?: string;
  available_checksum?: string;
  current_download_sha256?: string;
  available_download_sha256?: string;
  update_available: boolean;
  plan: LibraryRemotePlan;
  warnings?: string[];
  checked_at: string;
}

export interface LibraryPackManifest {
  apiVersion: string;
  kind: string;
  metadata: {
    id: string;
    name: string;
    version: string;
    description?: string;
    source?: string;
    tags?: string[];
  };
  spec: {
    resources: Array<{
      id: string;
      path: string;
      kind: LibraryKind;
      name?: string;
      targets: Array<{ agent: string; mode?: string }>;
      requires?: string[];
    }>;
  };
  path?: string;
}

export interface LibraryPackTargetPlan {
  agent: string;
  mode: string;
  destination: string;
  actions?: LibraryPlanAction[];
}

export interface LibraryPackMemberPlan {
  id: string;
  name: string;
  kind: LibraryKind;
  source: string;
  targets: LibraryPackTargetPlan[];
  requires?: string[];
  file_count: number;
  size_bytes: number;
  checksum: string;
  warnings?: string[];
}

export interface LibraryPackPlan {
  ok: boolean;
  plan_id: string;
  pack: LibraryPackManifest;
  root: string;
  members: LibraryPackMemberPlan[];
  order: string[];
  warnings?: string[];
  created_at: string;
  expires_at: string;
  total_files: number;
  total_bytes: number;
  destinations: string[];
}

export interface LibraryInstalledPackTarget {
  agent: string;
  destination: string;
}

export interface LibraryInstalledPackMember {
  id: string;
  name: string;
  kind: LibraryKind;
  checksum: string;
  targets: LibraryInstalledPackTarget[];
}

export interface LibraryPackInstallation {
  id: string;
  plan_id: string;
  pack_id: string;
  name: string;
  version?: string;
  description?: string;
  source?: string;
  root: string;
  state: "installed" | "rolled_back" | string;
  installed_at: string;
  rolled_back_at?: string;
  trash_path?: string;
  missing?: string[];
  members: LibraryInstalledPackMember[];
}

// ---------------------------------------------------------------- 便捷方法

export interface RoomParticipant {
  agent: string; name: string; role?: string; status: string; session_id?: string;
}
export interface RoomMessage {
  seq: number; from: string; name: string; kind: string; round: number; text: string; at: string;
}
export interface RoomAction {
  id: string;
  text: string;
  agent?: string;
  dispatch_id?: string;
  status: string;
  session_id?: string;
  result?: string;
  error?: string;
  base_commit?: string;
}
export interface Room {
  id: string; title: string; topic: string; cwd: string; rounds: number; round: number;
  status: "running" | "paused" | "summarizing" | "ended";
  speaking?: string; next_speaker?: string; end_requested?: boolean;
  participants: RoomParticipant[]; messages?: RoomMessage[]; actions?: RoomAction[]; minutes?: string;
  message_count?: number; preview?: string; note?: string; updated_at: string;
}
export interface RoomAgentChoice { id: string; name: string }
export interface NewRoom {
  topic: string; cwd: string; rounds: number;
  participants: Array<{ agent: string; role: string }>;
}

export const endpoints = {
  rooms: () => api.get<{ rooms: Room[]; agents: RoomAgentChoice[] }>("/api/rooms"),
  room: (id: string) => api.get<{ room: Room }>(`/api/rooms/${encodeURIComponent(id)}`),
  createRoom: (body: NewRoom) => api.post<{ room: Room }>("/api/rooms", body),
  sayInRoom: (id: string, text: string) => api.post<{ room: Room }>(`/api/rooms/${encodeURIComponent(id)}/messages`, { text }),
  controlRoom: (id: string, action: string, agent?: string) => api.post<{ room: Room }>(`/api/rooms/${encodeURIComponent(id)}/control`, { action, ...(agent ? { agent } : {}) }),
  dispatchRoomAction: (id: string, actionId: string, body: { confirmed: boolean; agent: string; text: string; base_commit?: string }) =>
    api.post<{ ok: boolean; room: Room }>(`/api/rooms/${encodeURIComponent(id)}/actions/${encodeURIComponent(actionId)}/dispatch`, body),

  agents: () => api.get<{ agents: Agent[]; generated_at: string }>("/api/agents"),
  agentCatalog: () => api.get<AgentCatalogPayload>("/api/agent-catalog"),
  installCatalogAgent: (id: string) => api.post<CatalogInstallResult>(`/api/agent-catalog/${id}/install`),
  installCatalogKit: (id: string) => api.post<CatalogKitResult>(`/api/agent-catalog/${id}/kit`),
  quickConnectCatalogAgent: (id: string) => api.post<CatalogQuickResult>(`/api/agent-catalog/${id}/quick`),
  setupCatalogAgent: (id: string, index: number) =>
    api.post<{ ok: boolean; job: SetupJob }>(`/api/agent-catalog/${id}/setup`, { index }),
  setupJob: (job: string) => api.get<{ ok: boolean; job: SetupJob }>(`/api/agent-setup/${job}`),
  cloudPairStart: () => api.post<CloudPairInvite>("/api/cloud/pair/start", {}),
  cloudPairRequests: () => api.get<{ mode: string; requests: CloudPairRequest[] }>("/api/cloud/pair/requests"),
  cloudPairDecision: (clientId: string, accept: boolean) =>
    api.post<{ ok: boolean }>("/api/cloud/pair/decision", { client_id: clientId, accept }),
  flow: (since = 0) => api.get<{ seq: number; hops: FlowHop[]; nodes: FlowNodeInfo[] }>(`/api/flow?since=${since}`),
  enableCatalogDrive: (id: string) => api.post<CatalogDriveResult>(`/api/agent-catalog/${id}/drive`),
  removeCatalogDrive: (id: string) => api.del<CatalogDriveRemoveResult>(`/api/agent-catalog/${id}/drive`),
  removeCatalogKit: (id: string) => api.del<CatalogKitRemoveResult>(`/api/agent-catalog/${id}/kit`),
  removeCatalogAgent: (id: string) =>
    api.del<{ ok: boolean; id: string; removed: Array<{ file: string; trash: string }>; settings: AgentSettings }>(
      `/api/agent-catalog/${id}`,
    ),
  agentPaths: () => api.get<AgentPaths>("/api/agent-paths"),
  inbox: () => api.get<Inbox>("/api/inbox"),
  home: () => api.get<Home>("/api/home"),
  away: () => api.get<AwayState>("/api/away"),
  autoAwaySettings: () => api.get<AutoAwaySettings>("/api/away/settings"),
  setAutoAwaySettings: (body: { enabled: boolean; idle_minutes?: number }) =>
    api.patch<{ ok: boolean; enabled: boolean; idle_minutes: number }>("/api/away/settings", body),
  setAway: (enabled: boolean) =>
    api.post<{ ok: boolean; enabled: boolean; period?: AwayPeriod; current?: AwayPeriod; last?: AwayPeriod }>(
      "/api/away",
      { enabled },
    ),
  clearAway: (id: string) =>
    api.post<{ ok: boolean; period: AwayPeriod }>(`/api/away/periods/${encodeURIComponent(id)}/clear`),
  homeSettings: () => api.get<{ ok: boolean; settings: HomeSettings }>("/api/home/settings"),
  updateHomeSettings: (body: Partial<Pick<HomeSettings, "new_activity" | "collab_return">>) =>
    api.patch<{ ok: boolean; settings: HomeSettings }>("/api/home/settings", body),
  setHomeNewActivity: (scope: "away" | "unread") =>
    api.patch<{ ok: boolean; settings: HomeSettings }>("/api/home/settings", {
      new_activity: scope,
    }),
  sessions: (limit = 300) => api.get<{ sessions: SessionSummary[]; count: number }>(`/api/sessions?limit=${limit}`),
  searchSessions: (query: string, scope: "title" | "content" = "content", source = "", limit = 50) => {
    const params = new URLSearchParams({
      q: query,
      scope,
      limit: String(Math.max(1, Math.min(200, limit))),
    });
    if (source) params.set("source", source);
    return api.get<SearchResult>(`/api/search?${params.toString()}`);
  },
  startSession: (body: {
    source: string;
    cwd: string;
    text: string;
    files?: string[];
    model?: string | null;
    effort?: string | null;
    permission_mode?: string | null;
  }) => api.post<StartSessionResult>("/api/sessions/new", body),
  session: (id: string) => api.get<SessionSummary>(`/api/sessions/${encodeURIComponent(id)}`),
  sessionItems: async (id: string, limit = 600, afterSeq?: number, includeReasoning = false) => {
    const payload = await api.get<SessionItemsPayload>(
      `/api/sessions/${encodeURIComponent(id)}/items?limit=${limit}` +
        (afterSeq && afterSeq > 0 ? `&after_seq=${afterSeq}` : "") +
        (includeReasoning ? "&include_reasoning=1" : ""),
    );
    return {
      ...payload,
      items: payload.items || [],
      turns: payload.turns || [],
    };
  },
  sessionCommands: (id: string) =>
    api.get<{ session_id: string; source: string; commands: SessionCommand[]; count: number }>(
      `/api/sessions/${encodeURIComponent(id)}/commands`,
    ),
  patchSession: (id: string, body: { title?: string; read?: boolean; pinned?: boolean; muted?: boolean; archived?: boolean }) =>
    api.patch<{ session_id: string; source: string; state: unknown; session: SessionSummary | null }>(
      `/api/sessions/${encodeURIComponent(id)}`,
      body,
    ),
  stopSession: (id: string) =>
    api.post<{ ok: boolean; session_id: string; via?: string; error?: string; message?: string }>(
      `/api/sessions/${encodeURIComponent(id)}/stop`,
      {},
    ),
  fsComplete: (cwd: string, prefix: string) =>
    api.get<{ cwd: string; prefix: string; items: Array<{ path: string; type: string }>; count: number; truncated: boolean }>(
      `/api/fs/complete?cwd=${encodeURIComponent(cwd)}&prefix=${encodeURIComponent(prefix)}`,
    ),
  uploadAttachment: (cwd: string, file: File) => uploadFile(cwd, file),
  compose: (body: {
    session_id: string;
    source?: string;
    cwd?: string;
    text: string;
    files?: string[];
    model?: string | null;
    effort?: string | null;
    permission_mode?: string | null;
    collab?: { context?: string[]; workspace?: string; return?: string };
    mentions?: Array<{
      agent?: string;
      target?: "auto" | "new" | "continue";
      context?: string[];
      workspace?: "auto" | "shared" | "readonly" | "worktree";
    }>;
    dry_run?: boolean;
  }) => api.post<ComposeResult>("/api/compose", body),
  dispatches: (parentSessionId?: string) =>
    api.get<{ dispatches: DispatchRecord[]; count: number }>(
      `/api/dispatches${parentSessionId ? `?parent_session_id=${encodeURIComponent(parentSessionId)}` : ""}`,
    ),
  forwardDispatch: (id: string, text?: string) =>
    api.post<{ ok: boolean; dispatch_id: string; reply: { reply_id: number; status: string; message?: string } }>(
      `/api/dispatches/${encodeURIComponent(id)}/forward`,
      text ? { text } : {},
    ),
  replyDispatch: (id: string, text: string) =>
    api.post<{ ok: boolean; dispatch: DispatchRecord; reply: { reply_id: number; status: string; message?: string } }>(
      `/api/dispatches/${encodeURIComponent(id)}/reply`,
      { text },
    ),
  cancelDispatch: (id: string) =>
    api.post<{ ok: boolean; dispatch: DispatchRecord; stopped?: boolean; error?: string }>(
      `/api/dispatches/${encodeURIComponent(id)}/cancel`,
      {},
    ),
  discardDispatchWorktree: (id: string, confirm = true) =>
    api.post<{
      ok: boolean;
      dispatch: DispatchRecord;
      removed_worktree?: boolean;
      removed_branch?: boolean;
      error?: string;
      code?: string;
    }>(`/api/dispatches/${encodeURIComponent(id)}/discard`, { confirm }),
  events: () => api.get<HomingEvent[]>("/api/events"),
  devices: () => api.get<{ devices: Device[]; count: number; online: number }>("/api/devices"),
  connection: () => api.get<ConnectionInfo>("/api/connection"),
  searchIndexStatus: () => api.get<SearchIndexStatus>("/api/search/status"),
  searchIndexAction: (action: "enable" | "disable" | "rebuild" | "delete") =>
    api.post<SearchIndexActionResult>("/api/search/index", { action }),
  configureCloud: (server?: string, useSystemProxy?: boolean, relayKey?: string) =>
    api.put<{ ok: boolean; cloud: CloudStatus }>("/api/cloud/config", {
      ...(server !== undefined ? { server } : {}),
      ...(useSystemProxy !== undefined ? { use_system_proxy: useSystemProxy } : {}),
      ...(relayKey !== undefined ? { relay_key: relayKey } : {}),
    }),
  keepAwake: () => api.get<KeepAwakeState>("/api/keepawake"),
  updateKeepAwake: (body: { enabled?: boolean; auto_on_away?: boolean; max_minutes?: number }) =>
    api.patch<KeepAwakeState>("/api/keepawake", body),
  openDesktopWindow: (path: string) =>
    api.post<{ ok: boolean; handled_by_shell: boolean; path: string }>("/api/desktop/window", {
      action: "open",
      path,
    }),
  openExternal: (url: string) =>
    api.post<{ ok: boolean; opened: boolean; url: string }>("/api/desktop/open-external", { url }),
  relayProxy: <T = Record<string, unknown>>(body: {
    server: string;
    method?: string;
    path: string;
    admin_token?: string;
    payload?: unknown;
    use_system_proxy?: boolean;
  }) => api.post<{ ok: boolean; status: number; data: T }>("/api/desktop/relay-proxy", body),
  stats: () => api.get<StatsPayload>("/api/stats"),
  logs: (limit = 200) => api.get<{ source: string | null; count: number; lines: LogLine[] }>(`/api/logs?limit=${limit}`),
  setup: () => api.get<SetupState>("/api/setup"),
  saveSetup: (body: { agents: string[]; method: string }) => api.post<SetupState>("/api/setup", body),
  newOffer: () => api.post<PairOffer>("/api/pair/offer", {}),
  offer: (id: string) => api.get<PairOffer>(`/api/pair/offer/${id}`),
  latestOffer: () => api.get<{ offer: PairOffer | null }>("/api/pair/latest"),
  revokeDevice: (id: string) => api.del<{ ok: boolean }>(`/api/devices/${id}`),
  renameDevice: (id: string, name: string) => api.patch<{ ok: boolean }>(`/api/devices/${id}`, { name }),
  setDeviceScopes: (id: string, scopes: string[]) =>
    api.patch<{ ok: boolean; device: Device }>(`/api/devices/${id}`, { scopes }),
  installHooks: (id: string) => api.post<HookInstallResult>(`/api/agents/${id}/hooks`),
  pausePush: (minutes: number) => api.post<PauseState>("/api/push/pause", { minutes }),
  resumePush: () => api.del<PauseState>("/api/push/pause"),

  // 收件箱动作
  resolveApproval: (eventId: number, decision: "allow" | "deny", optionId?: string) =>
    api.post<{ ok: boolean }>(`/api/approvals/${eventId}`, {
      decision,
      option_id: optionId || undefined,
    }),
  approve: (eventId: number) => api.post<{ ok: boolean }>(`/api/approvals/${eventId}`, { decision: "allow" }),
  deny: (eventId: number) => api.post<{ ok: boolean }>(`/api/approvals/${eventId}`, { decision: "deny" }),
  reply: (body: { event_id?: number | null; session_id?: string; source?: string; text: string; model?: string | null }) =>
    api.post<{ ok: boolean }>("/api/reply", body),
  replies: (sessionId?: string) =>
    api.get<{ replies: ReplyRecord[] }>(`/api/replies${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ""}`),
  cancelReply: (replyId: string) =>
    api.post<{ ok: boolean; reply_id?: string; status?: string }>(`/api/replies/${encodeURIComponent(replyId)}/cancel`, {}),

  // 接入设置
  agentSettings: () => api.get<{ settings: Record<string, AgentSettings>; order: string[] }>("/api/agents/settings"),
  patchAgent: (id: string, changes: Partial<AgentSettings>) =>
    api.patch<AgentSettings>(`/api/agents/${id}/settings`, changes),
  saveAgentOrder: (order: string[]) => api.put<{ ok: boolean; order: string[] }>("/api/agents/order", { order }),
  testPush: (id: string) =>
    api.post<{ ok: boolean; placeholder?: boolean; message?: string; devices?: number; delivered?: number }>(
      `/api/agents/${id}/test-push`,
    ),
  disconnectAgent: (id: string) =>
    api.post<{ ok: boolean; settings: AgentSettings; file: string | null; note: string }>(`/api/agents/${id}/disconnect`),

  // 自定义接入（清单）
  adapters: () => api.get<AdaptersPayload>("/api/adapters"),
  saveAdapter: (adapter: Record<string, unknown>) => api.post<{ ok: boolean; adapter?: CustomAdapter; errors?: string[]; file?: string }>("/api/adapters", { adapter }),
  testAdapter: (body: { path: string; format: string }) => api.post<AdapterTestResult>("/api/adapters/test", body),
  removeAdapter: (id: string) => api.del<{ ok: boolean }>(`/api/adapters/${id}`),
  issueIngestToken: (agent: string, replyUrl?: string) =>
    api.post<IngestTokenResult>("/api/ingest/tokens", { agent, reply_url: replyUrl || undefined }),

  // 资源库
  library: (query?: { query?: string; kind?: string; agent?: string }) => {
    const params = new URLSearchParams();
    if (query?.query) params.set("query", query.query);
    if (query?.kind) params.set("kind", query.kind);
    if (query?.agent) params.set("agent", query.agent);
    const suffix = params.toString() ? `?${params.toString()}` : "";
    return api.get<LibrarySnapshot>(`/api/library${suffix}`);
  },
  libraryDetail: (id: string) => api.get<LibraryDetail>(`/api/library/resources/${encodeURIComponent(id)}`),
  librarySources: () => api.get<{ sources: LibrarySource[]; count: number; generated_at: string }>("/api/library/sources"),
  libraryProviders: () =>
    api.get<{ sources: LibrarySource[]; providers: LibraryRemoteProvider[]; count: number }>(
      "/api/library/sources/available",
    ),
  libraryAudit: () => api.get<{ entries: LibraryAuditEntry[]; count: number }>("/api/library/audit"),
  inspectLibrary: (path: string, kind?: string) =>
    api.post<{ ok: boolean; inspection: LibraryInspection }>("/api/library/inspect", { path, kind }),
  planLibrary: (body: { path: string; kind?: string; name?: string }) =>
    api.post<LibraryImportPlan>("/api/library/plan", body),
  installLibrary: (body: { path: string; kind?: string; name?: string }) =>
    api.post<{ ok: boolean; plan: LibraryImportPlan; resource_id: string }>("/api/library/install", body),
  planRemoteLibrary: (body: { url: string; kind?: string; name?: string; ref?: string }) =>
    api.post<LibraryRemotePlan>("/api/library/remote/plan", body),
  installRemoteLibrary: (fetchId: string) =>
    api.post<{ ok: boolean; plan: LibraryRemotePlan; installation: LibraryInstallation }>(
      "/api/library/remote/install",
      { fetch_id: fetchId },
    ),
  libraryInstallations: () =>
    api.get<{ installations: LibraryInstallation[]; count: number }>("/api/library/installations"),
  planLibraryInstallationUpdate: (id: string) =>
    api.post<LibraryRemoteUpdatePlan>(
      `/api/library/installations/${encodeURIComponent(id)}/update/plan`,
      {},
    ),
  applyLibraryInstallationUpdate: (id: string, fetchId: string) =>
    api.post<{ ok: boolean; plan: LibraryRemotePlan; installation: LibraryInstallation }>(
      `/api/library/installations/${encodeURIComponent(id)}/update`,
      { fetch_id: fetchId },
    ),
  rollbackLibraryInstallation: (id: string) =>
    api.post<{ ok: boolean; installation: LibraryInstallation; trash_path: string }>(
      `/api/library/installations/${encodeURIComponent(id)}/rollback`,
      {},
    ),
  libraryPacks: () =>
    api.get<{ packs: LibraryPackInstallation[]; count: number }>("/api/library/packs"),
  planLibraryPack: (path: string) =>
    api.post<LibraryPackPlan>("/api/library/packs/plan", { path }),
  installLibraryPack: (planId: string) =>
    api.post<{ ok: boolean; installation: LibraryPackInstallation }>(
      "/api/library/packs/install",
      { plan_id: planId },
    ),
  rollbackLibraryPack: (id: string) =>
    api.post<{ ok: boolean; installation: LibraryPackInstallation; trash_path: string; missing: string[] }>(
      `/api/library/packs/${encodeURIComponent(id)}/rollback`,
      {},
    ),
  removeLibrary: (id: string, target?: string) =>
    api.del<{ ok: boolean; id: string; moved: string[]; trash_path: string }>(
      `/api/library/resources/${encodeURIComponent(id)}${target ? `?target=${encodeURIComponent(target)}` : ""}`,
    ),
  checkLibraryPublish: (path: string, kind?: string) =>
    api.post<LibraryPublishCheck>(
      "/api/library/publish/check",
      { path, kind },
    ),
};

/** 轮询：页面卸载时一定要 dispose，避免离开页面还在打接口 */
export function poll(fn: () => void | Promise<void>, intervalMs: number): () => void {
  let disposed = false;
  let running = false;
  const tick = async () => {
    if (disposed || running) return;
    if (document.visibilityState === "visible") {
      running = true;
      try {
        await fn();
      } catch (err) {
        // 单次失败不打断轮询，但要在控制台留个痕迹，方便排查
        console.warn("[poll]", (err as Error)?.message || err);
      } finally {
        running = false;
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
