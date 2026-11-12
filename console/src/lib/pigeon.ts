// 灰鸽 v2 场景播放器：读 pigeon_v2.json（与安卓 res/raw/pigeon_v2.json 同一份），
// 在 SVG 里逐帧插值。八个场景：recv 收信 · send 寄出 · tap 轻点 · wake 唤醒 ·
// wait 等你处理 · sleep 离线 · pair 配对成功 · error 出错。
//
// 坐标：舞台 240×130，鸟身是 v1 的 120×120 图标坐标，放在 translate(60,17)。
// 每个部件绕自己的 pivot 做 translate → rotate → scale；关键帧之间用三次贝塞尔缓动。

import spec from "./pigeon_v2.json";

export type PigeonScene = "recv" | "send" | "tap" | "wake" | "wait" | "sleep" | "pair" | "error";
export type NeckState = "normal" | "waiting" | "error" | "offline";

interface Key {
  t: number;
  tx?: number;
  ty?: number;
  rot?: number;
  sx?: number;
  sy?: number;
  opacity?: number;
  fill?: string;
}

interface Track {
  easing: number[];
  keys: Key[];
}

interface SceneSpec {
  loop_period: number;
  /** 一次性播放的时长（秒），之后回到静止；循环场景用 loop_period */
  play: number;
  visible_extras: string[];
  hidden: string[];
  tracks: Record<string, Track>;
}

const SCENES = spec.scenes as unknown as Record<PigeonScene, SceneSpec>;
const PIVOTS = spec.pivots as unknown as Record<string, [number, number]>;
const NECK = spec.neck_state as unknown as Record<NeckState, string>;

const SVG_NS = "http://www.w3.org/2000/svg";

// 鸟身各层（v1 路径）
const BODY = "M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z";
const HEAD = "M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z";
const NECK_PATH = "M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z";
const WING = "M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z";

/** 每个部件的 pivot。鸟身部件用 JSON 里的值，舞台装饰用自己的中心。 */
const PART_PIVOT: Record<string, [number, number]> = {
  "b-bird": PIVOTS.bird ?? [55, 60],
  "b-squash": PIVOTS.squash ?? [58, 97],
  "b-head": PIVOTS.head ?? [72, 46],
  "b-eye": PIVOTS.eye ?? [78, 34],
  "b-wing": PIVOTS.wing ?? [72, 68],
  "b-legs": PIVOTS.legs ?? [60, 86],
  "b-neck": [74, 50],
  "b-beakletter": [99, 43],
  "b-eyes": [78, 34],
  "b-eyeo": [78, 34],
  "x-letter": [156, 61],
  "x-badge": [202, 28],
  "x-dot": [176, 34],
  "x-bang": [168, 30],
  "x-z1": [150, 48],
  "x-z2": [160, 38],
  "x-wind": [44, 62],
};

/** 默认隐藏、只有场景点名时才出现的部件 */
const EXTRAS = new Set([
  "b-legs",
  "b-beakletter",
  "b-eyes",
  "x-ground",
  "x-letter",
  "x-badge",
  "x-check",
  "x-ring",
  "x-tile",
  "x-dot",
  "x-z1",
  "x-z2",
  "x-bang",
  "x-wind",
  "x-pc",
  "x-phone",
  "x-arc",
]);

function el<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}): SVGElementTagNameMap[K] {
  const node = document.createElementNS(SVG_NS, tag);
  for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, String(value));
  return node;
}

function bezier(x1: number, y1: number, x2: number, y2: number, x: number): number {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  // 牛顿迭代解 t，再算 y
  let t = x;
  for (let i = 0; i < 6; i++) {
    const cx = 3 * x1 * t * (1 - t) ** 2 + 3 * x2 * t * t * (1 - t) + t ** 3 - x;
    const dx = 3 * x1 * (1 - t) ** 2 + 6 * (x2 - x1) * t * (1 - t) + 3 * (1 - x2) * t * t;
    if (Math.abs(dx) < 1e-6) break;
    t -= cx / dx;
    t = Math.min(1, Math.max(0, t));
  }
  return 3 * y1 * t * (1 - t) ** 2 + 3 * y2 * t * t * (1 - t) + t ** 3;
}

function hexToRgb(hex: string): [number, number, number] {
  const value = hex.replace("#", "");
  return [parseInt(value.slice(0, 2), 16), parseInt(value.slice(2, 4), 16), parseInt(value.slice(4, 6), 16)];
}

function mixColor(a: string, b: string, f: number): string {
  const [r1, g1, b1] = hexToRgb(a);
  const [r2, g2, b2] = hexToRgb(b);
  const mix = (x: number, y: number) => Math.round(x + (y - x) * f);
  return `rgb(${mix(r1, r2)},${mix(g1, g2)},${mix(b1, b2)})`;
}

