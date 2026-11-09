// Homing relay · 脚本适配器（C 层）
// ~/.homing/adapters/*.mjs 导出与内置 scanner 相同的接口，但在**独立子进程**里跑。
// 用 Node 的权限模型只放行它声明的读目录：node --permission --allow-fs-read=<dir>…
//
// 说明（诚实边界）：Node 22 的 --permission 管的是文件系统、子进程、worker，
// **不含网络**。"默认不给网络"目前靠约定：runner 不注入 fetch 便利层，环境变量
// HOMING_ADAPTER_NO_NET=1 由脚本自己遵守。等 Node 的权限模型覆盖网络再强制。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const RUNNER = path.join(HERE, "script-runner.mjs");
const HOME = os.homedir();

export function adaptersDir() {
  if (process.platform === "win32") {
    const appdata = process.env.APPDATA || path.join(HOME, "AppData", "Roaming");
    return path.join(appdata, "Homing", "adapters");
  }
  return path.join(HOME, ".homing", "adapters");
}

function expand(p) {
  if (typeof p !== "string") return p;
  if (p === "~") return HOME;
  if (p.startsWith("~/")) return `${HOME}${p.slice(1)}`;
  return p;
}

const state = new Map();   // file -> { id, name, ok, error, meta, at }

function invoke(file, op, args, { timeoutMs = 15_000 } = {}) {
  return new Promise((resolve) => {
    const payload = JSON.stringify({ op, args: args || {} });
    let out = "";
    let errOut = "";
    let child;
    try {
      child = spawn(process.execPath, ["--permission", `--allow-fs-read=${file}`, RUNNER, file, payload], {
        stdio: ["ignore", "pipe", "pipe"],
        shell: false,
        env: { ...process.env, HOMING_ADAPTER_NO_NET: "1" },
      });
    } catch (err) {
      resolve({ ok: false, error: `启动 runner 失败: ${err.message}` });
      return;
    }
    const timer = setTimeout(() => {
      try { child.kill("SIGKILL"); } catch { /* 忽略 */ }
      resolve({ ok: false, error: `超时 ${timeoutMs}ms` });
    }, timeoutMs);
    if (timer.unref) timer.unref();
    child.stdout?.on("data", (d) => { out += d.toString(); });
    child.stderr?.on("data", (d) => { errOut += d.toString(); });
    child.on("error", (err) => {
      clearTimeout(timer);
      resolve({ ok: false, error: err.message });
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      if (code !== 0) {
        resolve({ ok: false, error: (errOut || out || `退出码 ${code}`).slice(0, 800) });
        return;
      }
      try {
        resolve({ ok: true, result: JSON.parse(out || "{}") });
      } catch {
        resolve({ ok: false, error: `输出不是 JSON: ${out.slice(0, 200)}` });
      }
    });
  });
}

// 把脚本适配器包装成与内置相同的 Scanner（每次调用都进子进程）
function wrap(file) {
  const scanner = {
    id: null,
    name: null,
    icon: "generic",
    custom: true,
    scripted: true,
    file,
    async probe() {
      const r = await invoke(file, "meta");
      if (!r.ok) {
        state.set(file, { ok: false, error: r.error, at: Date.now() });
        return null;
      }
      const meta = r.result?.meta || r.result || {};
      state.set(file, { ok: true, error: null, meta, at: Date.now() });
      scanner.id = String(meta.id || path.basename(file, ".mjs"));
      scanner.name = String(meta.name || scanner.id);
      scanner.icon = String(meta.icon || "generic");
      return meta;
    },
    get pushSignal() {
      return state.get(file)?.meta?.pushSignal || "poll";
    },
    get capabilities() {
      const c = state.get(file)?.meta?.capabilities || {};
      return { reply: Boolean(c.reply), sessions: c.sessions !== false };
    },
    async detect() {
      const r = await invoke(file, "detect");
      if (!r.ok) return { installed: false, version: null, detail: r.error };
      return { installed: Boolean(r.result?.installed), version: r.result?.version || null, detail: r.result?.detail || null };
    },
    async listSessions({ limit = 200 } = {}) {
      const r = await invoke(file, "listSessions", { limit });
      return r.ok && Array.isArray(r.result?.sessions) ? r.result.sessions.map((s) => ({ ...s, source: scanner.id })) : [];
    },
    meta(sessionId, cwdHint) {
      // 同步接口：脚本适配器无法同步等子进程，退回最小信息
      return { cwd: cwdHint || null, nativeTitle: "", titleSource: "agent", firstPrompt: "", branch: null, model: null };
    },
    async readItemsRemote(sessionId, { limit = 100 } = {}) {
      const r = await invoke(file, "readItems", { sessionId, limit });
      return r.ok && Array.isArray(r.result?.items) ? r.result.items : [];
    },
    async reply(sessionId, text, opts = {}) {
      const r = await invoke(file, "reply", { sessionId, text, opts }, { timeoutMs: 60_000 });
      return r.ok ? { ok: true, via: "script" } : { ok: false, error: r.error || "脚本回复失败" };
    },
    async models() {
      const r = await invoke(file, "models");
      return r.ok ? (r.result || { models: [], defaultModel: null }) : { models: [], defaultModel: null };
    },
  };
  return scanner;
}

export async function loadScriptAdapters(dir = adaptersDir()) {
  const scanners = [];
  const errors = [];
  let entries = [];
  try { entries = fs.readdirSync(dir); } catch { return { scanners, errors, dir }; }
  for (const name of entries.sort()) {
    if (!name.endsWith(".mjs")) continue;
    const file = path.join(dir, name);
    const sc = wrap(file);
    const meta = await sc.probe();
    if (!meta) {
      const e = state.get(file);
      errors.push({ file, id: name, errors: [e?.error || "加载失败"] });
      continue;
    }
    scanners.push(sc);
  }
  return { scanners, errors, dir };
}

export function adapterStatuses(dir = adaptersDir()) {
  return [...state.entries()].map(([file, s]) => ({
    file,
    id: s.meta?.id || path.basename(file, ".mjs"),
    ok: s.ok,
    error: s.error,
    at: s.at ? new Date(s.at).toISOString() : null,
    reads: s.meta?.reads || [],
  }));
}

export { invoke as invokeScript };
