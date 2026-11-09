// Homing · 本机控制台（/console）与配对、设备、日志等本机接口
//
// 这个模块是「桌面端」的唯一后门：server.mjs 只在路由表里挂两行，
// 静态页面、配对接口、设备接口、日志、统计都在这里，改坏了不会波及会话采集与推送。
//
// 安全边界：
//   /console 只对本机 127.0.0.1 开放（局域网访问一律 403）；
//   页面本身不带 token，靠一个 HttpOnly 的 SameSite=Strict cookie 通过 /api/* 鉴权；
//   /api/pair/claim 是全模块唯一免鉴权的写接口，靠一次性令牌 + 5 分钟有效期 + 次数上限兜底。

import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import crypto from "node:crypto";
import platform, { VERSION, PORT, paths, lanUrls, lanUrls as allLanUrls, hostName, platformLabel, readJson, writeJson } from "./platform/index.mjs";
import * as devices from "./platform/devices.mjs";
import { getPause, setPause, clearPause } from "./platform/pause.mjs";
import { autostartInfo, setAutostart } from "./platform/autostart.mjs";
import { describeHome } from "./platform/homes.mjs";

const STARTED_AT = Date.now();
const MAX_LOG_LINES = 500;

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
  ".ico": "image/x-icon",
  ".map": "application/json; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
};

// ---------------------------------------------------------------- 静态资源

let seaModule = null;
let seaChecked = false;

async function getSea() {
  if (seaChecked) return seaModule;
  seaChecked = true;
  try {
    const mod = await import("node:sea");
    if (mod && typeof mod.getAsset === "function") seaModule = mod;
  } catch {
    seaModule = null;
  }
  return seaModule;
}

/** 单文件运行时页面是嵌在可执行文件里的（sea assets），开发时读 console/dist */
export function consoleDir() {
  if (process.env.HOMING_CONSOLE_DIR) return path.resolve(process.env.HOMING_CONSOLE_DIR);
  const candidates = [
    path.join(platform.relayDir, "..", "console", "dist"),
    path.join(platform.relayDir, "console", "dist"),
    path.join(platform.relayDir, "console-dist"),
    path.join(path.dirname(process.execPath), "console"),
  ];
  for (const dir of candidates) {
    try {
      if (fs.existsSync(path.join(dir, "index.html"))) return dir;
    } catch {
      /* 继续找 */
    }
  }
  return candidates[0];
}

function safeJoin(root, rel) {
  const cleaned = decodeURIComponent(rel).replace(/^\/+/, "");
  const target = path.resolve(root, cleaned);
  if (!target.startsWith(path.resolve(root))) return null;
  return target;
}

async function readConsoleFile(rel) {
  const sea = await getSea();
  if (sea) {
    const key = `console/${rel.replace(/^\/+/, "")}`;
    try {
      const raw = typeof sea.getRawAsset === "function" ? sea.getRawAsset(key) : sea.getAsset(key);
      if (raw) return Buffer.isBuffer(raw) ? raw : Buffer.from(raw);
    } catch {
      /* 落到磁盘 */
    }
  }
  const root = consoleDir();
  const target = safeJoin(root, rel);
  if (!target) return null;
  try {
    const stat = fs.statSync(target);
    if (!stat.isFile()) return null;
    return fs.readFileSync(target);
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------- 统计

function todayKey() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function createStats({ log = () => {} } = {}) {
  const file = path.join(platform.dataDir, "notify-stats.json");
  const loaded = readJson(file, null);
  const state = {
    day: loaded?.day || todayKey(),
    pushed: loaded?.pushed || 0,
    merged: loaded?.merged || 0,
    by_level: loaded?.by_level || { P0: 0, P1: 0, P2: 0 },
  };
  let timer = null;

  function roll() {
    const today = todayKey();
    if (state.day !== today) {
      state.day = today;
      state.pushed = 0;
      state.merged = 0;
      state.by_level = { P0: 0, P1: 0, P2: 0 };
    }
  }

  function flush() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    try {
      writeJson(file, state);
    } catch (err) {
      log(`通知统计写入失败：${err.message}`);
    }
  }

  function record(payload) {
    roll();
    state.pushed += 1;
    const level = payload?.level || "P2";
    state.by_level[level] = (state.by_level[level] || 0) + 1;
    if (payload?.merged) state.merged += Number(payload.count || 1);
    if (!timer) timer = setTimeout(flush, 3000);
  }

  return {
    record,
    flush,
    get() {
      roll();
      return { day: state.day, pushed: state.pushed, merged: state.merged, by_level: { ...state.by_level } };
    },
  };
}

// ---------------------------------------------------------------- 钩子安装

function backupThenWrite(file, content) {
  const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\..+$/, "");
  const backup = `${file}.bak-homing-${stamp}`;
  try {
    if (fs.existsSync(file)) fs.copyFileSync(file, backup);
  } catch (err) {
    return { ok: false, error: `备份 ${path.basename(file)} 失败：${err.message}` };
  }
  const tmp = `${file}.tmp-${process.pid}`;
  fs.writeFileSync(tmp, content, { encoding: "utf8", mode: 0o600 });
  fs.renameSync(tmp, file);
  return { ok: true, backup };
}