interface Frame {
  tx: number;
  ty: number;
  rot: number;
  sx: number;
  sy: number;
  opacity?: number;
  fill?: string;
}

function sample(track: Track, time: number): Frame {
  const keys = track.keys;
  const first = keys[0];
  const last = keys[keys.length - 1];
  const at = (key: Key): Frame => ({
    tx: key.tx ?? 0,
    ty: key.ty ?? 0,
    rot: key.rot ?? 0,
    sx: key.sx ?? 1,
    sy: key.sy ?? 1,
    opacity: key.opacity,
    fill: key.fill,
  });
  if (time <= first.t) return at(first);
  if (time >= last.t) return at(last);
  for (let i = 0; i < keys.length - 1; i++) {
    const a = keys[i];
    const b = keys[i + 1];
    if (time < a.t || time > b.t) continue;
    const span = Math.max(1e-4, b.t - a.t);
    const [x1, y1, x2, y2] = track.easing.length === 4 ? track.easing : [0, 0, 1, 1];
    const f = bezier(x1, y1, x2, y2, (time - a.t) / span);
    const lerp = (p?: number, q?: number, fallback = 0) => {
      const from = p ?? fallback;
      const to = q ?? fallback;
      return from + (to - from) * f;
    };
    return {
      tx: lerp(a.tx, b.tx),
      ty: lerp(a.ty, b.ty),
      rot: lerp(a.rot, b.rot),
      sx: lerp(a.sx, b.sx, 1),
      sy: lerp(a.sy, b.sy, 1),
      opacity: a.opacity === undefined && b.opacity === undefined ? undefined : lerp(a.opacity, b.opacity, 1),
      fill: a.fill && b.fill ? mixColor(a.fill, b.fill, f) : a.fill || b.fill,
    };
  }
  return at(last);
}

function transformOf(part: string, frame: Frame): string {
  const [px, py] = PART_PIVOT[part] ?? [0, 0];
  return `translate(${frame.tx.toFixed(2)} ${frame.ty.toFixed(2)}) translate(${px} ${py}) rotate(${frame.rot.toFixed(2)}) scale(${frame.sx.toFixed(3)} ${frame.sy.toFixed(3)}) translate(${-px} ${-py})`;
}

export interface PigeonOptions {
  size: number;
  /** bird：裁成图标大小（侧栏、按钮）；stage：整幅舞台（配对、空状态） */
  crop?: "bird" | "stage";
  /** 身体颜色，侧栏用浅灰 */
  bodyColor?: string;
  label?: string;
}

export interface PigeonPlayer {
  el: SVGSVGElement;
  /** 播一次（loop=false）或一直循环某个场景；hold=true 时播完停在最后一帧 */
  play: (scene: PigeonScene, loop?: boolean, hold?: boolean) => void;
  /** 回到静止姿态，可指定颈环颜色 */
  idle: (neck?: NeckState) => void;
  readonly scene: PigeonScene | null;
  destroy: () => void;
}

export function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    return false;
  }
}

