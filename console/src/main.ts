import "./styles.css";
import { h, clear, icon } from "./lib/dom";
import { icons, pigeon } from "./lib/icons";
import { endpoints, poll, type ConnectionInfo, type Inbox } from "./lib/api";
import { pill } from "./lib/ui";
import { renderInbox } from "./pages/inbox";
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
import { notFoundWithNav } from "./pages/notfound";

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
  /** 面包屑，最后一段是当前页 */
  crumbs: string[];
  iconSvg: string;
  render: (ctx: PageContext, query: URLSearchParams) => PageResult;
}

// ---------------------------------------------------------------- 路由表

const routes: Route[] = [
  { path: "/console/", crumbs: ["使用", "收件箱"], iconSvg: icons.inbox, render: renderInbox },
  { path: "/console/sessions", crumbs: ["使用", "会话"], iconSvg: icons.chat, render: renderSessions },
  { path: "/console/agents", crumbs: ["使用", "Agent"], iconSvg: icons.agents, render: renderAgents },
  { path: "/console/devices", crumbs: ["手机", "设备"], iconSvg: icons.phone, render: renderDevices },
  { path: "/console/notifications", crumbs: ["手机", "通知"], iconSvg: icons.bell, render: renderNotifications },
  { path: "/console/relay", crumbs: ["连接", "中转与局域网"], iconSvg: icons.plug, render: renderRelay },
  {
    path: "/console/adapters",
    crumbs: ["扩展", "接入", "内置"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "builtin"),
  },
  {
    path: "/console/adapters/discover",
    crumbs: ["扩展", "接入", "发现"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "discover"),
  },
  {
    path: "/console/adapters/installed",
    crumbs: ["扩展", "接入", "已安装"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "installed"),
  },
  {
    path: "/console/adapters/custom",
    crumbs: ["扩展", "接入", "自定义"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "custom"),
  },
  { path: "/console/settings", crumbs: ["设置"], iconSvg: icons.gear, render: renderSettings },
  { path: "/console/logs", crumbs: ["设置", "运行日志"], iconSvg: icons.logs, render: renderLogs },
  { path: "/console/about", crumbs: ["设置", "关于"], iconSvg: icons.info, render: renderAbout },
];

const setupRoute: Route = {
  path: "/console/setup",
  crumbs: ["设置", "首次设置"],
  iconSvg: icons.key,
  render: renderSetup,
};

// ---------------------------------------------------------------- 侧栏结构

interface NavLeaf {
  path: string;
  label: string;
  iconSvg?: string;
  badge?: "waiting";
}

interface NavGroup {
  label: string;
  items: NavLeaf[];
  /** 有 children 的项是父级：点它进第一个子项，同时把子项列出来 */
  children?: Array<{ parent: string; items: NavLeaf[] }>;
}

const navGroups: NavGroup[] = [
  {
    label: "使用",
    items: [
      { path: "/console/", label: "收件箱", iconSvg: icons.inbox, badge: "waiting" },
      { path: "/console/sessions", label: "会话", iconSvg: icons.chat },
      { path: "/console/agents", label: "Agent", iconSvg: icons.agents },
    ],
  },
  {
    label: "手机",
    items: [
      { path: "/console/devices", label: "设备", iconSvg: icons.phone },
      { path: "/console/notifications", label: "通知", iconSvg: icons.bell },
    ],
  },
  {
    label: "连接",
    items: [{ path: "/console/relay", label: "中转与局域网", iconSvg: icons.plug }],
  },
  {
    label: "扩展",
    items: [{ path: "/console/adapters", label: "接入", iconSvg: icons.puzzle }],
    children: [
      {
        parent: "/console/adapters",
        items: [
          { path: "/console/adapters", label: "内置" },
          { path: "/console/adapters/discover", label: "发现" },
          { path: "/console/adapters/installed", label: "已安装" },
          { path: "/console/adapters/custom", label: "自定义" },
        ],
      },
    ],
  },
];

// ---------------------------------------------------------------- 外壳状态

const shellState: {
  connection: ConnectionInfo | null;
  inbox: Inbox | null;
  online: boolean;
} = { connection: null, inbox: null, online: false };

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
  if (options.replace) history.replaceState({}, "", target);
  else history.pushState({}, "", target);
  renderRoute();
}

// ---------------------------------------------------------------- 侧栏

function navItem(leaf: NavLeaf, active: boolean): HTMLElement {
  const el = h(
    "div",
    { class: active ? "nav-item active" : "nav-item", dataset: { path: leaf.path }, role: "link", tabindex: "0" },
    leaf.iconSvg ? icon(leaf.iconSvg, 15) : null,
    h("span", { text: leaf.label }),
  );
  el.addEventListener("click", () => navigate(leaf.path));
  el.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter") navigate(leaf.path);
  });
  if (leaf.badge === "waiting") {
    navBadge = h("span", { class: "nav-badge" });
    el.append(navBadge);
  }
  return el;
}

