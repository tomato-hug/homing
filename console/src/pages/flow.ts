// 使用 › 实时流：手机、Homing 与各家 Agent 之间正在传递什么。
//
// 参考 Magpie 的路由视图（agents → magpie → providers，每个决定实时可见）：
// 左边是手机与电脑控制台，中间是 Homing，右边是已接入的 Agent，下方是会议室。
// 每一跳（hook 事件、回复送达、@ 协作派活、结果回传、会议发言、推送）都画成
// 一个沿连线移动的小包，下方的轨迹表可以点进对应会话。

import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { AGENT_LABELS, timeShort } from "../lib/format";
import { agentLogo } from "../lib/agent-icons";
import { createPigeon } from "../lib/pigeon";
import { endpoints, type FlowHop, type FlowNodeInfo } from "../lib/api";
import { button, segmented, tag } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

type Filter = "all" | "message" | "collab" | "room";

const KIND_COLOR: Record<string, string> = {
  event: "#2DD4BF",
  output: "#5EEAD4",
  notify: "#A78BFA",
  reply: "#60A5FA",
  action: "#60A5FA",
  dispatch: "#FBBF24",
  result: "#4ADE80",
  room: "#F472B6",
  turn: "#94A3B8",
};

const KIND_LABEL: Record<string, string> = {
  event: "事件",
  output: "输出",
  notify: "推送",
  reply: "回复",
  action: "操作",
  dispatch: "协作",
  result: "结果",
  room: "会议",
  turn: "状态",
};

const FILTER_KINDS: Record<Filter, string[] | null> = {
  all: null,
  message: ["event", "output", "notify", "reply", "action"],
  collab: ["dispatch", "result"],
  room: ["room"],
};

const SVG_NS = "http://www.w3.org/2000/svg";
const W = 1000;
const H = 470;

interface NodePos {
  id: string;
  x: number;
  y: number;
  label: string;
  detail: string;
  kind: string;
}

function svg<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}): SVGElementTagNameMap[K] {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [key, value] of Object.entries(attrs)) el.setAttribute(key, String(value));
  return el;
}

function agentName(id: string): string {
  return AGENT_LABELS[id] || id;
}

function nodeLabel(id: string, rooms: Map<string, string>): string {
  if (id === "phone") return "手机";
  if (id === "console") return "电脑控制台";
  if (id === "homing") return "Homing";
  if (id.startsWith("agent:")) return agentName(id.slice(6));
  if (id.startsWith("room:")) return rooms.get(id) || "会议室";
  return id;
}

