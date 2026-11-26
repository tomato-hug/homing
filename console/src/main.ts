import "./styles.css";
import "./design-system.css";
import { h, clear, icon } from "./lib/dom";
import { icons } from "./lib/icons";
import { createPigeon, type PigeonPlayer } from "./lib/pigeon";
import { ApiError, endpoints, poll, type ConnectionInfo, type Inbox } from "./lib/api";
import { pill, reportError, toast } from "./lib/ui";
import { getLocale, loc, onLocaleChange, t, toggleLocale } from "./i18n";
import { renderInbox } from "./pages/inbox";
import { renderHome } from "./pages/home";
import { renderHub } from "./pages/hub";
import { renderFlow } from "./pages/flow";
import { renderCollab } from "./pages/collab";
import { renderRooms } from "./pages/rooms";
import { renderSessions } from "./pages/sessions";
import { renderAgents } from "./pages/agents";
import { renderDevices } from "./pages/devices";
import { renderNotifications } from "./pages/notifications";
import { renderRelay } from "./pages/relay";
import { renderAdapters } from "./pages/adapters";
import { renderSettings } from "./pages/settings";
import { renderLogs } from "./pages/logs";
import { renderAbout } from "./pages/about";
import { renderSetup } from "./pages/setup";
import { renderLibrary } from "./pages/library";
import { notFoundWithNav } from "./pages/notfound";
import { renderTray } from "./pages/tray";

export interface PageContext {
  navigate: (path: string, options?: { replace?: boolean }) => void;
  /** 外壳（侧栏徽标 / 连接胶囊）需要跟着页面里的操作更新时调用 */
  refreshShell: () => void;
  /** 顶栏刷新按钮：重新拉一遍当前页面 */
  refreshPage: () => void;
}

export interface PageResult {
  el: HTMLElement;
  dispose?: () => void;
}

interface Route {
  path: string;
  /** 面包屑（动态按当前语言计算） */
  crumbs: () => string[];
  iconSvg: string;
  render: (ctx: PageContext, query: URLSearchParams) => PageResult;
}

// ---------------------------------------------------------------- 路由表

const routes: Route[] = [
  { path: "/console/tray", crumbs: () => ["Homing"], iconSvg: icons.overview, render: renderTray },
  { path: "/console/", crumbs: () => [t("nav.group.work"), t("nav.overview")], iconSvg: icons.overview, render: renderHome },
  { path: "/console/overview", crumbs: () => [t("nav.group.work"), t("nav.overview")], iconSvg: icons.overview, render: renderHome },
  { path: "/console/hub", crumbs: () => [t("nav.group.connect"), t("nav.hub")], iconSvg: icons.agents, render: renderHub },
  { path: "/console/flow", crumbs: () => [t("nav.group.work"), t("nav.flow")], iconSvg: icons.bolt, render: renderFlow },
  { path: "/console/inbox", crumbs: () => [t("nav.group.work"), loc("收件箱", "Inbox")], iconSvg: icons.inbox, render: renderInbox },
  { path: "/console/sessions", crumbs: () => [t("nav.group.work"), t("nav.sessions")], iconSvg: icons.chat, render: renderSessions },
  { path: "/console/rooms", crumbs: () => [t("nav.group.work"), t("nav.rooms")], iconSvg: icons.chat, render: renderRooms },
  { path: "/console/collab", crumbs: () => [t("nav.group.work"), t("nav.collab")], iconSvg: icons.cornerDown, render: renderCollab },
  { path: "/console/agents", crumbs: () => [t("nav.group.connect"), t("nav.agents")], iconSvg: icons.puzzle, render: renderAgents },
  { path: "/console/devices", crumbs: () => [t("nav.group.connect"), t("nav.devices")], iconSvg: icons.phone, render: renderDevices },
  { path: "/console/notifications", crumbs: () => [t("nav.group.connect"), t("nav.notifications")], iconSvg: icons.bell, render: renderNotifications },
  { path: "/console/relay", crumbs: () => [t("nav.group.connect"), t("nav.relay")], iconSvg: icons.plug, render: renderRelay },
  {
    path: "/console/adapters",
    crumbs: () => [t("nav.group.connect"), t("nav.adapters"), loc("内置", "Built-in")],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "builtin"),
  },
  {
    path: "/console/adapters/discover",
    crumbs: () => [t("nav.group.connect"), t("nav.adapters"), loc("发现", "Discover")],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "discover"),
  },
  {
    path: "/console/adapters/installed",
    crumbs: () => [t("nav.group.connect"), t("nav.adapters"), loc("已安装", "Installed")],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "installed"),
  },
  {
    path: "/console/adapters/custom",
    crumbs: () => [t("nav.group.connect"), t("nav.adapters"), loc("自定义", "Custom")],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "custom"),
  },
  { path: "/console/library", crumbs: () => [t("nav.group.connect"), t("nav.library")], iconSvg: icons.library, render: renderLibrary },
  { path: "/console/settings", crumbs: () => [t("nav.settings")], iconSvg: icons.gear, render: renderSettings },
  { path: "/console/logs", crumbs: () => [t("nav.settings"), t("nav.logs")], iconSvg: icons.logs, render: renderLogs },
  { path: "/console/about", crumbs: () => [t("nav.settings"), t("nav.about")], iconSvg: icons.info, render: renderAbout },
];