export function createPigeon(options: PigeonOptions): PigeonPlayer {
  const crop = options.crop ?? "bird";
  const body = options.bodyColor ?? "#E2E8F0";
  const svg = el("svg", {
    viewBox: crop === "bird" ? "56 11 120 120" : "0 0 240 130",
    width: crop === "bird" ? options.size : Math.round((options.size * 240) / 130),
    height: options.size,
    role: "img",
    "aria-label": options.label ?? "Homing",
    class: "pigeon-v2",
    overflow: "visible",
  });
  const parts: Record<string, SVGGElement> = {};
  const group = (name: string, parent: Element, attrs: Record<string, string | number> = {}) => {
    const g = el("g", attrs);
    parts[name] = g;
    parent.append(g);
    return g;
  };

  // 背景装饰
  const tile = group("x-tile", svg);
  tile.append(el("rect", { x: 56, y: 11, width: 120, height: 120, rx: 27, fill: "#0F172A" }));
  const ground = group("x-ground", svg);
  ground.append(el("line", { x1: 30, y1: 114, x2: 210, y2: 114, stroke: "#334155", "stroke-width": 1.5, "stroke-linecap": "round" }));
  const arc = group("x-arc", svg);
  arc.append(el("path", { d: "M34 64 Q120 -24 208 58", fill: "none", stroke: "#475569", "stroke-width": 1.4, "stroke-dasharray": "3 4" }));
  const pc = group("x-pc", svg);
  pc.append(
    el("rect", { x: 12, y: 74, width: 44, height: 28, rx: 3, fill: "#111827", stroke: "#64748B", "stroke-width": 1.4 }),
    el("path", { d: "M6 106 H62", stroke: "#64748B", "stroke-width": 2, "stroke-linecap": "round" }),
  );
  const phone = group("x-phone", svg);
  phone.append(el("rect", { x: 196, y: 68, width: 24, height: 44, rx: 5, fill: "#111827", stroke: "#64748B", "stroke-width": 1.4 }));

  // 鸟
  const birdRoot = el("g", { transform: "translate(60 17)" });
  svg.append(birdRoot);
  const bird = group("b-bird", birdRoot);
  const squash = group("b-squash", bird);
  const legs = group("b-legs", squash);
  legs.append(
    el("path", { d: "M56 87 L54 99 M66 87 L68 99", stroke: "#F59E0B", "stroke-width": 2.4, "stroke-linecap": "round", fill: "none" }),
  );
  const bodyPath = el("path", { d: BODY, fill: body });
  squash.append(bodyPath);
  const head = group("b-head", squash);
  const headPath = el("path", { d: HEAD, fill: body });
  head.append(headPath);
  const eye = group("b-eye", head);
  const eyeOpen = group("b-eyeo", eye);
  eyeOpen.append(el("circle", { cx: 78, cy: 34, r: 2.4, fill: "#0F172A" }));
  const eyeClosed = group("b-eyes", eye);
  eyeClosed.append(el("path", { d: "M75.2 33.6 Q78 36.4 80.8 33.6", fill: "none", stroke: "#0F172A", "stroke-width": 1.6, "stroke-linecap": "round" }));
  const beakLetter = group("b-beakletter", head);
  beakLetter.append(
    el("rect", { x: 92, y: 38, width: 14, height: 10, rx: 1.5, fill: "#F8FAFC", stroke: "#94A3B8", "stroke-width": 0.8 }),
    el("path", { d: "M92.5 38.5 L99 44 L105.5 38.5", fill: "none", stroke: "#94A3B8", "stroke-width": 0.8 }),
  );
  const neck = group("b-neck", squash);
  const neckPath = el("path", { d: NECK_PATH, fill: NECK.normal });
  neck.append(neckPath);
  const wing = group("b-wing", squash);
  wing.append(el("path", { d: WING, fill: "#94A3B8" }));

  // 前景装饰
  const letter = group("x-letter", svg);
  letter.append(
    el("rect", { x: 148, y: 56, width: 16, height: 11, rx: 1.6, fill: "#F8FAFC", stroke: "#94A3B8", "stroke-width": 0.9 }),
    el("path", { d: "M148.5 56.5 L156 62.5 L163.5 56.5", fill: "none", stroke: "#94A3B8", "stroke-width": 0.9 }),
  );
  const badge = group("x-badge", svg);
  badge.append(el("circle", { cx: 202, cy: 28, r: 7, fill: "#2DD4BF" }), el("circle", { cx: 202, cy: 28, r: 2.4, fill: "#042F2E" }));
  const check = group("x-check", svg);
  const checkCircle = el("circle", { cx: 190, cy: 40, r: 9, fill: "#2DD4BF" });
  const checkMark = el("path", { d: "M185.5 40 l3 3 l5.5 -6", fill: "none", stroke: "#042F2E", "stroke-width": 2.2, "stroke-linecap": "round", "stroke-linejoin": "round" });
  check.append(checkCircle, checkMark);
  const ring = group("x-ring", svg);
  const ringCircle = el("circle", { cx: 115, cy: 77, r: 46, fill: "none", stroke: "#2DD4BF", "stroke-width": 2 });
  ring.append(ringCircle);
  const dot = group("x-dot", svg);
  dot.append(el("circle", { cx: 176, cy: 34, r: 5.5, fill: "#FBBF24" }));
  const z1 = group("x-z1", svg);
  const z1Text = el("text", { x: 146, y: 52, "font-size": 11, "font-weight": 700, fill: "#94A3B8", "font-family": "system-ui, sans-serif" });
  z1Text.textContent = "z";
  z1.append(z1Text);
  const z2 = group("x-z2", svg);
  const z2Text = el("text", { x: 155, y: 43, "font-size": 15, "font-weight": 700, fill: "#94A3B8", "font-family": "system-ui, sans-serif" });
  z2Text.textContent = "Z";
  z2.append(z2Text);
  const bang = group("x-bang", svg);
  const bangText = el("text", { x: 163, y: 40, "font-size": 24, "font-weight": 800, fill: "#F87171", "font-family": "system-ui, sans-serif" });
  bangText.textContent = "!";
  bang.append(bangText);
  const wind = group("x-wind", svg);
  wind.append(el("path", { d: "M30 52 h26 M22 64 h34 M32 76 h20", stroke: "#94A3B8", "stroke-width": 2, "stroke-linecap": "round", fill: "none" }));

  let current: PigeonScene | null = null;
  let loop = false;
  let hold = false;
  let start = 0;
  let raf = 0;
  let idleNeck: NeckState = "normal";
  const reduced = prefersReducedMotion();

  function layout(scene: PigeonScene | null) {
    // 圆环与对勾在不同场景里的位置不同：配对时落在手机上
    if (scene === "pair") {
      ringCircle.setAttribute("cx", "208");
      ringCircle.setAttribute("cy", "60");
      ringCircle.setAttribute("r", "24");
      checkCircle.setAttribute("cx", "222");
      checkCircle.setAttribute("cy", "44");
      checkMark.setAttribute("d", "M217.5 44 l3 3 l5.5 -6");
      PART_PIVOT["x-ring"] = [208, 60];
      PART_PIVOT["x-check"] = [222, 44];
    } else {
      ringCircle.setAttribute("cx", "115");
      ringCircle.setAttribute("cy", "77");
      ringCircle.setAttribute("r", "46");
      checkCircle.setAttribute("cx", "190");
      checkCircle.setAttribute("cy", "40");
      checkMark.setAttribute("d", "M185.5 40 l3 3 l5.5 -6");
      PART_PIVOT["x-ring"] = [115, 77];
      PART_PIVOT["x-check"] = [190, 40];
    }
  }

  function render(scene: PigeonScene | null, time: number) {
    const sceneSpec = scene ? SCENES[scene] : null;
    for (const [name, node] of Object.entries(parts)) {
      const track = sceneSpec?.tracks[name];
      const visibleByDefault = !EXTRAS.has(name) || Boolean(sceneSpec?.visible_extras.includes(name));
      const hidden = Boolean(sceneSpec?.hidden.includes(name));
      let opacity = hidden ? 0 : visibleByDefault ? 1 : 0;
      if (track) {
        const frame = sample(track, time);
        node.setAttribute("transform", transformOf(name, frame));
        if (frame.opacity !== undefined) opacity = hidden ? 0 : frame.opacity;
        if (name === "b-neck" && frame.fill) neckPath.setAttribute("fill", frame.fill);
      } else {
        node.removeAttribute("transform");
        if (name === "b-neck") neckPath.setAttribute("fill", NECK[scene === "sleep" ? "offline" : idleNeck]);
      }
      node.setAttribute("opacity", opacity.toFixed(3));
    }
    if (!sceneSpec) neckPath.setAttribute("fill", NECK[idleNeck]);
  }

  function frame(now: number) {
    if (!current) return;
    const sceneSpec = SCENES[current];
    const elapsed = (now - start) / 1000;
    const period = sceneSpec.loop_period;
    if (!loop && elapsed >= sceneSpec.play) {
      raf = 0;
      if (hold) {
        render(current, sceneSpec.play);
        return;
      }
      // 一次性场景播完：回到静止姿态
      current = null;
      render(null, 0);
      return;
    }
    render(current, loop ? elapsed % period : elapsed);
    raf = window.requestAnimationFrame(frame);
  }

  function stop() {
    if (raf) window.cancelAnimationFrame(raf);
    raf = 0;
  }

  const player: PigeonPlayer = {
    el: svg,
    get scene() {
      return current;
    },
    play(scene, shouldLoop = false, shouldHold = false) {
      if (current === scene && loop && shouldLoop) return;
      stop();
      layout(scene);
      current = scene;
      loop = shouldLoop;
      hold = shouldHold;
      if (reduced) {
        // 减少动态效果：只停在场景的定格，循环场景保留颈环颜色
        render(scene, shouldLoop ? 0 : SCENES[scene].play);
        if (!shouldLoop && !shouldHold) {
          window.setTimeout(() => {
            if (current === scene && !loop) {
              current = null;
              render(null, 0);
            }
          }, 900);
        }
        return;
      }
      start = performance.now();
      raf = window.requestAnimationFrame(frame);
    },
    idle(neckState = "normal") {
      stop();
      current = null;
      loop = false;
      idleNeck = neckState;
      render(null, 0);
    },
    destroy() {
      stop();
      current = null;
    },
  };
  render(null, 0);
  return player;
}

/** 场景的中文名，给无障碍标签和调试面板用 */
export const PIGEON_SCENE_LABELS: Record<PigeonScene, string> = {
  recv: "收信",
  send: "寄出",
  tap: "轻点",
  wake: "唤醒",
  wait: "等你处理",
  sleep: "离线",
  pair: "配对成功",
  error: "出错",
};
