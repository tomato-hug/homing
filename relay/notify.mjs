// Homing relay · 通知分级调度
// P0 授权 / 出错  → 立即单条
// P1 等你回复     → 同一会话 120s 内合并成一条
// P2 完成         → 不单推，合并成「N 个会话已完成」摘要
// muted 会话一律不推。
import { isTempCwd } from "./session-state.mjs";
import { isPaused } from "./platform/pause.mjs";

const P0_KINDS = new Set(["permission", "error", "approval"]);
const P1_KINDS = new Set(["needs_input", "question", "awaiting_input"]);
const P2_KINDS = new Set(["reply", "done", "stop", "turn_complete", "completed"]);

const P1_WINDOW_MS = 120_000;   // 同一会话的 P1 合并窗口
const P2_SUMMARY_MS = 60_000;   // P2 摘要的聚合窗口

export function levelOf(kind) {
  if (P0_KINDS.has(kind)) return "P0";
  if (P1_KINDS.has(kind)) return "P1";
  if (P2_KINDS.has(kind)) return "P2";
  return null;
}

export function createNotifier({ sessionState, sink, log = () => {} }) {
  // sessionKey -> { payload, timer }
  const p1 = new Map();
  // sessionKey -> { title, at, source, session_id }
  const p2 = new Map();
  let p2Timer = null;

  function keyOf(event) {
    return `${event.source}:${event.session_id}`;
  }

  function muted(event) {
    try {
      return sessionState ? sessionState.isMuted(event.source, event.session_id) : false;
    } catch {
      return false;
    }
  }

  function base(event, level) {
    const title = event.title || event.source || "Agent";
    return {
      type: "notify",
      level,
      source: event.source,
      session_id: event.session_id,
      event_id: event.id,
      title,
      summary: String(event.text || "").replace(/\s+/g, " ").trim().slice(0, 160),
      at: event.ts || new Date().toISOString(),
      temp: isTempCwd(event.cwd),
    };
  }

  function emit(payload) {
    try { sink(payload); } catch (err) { log(`notify sink 失败: ${err.message}`); }
  }

  function flushP1(key) {
    const entry = p1.get(key);
    if (!entry) return;
    clearTimeout(entry.timer);
    p1.delete(key);
    emit({ ...entry.payload, merged: entry.count > 1, count: entry.count });
  }

  function flushP2() {
    if (!p2.size) return;
    const items = [...p2.values()];
    p2.clear();
    if (p2Timer) { clearTimeout(p2Timer); p2Timer = null; }
    if (items.length === 1) {
      emit({ ...items[0].payload, level: "P2", merged: false, count: 1 });
      return;
    }
    const names = items.slice(0, 3).map((i) => i.payload.session_id.slice(0, 8));
    emit({
      type: "notify",
      level: "P2",
      merged: true,
      count: items.length,
      title: `${items.length} 个会话已完成`,
      summary: `${names.join(" · ")}${items.length > 3 ? " …" : ""}`,
      at: new Date().toISOString(),
      sessions: items.map((i) => ({
        source: i.payload.source,
        session_id: i.payload.session_id,
        title: i.payload.title,
      })),
    });
  }

  function onEvent(event) {
    if (!event || !event.kind || !event.session_id) return;
    const level = levelOf(event.kind);
    if (!level) return;
    // 托盘/控制台的「暂停推送」：事件照常落库进收件箱，只是不出通知
    try {
      if (isPaused()) {
        log(`notify: 推送已暂停，跳过 ${keyOf(event)}`);
        return;
      }
    } catch {
      /* 暂停状态读不出来就按未暂停处理 */
    }
    if (muted(event)) {
      log(`notify: ${keyOf(event)} muted，跳过`);
      return;
    }
    const key = keyOf(event);
    const payload = base(event, level);

    if (level === "P0") {
      // 立即；同一会话已有的 P1 合并被这条取代
      const pending = p1.get(key);
      if (pending) { clearTimeout(pending.timer); p1.delete(key); }
      emit({ ...payload, merged: false, count: 1 });
      return;
    }

    if (level === "P1") {
      const prev = p1.get(key);
      if (prev) clearTimeout(prev.timer);
      const entry = { payload, count: (prev?.count || 0) + 1, timer: null };
      entry.timer = setTimeout(() => flushP1(key), P1_WINDOW_MS);
      if (entry.timer.unref) entry.timer.unref();
      p1.set(key, entry);
      return;
    }

    // P2：只留每个会话最新的一条，等摘要窗口
    p2.set(key, { payload });
    if (!p2Timer) {
      p2Timer = setTimeout(flushP2, P2_SUMMARY_MS);
      if (p2Timer.unref) p2Timer.unref();
    }
  }

  function flushAll() {
    for (const key of [...p1.keys()]) flushP1(key);
    flushP2();
  }

  return { onEvent, flushAll, flushP2 };
}
