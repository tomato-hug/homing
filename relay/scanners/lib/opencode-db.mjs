// Homing relay · OpenCode 系数据库读取（OpenCode 与 ZCode 共用同一套表）
// 只读：DatabaseSync(path, { readOnly: true })。不写、不 VACUUM、不加锁。
import { DatabaseSync } from "node:sqlite";

function open(dbPath) {
  return new DatabaseSync(dbPath, { readOnly: true, timeout: 2000 });
}

function safe(dbPath, fn, fallback) {
  let db;
  try {
    db = open(dbPath);
    return fn(db);
  } catch {
    return fallback;
  } finally {
    try { db?.close(); } catch { /* 忽略 */ }
  }
}

export function probe(dbPath) {
  return safe(dbPath, (db) => {
    const t = db.prepare("select name from sqlite_master where type='table' and name='session_v2'").get();
    if (!t) return { ok: false, sessions: 0 };
    const n = db.prepare("select count(*) c from session_v2").get();
    return { ok: true, sessions: Number(n?.c || 0) };
  }, { ok: false, sessions: 0 });
}

// 会话列表：session_v2 一行一个会话
export function listSessions(dbPath, limit = 200) {
  return safe(dbPath, (db) => {
    const rows = db.prepare(
      `select id, title, directory, model, parent_id, time_created, time_updated, time_archived
         from session_v2
        order by time_updated desc
        limit ?`,
    ).all(limit);
    return rows.map((r) => ({
      session_id: r.id,
      title: typeof r.title === "string" ? r.title : "",
      cwd: typeof r.directory === "string" ? r.directory : null,
      model: typeof r.model === "string" ? r.model : null,
      parent_id: r.parent_id || null,
      archived_native: Number(r.time_archived || 0) > 0,
      last_ts: r.time_updated ? new Date(Number(r.time_updated)).toISOString() : null,
    }));
  }, []);
}

export function oneSession(dbPath, sessionId) {
  return safe(dbPath, (db) => {
    const r = db.prepare(
      `select id, title, directory, model, parent_id, time_created, time_updated, time_archived,
              tokens_input, tokens_output, cost
         from session_v2 where id = ? limit 1`,
    ).get(sessionId);
    if (!r) return null;
    return {
      session_id: r.id,
      title: typeof r.title === "string" ? r.title : "",
      cwd: typeof r.directory === "string" ? r.directory : null,
      model: typeof r.model === "string" ? r.model : null,
      parent_id: r.parent_id || null,
      archived_native: Number(r.time_archived || 0) > 0,
      last_ts: r.time_updated ? new Date(Number(r.time_updated)).toISOString() : null,
    };
  }, null);
}

// 条目：session_message 一行一条（user.text / assistant.content）
export function readItems(dbPath, sessionId, limit = 100) {
  return safe(dbPath, (db) => {
    const rows = db.prepare(
      `select id, type, seq, time_created, data
         from session_message
        where session_id = ?
        order by seq desc
        limit ?`,
    ).all(sessionId, limit);
    return rows.reverse().map((r) => {
      let data = {};
      try { data = JSON.parse(r.data); } catch { /* 忽略坏行 */ }
      const role = r.type === "user" ? "user" : r.type === "assistant" ? "assistant" : r.type;
      let text = "";
      if (role === "user") {
        text = typeof data.text === "string" ? data.text : "";
      } else if (role === "assistant") {
        const c = data.content;
        if (typeof c === "string") text = c;
        else if (Array.isArray(c)) {
          text = c.map((b) => (typeof b === "string" ? b : b?.text || "")).join("");
        }
      }
      return {
        id: r.id,
        role,
        kind: r.type,
        seq: Number(r.seq || 0),
        at: r.time_created ? new Date(Number(r.time_created)).toISOString() : null,
        text: text.slice(0, 4000),
        error: data?.error?.message || null,
      };
    });
  }, []);
}

// 是否有任何 user / assistant 条目（用于 is_empty 判定）
export function hasConversation(dbPath, sessionId) {
  return safe(dbPath, (db) => {
    const r = db.prepare(
      "select count(*) c from session_message where session_id = ? and type in ('user','assistant')",
    ).get(sessionId);
    return Number(r?.c || 0) > 0;
  }, false);
}
