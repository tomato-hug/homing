// 使用 › Agent：已接入的卡片（可拖动排序、可展开调设置）+ 检测到但没接入的列表

import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { AGENT_LABELS, AGENT_INITIALS } from "../lib/format";
import {
  endpoints,
  poll,
  type AdaptersPayload,
  type Agent,
  type AgentSettings,
  type SessionSummary,
} from "../lib/api";
import { button, emptyState, groupHead, listCard, reportError, segmented, tag, toast, toggle } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

const SIGNAL_OPTIONS = [
  { value: "hook" as const, label: "通知钩子", title: "agent 主动调用本机接口，最及时" },
  { value: "watch" as const, label: "监听文件", title: "盯住会话目录的变化，比钩子慢几秒" },
  { value: "poll" as const, label: "轮询", title: "定时扫一遍会话文件，最省事但最慢" },
];

const REPLY_OPTIONS = [
  { value: "auto" as const, label: "直接回复", title: "你点了回复就发过去" },
  { value: "queue" as const, label: "排队确认", title: "先攒着，等你在手机上确认" },
  { value: "off" as const, label: "不回复", title: "只读，不往回写" },
];

const SIGNAL_TEXT: Record<string, string> = { hook: "钩子推送", watch: "监听文件", poll: "轮询读取" };