const setupRoute: Route = {
  path: "/console/setup",
  crumbs: () => [t("nav.settings"), loc("首次设置", "Setup")],
  iconSvg: icons.key,
  render: renderSetup,
};

// ---------------------------------------------------------------- 侧栏结构

interface NavLeaf {
  path: string;
  label: string;
  iconSvg?: string;
  badge?: "waiting";
  aliases?: string[];
}

interface NavGroup {
  label: string;
  collapsible?: boolean;
  items: NavLeaf[];
  children?: Array<{ parent: string; items: NavLeaf[] }>;
}

let workbenchExpanded = false;

export function getDesktopAppVersion(): string {
  try {
    return sessionStorage.getItem("homing.desktopVersion") || "";
  } catch {
    return "";
  }
}

function captureDesktopVersionFromURL() {
  try {
    const url = new URL(location.href);
    const dv = url.searchParams.get("desktop_version");
    if (dv) {
      sessionStorage.setItem("homing.desktopVersion", dv.trim());
    }
  } catch {
    /* ignore */
  }
}

function getNavGroups(): NavGroup[] {
  return [
    {
      label: loc("离席守护", "Away Guardian"),
      items: [
        { path: "/console/", label: loc("离席守护中心", "Guardian Hub"), iconSvg: icons.shield, badge: "waiting", aliases: ["/console/overview"] },
        { path: "/console/hub", label: loc("安装与配对向导", "Setup & Pair"), iconSvg: icons.phone },
        { path: "/console/notifications", label: t("nav.notifications"), iconSvg: icons.bell },
      ],
    },
    {
      label: loc("进阶工作台", "Workbench & Advanced"),
      collapsible: true,
      items: [
        { path: "/console/sessions", label: t("nav.sessions"), iconSvg: icons.chat },
        { path: "/console/rooms", label: t("nav.rooms"), iconSvg: icons.chat },
        { path: "/console/collab", label: t("nav.collab"), iconSvg: icons.cornerDown },
        { path: "/console/flow", label: t("nav.flow"), iconSvg: icons.bolt },
        { path: "/console/agents", label: t("nav.agents"), iconSvg: icons.puzzle },
        { path: "/console/adapters", label: t("nav.adapters"), iconSvg: icons.terminal },
        { path: "/console/library", label: t("nav.library"), iconSvg: icons.library },
        { path: "/console/devices", label: t("nav.devices"), iconSvg: icons.phone },
        { path: "/console/relay", label: t("nav.relay"), iconSvg: icons.plug },
      ],
      children: [
        {
          parent: "/console/adapters",
          items: [
            { path: "/console/adapters", label: loc("内置", "Built-in") },
            { path: "/console/adapters/discover", label: loc("发现", "Discover") },
            { path: "/console/adapters/installed", label: loc("已安装", "Installed") },
            { path: "/console/adapters/custom", label: loc("自定义", "Custom") },
          ],
        },
      ],
    },
  ];
}

// ---------------------------------------------------------------- 外壳状态

