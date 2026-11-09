// Homing relay · 适配器路由
// 对外只暴露 registerAdapterRoutes(app)；app 的形态由接线方决定：
//   - { addRoute(method, path, handler) }        → 逐条注册
//   - 数组                                        → push { method, path, handler }
//   - 其它对象                                    → 挂到 app.__homingRoutes
// handler 收到 { req, url, body, token, fromLoopback } → 返回 { status, body }。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { loadManifests, validateManifest, manifestDir } from "./manifest.mjs";
import { loadScriptAdapters, adapterStatuses, adaptersDir } from "./scripted.mjs";
import { createIngest } from "./ingest.mjs";
import { parseYaml } from "./yaml.mjs";

const HOME = os.homedir();

export function createAdapterRuntime({ dataDir, emit, log = () => {} }) {
  const ingest = createIngest({ dataDir, emit, log });
  const builtin = [];      // 由 index.mjs 塞入内置 scanner 的元信息（只读用途）
  let cache = { manifests: null, scripts: null, at: 0 };

  function collect() {
    const m = loadManifests();
    return { manifests: m, scripts: null };
  }

  async function loaded({ scripts = true } = {}) {
    if (cache.manifests && cache.scripts && Date.now() - cache.at < 15_000) return cache;
    const m = loadManifests();
    cache = {
      manifests: m,
      scripts: scripts ? await loadScriptAdapters() : { scanners: [], errors: [], dir: adaptersDir() },
      at: Date.now(),
    };
    return cache;
  }

  // ---- GET /api/adapters -------------------------------------------------
  async function listAdapters() {
    const { manifests, scripts } = await loaded();
    const items = [];
    for (const b of builtin) {
      items.push({
        kind: "builtin", id: b.id, name: b.name, icon: b.icon,
        push_signal: b.pushSignal || "hook",
        status: "builtin", error: null,
      });
    }
    for (const sc of manifests.scanners) {
      items.push({
        kind: "manifest", id: sc.id, name: sc.name, icon: sc.icon,
        push_signal: sc.pushSignal,
        status: "ok",
        error: null,
        file: sc.manifest.source_file,
      });
    }
    for (const e of manifests.errors) {
      items.push({ kind: "manifest", id: e.id, name: e.id, icon: "generic", push_signal: "poll", status: "error", error: e.errors.join("; "), file: e.file });
    }
    for (const sc of scripts.scanners) {
      items.push({
        kind: "script", id: sc.id, name: sc.name, icon: sc.icon,
        push_signal: sc.pushSignal,
        status: "ok", error: null, file: sc.file,
      });
    }
    for (const e of scripts.errors) {
      items.push({ kind: "script", id: e.id, name: e.id, icon: "generic", push_signal: "poll", status: "error", error: e.errors.join("; "), file: e.file });
    }
    return {
      items,
      dirs: { manifests: manifestDir(), scripts: adaptersDir() },
      counts: {
        builtin: builtin.length,
        manifest: manifests.scanners.length,
        script: scripts.scanners.length,
        errors: manifests.errors.length + scripts.errors.length,
      },
      ingest: ingest.list(),
      generated_at: new Date().toISOString(),
    };
  }

  // ---- POST /api/adapters（保存清单，先校验）-----------------------------
  function saveManifest(body) {
    const raw = body?.yaml;
    if (typeof raw !== "string" || !raw.trim()) return { status: 400, body: { error: "yaml 不能为空" } };
    let parsed;
    try {
      parsed = parseYaml(raw);
    } catch (err) {
      return { status: 400, body: { error: `YAML 解析失败: ${err.message}` } };
    }
    const v = validateManifest(parsed);
    if (!v.ok) return { status: 400, body: { error: "清单校验未通过", errors: v.errors } };

    const dir = manifestDir();
    fs.mkdirSync(dir, { recursive: true });
    const file = path.join(dir, `${v.manifest.id}.yaml`);
    const backup = fs.existsSync(file) ? `${file}.bak-${Date.now()}` : null;
    if (backup) fs.copyFileSync(file, backup);   // 改前备份
    fs.writeFileSync(file, raw, { mode: 0o600 });
    cache = { manifests: null, scripts: null, at: 0 };
    return { status: 200, body: { ok: true, id: v.manifest.id, file, backup } };
  }

  // ---- DELETE /api/adapters/<id> -----------------------------------------
  function deleteAdapter(id) {
    const dirs = [manifestDir(), adaptersDir()];
    const removed = [];
    for (const dir of dirs) {
      for (const name of [`${id}.yaml`, `${id}.yml`, `${id}.mjs`]) {
        const file = path.join(dir, name);
        if (!fs.existsSync(file)) continue;
        const trash = `${file}.deleted-${Date.now()}`;
        fs.renameSync(file, trash);   // 不真删，改名留痕
        removed.push({ file, trash });
      }
    }
    cache = { manifests: null, scripts: null, at: 0 };
    return removed.length
      ? { status: 200, body: { ok: true, removed } }
      : { status: 404, body: { error: `没有 id 为 ${id} 的适配器` } };
  }

  // ---- POST /api/adapters/test（试读）------------------------------------
  async function testAdapter(body) {
    const raw = body?.yaml;
    if (typeof raw !== "string" || !raw.trim()) return { status: 400, body: { error: "yaml 不能为空" } };
    let parsed;
    try { parsed = parseYaml(raw); } catch (err) {
      return { status: 400, body: { error: `YAML 解析失败: ${err.message}` } };
    }
    const v = validateManifest(parsed);
    if (!v.ok) return { status: 400, body: { error: "清单校验未通过", errors: v.errors } };

    // 写成临时文件再走与正式加载相同的路径，避免"测试和实跑是两套逻辑"
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "homing-adapter-test-"));
    const tmpFile = path.join(tmpDir, `${v.manifest.id}.yaml`);
    fs.writeFileSync(tmpFile, raw);
    try {
      const { scanners, errors } = loadManifests(tmpDir);
      if (errors.length) return { status: 400, body: { error: "加载失败", errors: errors[0].errors } };
      const sc = scanners[0];
      const detect = await sc.detect();
      const sessions = await sc.listSessions({ limit: 5 });
      const models = await sc.models().catch(() => ({ models: [] }));
      return {
        status: 200,
        body: {
          ok: true,
          id: sc.id,
          detect,
          session_count: sessions.length,
          sessions: sessions.map((s) => ({
            session_id: s.session_id,
            title: (s.title || "").slice(0, 80),
            title_source: s.title_source,
            last_ts: s.last_ts,
            cwd: s.cwd || null,
          })),
          models: models.models.length,
          reply: Boolean(v.manifest.reply),
        },
      };
    } finally {
      try { fs.rmSync(tmpDir, { recursive: true, force: true }); } catch { /* 忽略 */ }
    }
  }

  // ---- POST /api/ingest ---------------------------------------------------
  async function ingestEvent(body, { token, fromLoopback }) {
    const r = await ingest.ingest(body, { token, fromLoopback });
    return { status: r.status, body: r.error ? { error: r.error } : { ok: true, event_id: r.event?.id ?? null } };
  }

  // ---- 令牌 ---------------------------------------------------------------
  function issueToken(body) {
    const agent = String(body?.agent || "").trim();
    if (!agent) return { status: 400, body: { error: "agent 不能为空" } };
    const reply = body?.reply_url
      ? { url: String(body.reply_url), timeout_ms: Number(body.reply_timeout_ms || 10_000) }
      : null;
    try {
      const r = ingest.issue(agent, { reply });
      return { status: 200, body: { ok: true, agent: r.agent, token: r.token } };
    } catch (err) {
      return { status: 400, body: { error: err.message } };
    }
  }

  function revokeToken(body) {
    const key = String(body?.token || body?.agent || "");
    if (!key) return { status: 400, body: { error: "需要 token 或 agent" } };
    return ingest.revoke(key)
      ? { status: 200, body: { ok: true } }
      : { status: 404, body: { error: "没有匹配的令牌" } };
  }

  return {
    listAdapters, saveManifest, deleteAdapter, testAdapter,
    ingestEvent, issueToken, revokeToken,
    ingest,
    setBuiltin(list) { builtin.length = 0; builtin.push(...list); },
    invalidate() { cache = { manifests: null, scripts: null, at: 0 }; },
  };
}