export function renderFlow(ctx: PageContext): PageResult {
  const root = h("div", { class: "flow-page" });
  let disposed = false;
  let paused = false;
  let filter: Filter = "all";
  let seq = 0;
  let hops: FlowHop[] = [];
  let baseNodes: FlowNodeInfo[] = [];
  let connectedAgents: string[] = [];
  const roomTitles = new Map<string, string>();
  const counts = new Map<string, { inbound: number; outbound: number }>();
  let positions = new Map<string, NodePos>();
  let source: EventSource | null = null;
  let pollTimer: number | null = null;

  // ------------------------------------------------------------ 结构

  const live = h("span", { class: "flow-live" }, h("span", { class: "dot dot-ok" }), h("span", { text: "实时" }));
  const pauseBtn = button("暂停", {
    small: true,
    variant: "quiet",
    iconSvg: icons.pause,
    onClick: () => {
      paused = !paused;
      pauseBtn.querySelector("span")!.textContent = paused ? "继续" : "暂停";
      live.classList.toggle("paused", paused);
      (live.lastElementChild as HTMLElement).textContent = paused ? "已暂停" : "实时";
    },
  });
  const filterHost = h("div");
  const head = h(
    "div",
    { class: "flow-head" },
    h(
      "div",
      null,
      h("h1", { text: "实时流" }),
      h("p", { class: "note", text: "手机、Homing 与各家 Agent 之间正在传递的消息、协作和会议发言。" }),
    ),
    h("div", { class: "spacer" }),
    live,
    filterHost,
    pauseBtn,
  );
  const stage = svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "flow-stage", role: "img", "aria-label": "Agent 交互与数据流" });
  const edgesLayer = svg("g", { class: "flow-edges" });
  const packetsLayer = svg("g", { class: "flow-packets" });
  const nodesLayer = svg("g", { class: "flow-nodes" });
  stage.append(edgesLayer, nodesLayer, packetsLayer);
  const homingArt = createPigeon({ size: 46, crop: "bird", label: "Homing" });
  const legend = h("div", { class: "flow-legend" });
  for (const kind of ["event", "notify", "reply", "dispatch", "result", "room"]) {
    legend.append(h("span", null, h("i", { style: `background:${KIND_COLOR[kind]}` }), h("span", { text: KIND_LABEL[kind] })));
  }
  const stageWrap = h("div", { class: "flow-stage-wrap" }, stage, legend);
  const trace = h("div", { class: "flow-trace" });
  const traceHead = h("div", { class: "flow-trace-head" }, h("b", { text: "轨迹" }), h("span", { class: "note", text: "最近 400 跳，只保存在内存里" }));
  root.append(head, stageWrap, h("div", { class: "list-card flow-trace-card" }, traceHead, trace));

  function paintFilter() {
    clear(filterHost);
    filterHost.append(
      segmented<Filter>(
        [
          { value: "all", label: "全部" },
          { value: "message", label: "消息" },
          { value: "collab", label: "协作" },
          { value: "room", label: "会议室" },
        ],
        filter,
        (next) => {
          filter = next;
          paintFilter();
          paintTrace();
        },
      ),
    );
  }

  // ------------------------------------------------------------ 布局

  function visible(hop: FlowHop): boolean {
    const kinds = FILTER_KINDS[filter];
    return !kinds || kinds.includes(hop.kind);
  }

  function nodeIds(): string[] {
    const agents = new Set<string>(connectedAgents.map((id) => `agent:${id}`));
    const rooms = new Set<string>();
    for (const hop of hops) {
      for (const id of [hop.from, hop.to]) {
        if (id.startsWith("agent:") && id !== "agent:unknown") agents.add(id);
        if (id.startsWith("room:")) rooms.add(id);
      }
    }
    return ["phone", "console", "homing", ...[...agents].slice(0, 9), ...[...rooms].slice(-3)];
  }

  function layout() {
    const ids = nodeIds();
    const agents = ids.filter((id) => id.startsWith("agent:"));
    const rooms = ids.filter((id) => id.startsWith("room:"));
    const next = new Map<string, NodePos>();
    const info = new Map(baseNodes.map((node) => [node.id, node]));
    const put = (id: string, x: number, y: number, kind: string) =>
      next.set(id, { id, x, y, kind, label: info.get(id)?.label || nodeLabel(id, roomTitles), detail: info.get(id)?.detail || "" });
    put("phone", 120, 150, "phone");
    put("console", 120, 320, "console");
    put("homing", 470, 230, "homing");
    const top = 50;
    const bottom = rooms.length ? 330 : 420;
    agents.forEach((id, index) => {
      const y = agents.length === 1 ? 230 : top + ((bottom - top) * index) / Math.max(1, agents.length - 1);
      put(id, 840, y, "agent");
    });
    rooms.forEach((id, index) => put(id, 470 + (index - (rooms.length - 1) / 2) * 190, 415, "room"));
    const changed = ids.length !== positions.size || ids.some((id) => !positions.has(id));
    positions = next;
    return changed;
  }

  function pathBetween(from: NodePos, to: NodePos): string {
    if (from.id === to.id) {
      return `M${from.x - 30} ${from.y - 26} C${from.x - 20} ${from.y - 70} ${from.x + 20} ${from.y - 70} ${from.x + 30} ${from.y - 26}`;
    }
    if (from.kind === "agent" && to.kind === "agent") {
      // Agent 之间的协作：从右侧绕一个弧
      const bulge = 110 + Math.abs(from.y - to.y) * 0.25;
      return `M${from.x + 34} ${from.y} C${from.x + bulge} ${from.y} ${to.x + bulge} ${to.y} ${to.x + 34} ${to.y}`;
    }
    const dx = (to.x - from.x) * 0.45;
    return `M${from.x} ${from.y} C${from.x + dx} ${from.y} ${to.x - dx} ${to.y} ${to.x} ${to.y}`;
  }

  function edgeKey(a: string, b: string) {
    return [a, b].sort().join("|");
  }

  function paintStage() {
    clear(edgesLayer);
    clear(nodesLayer);
    const edges = new Set<string>();
    const pairs: Array<[string, string]> = [
      ["phone", "homing"],
      ["console", "homing"],
    ];
    for (const id of positions.keys()) if (id.startsWith("agent:") || id.startsWith("room:")) pairs.push([id, "homing"]);
    for (const hop of hops) if (positions.has(hop.from) && positions.has(hop.to)) pairs.push([hop.from, hop.to]);
    for (const [a, b] of pairs) {
      const key = edgeKey(a, b);
      if (edges.has(key) || a === b) continue;
      edges.add(key);
      const from = positions.get(a);
      const to = positions.get(b);
      if (!from || !to) continue;
      edgesLayer.append(svg("path", { d: pathBetween(from, to), class: "flow-edge", "data-edge": key }));
    }
    for (const node of positions.values()) nodesLayer.append(nodeEl(node));
  }

  function nodeEl(node: NodePos): SVGGElement {
    const g = svg("g", { class: `flow-node ${node.kind}`, transform: `translate(${node.x} ${node.y})`, "data-node": node.id });
    const wide = node.kind === "homing" ? 74 : 62;
    g.append(svg("rect", { x: -wide, y: -30, width: wide * 2, height: 60, rx: 14, class: "flow-node-box" }));
    const art = svg("g", { transform: `translate(${-wide + 12} -18)` });
    if (node.kind === "homing") {
      const fo = svg("foreignObject", { x: -wide + 6, y: -24, width: 48, height: 48 });
      fo.append(homingArt.el);
      g.append(fo);
    } else {
      const glyph =
        node.kind === "phone" ? icons.phone : node.kind === "console" ? icons.overview : node.kind === "room" ? icons.chat : null;
      const logo = node.kind === "agent" ? agentLogo(node.id.slice(6)) : null;
      const markup = logo?.svg || glyph;
      if (markup) {
        const holder = svg("g", { class: "flow-node-icon" });
        const parsed = new DOMParser().parseFromString(markup, "image/svg+xml").documentElement as unknown as SVGSVGElement;
        parsed.setAttribute("width", "28");
        parsed.setAttribute("height", "28");
        parsed.setAttribute("x", "4");
        parsed.setAttribute("y", "4");
        if (logo?.color) parsed.setAttribute("color", logo.color);
        holder.append(document.importNode(parsed, true));
        art.append(holder);
      }
      g.append(art);
    }
    const text = svg("text", { x: -wide + 56, y: -3, class: "flow-node-label" });
    text.textContent = truncate(node.label, node.kind === "homing" ? 10 : 9);
    const stat = counts.get(node.id);
    const sub = svg("text", { x: -wide + 56, y: 15, class: "flow-node-sub" });
    sub.textContent = node.kind === "homing" ? "本机服务" : stat ? `↑${stat.outbound} ↓${stat.inbound}` : node.detail ? truncate(node.detail, 9) : "待命";
    g.append(text, sub);
    return g;
  }

  function truncate(value: string, max: number) {
    return value.length > max ? `${value.slice(0, max - 1)}…` : value;
  }

  // ------------------------------------------------------------ 动画

  function animate(hop: FlowHop) {
    if (paused || document.hidden || !visible(hop)) return;
    const from = positions.get(hop.from);
    const to = positions.get(hop.to);
    if (!from || !to) return;
    const d = pathBetween(from, to);
    const path = svg("path", { d, fill: "none", stroke: "none" });
    packetsLayer.append(path);
    const color = KIND_COLOR[hop.kind] || "#94A3B8";
    const dot = svg("circle", { r: hop.bytes && hop.bytes > 2000 ? 7 : 5, fill: color, class: "flow-packet" });
    const halo = svg("circle", { r: 11, fill: color, opacity: 0.18 });
    packetsLayer.append(halo, dot);
    const edge = edgesLayer.querySelector<SVGPathElement>(`[data-edge="${CSS.escape(edgeKey(hop.from, hop.to))}"]`);
    edge?.classList.add("hot");
    edge?.style.setProperty("--hot", color);
    const length = path.getTotalLength();
    const duration = hop.from === hop.to ? 700 : 950;
    const startAt = performance.now();
    const step = (now: number) => {
      const t = Math.min(1, (now - startAt) / duration);
      const eased = t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
      const point = path.getPointAtLength(length * eased);
      dot.setAttribute("cx", String(point.x));
      dot.setAttribute("cy", String(point.y));
      halo.setAttribute("cx", String(point.x));
      halo.setAttribute("cy", String(point.y));
      if (t < 1) {
        window.requestAnimationFrame(step);
        return;
      }
      path.remove();
      dot.remove();
      halo.remove();
      window.setTimeout(() => edge?.classList.remove("hot"), 400);
      pulse(hop.to, color);
    };
    window.requestAnimationFrame(step);
    if (hop.to === "homing" && hop.from.startsWith("agent:")) homingArt.play("recv");
    else if (hop.from === "homing" && hop.to === "phone") homingArt.play("send");
    else if (hop.status === "fail") homingArt.play("error");
  }

  function pulse(id: string, color: string) {
    const node = nodesLayer.querySelector<SVGGElement>(`[data-node="${CSS.escape(id)}"]`);
    const box = node?.querySelector<SVGRectElement>(".flow-node-box");
    if (!box) return;
    box.style.setProperty("--pulse", color);
    box.classList.remove("pulse");
    void box.getBoundingClientRect();
    box.classList.add("pulse");
  }

  // ------------------------------------------------------------ 轨迹

  function chip(id: string) {
    const kind = id.split(":")[0];
    return h("span", { class: `flow-chip ${kind}`, text: nodeLabel(id, roomTitles) });
  }

  function paintTrace() {
    clear(trace);
    const rows = hops.filter(visible).slice(-120).reverse();
    if (rows.length === 0) {
      trace.append(
        h(
          "div",
          { class: "flow-empty" },
          h("b", { text: "还没有数据经过" }),
          h("span", { class: "note", text: "在手机上回复一条消息、让 Agent 跑一轮，或者在会话里 @ 另一个 Agent，这里就会动起来。" }),
        ),
      );
      return;
    }
    for (const hop of rows) {
      const statusTone = hop.status === "fail" ? "bad" : hop.status === "wait" ? "warn" : "ok";
      const row = h(
        "div",
        { class: `flow-row ${hop.session_id ? "link" : ""}`.trim() },
        h("span", { class: "flow-time mono", text: timeShort(hop.at) }),
        h("span", { class: "flow-kind", style: `--kind:${KIND_COLOR[hop.kind] || "#94A3B8"}`, text: KIND_LABEL[hop.kind] || hop.kind }),
        h("span", { class: "flow-route" }, chip(hop.from), icon(icons.arrowRight, 12), chip(hop.to)),
        h("span", { class: "flow-title", text: hop.title }),
        h("span", { class: "flow-preview", text: hop.preview || "" }),
        hop.via && hop.via !== "local" ? tag(hop.via === "cloud" ? "中转" : "局域网", "mute") : h("span"),
        h("span", { class: `dot dot-${statusTone}` }),
      );
      if (hop.session_id) {
        row.title = "打开这个会话";
        row.addEventListener("click", () => ctx.navigate(`/console/sessions?session=${encodeURIComponent(hop.session_id!)}`));
      }
      trace.append(row);
    }
  }

  // ------------------------------------------------------------ 数据

  function ingest(list: FlowHop[], animateNew: boolean) {
    if (list.length === 0) return;
    for (const hop of list) {
      if (hop.seq <= seq) continue;
      seq = hop.seq;
      hops.push(hop);
      const from = counts.get(hop.from) || { inbound: 0, outbound: 0 };
      from.outbound += 1;
      counts.set(hop.from, from);
      const to = counts.get(hop.to) || { inbound: 0, outbound: 0 };
      to.inbound += 1;
      counts.set(hop.to, to);
      if (hop.kind === "room" && hop.title.includes("·")) {
        const roomNode = hop.to.startsWith("room:") ? hop.to : hop.from;
        roomTitles.set(roomNode, hop.title.split("·").pop()!.trim());
      }
    }
    if (hops.length > 400) hops = hops.slice(-400);
    if (layout()) paintStage();
    else {
      // 只更新计数
      for (const node of positions.values()) {
        const el = nodesLayer.querySelector<SVGGElement>(`[data-node="${CSS.escape(node.id)}"]`);
        const sub = el?.querySelector<SVGTextElement>(".flow-node-sub");
        const stat = counts.get(node.id);
        if (sub && stat && node.kind !== "homing") sub.textContent = `↑${stat.outbound} ↓${stat.inbound}`;
      }
    }
    if (animateNew) {
      list.slice(-6).forEach((hop, index) => window.setTimeout(() => animate(hop), index * 160));
    }
    paintTrace();
  }

  async function initial() {
    try {
      const [payload, catalog] = await Promise.all([endpoints.flow(0), endpoints.agentCatalog().catch(() => null)]);
      if (disposed) return;
      baseNodes = payload.nodes;
      connectedAgents = (catalog?.agents || []).filter((entry) => entry.connected).map((entry) => entry.id);
      layout();
      paintStage();
      ingest(payload.hops, false);
      paintTrace();
      connect();
    } catch (err) {
      clear(trace);
      trace.append(h("div", { class: "flow-empty" }, h("b", { text: "读不到实时流" }), h("span", { class: "note", text: (err as Error).message })));
    }
  }

  function connect() {
    if (disposed) return;
    try {
      source = new EventSource("/api/flow/stream");
      source.addEventListener("hop", (event) => {
        try {
          ingest([JSON.parse((event as MessageEvent).data) as FlowHop], true);
        } catch {
          /* 忽略坏帧 */
        }
      });
      source.onerror = () => {
        source?.close();
        source = null;
        live.classList.add("paused");
        startPolling();
      };
      source.onopen = () => live.classList.remove("paused");
    } catch {
      startPolling();
    }
  }

  function startPolling() {
    if (pollTimer || disposed) return;
    pollTimer = window.setInterval(async () => {
      try {
        const payload = await endpoints.flow(seq);
        ingest(payload.hops, true);
        if (!source) {
          window.clearInterval(pollTimer!);
          pollTimer = null;
          connect();
        }
      } catch {
        /* 下次再试 */
      }
    }, 3000);
  }

  paintFilter();
  layout();
  paintStage();
  paintTrace();
  homingArt.play("wake");
  void initial();

  return {
    el: root,
    dispose: () => {
      disposed = true;
      source?.close();
      if (pollTimer) window.clearInterval(pollTimer);
      homingArt.destroy();
    },
  };
}