const shellState: {
  connection: ConnectionInfo | null;
  inbox: Inbox | null;
  home: Awaited<ReturnType<typeof endpoints.home>> | null;
  online: boolean;
  authExpired: boolean;
} = { connection: null, inbox: null, home: null, online: false, authExpired: false };

let appRoot: HTMLElement;
let sidebarNav: HTMLElement;
let sidebarStatus: HTMLElement;
let topbar: HTMLElement;
let content: HTMLElement;
let connectionSlot: HTMLElement | null = null;
let navBadge: HTMLElement | null = null;
let currentDispose: (() => void) | null = null;
let shellPoll: (() => void) | null = null;
let currentPath = "/console/";
let versionBadge: HTMLElement | null = null;

// 客户端导航历史栈：为 macOS / Windows 原生桌面壳提供与浏览器一致的后退 / 前进能力。
const navHistory: string[] = [];
let navIndex = -1;

function fullLocationPath(): string {
  return `${normalizePath(location.pathname)}${location.search || ""}`;
}

function syncNavStackOnPopState() {
  const current = fullLocationPath();
  const stateIdx = typeof history.state?.homingNavIdx === "number" ? history.state.homingNavIdx : -1;
  if (stateIdx >= 0 && stateIdx < navHistory.length) {
    navIndex = stateIdx;
    navHistory[navIndex] = current;
    return;
  }
  if (navHistory.length === 0) {
    navHistory.push(current);
    navIndex = 0;
    return;
  }
  if (navIndex > 0 && navHistory[navIndex - 1] === current) {
    navIndex -= 1;
  } else if (navIndex < navHistory.length - 1 && navHistory[navIndex + 1] === current) {
    navIndex += 1;
  } else {
    navHistory.splice(navIndex + 1);
    navHistory.push(current);
    navIndex = navHistory.length - 1;
  }
}

export function canGoBack(): boolean {
  return navIndex > 0;
}

export function canGoForward(): boolean {
  return navIndex >= 0 && navIndex < navHistory.length - 1;
}

export function goBack() {
  if (!canGoBack()) return;
  history.back();
}

export function goForward() {
  if (!canGoForward()) return;
  history.forward();
}

function normalizePath(path: string): string {
  const url = new URL(path, location.origin);
  let p = url.pathname.replace(/\/+$/, "");
  if (p === "" || p === "/console") p = "/console/";
  if (p === "/console/") return p;
  return p;
}

function findRoute(path: string): Route | null {
  const normalized = normalizePath(path);
  if (normalized === "/console/setup") return setupRoute;
  return routes.find((r) => r.path === normalized) || null;
}

export function navigate(path: string, options: { replace?: boolean } = {}) {
  const normalized = normalizePath(path);
  const search = path.includes("?") ? path.slice(path.indexOf("?")) : "";
  const target = `${normalized}${search}`;
  if (!options.replace && target === fullLocationPath()) {
    renderRoute();
    return;
  }
  if (options.replace) {
    if (navIndex >= 0 && navIndex < navHistory.length) {
      navHistory[navIndex] = target;
    } else {
      navHistory.push(target);
      navIndex = navHistory.length - 1;
    }
    history.replaceState({ homingNavIdx: navIndex }, "", target);
  } else {
    navHistory.splice(navIndex + 1);
    navHistory.push(target);
    navIndex = navHistory.length - 1;
    history.pushState({ homingNavIdx: navIndex }, "", target);
  }
  renderRoute();
}

// ---------------------------------------------------------------- 侧栏

let brandPigeon: PigeonPlayer | null = null;
let lastNeedsYou = -1;

function brandPigeonEl(): HTMLElement {
  if (!brandPigeon) {
    brandPigeon = createPigeon({ size: 28, crop: "bird", label: "Homing" });
    brandPigeon.play("wake");
  }
  const btn = h("button", { class: "brand-pigeon", type: "button", title: "Homing", "aria-label": "Homing" });
  btn.append(brandPigeon.el);
  btn.addEventListener("click", (event) => {
    event.stopPropagation();
    brandPigeon?.play("tap");
  });
  return btn;
}

