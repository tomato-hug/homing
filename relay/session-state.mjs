// Homing relay · 会话治理状态（read / pinned / muted / archived）
// 存放在 relay/data/session-state.json；原子写（tmp + rename），坏了就重建，不影响事件流。
import fs from "node:fs";
import path from "node:path";

const EMPTY = { version: 1, sessions: {} };

export function createSessionState(filePath) {
  let cache = null;
  let cacheMtime = 0;

  function load() {
    try {
      const stat = fs.statSync(filePath);
      if (cache && stat.mtimeMs === cacheMtime) return cache;
      const raw = JSON.parse(fs.readFileSync(filePath, "utf8"));
      const data = raw && typeof raw === "object" && raw.sessions ? raw : EMPTY;
      cache = data;
      cacheMtime = stat.mtimeMs;
      return cache;
    } catch {
      cache = { ...EMPTY, sessions: {} };
      cacheMtime = 0;
      return cache;
    }
  }

  function save(next) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    const tmp = `${filePath}.tmp-${process.pid}`;
    fs.writeFileSync(tmp, JSON.stringify(next, null, 2));
    fs.renameSync(tmp, filePath);
    cache = next;
    try { cacheMtime = fs.statSync(filePath).mtimeMs; } catch { cacheMtime = 0; }
  }

  function keyOf(source, sessionId) {
    return `${source}:${sessionId}`;
  }

  function get(source, sessionId) {
    const rec = load().sessions[keyOf(source, sessionId)] || {};
    return {
      read: rec.read === true,
      pinned: rec.pinned === true,
      muted: rec.muted === true,
      archived: rec.archived === true,
    };
  }

  // patch: { read?, pinned?, muted?, archived? }，只写传入的键
  function patch(source, sessionId, patchIn) {
    const cur = load();
    const key = keyOf(source, sessionId);
    const prev = cur.sessions[key] || {};
    const nextRec = { ...prev };
    for (const field of ["read", "pinned", "muted", "archived"]) {
      if (Object.prototype.hasOwnProperty.call(patchIn, field)) {
        nextRec[field] = patchIn[field] === true;
      }
    }
    nextRec.updated_at = new Date().toISOString();
    const next = { ...cur, sessions: { ...cur.sessions, [key]: nextRec } };
    save(next);
    return get(source, sessionId);
  }

  function isMuted(source, sessionId) {
    return get(source, sessionId).muted;
  }

  function all() {
    return load().sessions;
  }

  return { get, patch, isMuted, all, keyOf };
}

// 临时会话：cwd 落在系统临时目录下
export function isTempCwd(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) return false;
  const p = cwd.replace(/\\/g, "/").replace(/\/+$/, "");
  return /^\/(private\/)?tmp(\/|$)/.test(p)
    || /^\/private\/var\/folders(\/|$)/.test(p)
    || /^\/var\/folders(\/|$)/.test(p)
    || /\/AppData\/Local\/Temp(\/|$)/i.test(p);
}
