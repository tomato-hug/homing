// 首页 · Agent：Homing 的核心流程放在一页里做完。
//
//   ① 本机检测到的 Agent → 点「接入」
//   ② 没装的 Agent → 「一键安装」（在这台电脑上执行官方安装命令）或去官网下载
//   ③ 手机：一个二维码，相机扫了能下载 App，App 里扫了直接配对；电脑上核对四个词
//   ④ 之后在手机上收消息、管理会话
//
// 完整的目录、钩子与清单细节仍在「全部 Agent」页。

import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { AGENT_INITIALS } from "../lib/format";
import { agentLogo } from "../lib/agent-icons";
import { createPigeon, type PigeonPlayer } from "../lib/pigeon";
import { createPairCard } from "../lib/pair-card";
import { endpoints, type AgentCatalogEntry, type SetupJob } from "../lib/api";
import { button, openExternalUrl, reportError, tag, toast } from "../lib/ui";
import { loc } from "../i18n";
import type { PageContext, PageResult } from "../main";

type Bucket = "connected" | "ready" | "install" | "more";

/** 首页「可以一键安装」只放 Homing 自己核实过的；其余在全部 Agent 页 */
const FEATURED_INSTALL = ["codex", "claude", "workbuddy", "opencode", "kimi", "gemini-cli", "dsh", "codebuddy", "cursor", "copilot-cli"];