function paintBrandPigeon() {
  const bird = brandPigeon;
  if (!bird) return;
  const waiting = shellState.home?.counts.needs_you ?? shellState.inbox?.counts.waiting ?? 0;
  if (!shellState.online) {
    if (bird.scene !== "sleep") bird.play("sleep", true);
    return;
  }
  if (bird.scene === "sleep") bird.play("wake");
  else if (lastNeedsYou >= 0 && waiting > lastNeedsYou) bird.play("recv");
  else if (waiting > 0 && bird.scene === null) bird.play("wait", true);
  else if (waiting === 0 && bird.scene === "wait") bird.idle();
  lastNeedsYou = waiting;
}

function navItem(leaf: NavLeaf, active: boolean): HTMLElement {
  const el = h(
    "a",
    {
      href: leaf.path,
      class: active ? "nav-item active" : "nav-item",
      dataset: { path: leaf.path },
    },
    leaf.iconSvg ? icon(leaf.iconSvg, 15) : null,
    h("span", { text: leaf.label }),
  );
  if (active) el.setAttribute("aria-current", "page");
  el.addEventListener("click", (event) => {
    const mouse = event as MouseEvent;
    if (mouse.metaKey || mouse.ctrlKey || mouse.shiftKey || mouse.altKey || mouse.button !== 0) return;
    event.preventDefault();
    navigate(leaf.path);
  });
  if (leaf.badge === "waiting") {
    navBadge = h("span", { class: "nav-badge" });
    el.append(navBadge);
  }
  return el;
}

function buildSidebar(): HTMLElement {
  sidebarNav = h("nav", { class: "nav" });
  const navGroups = getNavGroups();

  for (const group of navGroups) {
    const groupHasActive = group.items.some((leaf) => {
      const isParentActive = group.children?.some(
        (child) => child.parent === leaf.path && currentPath.startsWith(leaf.path),
      );
      const isAliasActive = leaf.aliases?.includes(currentPath) ?? false;
      return currentPath === leaf.path || isAliasActive || Boolean(isParentActive);
    });
    if (group.collapsible) {
      const open = workbenchExpanded || groupHasActive;
      const toggleBtn = h(
        "button",
        {
          class: `nav-label nav-label-toggle ${open ? "open" : ""}`.trim(),
          type: "button",
          "aria-expanded": open ? "true" : "false",
        },
        h("span", { text: group.label }),
        icon(open ? icons.chevronDown : icons.chevronRight, 12),
      );
      toggleBtn.addEventListener("click", () => {
        workbenchExpanded = !open;
        renderRoute();
      });
      sidebarNav.append(toggleBtn);
      if (!open) continue;
    } else {
      sidebarNav.append(h("div", { class: "nav-label", text: group.label }));
    }
    for (const leaf of group.items) {
      const isParentActive = group.children?.some(
        (child) => child.parent === leaf.path && currentPath.startsWith(leaf.path),
      );
      const isAliasActive = leaf.aliases?.includes(currentPath) ?? false;
      const el = navItem(leaf, currentPath === leaf.path || isAliasActive || Boolean(isParentActive));
      el.dataset.nav = `top:${leaf.path}`;
      sidebarNav.append(el);

      const subs = group.children?.find((child) => child.parent === leaf.path);
      if (subs && currentPath.startsWith(leaf.path)) {
        const wrap = h("div", { class: "nav-sub" });
        for (const sub of subs.items) wrap.append(navItem(sub, currentPath === sub.path));
        sidebarNav.append(wrap);
      }
    }
  }

  sidebarStatus = h("div", { class: "sidebar-status" });
  const settingsEl = navItem(
    { path: "/console/settings", label: t("nav.settings"), iconSvg: icons.gear },
    currentPath === "/console/settings" || currentPath === "/console/logs" || currentPath === "/console/about",
  );
  settingsEl.dataset.nav = "settings";

  const foot = h("div", { class: "sidebar-foot" }, settingsEl, sidebarStatus);
  versionBadge = h("span", {
    class: "brand-ver",
    text: shellState.connection?.version || "",
    title: loc("本机服务版本", "Local daemon version"),
  });

  return h(
    "aside",
    { class: "sidebar" },
    h(
      "div",
      { class: "brand" },
      brandPigeonEl(),
      h("span", { class: "brand-name", text: "Homing" }),
      versionBadge,
    ),
    sidebarNav,
    foot,
  );
}