export function renderAgents(ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  let agents: Agent[] = [];
  let settings: Record<string, AgentSettings> = {};
  let order: string[] = [];
  let adapters: AdaptersPayload | null = null;
  let sessionCounts: Record<string, number> = {};
  const expanded = new Set<string>();
  let dragId: string | null = null;

  // ------------------------------------------------------------ 数据

  async function load(silent = false) {
    try {
      const [agentsPayload, settingsPayload, adaptersPayload, sessionsPayload] = await Promise.all([
        endpoints.agents(),
        endpoints.agentSettings(),
        endpoints.adapters().catch(() => null),
        endpoints.sessions(500).catch(() => ({ sessions: [] as SessionSummary[], count: 0 })),
      ]);
      if (disposed) return;
      agents = agentsPayload.agents;
      settings = settingsPayload.settings;
      order = settingsPayload.order;
      adapters = adaptersPayload;
      sessionCounts = {};
      for (const session of sessionsPayload.sessions) {
        sessionCounts[session.source] = (sessionCounts[session.source] || 0) + 1;
      }
      // 后端没存过设置的 agent，用注册表里的默认值兜底
      for (const agent of agents) {
        if (!settings[agent.source]) {
          settings[agent.source] = {
            id: agent.source,
            connected: Boolean(agent.installed),
            push_to_phone: Boolean(agent.installed),
            read_sessions: true,
            push_signal: (agent.push_signal === "http_events" ? "watch" : (agent.push_signal as AgentSettings["push_signal"])) || "poll",
            reply_mode: "auto",
            model: agent.default_model || null,
            order: null,
            updated_at: null,
          };
        }
      }
      paint();
      if (!silent) ctx.refreshShell();
    } catch (err) {
      if (disposed) return;
      clear(root);
      root.append(
        h(
          "div",
          { class: "list-card" },
          h("div", { class: "empty" }, h("div", { text: "读取 agent 失败" }), h("div", { class: "note", text: (err as Error).message })),
        ),
      );
    }
  }

  function configFileFor(id: string): string | null {
    return adapters?.builtin.find((item) => item.id === id)?.config_file || null;
  }

  function sortedAgents(): Agent[] {
    const byId = new Map(agents.map((agent) => [agent.source, agent]));
    const result: Agent[] = [];
    for (const id of order) {
      const agent = byId.get(id);
      if (agent) {
        result.push(agent);
        byId.delete(id);
      }
    }
    for (const agent of byId.values()) result.push(agent);
    return result;
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

  async function disconnect(id: string) {
    try {
      const result = await endpoints.disconnectAgent(id);
      settings[id] = result.settings;
      toast("已断开，配置文件没动", "ok", 6000);
      expanded.delete(id);
      await load(true);
    } catch (err) {
      reportError(err, "断开失败");
    }
  }

  async function testPush(id: string) {
    try {
      const result = await endpoints.testPush(id);
      toast(result.message || "已发一条测试", result.ok ? "ok" : "bad", 6000);
    } catch (err) {
      reportError(err, "试发失败");
    }
  }

  async function connect(id: string) {
    try {
      const next = await endpoints.patchAgent(id, { connected: true, push_to_phone: true });
      settings[id] = next;
      expanded.add(id);
      paint();
      toast(`${AGENT_LABELS[id] || id} 已接入`, "ok");
      ctx.refreshShell();
    } catch (err) {
      reportError(err, "接入失败");
    }
  }

  async function persistOrder(nextOrder: string[]) {
    order = nextOrder;
    paint();
    try {
      await endpoints.saveAgentOrder(nextOrder);
    } catch (err) {
      reportError(err, "保存顺序失败");
      await load(true);
    }
  }

  // ------------------------------------------------------------ 卡片

  function agentCard(agent: Agent): HTMLElement {
    const id = agent.source;
    const s = settings[id];
    const open = expanded.has(id);
    const connected = Boolean(s?.connected) && agent.installed !== false;
    const count = sessionCounts[id] || 0;
    const replyable = agent.capabilities?.reply !== false && agent.capabilities?.reply !== undefined;

    const card = h("div", { class: "agent-card", dataset: { agent: id } });

    const handle = h("span", { class: "drag", title: "拖动排序", draggable: "true" }, icon(icons.grip, 14));
    handle.addEventListener("dragstart", (event) => {
      dragId = id;
      card.classList.add("dragging");
      const dragEvent = event as DragEvent;
      dragEvent.dataTransfer?.setData("text/plain", id);
      if (dragEvent.dataTransfer) dragEvent.dataTransfer.effectAllowed = "move";
    });
    handle.addEventListener("dragend", () => {
      dragId = null;
      card.classList.remove("dragging");
      for (const el of Array.from(root.querySelectorAll(".agent-card"))) el.classList.remove("drop-target");
    });

    card.addEventListener("dragover", (event) => {
      if (!dragId || dragId === id) return;
      event.preventDefault();
      card.classList.add("drop-target");
    });
    card.addEventListener("dragleave", () => card.classList.remove("drop-target"));
    card.addEventListener("drop", (event) => {
      event.preventDefault();
      card.classList.remove("drop-target");
      if (!dragId || dragId === id) return;
      const ids = sortedAgents().map((item) => item.source);
      const from = ids.indexOf(dragId);
      const to = ids.indexOf(id);
      if (from < 0 || to < 0) return;
      ids.splice(to, 0, ...ids.splice(from, 1));
      dragId = null;
      void persistOrder(ids);
    });

    card.append(
      h(
        "div",
        { class: "agent-card-head" },
        handle,
        h("span", { class: "agent-square", text: AGENT_INITIALS[id] || id.slice(0, 1).toUpperCase() }),
        h(
          "div",
          { class: "agent-id" },
          h(
            "div",
            { class: "agent-name" },
            h("span", { text: agent.display_name || AGENT_LABELS[id] || id }),
            agent.version ? h("span", { class: "ver", text: `v${agent.version}` }) : null,
          ),
          h(
            "div",
            { class: "agent-meta" },
            h("span", { text: connected ? "已接入" : "未接入" }),
            h("span", { class: "sep", text: "·" }),
            h("span", { text: SIGNAL_TEXT[s?.push_signal || "poll"] || "轮询读取" }),
            h("span", { class: "sep", text: "·" }),
            h("span", { text: `${count} 个会话` }),
            h("span", { class: "sep", text: "·" }),
            h("span", { text: replyable ? "可回复" : "只读" }),
            agent.installed === false ? tag("没装这个 agent", "mute") : null,
          ),
        ),
        h(
          "div",
          { style: "display:flex;align-items:center;gap:10px;flex:none" },
          h("span", { class: "note", text: "推送到手机" }),
          toggle({
            on: Boolean(s?.push_to_phone),
            title: s?.push_to_phone ? "关掉就不再往手机推" : "打开后这个 agent 的事会推到手机",
            onChange: (next) => void patch(id, { push_to_phone: next }),
          }),
          button(open ? "收起" : "详情", {
            variant: "quiet",
            small: true,
            onClick: () => {
              if (open) expanded.delete(id);
              else expanded.add(id);
              paint();
            },
          }),
        ),
      ),
    );

    if (open) {
      const body = h("div", { class: "agent-card-body" });

      body.append(
        h(
          "div",
          { class: "field-row" },
          h(
            "div",
            { class: "field-label" },
            h("div", { text: "允许 Homing 读取这个 agent 的会话" }),
            h("div", { class: "hint", text: "关掉之后只保留已经收到的事件，不再读新的会话文件" }),
          ),
          toggle({ on: s?.read_sessions !== false, onChange: (next) => void patch(id, { read_sessions: next }) }),
        ),
      );

      body.append(
        h(
          "div",
          { class: "field-row", style: "align-items:flex-start" },
          h(
            "div",
            { class: "field-label" },
            h("div", { text: "推送方式" }),
            h("div", { class: "hint", text: agent.installed === false ? "这个 agent 本机没装，装上之后才能改" : "钩子最及时；轮询不用改 agent 配置" }),
          ),
          segmented(
            SIGNAL_OPTIONS.map((option) => ({ ...option, disabled: agent.installed === false })),
            (s?.push_signal || "poll") as "hook" | "watch" | "poll",
            (next) => void patch(id, { push_signal: next }),
          ),
        ),
      );

      body.append(
        h(
          "div",
          { class: "field-row", style: "align-items:flex-start" },
          h(
            "div",
            { class: "field-label" },
            h("div", { text: "回复方式" }),
            h("div", { class: "hint", text: replyable ? "「直接回复」会把你的话立刻发过去" : "这个 agent 目前只能读，回复不了" }),
          ),
          h(
            "div",
            { style: "display:flex;gap:8px;align-items:center;flex-wrap:wrap;justify-content:flex-end" },
            segmented(REPLY_OPTIONS, (s?.reply_mode || "auto") as "auto" | "queue" | "off", (next) => void patch(id, { reply_mode: next })),
            button("试发一条到测试会话", { small: true, iconSvg: icons.bolt, onClick: () => void testPush(id) }),
          ),
        ),
      );

      const models = agent.models || [];
      if (models.length > 0) {
        const select = h("select", { class: "select" });
        for (const model of models) {
          const option = h("option", { value: model.id, text: model.label || model.id });
          if ((s?.model || agent.default_model) === model.id) option.selected = true;
          select.append(option);
        }
        select.addEventListener("change", () => void patch(id, { model: (select as HTMLSelectElement).value }));
        body.append(
          h(
            "div",
            { class: "field-row", style: "align-items:flex-start" },
            h("div", { class: "field-label" }, h("div", { text: "回复时用的模型" }), h("div", { class: "hint", text: "只影响从 Homing 发出去的回复" })),
            h("div", { style: "width:220px;flex:none" }, select),
          ),
        );
      }

      card.append(body);
    }

    const file = configFileFor(id);
    card.append(
      h(
        "div",
        { class: "agent-card-foot" },
        h("span", { text: file ? "改动了" : "不写配置文件" }),
        h("span", { class: "mono", text: file || "只用轮询读会话，不碰任何文件" }),
        h("span", { class: "spacer" }),
        connected
          ? button("断开并还原", { small: true, variant: "quiet", onClick: () => void disconnect(id) })
          : button("接入", { small: true, variant: "primary", onClick: () => void connect(id) }),
      ),
    );

    return card;
  }

  // ------------------------------------------------------------ 渲染

  function paint() {
    clear(root);
    const all = sortedAgents();
    const connected = all.filter((agent) => settings[agent.source]?.connected && agent.installed !== false);
    const detected = all.filter((agent) => !(settings[agent.source]?.connected && agent.installed !== false));

    const connectedHost = h("div");
    if (connected.length === 0) {
      connectedHost.append(h("div", { class: "list-card" }, emptyState("还没有接入任何 agent", "从下面「在这台电脑上检测到」里挑一个接入")));
    } else {
      for (const agent of connected) connectedHost.append(agentCard(agent));
    }
    root.append(h("div", { class: "group" }, groupHead("已接入", connected.length), connectedHost));

    const detectedHost = h("div");
    if (detected.length === 0) {
      detectedHost.append(h("div", { class: "note", text: "这台电脑上没有检测到还没接入的 agent。" }));
    } else {
      detectedHost.append(
        listCard(
          ...detected.map((agent) =>
            h(
              "div",
              { class: "lrow", style: "align-items:center" },
              h("span", { class: "agent-square", text: AGENT_INITIALS[agent.source] || agent.source.slice(0, 1).toUpperCase() }),
              h(
                "div",
                { class: "lrow-main" },
                h(
                  "div",
                  { class: "lrow-title" },
                  h("span", { class: "txt", text: agent.display_name || AGENT_LABELS[agent.source] || agent.source }),
                  agent.version ? h("span", { class: "note", text: `v${agent.version}` }) : null,
                ),
                h("div", { class: "lrow-sub", text: `${agent.installed === false ? "本机没装" : "已检测到"} · ${SIGNAL_TEXT[agent.push_signal || "poll"] || "轮询读取"}` }),
              ),
              h(
                "div",
                { class: "lrow-actions" },
                button("接入", {
                  variant: "primary",
                  small: true,
                  disabled: agent.installed === false,
                  onClick: () => void connect(agent.source),
                }),
              ),
            ),
          ),
        ),
      );
    }
    root.append(h("div", { class: "group" }, groupHead("在这台电脑上检测到，未接入", detected.length), detectedHost));

    const communityCount = adapters?.community.length || 0;
    root.append(
      h(
        "div",
        { style: "display:flex;align-items:center;gap:10px;padding:2px 2px 14px;font-size:12px;color:var(--muted)" },
        h("span", { text: `社区适配器里还有 ${communityCount} 个（Cursor、Zed、Aider…）` }),
        (() => {
          const link = h("button", { class: "group-head link", type: "button", text: "去看看", style: "padding:0" });
          link.addEventListener("click", () => ctx.navigate("/console/adapters/discover"));
          return link;
        })(),
      ),
    );

    const dashed = h("button", { class: "dashed", type: "button" });
    dashed.append(
      icon(icons.plus, 16),
      h(
        "div",
        null,
        h("div", { class: "t", text: "添加自定义 agent" }),
        h("div", { class: "note", text: "写一份接入清单，或者用脚本适配器接一个没被内置支持的 agent" }),
      ),
    );
    dashed.addEventListener("click", () => ctx.navigate("/console/adapters/custom"));
    root.append(dashed);
  }

  clear(root);
  root.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取 agent…" })));
  void load();
  const stop = poll(() => load(true), 12_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
