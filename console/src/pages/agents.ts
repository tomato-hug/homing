// 使用 › Agent：一个入口完成 Agent 发现、快速接入和事件桥配置。
// 已接入、本机检测、快速接入与更多适配都使用横排列表，搜索保留紧凑结果卡。

import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { AGENT_INITIALS, AGENT_LABELS } from "../lib/format";
import { agentLogo } from "../lib/agent-icons";
import {
  endpoints,
  poll,
  type AdaptersPayload,
  type Agent,
  type AgentCatalogEntry,
  type AgentSettings,
  type CatalogIntegration,
} from "../lib/api";
import {
  button,
  emptyState,
  groupHead,
  iconButton,
  listCard,
  reportError,
  segmented,
  toast,
  toggle,
} from "../lib/ui";
import type { PageContext, PageResult } from "../main";

type Filter = "all" | "connected" | "detected" | "installable" | "discover";
type Bucket = Exclude<Filter, "all">;

const FILTERS: Array<{ id: Filter; label: string }> = [
  { id: "all", label: "全部" },
  { id: "connected", label: "已接入" },
  { id: "detected", label: "本机检测" },
  { id: "installable", label: "快速接入" },
  { id: "discover", label: "更多适配" },
];

const BUCKET_TITLES: Record<Bucket, string> = {
  connected: "已接入",
  detected: "本机已检测",
  installable: "快速接入",
  discover: "更多适配",
};

const SIGNAL_OPTIONS = [
  { value: "hook" as const, label: "事件钩子", title: "agent 主动调用本机接口，最及时" },
  { value: "watch" as const, label: "监听文件", title: "盯住会话目录的变化，比钩子慢几秒" },
  { value: "poll" as const, label: "轮询", title: "定时扫一遍会话文件，最省事但最慢" },
];

const REPLY_OPTIONS = [
  { value: "auto" as const, label: "直接回复", title: "你点了回复就发过去" },
  { value: "queue" as const, label: "排队确认", title: "先攒着，等你在手机上确认" },
  { value: "off" as const, label: "不回复", title: "只读，不往回写" },
];

const INTEGRATION_ICONS: Record<CatalogIntegration["kind"], string> = {
  scanner: icons.agents,
  manifest: icons.fileText,
  hook: icons.bolt,
  wrapper: icons.terminal,
  plugin: icons.puzzle,
  ingest: icons.server,
};

interface RowCommand {
  label: string;
  text: string;
  extra?: number;
}

