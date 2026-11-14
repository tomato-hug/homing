import "./styles.css";
import { h, clear, icon } from "./lib/dom";
import { icons } from "./lib/icons";
import { createPigeon, type PigeonPlayer } from "./lib/pigeon";
import { endpoints, poll, type ConnectionInfo, type Inbox } from "./lib/api";
import { pill } from "./lib/ui";
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
  /** 面包屑，最后一段是当前页 */
  crumbs: string[];
  iconSvg: string;
  render: (ctx: PageContext, query: URLSearchParams) => PageResult;
}

// ---------------------------------------------------------------- 路由表

const routes: Route[] = [
  { path: "/console/tray", crumbs: ["Homing"], iconSvg: icons.overview, render: renderTray },
  { path: "/console/", crumbs: ["Agent"], iconSvg: icons.agents, render: renderHub },
  { path: "/console/overview", crumbs: ["使用", "总览"], iconSvg: icons.overview, render: renderHome },
  { path: "/console/flow", crumbs: ["使用", "实时流"], iconSvg: icons.bolt, render: renderFlow },
  { path: "/console/inbox", crumbs: ["使用", "收件箱"], iconSvg: icons.inbox, render: renderInbox },
  { path: "/console/sessions", crumbs: ["使用", "会话"], iconSvg: icons.chat, render: renderSessions },
  { path: "/console/rooms", crumbs: ["使用", "会议室"], iconSvg: icons.chat, render: renderRooms },
  { path: "/console/collab", crumbs: ["使用", "协作"], iconSvg: icons.cornerDown, render: renderCollab },
  { path: "/console/agents", crumbs: ["Agent", "全部 Agent"], iconSvg: icons.puzzle, render: renderAgents },
  { path: "/console/devices", crumbs: ["手机", "设备"], iconSvg: icons.phone, render: renderDevices },
  { path: "/console/notifications", crumbs: ["手机", "通知"], iconSvg: icons.bell, render: renderNotifications },
  { path: "/console/relay", crumbs: ["连接", "中转与局域网"], iconSvg: icons.plug, render: renderRelay },
  {
    path: "/console/adapters",
    crumbs: ["Agent", "目录与接入", "内置"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "builtin"),
  },
  {
    path: "/console/adapters/discover",
    crumbs: ["Agent", "目录与接入", "发现"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "discover"),
  },
  {
    path: "/console/adapters/installed",
    crumbs: ["Agent", "目录与接入", "已安装"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "installed"),
  },
  {
    path: "/console/adapters/custom",
    crumbs: ["Agent", "目录与接入", "自定义"],
    iconSvg: icons.puzzle,
    render: (ctx) => renderAdapters(ctx, "custom"),
  },
  { path: "/console/library", crumbs: ["Agent", "资源库"], iconSvg: icons.library, render: renderLibrary },
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
    label: "开始",
    items: [{ path: "/console/", label: "Agent", iconSvg: icons.agents }],
  },
  {
    label: "使用",
    items: [
      { path: "/console/overview", label: "总览", iconSvg: icons.overview, badge: "waiting" },
      { path: "/console/sessions", label: "会话", iconSvg: icons.chat },
      { path: "/console/flow", label: "实时流", iconSvg: icons.bolt },
      { path: "/console/rooms", label: "会议室", iconSvg: icons.chat },
      { path: "/console/collab", label: "协作", iconSvg: icons.cornerDown },
    ],
  },
  {
    label: "Agent 管理",
    items: [
      { path: "/console/agents", label: "全部 Agent", iconSvg: icons.puzzle },
      { path: "/console/adapters", label: "适配器", iconSvg: icons.terminal },
      { path: "/console/library", label: "资源库", iconSvg: icons.library },
    ],
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
];

// ---------------------------------------------------------------- 外壳状态

const shellState: {
  connection: ConnectionInfo | null;
  inbox: Inbox | null;
  home: Awaited<ReturnType<typeof endpoints.home>> | null;
  online: boolean;
} = { connection: null, inbox: null, home: null, online: false };

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
const consoleVersion = "v0.9.0";

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

// 侧栏品牌鸽子在整个会话里只有一个实例：换页时把同一个 SVG 挂回去，动画不中断。
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

/** 按服务状态切换鸽子：离线睡觉、有事等你时衔信张望、来新消息时收信 */
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
    "div",
    { class: active ? "nav-item active" : "nav-item", dataset: { path: leaf.path }, role: "link", tabindex: "0" },
    leaf.iconSvg ? icon(leaf.iconSvg, 15) : null,
    h("span", { text: leaf.label }),
  );
  if (active) el.setAttribute("aria-current", "page");
  el.addEventListener("click", () => navigate(leaf.path));
  el.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter" || (event as KeyboardEvent).key === " ") {
      event.preventDefault();
      navigate(leaf.path);
    }
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
      brandPigeonEl(),
      h("span", { class: "brand-name", text: "Homing" }),
      h("span", { class: "brand-ver", text: consoleVersion }),
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
  if (!shellState.online) {
    connectionSlot.append(pill("本机服务未响应", "bad", "homingd 可能已退出"));
    return;
  }
  const waiting = shellState.home?.counts.needs_you ?? shellState.inbox?.counts.waiting ?? 0;
  const running = shellState.home?.counts.running ?? shellState.inbox?.counts.running ?? 0;
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
  let connection: ConnectionInfo;
  try {
    connection = await endpoints.connection();
    shellState.connection = connection;
    shellState.online = true;
  } catch {
    shellState.online = false;
    paintConnection();
    paintShellStatus();
    paintSidebarStatus();
    return;
  }

  // 連線本身很快，會話掃描可能還要十幾秒。先把「已連上」畫出來，避免把
  // 正在載入誤報成服務離線。
  paintConnection();
  paintSidebarStatus();

  const home = await endpoints.home().catch(() => null);
  shellState.home = home;
  shellState.inbox = home ? null : await endpoints.inbox().catch(() => null);
  paintShellStatus();
}

// ---------------------------------------------------------------- 路由渲染

function buildTopbar(route: Route, refreshBtn: HTMLButtonElement): HTMLElement {
  connectionSlot = h("div", { class: "connection-slot" });
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

  if (path === "/console/tray") {
    // 托盤面板由 Wails 以 360px 寬的獨立視窗載入，不能套主控制台側欄。
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
  const contentClass =
    route.path === "/console/sessions"
      ? "content content-workbench"
      : route.path === "/console/" ||
          route.path === "/console/overview" ||
          route.path === "/console/flow" ||
          route.path === "/console/agents" ||
          route.path === "/console/library" ||
          route.path === "/console/rooms" ||
          route.path === "/console/collab"
        ? "content content-wide"
        : "content";
  content = h(
    "div",
    { class: contentClass },
    result.el,
  );

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

  // 首页就是「Agent」：检测、接入、装手机端、配对都在那一页完成，
  // 不再先把人送进单独的设置向导。
  renderRoute();
}

boot();