function paintSidebarStatus() {
  if (!sidebarStatus) return;
  clear(sidebarStatus);
  if (shellState.authExpired) {
    sidebarStatus.append(
      h("span", { class: "dot dot-warn" }),
      h(
        "div",
        null,
        h("div", { text: loc("凭证已刷新", "Session refreshed") }),
        h("div", { class: "mono", text: loc("点击顶栏刷新页面", "Reload page to re-auth") }),
      ),
    );
    return;
  }
  const online = shellState.online;
  sidebarStatus.append(
    h("span", { class: online ? "dot dot-ok" : "dot" }),
    h(
      "div",
      null,
      h("div", { text: online ? t("shell.online") : t("shell.offline") }),
      h("div", { class: "mono", text: `127.0.0.1:${shellState.connection?.port || 18787}` }),
    ),
  );
}

function paintNavBadge() {
  if (!navBadge) return;
  const count = shellState.home?.counts.needs_you ?? shellState.inbox?.counts.waiting ?? 0;
  navBadge.textContent = count > 0 ? String(count) : "";
}

function paintShellStatus() {
  paintNavBadge();
  paintBrandPigeon();
}

// ---------------------------------------------------------------- 顶栏

function paintConnection() {
  if (!connectionSlot) return;
  clear(connectionSlot);
  if (shellState.authExpired) {
    const reloadPill = h(
      "button",
      {
        class: "auth-expired-pill",
        type: "button",
        title: loc("服务可能已重启，点击重新获取本地访问凭证", "Daemon may have restarted; click to reload"),
        onClick: () => location.reload(),
      },
      h("span", { class: "dot dot-warn" }),
      h("span", { text: t("shell.authExpired") }),
    );
    connectionSlot.append(reloadPill);
    return;
  }
  if (!shellState.online) {
    connectionSlot.append(pill(t("shell.offline"), "bad", loc("homingd 可能已退出", "homingd may have exited")));
    return;
  }
  const awayEnabled = Boolean(shellState.home?.away?.enabled);
  const awayBtn = h(
    "button",
    {
      class: awayEnabled ? "away-toggle-pill on" : "away-toggle-pill",
      type: "button",
      title: awayEnabled
        ? loc("离席监听开启中：新消息会推送至手机并防止电脑休眠（点击关闭）", "Away monitoring is ON: new messages push to phone (click to turn off)")
        : loc("离席监听未开启：在电脑前工作不打扰手机（离开电脑前点此开启）", "Away monitoring is OFF: click before leaving desk to forward new messages to phone"),
      onClick: async () => {
        const next = !awayEnabled;
        awayBtn.setAttribute("disabled", "disabled");
        try {
          await endpoints.setAway(next);
          toast(
            next
              ? loc("已开启离席监听：离开期间的新消息将推送至手机", "Away monitoring enabled")
              : loc("已关闭离席监听：回到电脑前工作", "Away monitoring disabled"),
            "ok",
          );
          await refreshShell();
          renderRoute();
        } catch (err) {
          reportError(err, loc("切换离席监听失败", "Failed to toggle away monitoring"));
        } finally {
          awayBtn.removeAttribute("disabled");
        }
      },
    },
    h("span", { class: awayEnabled ? "dot dot-ok" : "dot" }),
    h("span", {
      text: awayEnabled
        ? loc("离席监听中 · 点击关闭", "Monitoring ON")
        : loc("开启离席监听", "Start Monitoring"),
    }),
  );

  const waiting = shellState.home?.counts.needs_you ?? shellState.inbox?.counts.waiting ?? 0;
  const running = shellState.home?.counts.running ?? shellState.inbox?.counts.running ?? 0;
  const text =
    waiting || running
      ? [
          waiting ? loc(`${waiting} 等处理`, `${waiting} waiting`) : "",
          running ? loc(`${running} 运行中`, `${running} running`) : "",
        ]
          .filter(Boolean)
          .join(" · ")
      : t("shell.connected");
  const tone = waiting > 0 ? "warn" : running > 0 ? "run" : "ok";
  connectionSlot.append(
    awayBtn,
    pill(
      text,
      tone,
      `${shellState.connection?.host_name || loc("本机", "Local")} · ${loc("端口", "Port")} ${shellState.connection?.port || 18787}`,
    ),
  );
}