export function renderAgents(ctx: PageContext): PageResult {
  const root = h("div", { class: "agent-library" });
  let disposed = false;
  let agents: Agent[] = [];
  let catalog: AgentCatalogEntry[] = [];
  let settings: Record<string, AgentSettings> = {};
  let order: string[] = [];
  let adapters: AdaptersPayload | null = null;
  let sessionCounts: Record<string, number> = {};
  let sessionCountsLoaded = false;
  let filter: Filter = "all";
  let query = "";
  const expanded = new Set<string>();
  let dragId: string | null = null;

  const libraryHead = h(
    "div",
    { class: "agent-library-head compact" },
    h(
      "div",
      { class: "agent-library-copy" },
      h("h1", { text: "Agent 支援庫" }),
      h("p", { text: "按真實能力接入；打開詳情可查看鉤子、命令與本機路徑。" }),
    ),
  );
  const tabsHost = h("div", { class: "segmented agent-filter" });
  const searchInput = h("input", {
    class: "input agents-search",
    type: "search",
    placeholder: "搜索 Agent、厂商、接入方式",
    spellcheck: "false",
  });
  const countNote = h("div", { class: "agents-count" });
  const toolbar = h(
    "div",
    { class: "agents-toolbar" },
    tabsHost,
    h("div", { class: "agents-tools" }, searchInput, countNote),
  );
  const bodyHost = h("div", { class: "agent-library-body" });
  root.append(libraryHead, toolbar, bodyHost);
  bodyHost.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取 Agent 目录…" })));

  searchInput.addEventListener("input", () => {
    query = (searchInput as HTMLInputElement).value;
    renderBody();
  });

  // ------------------------------------------------------------ 数据

  async function load(silent = false) {
    try {
      const [agentsPayload, settingsPayload, catalogPayload, adaptersPayload] = await Promise.all([
        endpoints.agents(),
        endpoints.agentSettings(),
        endpoints.agentCatalog(),
        endpoints.adapters().catch(() => null),
      ]);
      if (disposed) return;
      agents = agentsPayload.agents;
      settings = settingsPayload.settings;
      order = settingsPayload.order;
      catalog = catalogPayload.agents;
      adapters = adaptersPayload;
      for (const agent of agents) {
        if (!settings[agent.source]) settings[agent.source] = defaultSettings(agent);
      }
      paint();
      if (!silent) ctx.refreshShell();
      void loadSessions();
    } catch (err) {
      if (disposed) return;
      clear(bodyHost);
      bodyHost.append(
        h(
          "div",
          { class: "list-card" },
          h(
            "div",
            { class: "empty" },
            h("div", { text: "读取 Agent 失败" }),
            h("div", { class: "note", text: (err as Error).message }),
          ),
        ),
      );
    }
  }

  function loadSessions() {
    return endpoints
      .sessions(500)
      .then((sessionsPayload) => {
        if (disposed) return;
        sessionCounts = {};
        for (const session of sessionsPayload.sessions) {
          sessionCounts[session.source] = (sessionCounts[session.source] || 0) + 1;
        }
        sessionCountsLoaded = true;
        renderBody();
      })
      .catch(() => {
        if (!disposed) {
          sessionCountsLoaded = true;
          renderBody();
        }
      });
  }

  function defaultSettings(agent: Agent): AgentSettings {
    return {
      id: agent.source,
      connected: Boolean(agent.installed),
      push_to_phone: Boolean(agent.installed),
      read_sessions: true,
      push_signal:
        agent.push_signal === "http_events"
          ? "watch"
          : ((agent.push_signal as AgentSettings["push_signal"]) || "poll"),
      reply_mode: "auto",
      model: agent.default_model || null,
      order: null,
      updated_at: null,
    };
  }

  function settingFor(id: string): AgentSettings {
    return (
      settings[id] || {
        id,
        connected: false,
        push_to_phone: false,
        read_sessions: true,
        push_signal: "poll",
        reply_mode: "auto",
        model: null,
        order: null,
        updated_at: null,
      }
    );
  }

  function liveAgent(id: string): Agent | undefined {
    return agents.find((agent) => agent.source === id);
  }

  function rows(): AgentCatalogEntry[] {
    const byId = new Map(catalog.map((entry) => [entry.id, entry]));
    for (const agent of agents) {
      if (!byId.has(agent.source)) byId.set(agent.source, customRow(agent));
    }
    return [...byId.values()];
  }

  function customRow(agent: Agent): AgentCatalogEntry {
    return {
      id: agent.source,
      name: agent.display_name || AGENT_LABELS[agent.source] || agent.source,
      vendor: "自定义",
      category: "自定义",
      icon: agent.icon,
      readiness: "adapter",
      detected: agent.installed !== false,
      connected: Boolean(settingFor(agent.source).connected),
      available: true,
      manifest_installed: true,
      can_install: false,
      can_connect: true,
      can_generate_kit: false,
      install_kind: "manifest",
      sessions: agent.capabilities?.sessions !== false,
      reply: Boolean(agent.capabilities?.reply),
      models: (agent.models?.length || 0) > 0,
      push_signals: [agent.push_signal || "poll"],
      hook_support: "generic",
      hook_install: "generic",
      hook_installed: false,
      version: agent.version || null,
      notes: "通过你自己的 YAML 清单或脚本适配器接入。",
      catalog_source: "homing",
      integrations: [],
      kit_installed: false,
    };
  }

  function configFileFor(entry: AgentCatalogEntry): string {
    return (
      entry.config_file ||
      entry.hook_file ||
      adapters?.builtin.find((item) => item.id === entry.id)?.config_file ||
      entry.manifest_file ||
      ""
    );
  }

  // ------------------------------------------------------------ 动作

  async function patch(id: string, changes: Partial<AgentSettings>, after?: () => void) {
    try {
      const next = await endpoints.patchAgent(id, changes);
      settings[id] = next;
      paint();
      after?.();
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "保存失败");
      await load(true);
    }
  }

  async function connect(entry: AgentCatalogEntry) {
    try {
      const next = await endpoints.patchAgent(entry.id, { connected: true, push_to_phone: true });
      settings[entry.id] = next;
      expanded.add(entry.id);
      paint();
      const message = entry.detected
        ? `${entry.name} 已接入`
        : `${entry.name} 已开启事件接入；挂上套件里的 hook 后，事件会进入收件箱`;
      toast(message, "ok", entry.detected ? 4200 : 7000);
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "接入失败");
    }
  }

  async function installKit(entry: AgentCatalogEntry) {
    try {
      const result = await endpoints.installCatalogKit(entry.id);
      expanded.add(entry.id);
      toast(
        result.existing
          ? `${entry.name} 的接入套件已存在，已保留你改过的文件`
          : `${entry.name} 的接入套件已生成：${result.dir}`,
        "ok",
        7000,
      );
      await load(true);
    } catch (err) {
      reportError(err, "生成接入套件失败");
    }
  }

  async function quickConnect(entry: AgentCatalogEntry) {
    try {
      const result = await endpoints.quickConnectCatalogAgent(entry.id);
      settings[entry.id] = result.settings;
      expanded.add(entry.id);
      const kitNote = result.kit_existing ? "沿用现有套件" : "已生成接入套件";
      const manifestNote = result.manifest_installed && !entry.manifest_installed ? "，已安装扫描清单" : "";
      toast(`${entry.name} 已快速接入：${kitNote}${manifestNote}`, "ok", 7000);
      await load(true);
    } catch (err) {
      reportError(err, "快速接入失败");
    }
  }

  async function enableDrive(entry: AgentCatalogEntry) {
    const command = entry.drive?.command || "ACP";
    if (!window.confirm(`跑一次隔离握手来启用 ${entry.name} 的驱动？\n\n${command}\n\n握手只在临时目录创建空会话；失败不会写入清单。`)) {
      return;
    }
    try {
      const result = await endpoints.enableCatalogDrive(entry.id);
      settings[entry.id] = result.settings;
      expanded.add(entry.id);
      toast(
        `${entry.name} 驱动已启用；握手 ${result.probe.handshake_duration_ms} ms，清单已写入`,
        "ok",
        7000,
      );
      await load(true);
    } catch (err) {
      reportError(err, "启用驱动失败，未写入清单");
    }
  }

  async function removeDrive(entry: AgentCatalogEntry) {
    if (!window.confirm(`停用 ${entry.name} 的 ACP 驱动？只会移除 Homing 的 drive 清单，不碰 Agent 本体。`)) {
      return;
    }
    try {
      const result = await endpoints.removeCatalogDrive(entry.id);
      toast(
        result.manifest_removed
          ? `驱动已停用，清单已移到 ${result.trash}`
          : "驱动已停用；其余会话读取配置保留",
        "ok",
        7000,
      );
      await load(true);
    } catch (err) {
      reportError(err, "停用驱动失败");
    }
  }

  async function removeKit(entry: AgentCatalogEntry) {
    if (!window.confirm(`移除 ${entry.name} 的接入套件？目录会改名保留，不会直接删除。`)) return;
    try {
      const result = await endpoints.removeCatalogKit(entry.id);
      toast(`已移除接入套件，原目录保存在 ${result.trash}`, "ok", 7000);
      await load(true);
    } catch (err) {
      reportError(err, "移除接入套件失败");
    }
  }

  async function remove(entry: AgentCatalogEntry) {
    if (!window.confirm(`移除 ${entry.name} 的接入清单？原文件会改名保留，不会真的删除。`)) return;
    try {
      const result = await endpoints.removeCatalogAgent(entry.id);
      settings[entry.id] = result.settings;
      expanded.delete(entry.id);
      toast("已移除清单，原文件改名保留", "ok", 6000);
      await load(true);
    } catch (err) {
      reportError(err, "移除清单失败");
    }
  }

  async function disconnect(entry: AgentCatalogEntry) {
    try {
      const result = await endpoints.disconnectAgent(entry.id);
      settings[entry.id] = result.settings;
      toast("已断开；没有改动 Agent 自己的配置文件", "ok", 6000);
      expanded.delete(entry.id);
      await load(true);
    } catch (err) {
      reportError(err, "断开失败");
    }
  }

  async function testPush(entry: AgentCatalogEntry) {
    try {
      const result = await endpoints.testPush(entry.id);
      toast(result.message || "已发一条测试", result.ok ? "ok" : "bad", 6000);
    } catch (err) {
      reportError(err, "试发失败");
    }
  }

  async function installHooks(entry: AgentCatalogEntry) {
    try {
      const result = await endpoints.installHooks(entry.id);
      const message = result.message || result.error || (result.ok ? "钩子已安装" : "需要手工安装");
      toast(message, result.ok || result.manual ? "ok" : "bad", 7000);
      if (result.ok) await load(true);
    } catch (err) {
      reportError(err, "安装钩子失败");
    }
  }

  async function persistOrder(nextOrder: string[]) {
    order = nextOrder;
    renderBody();
    try {
      await endpoints.saveAgentOrder(nextOrder);
    } catch (err) {
      reportError(err, "保存顺序失败");
      await load(true);
    }
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("已复制", "ok");
    } catch {
      toast("复制失败，请手动选中命令", "bad");
    }
  }

  // ------------------------------------------------------------ 分组与计数

  function isConnected(entry: AgentCatalogEntry): boolean {
    return Boolean(settingFor(entry.id).connected);
  }

  function bucketOf(entry: AgentCatalogEntry): Bucket {
    if (isConnected(entry)) return "connected";
    if ((entry.can_install && !entry.manifest_installed) || entry.kit_installed) return "installable";
    if (entry.detected) return "detected";
    if (entry.readiness === "builtin") return "installable";
    return "discover";
  }

  function countsOf(list: AgentCatalogEntry[]): Record<Filter, number> {
    const counts: Record<Filter, number> = { all: list.length, connected: 0, detected: 0, installable: 0, discover: 0 };
    for (const entry of list) counts[bucketOf(entry)] += 1;
    return counts;
  }

  function sortedRows(list: AgentCatalogEntry[]): AgentCatalogEntry[] {
    const rank = new Map(order.map((id, index) => [id, index]));
    return [...list].sort((a, b) => {
      const ra = rank.get(a.id);
      const rb = rank.get(b.id);
      if (ra !== undefined && rb !== undefined) return ra - rb;
      if (ra !== undefined) return -1;
      if (rb !== undefined) return 1;
      return a.name.localeCompare(b.name);
    });
  }

  function matchesQuery(entry: AgentCatalogEntry): boolean {
    const text = query.trim().toLowerCase();
    if (!text) return true;
    return [
      entry.name,
      entry.id,
      entry.vendor,
      entry.category,
      entry.notes,
      entry.tagline,
      ...(entry.detect_bins || []),
      ...(entry.detect_paths || []),
      ...(entry.integrations || []).map((item) => item.label),
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
      .includes(text);
  }

  // ------------------------------------------------------------ 行

  function statusOf(entry: AgentCatalogEntry): { text: string; tone: "ok" | "warn" | "mute" } {
    if (isConnected(entry)) {
      if (liveAgent(entry.id)?.capabilities?.new_session_ready === false) {
        return { text: "已接入 · 新建未就绪", tone: "warn" };
      }
      return entry.detected
        ? { text: "已接入", tone: "ok" }
        : { text: "已接入 · 待检测", tone: "warn" };
    }
    if (entry.detected && entry.can_connect) return { text: "可接入", tone: "warn" };
    if (entry.detected) return { text: "已检测", tone: "mute" };
    if (entry.kit_installed) return { text: "套件已生成", tone: "warn" };
    if (entry.manifest_installed) return { text: "已装清单", tone: "warn" };
    if (entry.can_install) return { text: "可安装清单", tone: "mute" };
    if (entry.can_generate_kit) return { text: "可生成套件", tone: "mute" };
    return { text: "待核实", tone: "mute" };
  }

  function readinessText(entry: AgentCatalogEntry): string {
    if (entry.readiness === "builtin") return "内置扫描";
    if (entry.readiness === "adapter") return entry.manifest_installed ? "已验证清单" : "可装清单";
    if (entry.readiness === "hook") return entry.hook_support === "native" ? "原生钩子" : "通用事件";
    return "发现目录";
  }

  function capabilityChips(entry: AgentCatalogEntry): HTMLElement {
    const chips = h("div", { class: "agent-capabilities" });
    const caps = liveAgent(entry.id)?.capabilities;
    const session = entry.sessions ? "读取会话" : "会话未核实";
    chips.append(h("span", { class: `cap-chip ${entry.sessions ? "ok" : "mute"}`, text: session }));
    if (entry.hook_support === "native") {
      chips.append(h("span", { class: "cap-chip", text: "原生钩子" }));
    } else if (entry.hook_support === "generic") {
      chips.append(h("span", { class: "cap-chip", text: "事件钩子" }));
    } else {
      chips.append(h("span", { class: "cap-chip", text: entry.can_generate_kit ? "插件 / API" : "只读观察" }));
    }
    chips.append(h("span", { class: `cap-chip ${entry.reply ? "ok" : "mute"}`, text: entry.reply ? "可回复" : "只读" }));
    if (caps) {
      chips.append(h("span", {
        class: `cap-chip ${caps.new_session_ready ? "ok" : "mute"}`,
        text: caps.new_session_ready ? "可新建" : "新建未就绪",
      }));
    }
    if (entry.models) chips.append(h("span", { class: "cap-chip", text: "模型" }));
    return chips;
  }

  type LadderState = "ready" | "attachable" | "missing";

  function ladderState(entry: AgentCatalogEntry, step: "notify" | "read" | "reply" | "drive" | "collab"): LadderState {
    const live = liveAgent(entry.id);
    const caps = live?.capabilities || {};
    const driveEnabled = Boolean(entry.drive?.enabled || caps.new_session);
    switch (step) {
      case "notify":
        if (entry.hook_installed || entry.hook_support === "native") return "ready";
        return entry.hook_support !== "none" || entry.can_generate_kit ? "attachable" : "missing";
      case "read":
        if (entry.sessions && isConnected(entry)) return "ready";
        return entry.can_install || entry.manifest_installed || entry.can_generate_kit ? "attachable" : "missing";
      case "reply":
        if (entry.reply && isConnected(entry)) return "ready";
        return entry.can_connect || entry.can_generate_kit ? "attachable" : "missing";
      case "drive":
        if (driveEnabled) return "ready";
        return entry.drive?.supported || live?.driver || entry.can_generate_kit || entry.manifest_installed ? "attachable" : "missing";
      case "collab":
        if (caps.dispatch) return "ready";
        return driveEnabled || entry.can_generate_kit ? "attachable" : "missing";
    }
  }

  function ladderHint(entry: AgentCatalogEntry, step: "notify" | "read" | "reply" | "drive" | "collab"): string {
    const live = liveAgent(entry.id);
    const driver = live?.driver;
    const caps = live?.capabilities || {};
    switch (step) {
      case "notify":
        return entry.hook_support === "native"
          ? "原生钩子可把权限请求和回合结束送进收件箱"
          : entry.hook_support === "generic"
            ? "通过 /hook/generic 或接入套件上报事件"
            : "还没有核实到生命周期事件接口";
      case "read":
        return entry.sessions ? "可读取本地会话与正文" : "会话语义尚未核实，需要清单或脚本适配器";
      case "reply":
        return entry.reply ? "可从 Homing 向已有会话注入回复" : "当前只读；需要命令数组或事件回复接口";
      case "drive":
        if (entry.drive?.enabled) return "已启用，可新建会话并作为 @agent 目标";
        if (entry.drive?.supported) {
          return entry.drive.installed
            ? `${entry.drive.command || "ACP"} · 尚未启用；点击按钮先握手`
            : `未安装执行文件${entry.drive.command ? ` · ${entry.drive.command}` : ""}`;
        }
        if (caps.new_session) {
          return caps.dispatch
            ? "已由 Agent 原生 CLI 启动器驱动，可新建会话并作为 @agent 目标"
            : "原生启动器可新建会话，但为避免复用活跃对话，当前不开放 @ 派发";
        }
        if (!driver) return "需要 ACP / CLI 驱动与已核实的启动命令";
        return `${driver.command || "ACP"} · ${driver.ready ? "可握手" : driver.detail || "尚不可用"}${
          driver.experimental ? " · 实验" : ""
        }`;
      case "collab":
        return live?.capabilities?.dispatch
          ? "可作为 @agent 的目标，结果回到父会话"
          : "需要先达到「驱动」能力，且启动器能返回或绑定子会话";
    }
  }

  function capabilityLadder(entry: AgentCatalogEntry): HTMLElement {
    const steps: Array<{ id: "notify" | "read" | "reply" | "drive" | "collab"; label: string; level: string }> = [
      { id: "notify", label: "通知", level: "L0" },
      { id: "read", label: "读取", level: "L1" },
      { id: "reply", label: "回复", level: "L2" },
      { id: "drive", label: "驱动", level: "L3" },
      { id: "collab", label: "协作", level: "L4" },
    ];
    const ladder = h("div", { class: "agent-capability-ladder", role: "list", "aria-label": "能力阶梯" });
    for (const step of steps) {
      const state = ladderState(entry, step.id);
      const cell = h(
        "div",
        {
          class: `agent-capability-step ${state}`,
          role: "listitem",
          title: `${step.level} ${step.label}：${ladderHint(entry, step.id)}`,
        },
        h("span", { class: "agent-capability-dot" }),
        h("span", { class: "agent-capability-label", text: step.label }),
        h("span", { class: "agent-capability-level", text: step.level }),
      );
      ladder.append(cell);
    }
    return ladder;
  }

  function primaryCommand(entry: AgentCatalogEntry): RowCommand | null {
    if (entry.kit_installed && entry.kit_dir) {
      return { label: "套件", text: entry.kit_dir };
    }
    const install = entry.install_commands || [];
    if (install.length > 0 && !entry.detected) {
      return { label: install[0].label, text: install[0].command, extra: install.length > 1 ? install.length - 1 : 0 };
    }
    if (entry.manifest_installed && entry.manifest_file) {
      return { label: "清单", text: entry.manifest_file };
    }
    const config = configFileFor(entry);
    if (config) return { label: "配置", text: config };
    if (install.length > 0) {
      return { label: install[0].label, text: install[0].command, extra: install.length > 1 ? install.length - 1 : 0 };
    }
    return null;
  }

  function fallbackAvatar(entry: AgentCatalogEntry): HTMLElement {
    let hash = 0;
    for (const char of entry.id) hash = (hash * 31 + char.charCodeAt(0)) % 360;
    const avatar = h("span", {
      class: "agent-avatar fallback",
      title: entry.name,
      text: AGENT_INITIALS[entry.id] || entry.name.slice(0, 2).toUpperCase(),
    });
    avatar.style.setProperty("--avatar-bg", `hsl(${hash} 42% 18%)`);
    avatar.style.setProperty("--avatar-fg", `hsl(${hash} 78% 74%)`);
    return avatar;
  }

  function agentAvatar(entry: AgentCatalogEntry): HTMLElement {
    const logo = agentLogo(entry.id) || (entry.icon ? agentLogo(entry.icon) : null);
    const live = liveAgent(entry.id);
    const avatar = logo
      ? h("span", { class: `agent-avatar${logo.mono ? " mono" : ""}`, title: entry.name })
      : fallbackAvatar(entry);
    if (logo) {
      if (logo.color) avatar.style.color = logo.color;
      avatar.append(icon(logo.svg, 26));
    } else if (live?.icon && live.icon.trim().startsWith("<svg")) {
      avatar.append(icon(live.icon, 24));
    }
    return avatar;
  }

  function connectionSwitch(entry: AgentCatalogEntry): HTMLElement {
    const connected = isConnected(entry);
    return toggle({
      on: connected,
      title: connected ? `断开 ${entry.name}` : `接入 ${entry.name}`,
      onChange: (next) => {
        if (next) void connect(entry);
        else void disconnect(entry);
      },
    });
  }

  function rowActions(entry: AgentCatalogEntry): HTMLElement {
    const connected = isConnected(entry);
    const action = h("div", { class: "agent-row-actions" });
    if (connected) {
      action.append(connectionSwitch(entry));
    } else {
      action.append(
        button("快速接入", {
          small: true,
          variant: "primary",
          iconSvg: icons.bolt,
          onClick: () => void quickConnect(entry),
        }),
      );
    }
    action.append(iconButton(expanded.has(entry.id) ? icons.chevronDown : icons.chevronRight, "查看接入详情", () => toggleDetail(entry.id)));
    return action;
  }

  function agentRow(entry: AgentCatalogEntry, allowDrag: boolean): HTMLElement {
    const connected = isConnected(entry);
    const status = statusOf(entry);
    const count = sessionCounts[entry.id] || 0;
    const wrapper = h("div", { class: "agent-entry" });
    const row = h("div", {
      class: "agent-row",
      dataset: { agent: entry.id, connected: String(connected) },
    });

    if (allowDrag && connected) {
      const grip = h("span", { class: "agent-grip", title: "拖动排序", draggable: "true" }, icon(icons.grip, 14));
      grip.addEventListener("dragstart", (event) => {
        dragId = entry.id;
        row.classList.add("dragging");
        const dragEvent = event as DragEvent;
        dragEvent.dataTransfer?.setData("text/plain", entry.id);
        if (dragEvent.dataTransfer) dragEvent.dataTransfer.effectAllowed = "move";
      });
      grip.addEventListener("dragend", () => {
        dragId = null;
        row.classList.remove("dragging");
        for (const el of Array.from(root.querySelectorAll(".agent-row"))) el.classList.remove("drop-target");
      });
      row.append(grip);
    }

    const main = h(
      "div",
      { class: "agent-row-main", title: entry.tagline || entry.notes || "" },
      h(
        "div",
        { class: "agent-row-name" },
        h("span", { class: "name", text: entry.name }),
        entry.version ? h("span", { class: "ver", text: `v${entry.version}` }) : null,
      ),
      h(
        "div",
        { class: "agent-row-sub" },
        h("span", { class: "vendor", text: entry.vendor || "未标注厂商" }),
        h("span", { class: "sep", text: "·" }),
        h("span", { text: entry.category || readinessText(entry) }),
        entry.sessions && sessionCountsLoaded ? h("span", { class: "sep", text: "·" }) : null,
        entry.sessions && sessionCountsLoaded ? h("span", { text: `${count} 个会话` }) : null,
        h("span", { class: "tagline", text: entry.tagline || entry.notes || readinessText(entry) }),
      ),
      capabilityLadder(entry),
    );
    main.addEventListener("click", () => toggleDetail(entry.id));
    main.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        toggleDetail(entry.id);
      }
    });
    main.tabIndex = 0;
    main.setAttribute("role", "button");

    row.append(
      agentAvatar(entry),
      main,
      h(
        "div",
        { class: "agent-row-tail" },
        h(
          "div",
          { class: `agent-state ${status.tone}` },
          h("span", { class: status.tone === "ok" ? "dot dot-ok" : status.tone === "warn" ? "dot dot-warn" : "dot" }),
          h("span", { text: status.text }),
        ),
        rowActions(entry),
      ),
    );

    row.addEventListener("dragover", (event) => {
      if (!dragId || dragId === entry.id) return;
      const other = rows().find((item) => item.id === entry.id);
      if (!other || !isConnected(other)) return;
      event.preventDefault();
      row.classList.add("drop-target");
    });
    row.addEventListener("dragleave", () => row.classList.remove("drop-target"));
    row.addEventListener("drop", (event) => {
      event.preventDefault();
      row.classList.remove("drop-target");
      if (!dragId || dragId === entry.id) return;
      const connectedIds = sortedRows(rows().filter((item) => isConnected(item))).map((item) => item.id);
      const from = connectedIds.indexOf(dragId);
      const to = connectedIds.indexOf(entry.id);
      if (from < 0 || to < 0) return;
      connectedIds.splice(to, 0, ...connectedIds.splice(from, 1));
      const rest = order.filter((id) => !connectedIds.includes(id));
      dragId = null;
      void persistOrder([...connectedIds, ...rest]);
    });

    wrapper.append(row);
    if (expanded.has(entry.id)) wrapper.append(agentDetail(entry));
    return wrapper;
  }

  function compactAgentCard(entry: AgentCatalogEntry): HTMLElement {
    const connected = isConnected(entry);
    const status = statusOf(entry);
    const wrapper = h("div", {
      class: `agent-compact-entry${expanded.has(entry.id) ? " expanded" : ""}`,
      dataset: { agent: entry.id, connected: String(connected) },
    });
    const card = h("article", { class: "agent-compact-card" });
    const heading = h(
      "div",
      {
        class: "agent-compact-head",
        role: "button",
        tabindex: "0",
        title: `查看 ${entry.name} 的接入详情`,
      },
      agentAvatar(entry),
      h(
        "div",
        { class: "agent-compact-heading" },
        h(
          "div",
          { class: "agent-compact-name" },
          h("span", { text: entry.name }),
          entry.version ? h("span", { class: "ver", text: `v${entry.version}` }) : null,
        ),
        h("div", {
          class: "agent-compact-meta",
          text: [entry.vendor || "未标注厂商", entry.category || readinessText(entry)].filter(Boolean).join(" · "),
        }),
      ),
      h(
        "span",
        { class: `agent-compact-state ${status.tone}` },
        h("span", { class: status.tone === "ok" ? "dot dot-ok" : status.tone === "warn" ? "dot dot-warn" : "dot" }),
        h("span", { text: status.text }),
      ),
    );
    const openDetail = () => toggleDetail(entry.id);
    heading.addEventListener("click", openDetail);
    heading.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        openDetail();
      }
    });
    const description = h("p", {
      class: "agent-compact-desc",
      text: entry.tagline || entry.notes || readinessText(entry),
      title: `查看 ${entry.name} 的接入详情`,
    });
    description.addEventListener("click", openDetail);
    card.append(
      heading,
      description,
      h(
        "div",
        { class: "agent-compact-foot" },
        h(
          "div",
          { class: "agent-compact-actions" },
          connected
            ? connectionSwitch(entry)
            : button("快速接入", {
                small: true,
                variant: "primary",
                iconSvg: icons.bolt,
                onClick: () => void quickConnect(entry),
              }),
          iconButton(
            expanded.has(entry.id) ? icons.chevronDown : icons.chevronRight,
            "查看接入详情",
            () => toggleDetail(entry.id),
          ),
        ),
      ),
    );
    wrapper.append(card);
    if (expanded.has(entry.id)) wrapper.append(agentDetail(entry));
    return wrapper;
  }

  function toggleDetail(id: string) {
    if (expanded.has(id)) expanded.delete(id);
    else expanded.add(id);
    renderBody();
  }

  // ------------------------------------------------------------ 详情

  function fact(label: string, value: string): HTMLElement {
    return h(
      "div",
      { class: "agent-fact" },
      h("div", { class: "agent-fact-label", text: label }),
      h("div", { class: "agent-fact-value", text: value }),
    );
  }

  function fieldRow(label: string, hint: string, control: HTMLElement): HTMLElement {
    return h(
      "div",
      { class: "field-row" },
      h("div", { class: "field-label" }, h("div", { text: label }), h("div", { class: "hint", text: hint })),
      control,
    );
  }

  function externalLink(label: string, href: string): HTMLElement {
    return h(
      "a",
      { class: "agent-source-link", href, target: "_blank", rel: "noreferrer" },
      h("span", { text: label }),
      icon(icons.external, 13),
    );
  }

  function copyRow(label: string, text: string, hint?: string): HTMLElement {
    return h(
      "div",
      { class: "agent-copy-row" },
      h("span", { class: "agent-copy-label", text: label }),
      h("code", { class: "agent-copy-text", text, title: hint || text }),
      iconButton(icons.copy, "复制", () => void copyText(text)),
    );
  }

  function stateTag(state: CatalogIntegration["state"]): HTMLElement {
    const map = {
      ready: { text: "已就绪", tone: "ok" },
      available: { text: "可用", tone: "warn" },
      draft: { text: "草稿", tone: "mute" },
    } as const;
    const item = map[state];
    return h("span", { class: `integration-state ${item.tone}`, text: item.text });
  }

  function integrationCard(item: CatalogIntegration): HTMLElement {
    const card = h("div", { class: "integration-card" });
    card.append(
      h(
        "div",
        { class: "integration-card-head" },
        h("span", { class: "integration-icon" }, icon(INTEGRATION_ICONS[item.kind] || icons.plug, 16)),
        h("div", { class: "integration-title" }, h("div", { text: item.label }), stateTag(item.state)),
      ),
      h("p", { class: "integration-desc", text: item.description }),
    );
    const value = item.command || item.path;
    if (value) card.append(copyRow(item.kind === "manifest" ? "文件" : "使用", value));
    return card;
  }

  function integrationSection(entry: AgentCatalogEntry): HTMLElement {
    const integrations = entry.integrations || [];
    const box = h("div", { class: "agent-section" });
    box.append(h("div", { class: "agent-section-title", text: "怎么接" }));
    if (integrations.length === 0) {
      box.append(h("div", { class: "agent-note", text: "这类 Agent 没有公开的本地接入点；可用事件 API 或提交一个脱敏样本，再补插件。" }));
      return box;
    }
    const grid = h("div", { class: "integration-grid" });
    for (const item of integrations) grid.append(integrationCard(item));
    box.append(grid);
    return box;
  }

  function commandSection(entry: AgentCatalogEntry): HTMLElement {
    const box = h("div", { class: "agent-section" });
    box.append(h("div", { class: "agent-section-title", text: `安装 ${entry.name}` }));
    const commands = entry.install_commands || [];
    if (commands.length === 0) {
      box.append(h("div", { class: "agent-note", text: "没有可直接执行的安装命令；请从官方页面安装，或使用现有的本地客户端。" }));
    } else {
      const list = h("div", { class: "agent-command-list" });
      for (const command of commands) {
        list.append(copyRow(command.label, command.command));
      }
      box.append(list);
    }
    const links = h("div", { class: "agent-source-links" });
    if (entry.homepage) links.append(externalLink("官方页面", entry.homepage));
    if (entry.docs_url) links.append(externalLink("接入文档", entry.docs_url));
    if (links.childElementCount > 0) box.append(links);
    return box;
  }

  function pathsSection(entry: AgentCatalogEntry): HTMLElement {
    const values: string[] = [];
    const add = (value?: string) => {
      if (value && !values.includes(value)) values.push(value);
    };
    for (const path of entry.detect_paths || []) add(path);
    for (const bin of entry.detect_bins || []) add(`bin: ${bin}`);
    add(entry.config_file);
    add(entry.hook_file);
    add(entry.manifest_file);
    add(entry.kit_dir);
    add(entry.homepage);
    const box = h("div", { class: "agent-section" });
    box.append(h("div", { class: "agent-section-title", text: "本机路径" }));
    if (values.length === 0) {
      box.append(h("div", { class: "agent-note", text: "还没有核实到固定路径。" }));
      return box;
    }
    const list = h("div", { class: "agent-path-list" });
    for (const value of values) list.append(copyRow(value === entry.homepage ? "官方" : "路径", value));
    box.append(list);
    return box;
  }

  function settingsPanel(entry: AgentCatalogEntry): HTMLElement {
    const s = settingFor(entry.id);
    const live = liveAgent(entry.id);
    const panel = h("div", { class: "agent-settings" });

    if (entry.sessions || entry.manifest_installed) {
      panel.append(
        fieldRow(
          "允许 Homing 读取这个 Agent 的会话",
          "关掉之后只保留已经收到的事件，不再读新的会话文件",
          toggle({ on: s.read_sessions !== false, onChange: (next) => void patch(entry.id, { read_sessions: next }) }),
        ),
      );
    }

    const supported = new Set((entry.push_signals || []).map((value) => (value === "http_events" ? "watch" : value)));
    if (supported.size > 0) {
      panel.append(
        fieldRow(
          "推送方式",
          "只列出这个 Agent 已声明的方式；未核实的能力不会假装可用",
          segmented(
            SIGNAL_OPTIONS.map((option) => ({ ...option, disabled: !supported.has(option.value) })),
            s.push_signal as "hook" | "watch" | "poll",
            (next) => void patch(entry.id, { push_signal: next }),
          ),
        ),
      );
    }

    if (entry.reply) {
      panel.append(
        fieldRow(
          "回复方式",
          "「直接回复」会把你的话立刻发过去",
          segmented(
            REPLY_OPTIONS,
            s.reply_mode as "auto" | "queue" | "off",
            (next) => void patch(entry.id, { reply_mode: next }),
          ),
        ),
      );
    } else {
      panel.append(h("div", { class: "agent-note", text: "当前接入是只读的：Homing 可以接收事件，但不会向这个 Agent 注入回复。" }));
    }

    const models = live?.models || [];
    if (models.length > 0) {
      const select = h("select", { class: "select" });
      for (const model of models) {
        const option = h("option", { value: model.id, text: model.label || model.id });
        if ((s.model || live?.default_model) === model.id) option.selected = true;
        select.append(option);
      }
      select.addEventListener("change", () => void patch(entry.id, { model: (select as HTMLSelectElement).value }));
      panel.append(fieldRow("回复时用的模型", "只影响从 Homing 发出去的回复", h("div", { style: "width:220px;flex:none" }, select)));
    }

    const tools = h("div", { class: "agent-settings-actions" });
    tools.append(button("试发一条测试通知", { small: true, iconSvg: icons.bolt, onClick: () => void testPush(entry) }));
    if (entry.hook_install === "auto" || entry.hook_install === "snippet") {
      tools.append(
        button(entry.hook_installed ? "重新安装钩子" : "安装钩子", {
          small: true,
          iconSvg: icons.plug,
          onClick: () => void installHooks(entry),
        }),
      );
    }
    panel.append(tools);

    const file = configFileFor(entry);
    const foot = h(
      "div",
      { class: "agent-detail-foot" },
      h("span", { text: file ? "配置文件 / 清单" : "不写 Agent 配置文件" }),
      h("span", { class: "mono", text: file || "Homing 只读取自己的接入套件" }),
      h("span", { class: "spacer" }),
      button("断开接入", { small: true, variant: "quiet", onClick: () => void disconnect(entry) }),
    );
    if (entry.manifest_installed && entry.can_install) {
      foot.append(button("移除清单", { small: true, variant: "quiet", onClick: () => void remove(entry) }));
    }
    if (entry.kit_installed) {
      foot.append(button("移除接入套件", { small: true, variant: "quiet", onClick: () => void removeKit(entry) }));
    }
    panel.append(foot);
    return panel;
  }

  function detailActions(entry: AgentCatalogEntry): HTMLElement {
    const actions = h("div", { class: "agent-detail-actions" });
    if (!isConnected(entry)) {
      actions.append(
        button("快速接入", {
          small: true,
          variant: "primary",
          iconSvg: icons.bolt,
          onClick: () => void quickConnect(entry),
        }),
      );
    }
    if (entry.can_generate_kit && !entry.kit_installed) {
      actions.append(
        button("只生成套件", {
          small: true,
          variant: "quiet",
          iconSvg: icons.plug,
          onClick: () => void installKit(entry),
        }),
      );
    }
    if (entry.drive?.supported) {
      actions.append(
        entry.drive.enabled
          ? button("停用驱动", {
              small: true,
              variant: "quiet",
              iconSvg: icons.plug,
              onClick: () => void removeDrive(entry),
            })
          : button(entry.drive.installed ? "启用驱动" : "未安装", {
              small: true,
              variant: entry.drive.installed ? "quiet" : "quiet",
              iconSvg: icons.bolt,
              disabled: !entry.drive.installed,
              title: entry.drive.installed
                ? "先跑一次隔离握手，成功才写清单"
                : "本机找不到这个 ACP 命令；Homing 不代装第三方软件",
              onClick: () => void enableDrive(entry),
            }),
      );
    }
    return actions;
  }

  function agentDetail(entry: AgentCatalogEntry): HTMLElement {
    const detail = h("div", { class: "agent-detail" });
    const heading = h(
      "div",
      { class: "agent-detail-head" },
      h(
        "div",
        null,
        h("div", { class: "agent-detail-title" }, h("span", { text: entry.name })),
        h("div", { class: "agent-detail-tagline", text: entry.tagline || entry.notes || readinessText(entry) }),
      ),
      detailActions(entry),
    );
    detail.append(heading);

    const live = liveAgent(entry.id);
    const driver = live?.driver;
    const facts = h(
      "div",
      { class: "agent-facts" },
      fact("接入层级", readinessText(entry)),
      fact("读取会话", entry.sessions ? "支持" : "未核实"),
      fact("回复", entry.reply ? "支持" : "只读"),
      fact("事件", entry.hook_support === "none" ? "插件 / API" : entry.hook_support === "native" ? "原生钩子" : "通用钩子"),
      fact(
        "驱动",
        entry.drive?.enabled
          ? `${entry.drive.command || "ACP"} · 已启用`
          : entry.drive?.supported
            ? `${entry.drive.command || "ACP"} · ${entry.drive.installed ? "待握手启用" : "未安装"}`
            : live?.capabilities?.new_session
              ? "CLI / HTTP"
              : "未启用",
      ),
      fact("权限模式", live?.permission_modes?.length ? live.permission_modes.join(" / ") : "未声明"),
      fact("版本", entry.version ? `v${entry.version}` : "—"),
      fact("平台", entry.platforms?.length ? entry.platforms.join(" / ") : "跟随本机"),
    );
    detail.append(facts);
    detail.append(h("div", { class: "agent-detail-capabilities" }, capabilityChips(entry)));

    const command = primaryCommand(entry);
    if (command) {
      detail.append(
        h(
          "div",
          { class: "agent-section agent-primary-command" },
          h("div", { class: "agent-section-title", text: "主接入位置" }),
          copyRow(command.label, command.text, command.extra ? `另有 ${command.extra} 个入口` : undefined),
        ),
      );
    }

    if (entry.notes) detail.append(h("div", { class: "agent-note agent-note-box", text: entry.notes }));
    if (entry.drive?.supported) {
      detail.append(
        h(
          "div",
          { class: entry.drive.enabled ? "agent-caveat driver-ready" : "agent-caveat" },
          icon(entry.drive.enabled ? icons.check : icons.info, 16),
          h("span", {
            text: entry.drive.enabled
              ? `ACP 驱动已启用：${entry.drive.command || "ACP"}${
                  entry.drive.experimental ? "（实验）" : ""
                } · 新建与 @ 协作会走这条命令`
              : entry.drive.installed
                ? `检测到 ACP 驱动 ${entry.drive.command || ""}${entry.drive.ready ? "" : `，但自检未通过：${entry.drive.detail || "原因未知"}`}。启用时仍会先跑完整握手，失败不写清单。`
                : `本机找不到 ${entry.drive.command || "ACP 命令"}；先安装该 Agent，Homing 不会代跑第三方安装命令。`,
          }),
        ),
      );
    } else if (driver) {
      detail.append(
        h(
          "div",
          { class: driver.ready ? "agent-caveat driver-ready" : "agent-caveat" },
          icon(driver.ready ? icons.check : icons.info, 16),
          h("span", {
            text: driver.ready
              ? `ACP 驱动已就绪：${driver.command || "ACP"}${driver.experimental ? "（实验）" : ""}`
              : `ACP 驱动尚不可用：${driver.detail || "握手检查未通过"}${driver.command ? ` · ${driver.command}` : ""}`,
          }),
        ),
      );
    }
    if (!entry.sessions) {
      detail.append(
        h(
          "div",
          { class: "agent-caveat" },
          icon(icons.info, 16),
          h("span", {
            text: entry.hook_support === "none"
              ? "这是一个发现条目：Homing 还没有核实它的本地会话或生命周期接口，先生成插件骨架，不把它记成“会读会话”。"
              : "会话语义尚未核实。现在可以接生命周期事件与命令包装器，但不会把对话正文假装成已支持。",
          }),
        ),
      );
    }

    detail.append(integrationSection(entry));
    detail.append(commandSection(entry));
    detail.append(pathsSection(entry));
    if (isConnected(entry)) detail.append(settingsPanel(entry));
    return detail;
  }

  // ------------------------------------------------------------ 渲染

  function renderTabs(counts: Record<Filter, number>) {
    clear(tabsHost);
    for (const item of FILTERS) {
      const tab = h("button", { class: item.id === filter ? "on" : "", type: "button" });
      tab.append(h("span", { text: item.label }), h("span", { class: "tab-count", text: String(counts[item.id]) }));
      tab.addEventListener("click", () => {
        filter = item.id;
        paint();
      });
      tabsHost.append(tab);
    }
  }

  function groupSection(
    title: string,
    group: AgentCatalogEntry[],
    options: { allowDrag?: boolean; compact?: boolean } = {},
  ): HTMLElement {
    const content = options.compact
      ? h("div", { class: "agent-compact-grid" }, ...group.map((entry) => compactAgentCard(entry)))
      : listCard(...group.map((entry) => agentRow(entry, Boolean(options.allowDrag))));
    return h(
      "div",
      { class: "group" },
      groupHead(title, group.length),
      content,
    );
  }

  function renderBody() {
    const all = sortedRows(rows());
    const counts = countsOf(all);
    renderTabs(counts);

    const searched = all.filter(matchesQuery);
    const visible = filter === "all" ? searched : searched.filter((entry) => bucketOf(entry) === filter);
    countNote.textContent = `${visible.length} / ${all.length}`;
    clear(bodyHost);

    if (visible.length === 0) {
      const hints: Record<Filter, string> = {
        all: "换一个搜索词，或到「接入 › 自定义」写一份清单。",
        connected: "从「本机检测」或「更多适配」里挑一个开始接入。",
        detected: "这台电脑上暂时没有检测到还没接入的 Agent。",
        installable: "没有现成清单或套件；「更多适配」里仍可一键生成接入骨架。",
        discover: "没有匹配的适配条目。",
      };
      bodyHost.append(
        h(
          "div",
          { class: "group" },
          listCard(emptyState(query ? "没有匹配的 Agent" : "这里还没有 Agent", hints[filter])),
        ),
      );
      return;
    }

    if (query.trim()) {
      bodyHost.append(groupSection("搜索结果", visible, { compact: true }));
      return;
    }

    const buckets: Bucket[] = ["connected", "detected", "installable", "discover"];
    for (const bucket of buckets) {
      if (filter !== "all" && filter !== bucket) continue;
      const group = visible.filter((entry) => bucketOf(entry) === bucket);
      if (group.length === 0) continue;
      bodyHost.append(
        groupSection(BUCKET_TITLES[bucket], group, {
          allowDrag: bucket === "connected",
          // Keep the small, high-signal lists full-width; collapse the long
          // discovery/quick-start tail into a responsive grid so 57 agents
          // don't become a single 44-row column.
          compact: bucket === "installable" || bucket === "discover",
        }),
      );
    }

    if (filter === "all" || filter === "discover") {
      const dashed = h("button", { class: "dashed", type: "button" });
      dashed.append(
        icon(icons.plus, 16),
        h(
          "div",
          null,
          h("div", { class: "t", text: "接入没有列出的 Agent" }),
          h("div", { class: "note", text: "用 YAML 清单、脚本适配器、/hook/generic 或 /api/ingest；也可以提交样本扩充目录。" }),
        ),
      );
      dashed.addEventListener("click", () => ctx.navigate("/console/adapters/custom"));
      bodyHost.append(dashed);
    }
  }

  function paint() {
    renderBody();
  }

  void load();
  const stop = poll(() => load(true), 15_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