function claudeHooksSpec(hookUrl) {
  return {
    Stop: [{ hooks: [{ type: "http", url: hookUrl, timeout: 660 }] }],
    Notification: [{ hooks: [{ type: "http", url: hookUrl, timeout: 5 }] }],
    PermissionRequest: [{ hooks: [{ type: "http", url: hookUrl, timeout: 660 }] }],
  };
}

function mergeClaudeHooks(settings, hookUrl) {
  const next = { ...settings };
  next.hooks = { ...(settings.hooks || {}) };
  let changed = false;
  for (const [event, entries] of Object.entries(claudeHooksSpec(hookUrl))) {
    const existing = Array.isArray(next.hooks[event]) ? next.hooks[event] : [];
    const already = existing.some((group) =>
      Array.isArray(group?.hooks) && group.hooks.some((h) => typeof h?.url === "string" && h.url === hookUrl),
    );
    if (already) continue;
    next.hooks[event] = [...existing, ...entries];
    changed = true;
  }
  return { next, changed };
}

/**
 * 装钩子。只自动改 Claude Code 的 settings.json；
 * Codex 的 hooks.json 牵涉信任状态，按约束不自动改，只返回手工步骤。
 */
export function installHooks(agentId) {
  const hookUrl = `http://127.0.0.1:${PORT}/hook/claude`;
  if (agentId === "claude" || agentId === "workbuddy") {
    const home = os.homedir();
    const dir = agentId === "claude" ? path.join(home, ".claude") : path.join(home, ".workbuddy");
    const file = path.join(dir, "settings.json");
    const url = agentId === "claude" ? hookUrl : `http://127.0.0.1:${PORT}/hook/workbuddy`;
    let settings = {};
    if (fs.existsSync(file)) {
      try {
        settings = JSON.parse(fs.readFileSync(file, "utf8"));
      } catch (err) {
        return { ok: false, error: `${file} 不是合法 JSON（${err.message}），没有改动` };
      }
    } else if (agentId === "workbuddy") {
      return { ok: false, manual: true, error: "没找到 WorkBuddy 的 settings.json，请在 WorkBuddy 设置里按文档手工添加钩子" };
    }
    const { next, changed } = mergeClaudeHooks(settings, url);
    if (!changed) return { ok: true, changed: false, message: "钩子已经装好了", file };
    const written = backupThenWrite(file, `${JSON.stringify(next, null, 2)}\n`);
    if (!written.ok) return { ok: false, error: written.error };
    return { ok: true, changed: true, file, backup: written.backup, message: "已安装钩子，重启对应的 agent 后生效" };
  }

  if (agentId === "codex") {
    return {
      ok: false,
      manual: true,
      file: path.join(os.homedir(), ".codex", "hooks.json"),
      message:
        "Codex 的 PermissionRequest 钩子需要你在 hooks.json 里自行添加并在 Codex 里确认信任，Homing 不会改动它的信任状态",
      snippet: {
        hooks: {
          PermissionRequest: [{ hooks: [{ type: "http", url: `http://127.0.0.1:${PORT}/hook/codex`, timeout: 660 }] }],
        },
      },
    };
  }

  return { ok: false, unsupported: true, message: "这个 agent 走轮询读取，不需要钩子" };
}