function crumbTargetFor(label: string): string | null {
  if (label === t("nav.group.work")) return "/console/";
  if (label === t("nav.group.connect")) return "/console/hub";
  if (label === t("nav.adapters")) return "/console/adapters";
  if (label === t("nav.settings")) return "/console/settings";
  return null;
}

function buildNavHistoryButtons(): HTMLElement {
  const backBtn = h(
    "button",
    {
      class: "icon-btn nav-history-btn",
      type: "button",
      disabled: !canGoBack(),
      title: loc("返回上一页（⌘[ / Alt+←）", "Back (⌘[ / Alt+←)"),
      "aria-label": loc("返回上一页", "Back"),
      onClick: () => goBack(),
    },
    icon(icons.arrowLeft, 14),
  );
  const fwdBtn = h(
    "button",
    {
      class: "icon-btn nav-history-btn",
      type: "button",
      disabled: !canGoForward(),
      title: loc("前进下一页（⌘] / Alt+→）", "Forward (⌘] / Alt+→)"),
      "aria-label": loc("前进下一页", "Forward"),
      onClick: () => goForward(),
    },
    icon(icons.arrowRight, 14),
  );
  return h("div", { class: "nav-history-group" }, backBtn, fwdBtn);
}

function buildCrumbs(route: Route): HTMLElement {
  const wrap = h("div", { class: "crumbs" });
  const parts = route.crumbs();
  parts.forEach((part, index) => {
    if (index > 0) wrap.append(h("span", { class: "sep" }, icon(icons.chevronRight, 12)));
    const isLast = index === parts.length - 1;
    const target = !isLast ? crumbTargetFor(part) : null;
    if (target) {
      wrap.append(
        h("button", {
          class: "crumb-link",
          type: "button",
          text: part,
          onClick: () => navigate(target),
        }),
      );
    } else {
      wrap.append(h("span", { class: isLast ? "here" : "", text: part }));
    }
  });
  return wrap;
}

async function refreshShell() {
  let connection: ConnectionInfo;
  try {
    connection = await endpoints.connection();
    shellState.connection = connection;
    if (versionBadge) versionBadge.textContent = connection.version;
    shellState.online = true;
    shellState.authExpired = false;
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      shellState.authExpired = true;
      shellState.online = true;
    } else {
      shellState.online = false;
      shellState.authExpired = false;
    }
    paintConnection();
    paintShellStatus();
    paintSidebarStatus();
    return;
  }

  paintConnection();
  paintSidebarStatus();

  const home = await endpoints.home().catch(() => null);
  shellState.home = home;
  shellState.inbox = home ? null : await endpoints.inbox().catch(() => null);
  paintConnection();
  paintShellStatus();
}

// ---------------------------------------------------------------- 路由渲染

function buildTopbar(route: Route, refreshBtn: HTMLButtonElement): HTMLElement {
  connectionSlot = h("div", { class: "connection-slot" });
  const langBtn = h("button", {
    class: "lang-btn",
    type: "button",
    text: getLocale() === "en" ? "中文" : "EN",
    title: loc("切换界面语言（简体中文 / English）", "Switch language (English / 简体中文)"),
    "aria-label": loc("切换语言", "Switch language"),
  });
  langBtn.addEventListener("click", () => toggleLocale());
  return h(
    "header",
    { class: "topbar" },
    buildNavHistoryButtons(),
    buildCrumbs(route),
    h("div", { class: "spacer" }),
    connectionSlot,
    langBtn,
    refreshBtn,
  );
}