export function renderHub(ctx: PageContext): PageResult {
  const root = h("div", { class: "hub" });
  let disposed = false;
  let catalog: AgentCatalogEntry[] = [];
  let pairedCount = 0;
  let sessionCounts: Record<string, number> = {};
  let justPaired = false;
  let pairing = false;
  let showMore = false;
  const busy = new Set<string>();
  const jobs = new Map<string, SetupJob>();
  let jobTimer: number | null = null;
  let activeModalClose: (() => void) | null = null;

  const hero = h("section", { class: "hub-hero" });
  const steps = h("div", { class: "hub-steps" });
  const main = h("div", { class: "hub-main" });
  const side = h("aside", { class: "hub-side" });
  const grid = h("div", { class: "hub-grid" }, main, side);
  root.append(hero, grid);

  // ------------------------------------------------------------ 头部

  const heroPigeon: PigeonPlayer = createPigeon({ size: 92, crop: "stage", label: "Homing 灰鸽" });
  hero.append(
    h("div", { class: "hub-hero-art" }, heroPigeon.el),
    h(
      "div",
      { class: "hub-hero-copy" },
      h("h1", { text: "把电脑里的 Agent 装进手机" }),
      h("p", { text: "接入本机的 Agent，装好手机 App 并配对。之后会话消息会推到手机，在手机上就能回复、审批、开新会话。" }),
      steps,
    ),
  );
  heroPigeon.play("wake");

  function stepChip(index: number, title: string, detail: string, done: boolean, current: boolean, onClick?: () => void) {
    const el = h(
      "button",
      { class: `hub-step ${done ? "done" : ""} ${current ? "current" : ""}`.trim(), type: "button" },
      h("span", { class: "hub-step-num" }, done ? icon(icons.check, 13) : h("span", { text: String(index) })),
      h("span", { class: "hub-step-text" }, h("b", { text: title }), h("small", { text: detail })),
    );
    if (onClick) el.addEventListener("click", onClick);
    return el;
  }

  function paintSteps() {
    clear(steps);
    const connected = catalog.filter((entry) => bucketOf(entry) === "connected").length;
    const phoneDone = pairedCount > 0 || justPaired;
    steps.append(
      stepChip(1, "接入 Agent", connected > 0 ? `已接入 ${connected} 个` : "点下面的「接入」", connected > 0, connected === 0, () =>
        main.querySelector(".hub-section")?.scrollIntoView({ behavior: "smooth", block: "start" }),
      ),
      stepChip(2, "装好手机 App", phoneDone ? "已安装" : "相机扫右侧二维码", phoneDone, connected > 0 && !phoneDone, () =>
        side.scrollIntoView({ behavior: "smooth", block: "start" }),
      ),
      stepChip(3, "配对手机", phoneDone ? `已配对 ${Math.max(pairedCount, 1)} 台` : "电脑上核对四个词", phoneDone, false, () =>
        side.scrollIntoView({ behavior: "smooth", block: "start" }),
      ),
      stepChip(
        4,
        loc("发起或管理会话", "Manage Sessions"),
        phoneDone ? loc("去新建第一个会话", "Start a session") : loc("配好后自动开始", "Ready after pairing"),
        false,
        phoneDone,
        () => ctx.navigate("/console/sessions?new=1"),
      ),
    );
  }

  // ------------------------------------------------------------ 数据

  async function load() {
    try {
      const [catalogPayload, info, devices] = await Promise.all([
        endpoints.agentCatalog(),
        endpoints.connection().catch(() => null),
        endpoints.devices().catch(() => null),
      ]);
      if (disposed) return;
      catalog = catalogPayload.agents;
      // /api/devices already merges legacy token devices and cloud E2E peers;
      // fall back to info.cloud.peers only if /api/devices failed.
      pairedCount = devices ? devices.count : (info?.cloud?.peers || 0);
      paint();
      void endpoints
        .sessions(500)
        .then((payload) => {
          if (disposed) return;
          sessionCounts = {};
          for (const session of payload.sessions) sessionCounts[session.source] = (sessionCounts[session.source] || 0) + 1;
          paintMain();
        })
        .catch(() => undefined);
    } catch (err) {
      if (disposed) return;
      clear(main);
      main.append(h("div", { class: "list-card hub-empty" }, h("div", { text: "读取 Agent 失败" }), h("div", { class: "note", text: (err as Error).message })));
    }
  }

  function bucketOf(entry: AgentCatalogEntry): Bucket {
    if (entry.connected) return "connected";
    if (entry.detected && (entry.can_connect || entry.readiness === "builtin" || entry.can_install)) return "ready";
    if (!entry.detected && (entry.can_setup || entry.download_url || (entry.install_commands?.length || 0) > 0)) return "install";
    return "more";
  }

  // ------------------------------------------------------------ 动作

  async function connect(entry: AgentCatalogEntry) {
    busy.add(entry.id);
    paintMain();
    try {
      let hookChanged = false;
      if (entry.readiness === "builtin") {
        await endpoints.patchAgent(entry.id, { connected: true, push_to_phone: true });
        if (entry.hook_install === "auto" && !entry.hook_installed) {
          const result = await endpoints.installHooks(entry.id).catch(() => null);
          if (result && result.ok && result.changed) hookChanged = true;
          else if (result && !result.ok) toast(result.error || result.message || loc("钩子需要手动添加，见 Agent 页", "Hooks require manual setup"), "plain", 7000);
        }
      } else {
        await endpoints.quickConnectCatalogAgent(entry.id);
      }
      toast(
        hookChanged
          ? loc(`${entry.name} 已接入并装好钩子，请重启该 Agent 进程使推送生效`, `${entry.name} connected with hooks; restart the agent to activate push`)
          : loc(`${entry.name} 已接入，新消息会推到手机`, `${entry.name} connected; new messages will push to your phone`),
        "ok",
        hookChanged ? 7000 : 4200,
      );
      heroPigeon.play("recv");
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "接入失败");
    } finally {
      busy.delete(entry.id);
      await load();
    }
  }

  async function installHook(entry: AgentCatalogEntry) {
    busy.add(entry.id);
    paintMain();
    try {
      const result = await endpoints.installHooks(entry.id);
      if (result.ok) toast(result.changed ? "钩子已安装，重启该 Agent 后生效" : result.message || "钩子已经装好了", "ok");
      else toast(result.error || result.message || "这个 Agent 的钩子要手动添加", "plain", 8000);
    } catch (err) {
      reportError(err, "安装钩子失败");
    } finally {
      busy.delete(entry.id);
      await load();
    }
  }

  function openInstall(entry: AgentCatalogEntry) {
    const commands = entry.install_commands || [];
    const runnable = commands.map((command, index) => ({ command, index })).filter((item) => item.command.runnable);
    let chosen = runnable[0]?.index ?? -1;
    const overlay = h("div", { class: "hub-modal-backdrop" });
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    const close = () => {
      window.removeEventListener("keydown", onKeyDown);
      overlay.remove();
      if (activeModalClose === close) activeModalClose = null;
    };
    activeModalClose?.();
    activeModalClose = close;
    window.addEventListener("keydown", onKeyDown);
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) close();
    });
    const list = h("div", { class: "hub-cmds" });
    const paintList = () => {
      clear(list);
      commands.forEach((command, index) => {
        const row = h(
          "label",
          { class: `hub-cmd ${chosen === index ? "on" : ""} ${command.runnable ? "" : "off"}`.trim() },
          h("input", { type: "radio", name: "hub-cmd", disabled: !command.runnable, checked: chosen === index }),
          h(
            "span",
            { class: "hub-cmd-body" },
            h("b", { text: command.label + (command.runnable ? "" : "（这台电脑没有对应工具）") }),
            h("code", { text: command.command }),
          ),
        );
        row.addEventListener("click", () => {
          if (!command.runnable) return;
          chosen = index;
          paintList();
        });
        list.append(row);
      });
    };
    paintList();
    const runBtn = button("开始安装", {
      variant: "primary",
      iconSvg: icons.download,
      disabled: chosen < 0,
      onClick: () => {
        if (chosen < 0) return;
        close();
        void startSetup(entry, chosen);
      },
    });
    const copyBtn = button("复制命令", {
      variant: "quiet",
      onClick: () => {
        const command = commands[Math.max(0, chosen)]?.command || commands[0]?.command || "";
        void navigator.clipboard?.writeText(command).then(
          () => toast("已复制，可以粘贴到终端运行", "ok"),
          () => toast(command, "plain", 9000),
        );
      },
    });
    overlay.append(
      h(
        "div",
        { class: "hub-modal", role: "dialog", "aria-modal": "true", "aria-label": `安装 ${entry.name}` },
        h("div", { class: "hub-modal-head" }, avatar(entry), h("div", null, h("h3", { text: `安装 ${entry.name}` }), h("p", { class: "note", text: "Homing 会在这台电脑上替你运行下面这条官方安装命令。" }))),
        list,
        h(
          "div",
          { class: "note hub-modal-note" },
          runnable.length
            ? "安装过程的输出会显示在卡片上；需要登录的 Agent，装好后在终端运行一次完成登录。"
            : "这台电脑缺少 curl / Homebrew / npm，暂时没法一键安装。复制命令到终端，或先装好 Node.js。",
        ),
        h("div", { class: "hub-modal-foot" }, entry.homepage ? button("官网", { variant: "quiet", onClick: () => void openExternalUrl(entry.homepage || "") }) : null, h("div", { class: "spacer" }), copyBtn, runBtn),
      ),
    );
    document.body.append(overlay);
  }

  async function startSetup(entry: AgentCatalogEntry, index: number) {
    try {
      const result = await endpoints.setupCatalogAgent(entry.id, index);
      jobs.set(entry.id, result.job);
      paintMain();
      pollJobs();
    } catch (err) {
      reportError(err, "没能开始安装");
    }
  }

  function pollJobs() {
    if (jobTimer) return;
    jobTimer = window.setInterval(async () => {
      const running = [...jobs.values()].filter((job) => job.status === "running");
      if (running.length === 0) {
        if (jobTimer) window.clearInterval(jobTimer);
        jobTimer = null;
        return;
      }
      for (const job of running) {
        try {
          const fresh = (await endpoints.setupJob(job.id)).job;
          jobs.set(fresh.agent_id, fresh);
          if (fresh.status !== "running") {
            if (fresh.status === "ok") {
              toast(fresh.detected ? `${fresh.agent_name} 装好了，点「接入」就能用` : `${fresh.agent_name} 安装命令已完成`, "ok", 6000);
              heroPigeon.play("recv");
            } else {
              toast(`${fresh.agent_name} 安装没成功，看卡片上的输出`, "bad", 7000);
              heroPigeon.play("error");
            }
            await load();
          }
        } catch {
          /* 下次再试 */
        }
      }
      paintMain();
    }, 1200);
  }

  // ------------------------------------------------------------ Agent 区

  function avatar(entry: AgentCatalogEntry): HTMLElement {
    const logo = agentLogo(entry.id) || (entry.icon ? agentLogo(entry.icon) : null);
    if (logo) {
      const el = h("span", { class: `agent-avatar${logo.mono ? " mono" : ""}`, title: entry.name });
      if (logo.color) el.style.color = logo.color;
      el.append(icon(logo.svg, 26));
      return el;
    }
    let hash = 0;
    for (const char of entry.id) hash = (hash * 31 + char.charCodeAt(0)) % 360;
    const el = h("span", { class: "agent-avatar fallback", text: AGENT_INITIALS[entry.id] || entry.name.slice(0, 2).toUpperCase() });
    el.style.setProperty("--avatar-bg", `hsl(${hash} 42% 18%)`);
    el.style.setProperty("--avatar-fg", `hsl(${hash} 78% 74%)`);
    return el;
  }

  function capabilityTags(entry: AgentCatalogEntry): HTMLElement {
    const row = h("div", { class: "hub-tags" });
    if (entry.reply) row.append(tag("手机可回复", "ok"));
    else if (entry.sessions) row.append(tag("只读会话", "mute"));
    else row.append(tag("只收事件", "mute"));
    if (entry.drive?.enabled || entry.id === "codex" || entry.id === "claude") row.append(tag("手机可开新会话", "plain"));
    if (entry.connected && entry.hook_support === "native") {
      row.append(entry.hook_installed ? tag("钩子已装", "ok") : tag("钩子未装 · 通知会慢", "warn"));
    }
    const count = sessionCounts[entry.id];
    if (count) row.append(tag(`${count} 个会话`, "plain"));
    return row;
  }

  function jobView(job: SetupJob): HTMLElement {
    const status =
      job.status === "running" ? tag("正在安装…", "warn") : job.status === "ok" ? tag("安装完成", "ok") : tag("安装失败", "bad");
    const log = h("pre", { class: "hub-log", text: job.log.split("\n").slice(-8).join("\n") });
    return h("div", { class: "hub-job" }, h("div", { class: "hub-job-head" }, status, h("code", { text: job.command })), log);
  }

  function card(entry: AgentCatalogEntry, bucket: Bucket): HTMLElement {
    const job = jobs.get(entry.id);
    const actions = h("div", { class: "hub-card-actions" });
    const working = busy.has(entry.id);
    if (bucket === "connected") {
      if (entry.hook_support === "native" && entry.hook_install === "auto" && !entry.hook_installed) {
        actions.append(button("装钩子", { small: true, variant: "primary", disabled: working, onClick: () => void installHook(entry) }));
      }
      actions.append(button("详情", { small: true, variant: "quiet", onClick: () => ctx.navigate(`/console/agents?focus=${encodeURIComponent(entry.id)}`) }));
    } else if (bucket === "ready") {
      actions.append(
        button(working ? "接入中…" : "接入", { variant: "primary", iconSvg: icons.plug, disabled: working, onClick: () => void connect(entry) }),
      );
    } else if (bucket === "install") {
      const running = job?.status === "running";
      if (entry.can_setup) {
        actions.append(button(running ? "安装中…" : "一键安装", { variant: "primary", iconSvg: icons.download, disabled: running, onClick: () => openInstall(entry) }));
      } else if (entry.download_url) {
        actions.append(button("去官网下载", { variant: "primary", iconSvg: icons.download, onClick: () => void openExternalUrl(entry.download_url || "") }));
      } else {
        actions.append(button("查看安装命令", { onClick: () => openInstall(entry) }));
      }
    }
    const note =
      bucket === "connected"
        ? entry.tagline || "消息会推到手机"
        : bucket === "ready"
          ? "这台电脑已安装，接入后消息会推到手机"
          : entry.tagline || (entry.download_url ? "桌面应用，下载安装后回来点「接入」" : "装好后回来点「接入」");
    return h(
      "article",
      { class: `hub-card ${bucket}` },
      h(
        "div",
        { class: "hub-card-top" },
        avatar(entry),
        h(
          "div",
          { class: "hub-card-title" },
          h("b", { text: entry.name }),
          h("small", { text: [entry.vendor, entry.category].filter(Boolean).join(" · ") }),
        ),
        bucket === "connected" ? h("span", { class: "hub-live", title: "已接入" }, h("span", { class: "dot dot-ok" }), h("span", { text: "已接入" })) : null,
      ),
      h("p", { class: "hub-card-note", text: note }),
      bucket === "connected" || bucket === "ready" ? capabilityTags(entry) : null,
      job ? jobView(job) : null,
      actions,
    );
  }

  function section(title: string, hint: string, entries: AgentCatalogEntry[], bucket: Bucket, extra?: HTMLElement) {
    const wrap = h(
      "section",
      { class: "hub-section" },
      h("div", { class: "hub-section-head" }, h("h2", null, h("span", { text: title }), h("em", { text: String(entries.length) })), h("span", { class: "note", text: hint })),
    );
    const cards = h("div", { class: "hub-cards" });
    for (const entry of entries) cards.append(card(entry, bucket));
    wrap.append(cards);
    if (extra) wrap.append(extra);
    return wrap;
  }

  function paintMain() {
    clear(main);
    const groups: Record<Bucket, AgentCatalogEntry[]> = { connected: [], ready: [], install: [], more: [] };
    for (const entry of catalog) groups[bucketOf(entry)].push(entry);
    const featured = groups.install
      .filter((entry) => FEATURED_INSTALL.includes(entry.id) || entry.catalog_source === "homing" || !entry.catalog_source)
      .sort((a, b) => rank(a) - rank(b));
    const others = groups.install.filter((entry) => !featured.includes(entry));

    if (groups.ready.length > 0) {
      main.append(section("这台电脑上已安装", "点「接入」，之后它的消息会推到手机", groups.ready, "ready"));
    }
    if (groups.connected.length > 0) {
      main.append(section("已接入", "这些 Agent 的会话已经同步到手机", groups.connected, "connected"));
    } else if (groups.ready.length === 0) {
      main.append(
        h(
          "div",
          { class: "hub-empty list-card" },
          h("b", { text: "这台电脑上还没找到 Agent" }),
          h("p", { class: "note", text: "从下面挑一个一键安装；装好后 Homing 会自动发现它。" }),
        ),
      );
    }
    const moreToggle = h(
      "button",
      { class: "hub-more", type: "button" },
      h("span", { text: showMore ? "收起" : `还有 ${others.length} 个可以安装（来自 Magpie 目录）` }),
      icon(icons.chevronRight, 13),
    );
    moreToggle.addEventListener("click", () => {
      showMore = !showMore;
      paintMain();
    });
    const shown = showMore ? [...featured, ...others] : featured;
    if (shown.length > 0) {
      main.append(section("还没安装，可以一键装上", "命令来自各家官方文档，执行前会再给你确认", shown, "install", others.length ? moreToggle : undefined));
    }
    main.append(
      h(
        "div",
        { class: "hub-foot note" },
        h("span", { text: "找不到你的 Agent？" }),
        h("a", { href: "/console/agents", "data-link": "1", text: "全部 Agent" }),
        h("span", { text: "里有更多适配方式（通用钩子、自定义清单）。" }),
      ),
    );
    paintSteps();
  }

  function rank(entry: AgentCatalogEntry): number {
    const index = FEATURED_INSTALL.indexOf(entry.id);
    return index < 0 ? 100 : index;
  }

  // ------------------------------------------------------------ 手机区

  const pairCard = createPairCard({
    onWaiting: (waiting) => {
      if (waiting) heroPigeon.play("wait", true);
      else if (heroPigeon.scene === "wait") heroPigeon.idle();
    },
    onPaired: () => {
      justPaired = true;
      pairedCount += 1;
      heroPigeon.play("pair", false, true);
      paintSide();
      paintSteps();
    },
  });

  function startPairing() {
    pairing = true;
    paintSide();
    void pairCard.start();
  }

  function paintSide() {
    clear(side);
    const phoneDone = pairedCount > 0 || justPaired;
    const box = h(
      "div",
      { class: "hub-phone" },
      h(
        "div",
        { class: "hub-phone-head" },
        icon(icons.phone, 18),
        h("b", { text: phoneDone ? "手机已配对" : "装手机 App 并配对" }),
        phoneDone ? tag(`${Math.max(pairedCount, 1)} 台`, "ok") : null,
      ),
    );
    if (phoneDone) {
      box.append(
        h("p", { class: "note", text: "在手机上打开 Homing：收件箱看新动态，会话里直接回复和审批，「会议室」让几个 Agent 一起讨论。" }),
      );
      if (!pairing) box.append(button("再配一台手机", { iconSvg: icons.plus, onClick: startPairing }));
    }
    if (pairing || !phoneDone) box.append(pairCard.el);
    side.append(box);
  }

  function paint() {
    paintMain();
    paintSide();
  }

  paint();
  void load().then(() => {
    if (disposed) return;
    if (pairedCount === 0) startPairing();
  });
  const refresh = window.setInterval(() => {
    if (!document.hidden && jobs.size === 0) void load();
  }, 15000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      activeModalClose?.();
      heroPigeon.destroy();
      pairCard.dispose();
      if (jobTimer) window.clearInterval(jobTimer);
      window.clearInterval(refresh);
    },
  };
}