// ---------------------------------------------------------------- 主模块

const EMPTY = Symbol("unhandled");

export function createConsole({ authorize, localToken, isLoopback, isLocalOrLan, sendJson, readBody, log = () => {} }) {
  const stats = createStats({ log });

  function json(res, status, payload) {
    sendJson(res, status, payload);
  }

  function requireAuth(req, res) {
    if (authorize(req)) return true;
    json(res, 401, { error: "Unauthorized" });
    return false;
  }

  function clientIp(req) {
    return (req.socket?.remoteAddress || "").replace(/^::ffff:/, "");
  }

  // ------------------------------------------------------------ 静态页面

  async function serveConsole(req, res, url) {
    if (!isLoopback(req)) {
      json(res, 403, {
        error: "控制台只对本机开放",
        hint: "请在运行 homingd 的那台电脑上用 http://127.0.0.1:18787/console 打开。手机端请用 Homing App 配对。",
      });
      return;
    }
    if (req.method !== "GET" && req.method !== "HEAD") {
      json(res, 405, { error: "Method Not Allowed" });
      return;
    }

    let rel = url.pathname.replace(/^\/console\/?/, "");
    if (!rel || rel === "") rel = "index.html";
    if (!/\.[a-z0-9]+$/i.test(rel)) rel = "index.html"; // SPA 路由回落到 index.html

    const body = await readConsoleFile(rel);
    if (!body) {
      // 页面还没构建时给一句人话，而不是空白
      const hint = `<!doctype html><meta charset="utf-8"><title>Homing 控制台</title>
<body style="background:#0F172A;color:#F8FAFC;font:15px/1.7 -apple-system,'PingFang SC',sans-serif;padding:48px">
<h1 style="font-size:20px;margin:0 0 12px">控制台还没有构建</h1>
<p style="color:#94A3B8;margin:0 0 16px">在仓库里执行 <code style="font-family:ui-monospace,monospace;color:#CBD5E1">cd console &amp;&amp; npm install &amp;&amp; npm run build</code>，然后刷新这个页面。</p>
<p style="color:#94A3B8;margin:0">当前查找路径：<code style="font-family:ui-monospace,monospace;color:#CBD5E1">${consoleDir()}</code></p>
</body>`;
      res.writeHead(404, { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" });
      res.end(hint);
      return;
    }

    const ext = path.extname(rel).toLowerCase();
    const type = MIME[ext] || "application/octet-stream";
    const cache = ext === ".html" ? "no-store" : "public, max-age=31536000, immutable";
    const headers = {
      "Content-Type": type,
      "Content-Length": body.length,
      "Cache-Control": cache,
      "X-Content-Type-Options": "nosniff",
      "Referrer-Policy": "no-referrer",
    };
    // 页面与本机接口同源，靠这个 cookie 免去在页面里塞 token
    if (ext === ".html") {
      const token = typeof localToken === "function" ? localToken() : "";
      if (token) {
        headers["Set-Cookie"] = `homing_local=${token}; HttpOnly; SameSite=Strict; Path=/; Max-Age=2592000`;
      }
    }
    res.writeHead(200, headers);
    res.end(req.method === "HEAD" ? undefined : body);
  }

  // ------------------------------------------------------------ 接口

  async function handlePair(req, res, url) {
    // 创建配对 offer：本机只有本机可以调
    if (url.pathname === "/api/pair/offer" && req.method === "POST") {
      if (!requireAuth(req, res)) return;
      const body = await readBody(req).catch(() => ({}));
      const offer = devices.createOffer({ lanUrl: body?.lan_url || null });
      log(`配对：已生成 6 位码（${offer.offer_id}），5 分钟内有效`);
      json(res, 200, offer);
      return;
    }

    if (url.pathname === "/api/pair/latest" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      json(res, 200, { offer: devices.latestOffer() });
      return;
    }

    const offerMatch = /^\/api\/pair\/offer\/([0-9a-f]{16})$/.exec(url.pathname);
    if (offerMatch && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      const offer = devices.getOffer(offerMatch[1]);
      if (!offer) {
        json(res, 404, { error: "没有这个配对请求" });
        return;
      }
      json(res, 200, offer);
      return;
    }

    // 手机扫码 / 输入 6 位码后换长期设备令牌：唯一免鉴权接口
    if (url.pathname === "/api/pair/claim" && req.method === "POST") {
      if (!isLocalOrLan(req.socket?.remoteAddress)) {
        json(res, 403, { error: "只接受本机与局域网来源" });
        return;
      }
      const body = await readBody(req).catch(() => ({}));
      const result = devices.claim({
        token: body?.token || null,
        code: body?.code || null,
        device: body?.device || {},
        ip: clientIp(req),
      });
      if (!result.ok) {
        log(`配对失败：${result.status} ${result.error}`);
        json(res, result.status, { error: result.error, code: result.status });
        return;
      }
      log(`配对成功：${result.device.name}（${result.device.platform}）· 步骤 ${result.device.paired_via}`);
      json(res, 200, {
        ...result,
        // 手机端下一次请求用它，Homing 不会把这个明文写进任何日志
        token_type: "device",
      });
      return;
    }

    if (url.pathname === "/api/pair/cancel" && req.method === "POST") {
      if (!requireAuth(req, res)) return;
      const body = await readBody(req).catch(() => ({}));
      // 只是把 offer 标成过期，不删记录，便于日志里看
      const offer = body?.offer_id ? devices.getOffer(body.offer_id) : null;
      json(res, 200, { ok: true, offer });
      return;
    }

    json(res, 404, { error: "Not Found" });
  }

  async function handleDevices(req, res, url) {
    if (url.pathname === "/api/devices" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      const list = devices.listDevices();
      json(res, 200, {
        devices: list,
        count: list.length,
        online: list.filter((d) => d.online).length,
      });
      return;
    }

    if (url.pathname === "/api/devices/heartbeat" && req.method === "POST") {
      const body = await readBody(req).catch(() => ({}));
      const header = req.headers.authorization || "";
      const bearer = header.startsWith("Bearer ") ? header.slice(7) : "";
      const device = devices.touchByToken(bearer, {
        app_version: body?.app_version || null,
        ip: clientIp(req),
      });
      if (!device) {
        json(res, 401, { error: "设备令牌无效" });
        return;
      }
      const pause = getPause();
      json(res, 200, { ok: true, device, server_time: new Date().toISOString(), paused: pause.paused, pause_until: pause.until });
      return;
    }

    const one = /^\/api\/devices\/([0-9a-f]{12})$/.exec(url.pathname);
    if (one) {
      if (!requireAuth(req, res)) return;
      if (req.method === "DELETE") {
        const result = devices.revokeDevice(one[1]);
        if (!result.ok) {
          json(res, 404, result);
          return;
        }
        log(`配对：已移除设备 ${one[1]}`);
        json(res, 200, result);
        return;
      }
      if (req.method === "PATCH") {
        const body = await readBody(req).catch(() => ({}));
        const result = devices.renameDevice(one[1], body?.name);
        json(res, result.ok ? 200 : 404, result);
        return;
      }
    }

    json(res, 404, { error: "Not Found" });
  }

  async function handleConsoleApi(req, res, url) {
    if (url.pathname === "/api/connection" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      json(res, 200, {
        version: VERSION,
        product: "Homing",
        mode: "lan",
        port: PORT,
        local_url: `http://127.0.0.1:${PORT}`,
        lan_urls: allLanUrls(PORT),
        host_name: hostName(),
        platform: platformLabel(),
        platform_raw: process.platform,
        arch: process.arch,
        node: process.versions.node,
        data_dir: platform.dataDir,
        console_dir: consoleDir(),
        packaged: platform.isPackaged,
        uptime_s: Math.round((Date.now() - STARTED_AT) / 1000),
        autostart: autostartInfo(),
        security: {
          tls: false,
          e2e: "planned_m4",
          local_auth: "cookie",
          remote_auth: "device_token",
        },
        options: {
          lan: { available: true, recommended: true, label: "局域网直连" },
          cloud: { available: false, label: "Homing 云中转", note: "即将推出" },
          self_hosted: { available: false, label: "自建服务器", note: "这一版不可选" },
        },
      });
      return;
    }

    if (url.pathname === "/api/stats" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      json(res, 200, { notify: stats.get(), pause: getPause(), devices: devices.listDevices().length });
      return;
    }

    if (url.pathname === "/api/push/pause") {
      if (!requireAuth(req, res)) return;
      if (req.method === "GET") {
        json(res, 200, getPause());
        return;
      }
      if (req.method === "POST") {
        const body = await readBody(req).catch(() => ({}));
        const minutes = Number(body?.minutes || 0);
        const state = minutes > 0 ? setPause(minutes, body?.reason || "手动暂停") : clearPause();
        log(minutes > 0 ? `推送已暂停 ${minutes} 分钟` : "推送已恢复");
        json(res, 200, state);
        return;
      }
      if (req.method === "DELETE") {
        json(res, 200, clearPause());
        return;
      }
    }

    if (url.pathname === "/api/agent-paths" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      const out = {};
      for (const id of ["codex", "claude", "workbuddy", "opencode", "zcode"]) {
        try {
          out[id] = describeHome(id);
        } catch (err) {
          out[id] = { path: null, wsl: [], error: err.message };
        }
      }
      json(res, 200, { paths: out, homedir: os.homedir(), wsl_supported: process.platform === "win32" });
      return;
    }

    const hookMatch = /^\/api\/agents\/([a-z]+)\/hooks$/.exec(url.pathname);
    if (hookMatch) {
      if (!isLoopback(req)) {
        json(res, 403, { error: "装钩子只允许在本机操作" });
        return;
      }
      if (!requireAuth(req, res)) return;
      if (req.method !== "POST") {
        json(res, 405, { error: "Method Not Allowed" });
        return;
      }
      const result = installHooks(hookMatch[1]);
      log(`钩子安装 ${hookMatch[1]}：${result.ok ? "完成" : "未完成"}（${result.message || result.error || ""}）`);
      json(res, result.ok ? 200 : result.unsupported ? 400 : 202, result);
      return;
    }

    if (url.pathname === "/api/logs" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      const limit = Math.min(Math.max(Number(url.searchParams.get("limit") || 200), 1), MAX_LOG_LINES);
      const source = pickLogFile();
      const lines = tailLines(source, limit);
      json(res, 200, { source, count: lines.length, lines });
      return;
    }

    if (url.pathname === "/api/setup" && req.method === "GET") {
      if (!requireAuth(req, res)) return;
      const state = readJson(path.join(platform.dataDir, "setup.json"), null);
      const agents = devices.listDevices();
      json(res, 200, {
        completed: Boolean(state?.completed_at),
        completed_at: state?.completed_at || null,
        chosen_agents: state?.agents || [],
        method: state?.method || "lan",
        devices: agents.length,
      });
      return;
    }

    if (url.pathname === "/api/setup" && req.method === "POST") {
      if (!requireAuth(req, res)) return;
      const body = await readBody(req).catch(() => ({}));
      const record = {
        completed_at: new Date().toISOString(),
        agents: Array.isArray(body?.agents) ? body.agents.slice(0, 20) : [],
        method: body?.method === "cloud" || body?.method === "self_hosted" ? body.method : "lan",
      };
      writeJson(path.join(platform.dataDir, "setup.json"), record);
      log(`首次设置完成：${record.agents.join(", ") || "未选择 agent"} · ${record.method}`);
      json(res, 200, { ok: true, ...record });
      return;
    }

    if (url.pathname === "/api/autostart") {
      if (!isLoopback(req)) {
        json(res, 403, { error: "开机自启只允许本机设置" });
        return;
      }
      if (!requireAuth(req, res)) return;
      if (req.method === "GET") {
        json(res, 200, autostartInfo());
        return;
      }
      if (req.method === "POST") {
        const body = await readBody(req).catch(() => ({}));
        const result = setAutostart(Boolean(body?.enabled), {
          execPath: process.execPath,
          scriptPath: platform.isPackaged ? null : path.join(platform.relayDir, "server.mjs"),
        });
        log(`开机自启：${result.ok ? result.note : result.error}`);
        json(res, result.ok ? 200 : 500, result);
        return;
      }
    }

    json(res, 404, { error: "Not Found" });
  }

  function pickLogFile() {
    const candidates = [platform.paths.log, path.join(platform.relayDir, "relay.log")];
    for (const file of candidates) {
      try {
        const stat = fs.statSync(file);
        if (stat.isFile() && stat.size > 0) return file;
      } catch {
        /* 下一个 */
      }
    }
    return null;
  }

  function tailLines(file, limit) {
    if (!file) return [];
    try {
      const stat = fs.statSync(file);
      const size = Math.min(stat.size, 512 * 1024);
      const fd = fs.openSync(file, "r");
      const buffer = Buffer.alloc(size);
      fs.readSync(fd, buffer, 0, size, Math.max(0, stat.size - size));
      fs.closeSync(fd);
      return buffer
        .toString("utf8")
        .split("\n")
        .filter(Boolean)
        .slice(-limit)
        .map((raw) => {
          const text = redact(raw);
          const match = /^\[(\w+)\]\s*(.*)$/.exec(text);
          const level = /error|失败|Error/.test(text) ? "error" : match ? match[1] : "info";
          return { at: timestampOf(text), level, text };
        });
    } catch {
      return [];
    }
  }

  // 日志里可能出现 token / 配对令牌 / 设备令牌，一律打码后再给页面
  function redact(line) {
    return line
      .replace(/(Token:\s*)([0-9a-fA-F]{16,})/g, "$1••••••••（已隐藏）")
      .replace(/\bhm_[A-Za-z0-9_-]{20,}/g, "hm_••••••••（已隐藏）")
      .replace(/(token=)([0-9a-fA-F]{16,})/g, "$1••••••••（已隐藏）");
  }

  function timestampOf(text) {
    const iso = /(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)/.exec(text);
    return iso ? iso[1] : null;
  }

  /** 返回 true 表示这条请求已经被控制台接管 */
  async function handle(req, res, url) {
    if (url.pathname === "/console" || url.pathname.startsWith("/console/")) {
      await serveConsole(req, res, url);
      return true;
    }
    if (url.pathname === "/api/pair" || url.pathname.startsWith("/api/pair/")) {
      await handlePair(req, res, url);
      return true;
    }
    if (url.pathname.startsWith("/api/devices")) {
      await handleDevices(req, res, url);
      return true;
    }
    if (
      url.pathname === "/api/connection" ||
      url.pathname === "/api/stats" ||
      url.pathname === "/api/logs" ||
      url.pathname === "/api/setup" ||
      url.pathname === "/api/autostart" ||
      url.pathname === "/api/agent-paths" ||
      url.pathname.startsWith("/api/push/") ||
      /^\/api\/agents\/[a-z]+\/hooks$/.test(url.pathname)
    ) {
      await handleConsoleApi(req, res, url);
      return true;
    }
    return false;
  }

  return { handle, stats, consoleDir, EMPTY };
}