function renderRoute() {
  const path = normalizePath(location.pathname);
  const query = new URLSearchParams(location.search);
  const route = findRoute(path);

  if (currentDispose) {
    try {
      currentDispose();
    } catch {
      /* 忽略 */
    }
    currentDispose = null;
  }

  if (!route) {
    currentPath = path;
    const fake: Route = {
      path,
      crumbs: () => [loc("找不到页面", "Not found")],
      iconSvg: icons.warn,
      render: () => ({ el: h("div") }),
    };
    clear(appRoot);
    content = h("div", { class: "content" });
    appRoot.append(
      h(
        "div",
        { class: "shell" },
        buildSidebar(),
        h("div", { class: "main" }, h("header", { class: "topbar" }, buildNavHistoryButtons(), buildCrumbs(fake)), content),
      ),
    );
    content.append(notFoundWithNav({ navigate, refreshShell, refreshPage: renderRoute }).el);
    paintSidebarStatus();
    return;
  }

  currentPath = route.path;

  if (path === "/console/tray") {
    const result = renderTray({ navigate, refreshShell, refreshPage: renderRoute });
    clear(appRoot);
    document.body.classList.add("tray-mode");
    appRoot.append(result.el);
    currentDispose = result.dispose || null;
    void refreshShell();
    return;
  }
  document.body.classList.remove("tray-mode");

  if (path === "/console/setup") {
    const result = route.render({ navigate, refreshShell, refreshPage: renderRoute }, query);
    clear(appRoot);
    appRoot.append(result.el);
    currentDispose = result.dispose || null;
    return;
  }

  const result = route.render({ navigate, refreshShell, refreshPage: renderRoute }, query);

  const refreshBtn = h("button", {
    class: "icon-btn",
    type: "button",
    title: loc("刷新（⌘R）", "Refresh (⌘R)"),
    "aria-label": t("common.refresh"),
  });
  refreshBtn.append(icon(icons.refresh, 15));
  refreshBtn.addEventListener("click", () => {
    refreshBtn.classList.add("spinning");
    window.setTimeout(() => refreshBtn.classList.remove("spinning"), 700);
    renderRoute();
  });

  topbar = buildTopbar(route, refreshBtn);
  const contentClass =
    route.path === "/console/sessions"
      ? "content content-workbench"
      : route.path === "/console/" ||
          route.path === "/console/overview" ||
          route.path === "/console/hub" ||
          route.path === "/console/flow" ||
          route.path === "/console/agents" ||
          route.path === "/console/library" ||
          route.path === "/console/rooms" ||
          route.path === "/console/collab"
        ? "content content-wide"
        : "content";
  content = h("div", { class: contentClass }, result.el);

  clear(appRoot);
  appRoot.append(
    h(
      "div",
      { class: route.path === "/console/sessions" ? "shell shell-workbench" : "shell" },
      buildSidebar(),
      h("div", { class: "main" }, topbar, content),
    ),
  );

  currentDispose = result.dispose || null;
  paintConnection();
  paintNavBadge();
  paintSidebarStatus();
  window.requestAnimationFrame(() => {
    if (!window.matchMedia("(max-width: 760px)").matches) return;
    appRoot.querySelector<HTMLElement>(".nav-item.active")?.scrollIntoView({
      block: "nearest",
      inline: "nearest",
    });
  });
  void refreshShell();

  if (!shellPoll) {
    shellPoll = poll(refreshShell, 10_000);
  }
}

// ---------------------------------------------------------------- 启动

function boot() {
  captureDesktopVersionFromURL();
  const initial = fullLocationPath();
  navHistory.length = 0;
  navHistory.push(initial);
  navIndex = 0;
  try {
    history.replaceState({ homingNavIdx: 0 }, "", initial);
  } catch {
    /* ignore */
  }
  appRoot = document.getElementById("app") || document.body.appendChild(h("div", { id: "app" }));
  window.addEventListener("popstate", () => {
    syncNavStackOnPopState();
    renderRoute();
  });
  window.addEventListener("keydown", (event) => {
    const target = event.target as HTMLElement | null;
    const isEditing =
      target &&
      (target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.tagName === "SELECT" ||
        target.isContentEditable);
    if (isEditing) return;
    if ((event.metaKey && event.key === "[") || (event.altKey && event.key === "ArrowLeft")) {
      event.preventDefault();
      goBack();
    } else if ((event.metaKey && event.key === "]") || (event.altKey && event.key === "ArrowRight")) {
      event.preventDefault();
      goForward();
    }
  });
  onLocaleChange(() => renderRoute());
  document.addEventListener("click", (event) => {
    const anchor = (event.target as HTMLElement)?.closest?.("a[data-link]") as HTMLAnchorElement | null;
    if (!anchor) return;
    const href = anchor.getAttribute("href");
    if (!href || !href.startsWith("/console")) return;
    event.preventDefault();
    navigate(href);
  });

  renderRoute();
}

boot();
