// Homing relay · 自定义 agent 的事件上报（A 层）
// 每个 agent 一个令牌，存 relay/data/ingest-tokens.json，可吊销。
// 只监听回环来源（由 server 的 isLoopback 决定，这里再校验一次）。
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

export const INGEST_KINDS = new Set(["needs_reply", "approval", "done", "error", "message"]);

export function createIngest({ dataDir, emit, log = () => {} }) {
  const file = path.join(dataDir, "ingest-tokens.json");

  function load() {
    try {
      const j = JSON.parse(fs.readFileSync(file, "utf8"));
      return Array.isArray(j?.agents) ? j : { version: 1, agents: [] };
    } catch {
      return { version: 1, agents: [] };
    }
  }

  function save(next) {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    const tmp = `${file}.tmp-${process.pid}`;
    fs.writeFileSync(tmp, JSON.stringify(next, null, 2), { mode: 0o600 });
    fs.renameSync(tmp, file);
  }

  // 发一个令牌；agent 名字相同的会覆盖（视为重新登记）
  function issue(agent, { reply = null } = {}) {
    const id = String(agent || "").trim();
    if (!id) throw new Error("agent 不能为空");
    const token = `hmg_${crypto.randomBytes(24).toString("base64url")}`;
    const list = load();
    const rest = list.agents.filter((a) => a.agent !== id);
    rest.push({
      agent: id,
      token,
      created_at: new Date().toISOString(),
      revoked_at: null,
      reply: reply || null,
      last_seen_at: null,
    });
    save({ version: 1, agents: rest });
    return { agent: id, token };
  }

  function revoke(tokenOrAgent) {
    const list = load();
    let hit = null;
    for (const a of list.agents) {
      if (a.token === tokenOrAgent || a.agent === tokenOrAgent) {
        a.revoked_at = a.revoked_at || new Date().toISOString();
        hit = a;
      }
    }
    if (!hit) return false;
    save(list);
    return true;
  }

  function list() {
    return load().agents.map((a) => ({
      agent: a.agent,
      created_at: a.created_at,
      revoked_at: a.revoked_at,
      last_seen_at: a.last_seen_at,
      reply: a.reply ? (a.reply.url ? "url" : a.reply.command ? "command" : "none") : "none",
      token_tail: typeof a.token === "string" ? a.token.slice(-6) : null,
    }));
  }

  function identify(token) {
    if (!token) return null;
    const list = load();
    const hit = list.agents.find((a) => a.token === token && !a.revoked_at);
    return hit || null;
  }

  function touch(agent) {
    const list = load();
    const hit = list.agents.find((a) => a.agent === agent);
    if (!hit) return;
    hit.last_seen_at = new Date().toISOString();
    try { save(list); } catch { /* 记录失败不影响上报 */ }
  }

  // 处理一条上报：校验 → 转 event → 交给 server 的 addEvent
  async function ingest(body, { token, fromLoopback }) {
    if (!fromLoopback) return { status: 403, error: "只接受 127.0.0.1 来源" };
    const rec = identify(token);
    if (!rec) return { status: 401, error: "令牌无效或已吊销" };

    const agent = String(body?.agent || rec.agent);
    if (agent !== rec.agent) return { status: 403, error: "令牌与 agent 不匹配" };
    const sessionId = String(body?.session_id || "").trim();
    if (!sessionId) return { status: 400, error: "缺少 session_id" };
    const kind = String(body?.kind || "message");
    if (!INGEST_KINDS.has(kind)) {
      return { status: 400, error: `kind 必须是 ${[...INGEST_KINDS].join(" | ")}` };
    }

    const event = emit({
      source: agent,
      kind,
      session_id: sessionId,
      cwd: body?.cwd ? String(body.cwd) : null,
      title: body?.title ? String(body.title) : null,
      text: body?.text ? String(body.text) : null,
    });
    touch(agent);
    log(`ingest ${agent}/${sessionId.slice(0, 8)} ${kind}`);
    return { status: 200, event: { id: event?.id ?? null } };
  }

  // 回复：走 agent 登记的 callback URL
  async function reply(agent, sessionId, text) {
    const list = load();
    const rec = list.agents.find((a) => a.agent === agent && !a.revoked_at);
    if (!rec?.reply) return { ok: false, error: "该 agent 未登记回复通道" };
    if (rec.reply.url) {
      try {
        const res = await fetch(rec.reply.url, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...(rec.reply.header ? rec.reply.header : {}),
          },
          body: JSON.stringify({ agent, session_id: sessionId, text }),
          signal: AbortSignal.timeout(Number(rec.reply.timeout_ms || 10_000)),
        });
        if (!res.ok) {
          const detail = await res.text().catch(() => "");
          return { ok: false, error: `回调返回 ${res.status}${detail ? `: ${detail.slice(0, 200)}` : ""}` };
        }
        return { ok: true, via: "callback" };
      } catch (err) {
        return { ok: false, error: `回调失败: ${err.message}` };
      }
    }
    return { ok: false, error: "未登记回调 URL（命令式回复请走适配清单的 reply.command）" };
  }

  return { issue, revoke, list, identify, ingest, reply, file };
}