// ---- 对外入口 -------------------------------------------------------------
// app：接线方给的注册器；runtime：由 index.mjs 创建并传入（含 emit / dataDir）
export function registerAdapterRoutes(app, runtime) {
  const routes = [
    { method: "GET", path: "/api/adapters", handler: () => runtime.listAdapters() },
    { method: "POST", path: "/api/adapters", handler: ({ body }) => runtime.saveManifest(body) },
    { method: "POST", path: "/api/adapters/test", handler: ({ body }) => runtime.testAdapter(body) },
    { method: "DELETE", path: "/api/adapters/:id", handler: ({ id }) => runtime.deleteAdapter(id) },
    { method: "POST", path: "/api/ingest", handler: ({ body, token, fromLoopback }) => runtime.ingestEvent(body, { token, fromLoopback }) },
    { method: "POST", path: "/api/ingest/tokens", handler: ({ body }) => runtime.issueToken(body) },
    { method: "DELETE", path: "/api/ingest/tokens", handler: ({ body }) => runtime.revokeToken(body) },
  ];

  if (app && typeof app.addRoute === "function") {
    for (const r of routes) app.addRoute(r.method, r.path, r.handler);
  } else if (Array.isArray(app)) {
    app.push(...routes);
  } else if (app && typeof app === "object") {
    app.__homingRoutes = (app.__homingRoutes || []).concat(routes);
  }
  return routes;
}

// 供接线方直接用的分发器：命中返回 {status, body}，未命中返回 null
export function dispatchAdapterRoute(runtime, { method, pathname, body, token, fromLoopback }) {
  const m = String(method || "GET").toUpperCase();
  if (m === "GET" && pathname === "/api/adapters") return runtime.listAdapters();
  if (m === "POST" && pathname === "/api/adapters") return runtime.saveManifest(body);
  if (m === "POST" && pathname === "/api/adapters/test") return runtime.testAdapter(body);
  if (m === "POST" && pathname === "/api/ingest") return runtime.ingestEvent(body, { token, fromLoopback });
  if (m === "POST" && pathname === "/api/ingest/tokens") return runtime.issueToken(body);
  if (m === "DELETE" && pathname === "/api/ingest/tokens") return runtime.revokeToken(body);
  const del = /^\/api\/adapters\/([^/]+)$/.exec(pathname);
  if (m === "DELETE" && del) return runtime.deleteAdapter(decodeURIComponent(del[1]));
  return null;
}