function buildSidebar(): HTMLElement {
  sidebarNav = h("nav", { class: "nav" });

  for (const group of navGroups) {
    sidebarNav.append(h("div", { class: "nav-label", text: group.label }));
    for (const leaf of group.items) {
      const isParentActive = group.children?.some(
        (child) => child.parent === leaf.path && currentPath.startsWith(leaf.path),
      );
      const el = navItem(leaf, currentPath === leaf.path || Boolean(isParentActive));
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
  const settingsEl = navItem({ path: "/console/settings", label: "设置", iconSvg: icons.gear }, currentPath === "/console/settings");
  settingsEl.dataset.nav = "settings";

  const foot = h("div", { class: "sidebar-foot" }, settingsEl, sidebarStatus);

  return h(
    "aside",
    { class: "sidebar" },
    h(
      "div",
      { class: "brand" },
      pigeon(24, "#CBD5E1"),
      h("span", { class: "brand-name", text: "Homing" }),
      h("span", { class: "brand-ver", text: "v0.7.0" }),
    ),
    sidebarNav,
    foot,
  );
}

function paintSidebarStatus() {
  if (!sidebarStatus) return;
  clear(sidebarStatus);
  const online = shellState.online;
  sidebarStatus.append(
    h("span", { class: online ? "dot dot-ok" : "dot" }),
    h(
      "div",
      null,
      h("div", { text: online ? "本机服务运行中" : "本机服务未响应" }),
      h("div", { class: "mono", text: `127.0.0.1:${shellState.connection?.port || 18787}` }),
    ),
  );
}

function paintNavBadge() {
  if (!navBadge) return;
  const count = shellState.inbox?.counts.waiting || 0;
  navBadge.textContent = count > 0 ? String(count) : "";
}

// ---------------------------------------------------------------- 顶栏

function paintConnection() {
  if (!connectionSlot) return;
  clear(connectionSlot);
  if (!shellState.online) {
    connectionSlot.append(pill("本机服务未响应", "bad", "homingd 可能已退出"));
    return;
  }
  const waiting = shellState.inbox?.counts.waiting || 0;
  const running = shellState.inbox?.counts.running || 0;
  const text = waiting || running
    ? [waiting ? `${waiting} 等处理` : "", running ? `${running} 运行中` : ""].filter(Boolean).join(" · ")
    : "已连接";
  connectionSlot.append(
    pill(text, "ok", `${shellState.connection?.host_name || "本机"} · 端口 ${shellState.connection?.port || 18787}`),
  );
}

function buildCrumbs(route: Route): HTMLElement {
  const wrap = h("div", { class: "crumbs" });
  route.crumbs.forEach((part, index) => {
    if (index > 0) wrap.append(h("span", { class: "sep" }, icon(icons.chevronRight, 12)));
    wrap.append(h("span", { class: index === route.crumbs.length - 1 ? "here" : "", text: part }));
  });
  return wrap;
}

async function refreshShell() {
  try {
    const [connection, inbox] = await Promise.all([endpoints.connection(), endpoints.inbox()]);
    shellState.connection = connection;
    shellState.inbox = inbox;
    shellState.online = true;
  } catch {
    shellState.online = false;
  }
  paintConnection();
  paintNavBadge();
  paintSidebarStatus();
}

// ---------------------------------------------------------------- 路由渲染

function buildTopbar(route: Route, refreshBtn: HTMLButtonElement): HTMLElement {
  connectionSlot = h("div", { style: "display:flex;gap:8px;align-items:center" });
  return h("header", { class: "topbar" }, buildCrumbs(route), h("div", { class: "spacer" }), connectionSlot, refreshBtn);
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
    const fake: Route = { path, crumbs: ["找不到页面"], iconSvg: icons.warn, render: () => ({ el: h("div") }) };
    clear(appRoot);
    content = h("div", { class: "content" });
    appRoot.append(h("div", { class: "shell" }, buildSidebar(), h("div", { class: "main" }, h("header", { class: "topbar" }, buildCrumbs(fake)), content)));
    content.append(notFoundWithNav({ navigate, refreshShell, refreshPage: renderRoute }).el);
    paintSidebarStatus();
    return;
  }

  currentPath = route.path;

  if (path === "/console/setup") {
    // 向导不套侧栏：首次打开时只做一件事
    const result = route.render({ navigate, refreshShell, refreshPage: renderRoute }, query);
    clear(appRoot);
    appRoot.append(result.el);
    currentDispose = result.dispose || null;
    void refreshShell();
    return;
  }

  const result = route.render({ navigate, refreshShell, refreshPage: renderRoute }, query);

  const refreshBtn = h("button", { class: "icon-btn", type: "button", title: "刷新（⌘R）", "aria-label": "刷新" });
  refreshBtn.append(icon(icons.refresh, 15));
  refreshBtn.addEventListener("click", () => {
    refreshBtn.classList.add("spinning");
    window.setTimeout(() => refreshBtn.classList.remove("spinning"), 700);
    renderRoute();
  });

  topbar = buildTopbar(route, refreshBtn);
  content = h("div", { class: "content" }, result.el);

  clear(appRoot);
  appRoot.append(h("div", { class: "shell" }, buildSidebar(), h("div", { class: "main" }, topbar, content)));

  currentDispose = result.dispose || null;
  paintConnection();
  paintNavBadge();
  paintSidebarStatus();
  void refreshShell();

  if (!shellPoll) {
    shellPoll = poll(refreshShell, 8000);
  }
}

// ---------------------------------------------------------------- 启动

function boot() {
  appRoot = document.getElementById("app") || document.body.appendChild(h("div", { id: "app" }));
  window.addEventListener("popstate", renderRoute);
  document.addEventListener("click", (event) => {
    const anchor = (event.target as HTMLElement)?.closest?.("a[data-link]") as HTMLAnchorElement | null;
    if (!anchor) return;
    const href = anchor.getAttribute("href");
    if (!href || !href.startsWith("/console")) return;
    event.preventDefault();
    navigate(href);
  });

  const path = normalizePath(location.pathname);
  if (path === "/console/" && !location.search) {
    // 第一次进来且还没走过向导：直接进 /console/setup
    endpoints
      .setup()
      .then((state) => {
        const skipped = localStorage.getItem("homing.setup.skipped") === "1";
        if (!state.completed && !skipped) navigate("/console/setup", { replace: true });
        else renderRoute();
      })
      .catch(() => renderRoute());
    return;
  }
  renderRoute();
}

boot();
