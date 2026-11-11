#!/usr/bin/env node

import { createServer, request as httpRequest } from "node:http";
import { spawn, execFileSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { scanners, initAll as initScanners, get as getScanner, iconFor } from "./scanners/registry.mjs";
import { createSessionState, isTempCwd } from "./session-state.mjs";
import { createNotifier } from "./notify.mjs";
import platform from "./platform/index.mjs";
import { createConsole } from "./console.mjs";
import { verifyDeviceToken } from "./platform/devices.mjs";
import {
  mergeReplyRecord,
  normalizeReplyRecord,
  oldestQueuedReplyBeforeEvent,
  queuedReplyIDsConfirmedByEvents,
  resolveCodexFinishStatus,
  shouldIgnoreReplyStatusDowngrade,
} from "./reply-state.mjs";

const relayDir = platform.relayDir;

// ---- 命令行参数（纯命令行版 homingd：服务器、不想要界面的人都用这个） ----------
const argv = process.argv.slice(2);
function argValue(name) {
  const index = argv.indexOf(name);
  if (index >= 0 && argv[index + 1] && !argv[index + 1].startsWith("-")) return argv[index + 1];
  const inline = argv.find((a) => a.startsWith(`${name}=`));
  return inline ? inline.slice(name.length + 1) : null;
}

if (argv.includes("--help") || argv.includes("-h")) {
  console.log(`homingd ${platform.VERSION} — Homing 本机服务

用法: homingd [选项]

  --port <端口>        监听端口，默认 18787
  --data-dir <目录>    数据目录，默认 ${
    process.platform === "darwin" ? "~/Library/Application Support/Homing" : "%APPDATA%\\Homing"
  }
  --no-console         不提供 /console 网页界面（只留接口，适合跑在服务器上）
  --version            显示版本
  --help               显示这段帮助

配置与环境变量：
  PORT / HOMING_DATA_DIR 与上面的参数等价；配置文件在 <数据目录>/config.json。
`);
  process.exit(0);
}
if (argv.includes("--version") || argv.includes("-v")) {
  console.log(`homingd ${platform.VERSION}`);
  process.exit(0);
}
if (argValue("--port")) {
  platform.setPort(argValue("--port"));
}
if (argValue("--data-dir")) {
  process.env.HOMING_DATA_DIR = path.resolve(argValue("--data-dir"));
}
if (argv.includes("--no-console")) {
  process.env.HOMING_NO_CONSOLE = "1";
}

// 数据目录：Mac ~/Library/Application Support/Homing，Windows %APPDATA%\Homing
// 老的 relay/data 会在启动时搬过来（只复制、不删源），已有 token 与配对不受影响
platform.migrateLegacyData({ legacyRelayDir: relayDir, log: (m) => console.log(`[platform] ${m}`) });
const configPath = platform.paths.config;
const dataDir = platform.dataDir;
const eventsPath = path.join(dataDir, "events.jsonl");
const repliesPath = path.join(dataDir, "replies.jsonl");
const apkPath = process.env.APK_PATH
  ? path.resolve(process.env.APK_PATH)
  : path.join(
      relayDir,
      "..",
      "android",
      "app",
      "build",
      "outputs",
      "apk",
      "debug",
      "app-debug.apk",
    );
const host = "0.0.0.0";
const port = Number(process.env.PORT || 18787);

if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("PORT 必须是 1 到 65535 之间的整数");
}

fs.mkdirSync(dataDir, { recursive: true });

// ---- Homing v2：会话治理状态 + 通知分级 --------------------------------
const agentProbe = new Map();
const scannerRows = { at: 0, rows: [] };   // scanner.listSessions() 的缓存（60s）
const sessionState = createSessionState(path.join(dataDir, "session-state.json"));

// 本机控制台（/console + 配对 / 设备 / 日志 / 统计），实现全在 console.mjs
const consoleApp = createConsole({
  authorize: hasValidToken,
  localToken: () => config.token,
  isLoopback,
  isLocalOrLan: isLocalOrLanAddress,
  sendJson,
  readBody,
  log: (msg) => console.log(`[console] ${msg}`),
});

const notifier = createNotifier({
  sessionState,
  sink: (payload) => {
    try {
      consoleApp.stats.record(payload);
    } catch {
      /* 统计失败不影响推送 */
    }
    for (const client of sseClients) {
      try { client.write(`event: notify\ndata: ${JSON.stringify(payload)}\n\n`); } catch { /* 忽略 */ }
    }
  },
  log: (msg) => console.log(`[notify] ${msg}`),
});
initScanners({ transcriptPaths: () => transcriptPaths, log: (m) => console.log(`[scanner] ${m}`) });

function ensureConfig() {
  if (!fs.existsSync(configPath)) {
    const config = { token: crypto.randomBytes(16).toString("hex") };
    fs.writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
  }

  const config = JSON.parse(fs.readFileSync(configPath, "utf8"));
  if (typeof config.token !== "string" || config.token.length < 16) {
    throw new Error("relay/config.json 中的 token 无效");
  }
  // 离开模式（手机续聊）：默认关闭，持久化在 config.json。
  if (!config.away || typeof config.away !== "object") {
    config.away = { enabled: false };
    writeConfig(config);
  }
  config.away.enabled = config.away.enabled === true;

  // 本机端口代理：默认关闭，白名单端口持久化在 config.json。
  if (!config.proxy || typeof config.proxy !== "object") {
    config.proxy = { enabled: false, ports: [] };
    writeConfig(config);
  }
  config.proxy.enabled = config.proxy.enabled === true;
  config.proxy.ports = Array.isArray(config.proxy.ports)
    ? config.proxy.ports.filter((port) => Number.isInteger(port) && port > 0 && port < 65536)
    : [];
  return config;
}

function writeConfig(next) {
  fs.writeFileSync(configPath, `${JSON.stringify(next, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
}

function resolveCodexBin() {
  const candidates = [
    process.env.CODEX_BIN,
    "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
    path.join(os.homedir(), ".local/bin/codex"),
    "/opt/homebrew/bin/codex",
    "/usr/local/bin/codex",
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      if (fs.statSync(candidate).isFile()) {
        return candidate;
      }
    } catch {
      // 继续尝试下一个候选路径
    }
  }
  return "codex";
}

function readJsonLines(filePath) {
  if (!fs.existsSync(filePath)) {
    return [];
  }

  const records = [];
  for (const line of fs.readFileSync(filePath, "utf8").split("\n")) {
    if (!line.trim()) {
      continue;
    }
    try {
      records.push(JSON.parse(line));
    } catch {
      // A damaged trailing line should not prevent the relay from starting.
    }
  }
  return records;
}

const config = ensureConfig();
const events = readJsonLines(eventsPath);

// replies.jsonl 是追加式日志：同一个 id 可能有多行。正常会从 queued 前进到
// held / injected / failed；旧版本曾在 injected 之后误写回 queued，加载时修复这种
// 不可逆回退，避免历史消息永久留在“排队管理”里。
const replies = (() => {
  const byId = new Map();
  for (const record of readJsonLines(repliesPath)) {
    if (Number.isSafeInteger(record.id)) {
      byId.set(record.id, mergeReplyRecord(byId.get(record.id), record));
    }
  }
  const merged = [...byId.values()]
    .sort((a, b) => a.id - b.id)
    .map(normalizeReplyRecord);
  const confirmed = new Set(queuedReplyIDsConfirmedByEvents(merged, events));
  return merged.map((reply) =>
    confirmed.has(reply.id)
      ? {
          ...reply,
          status: "injected",
          injected: true,
          message: "Agent 已产生后续回复，确认已送达",
        }
      : reply,
  );
})();
let nextEventId = events.reduce((max, event) => {
  return Number.isSafeInteger(event.id) ? Math.max(max, event.id) : max;
}, 0) + 1;
let nextReplyId = replies.reduce((max, reply) => {
  return Number.isSafeInteger(reply.id) ? Math.max(max, reply.id) : max;
}, 0) + 1;

const sseClients = new Set();
const validSources = new Set(["claude", "codex", "workbuddy"]);
const validKinds = new Set(["reply", "needs_input", "permission", "error"]);
const sourceLabels = {
  claude: "Claude Code",
  codex: "Codex",
  workbuddy: "WorkBuddy",
};

function truncate(value, limit = 4000) {
  return Array.from(typeof value === "string" ? value : "").slice(0, limit).join("");
}

function projectFromCwd(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return "未命名项目";
  }

  const parts = cwd
    .trim()
    .replace(/[\\/]+$/, "")
    .split(/[\\/]/)
    .filter(Boolean);
  return parts.at(-1) || cwd.trim();
}

function appendJsonLine(filePath, value) {
  fs.appendFileSync(filePath, `${JSON.stringify(value)}\n`, "utf8");
}

function addEvent({ source, kind, session_id, cwd, title, text }) {
  if (!validSources.has(source) || !validKinds.has(kind)) {
    throw new Error("事件 source 或 kind 无效");
  }

  const normalizedCwd = typeof cwd === "string" ? cwd : "";
  const normalizedText = truncate(text);
  const attachments = buildAttachments(normalizedText, normalizedCwd);
  const localhostLinks = localhostLinksIn(
    `${typeof title === "string" ? title : ""}\n${normalizedText}`,
  );

  const sessionId = typeof session_id === "string" ? session_id : "";
  let sessionMeta = null;
  try {
    if (sessionId) {
      sessionMeta = sessionMetaFor(source, sessionId, normalizedCwd);
    }
  } catch {
    sessionMeta = null;
  }

  const event = {
    id: nextEventId++,
    ts: new Date().toISOString(),
    source,
    kind,
    session_id: sessionId,
    cwd: normalizedCwd,
    project: projectFromCwd(normalizedCwd),
    title: truncate(title || sourceLabels[source], 200),
    text: normalizedText,
    read: false,
    attachments,
    ...(localhostLinks.length ? { localhost_links: localhostLinks } : {}),
    ...(sessionMeta ? { session_meta: sessionMeta } : {}),
  };

  events.push(event);
  appendJsonLine(eventsPath, event);
  for (const client of sseClients) {
    sendEvent(client, event);
  }
  try {
    confirmQueuedReplyFromAgentEvent(event);
  } catch (error) {
    console.log(`[reply] 确认排队消息失败（已忽略）: ${error.message}`);
  }
  try {
    ensureTailer(event.session_id, event.source);
  } catch {
    // tail 失败不影响入库
  }
  try {
    claimLaunchForEvent(event);
  } catch {
    // 关联失败不影响事件推送
  }
  try {
    notifier.onEvent(event);
  } catch (err) {
    console.log(`[notify] 分级失败（已忽略）: ${err.message}`);
  }
  return event;
}

function confirmQueuedReplyFromAgentEvent(event) {
  const reply = oldestQueuedReplyBeforeEvent(replies, event);
  if (!reply) {
    return;
  }
  setReplyStatus(reply, "injected", "Agent 已产生后续回复，确认已送达");
}

function sendEvent(response, event) {
  if (response.destroyed || response.writableEnded) {
    return;
  }
  if (response.__sessionFilter && event.session_id !== response.__sessionFilter) {
    return;
  }
  response.write(`event: message\ndata: ${JSON.stringify(event)}\n\n`);
}

/* ------------------------------------------------------------------ *
 * 手机回复：注入 / 挂起 / 排队
 * ------------------------------------------------------------------ */

// Stop 钩子最长挂起 10 分钟（自测可用 HOLD_TIMEOUT_MS 环境变量缩短）
const HOLD_TIMEOUT_MS = Number(process.env.HOLD_TIMEOUT_MS || 10 * 60 * 1000);
const CODEX_INJECT_TIMEOUT_MS = 10 * 60 * 1000;
const codexBin = resolveCodexBin();
let codexDaemonReady = false;

// session_id -> [{ eventId, source, resolve, settled, timer }]
const pendingHolds = new Map();
// thread_id -> { running: boolean, queue: Reply[] }
const codexThreads = new Map();

const statusMessages = {
  injected: "已注入 Codex 会话",
  held: "已交给挂起中的钩子，Agent 继续执行",
  queued: "暂无挂起的钩子，已排队",
  failed: "投递失败",
};

function deliveryTextFor(reply) {
  const files = Array.isArray(reply.files) ? reply.files : [];
  if (!files.length) {
    return reply.text;
  }
  return `${reply.text}\n\n${uploadedFilesNote(files)}`;
}

function persistReply(reply) {
  appendJsonLine(repliesPath, {
    id: reply.id,
    event_id: reply.event_id,
    session_id: reply.session_id,
    text: reply.text,
    ts: reply.ts,
    injected: reply.status === "injected",
    status: reply.status,
    message: reply.message || "",
    files: Array.isArray(reply.files) ? reply.files : [],
  });
}

function broadcastReplyStatus(reply) {
  const payload = {
    reply_id: reply.id,
    event_id: reply.event_id,
    session_id: reply.session_id,
    status: reply.status,
    message: reply.message || statusMessages[reply.status] || "",
  };
  for (const client of sseClients) {
    if (client.destroyed || client.writableEnded) {
      continue;
    }
    if (client.__sessionFilter && client.__sessionFilter !== reply.session_id) {
      continue;
    }
    client.write(`event: reply_status\ndata: ${JSON.stringify(payload)}\n\n`);
  }
}

function setReplyStatus(reply, status, message) {
  if (shouldIgnoreReplyStatusDowngrade(reply.status, status)) {
    console.log(
      `[reply] #${reply.id} 忽略过期状态回退 ${reply.status} -> queued`,
    );
    return;
  }
  reply.status = status;
  reply.injected = status === "injected";
  reply.message = message || statusMessages[status] || "";
  persistReply(reply);
  broadcastReplyStatus(reply);
  console.log(
    `[reply] #${reply.id} event=#${reply.event_id} status=${reply.status} ${reply.message}`,
  );
}

function blockDecision(texts) {
  const joined = texts
    .map((text) => (typeof text === "string" ? text.trim() : ""))
    .filter(Boolean)
    .join("\n");
  return { decision: "block", reason: `用户从手机回复：${joined}` };
}

function holdsFor(sessionId) {
  return pendingHolds.get(sessionId) || [];
}

function settleHold(hold, decision) {
  if (hold.settled) {
    return;
  }
  hold.settled = true;
  clearTimeout(hold.timer);
  const list = pendingHolds.get(hold.sessionId);
  if (list) {
    const index = list.indexOf(hold);
    if (index >= 0) {
      list.splice(index, 1);
    }
    if (!list.length) {
      pendingHolds.delete(hold.sessionId);
    }
  }
  hold.resolve(decision);
}

function firstPendingHold(sessionId) {
  return holdsFor(sessionId).find((hold) => !hold.settled) || null;
}

function holdStopRequest(event) {
  return new Promise((resolve) => {
    const hold = {
      sessionId: event.session_id,
      eventId: event.id,
      source: event.source,
      settled: false,
      resolve,
      timer: null,
    };
    hold.timer = setTimeout(() => {
      if (!hold.settled) {
        console.log(`[hold] 超时 session=${event.session_id} event=#${event.id}`);
        settleHold(hold, {});
      }
    }, HOLD_TIMEOUT_MS);

    const list = pendingHolds.get(event.session_id) || [];
    list.push(hold);
    pendingHolds.set(event.session_id, list);
    console.log(
      `[hold] 挂起 session=${event.session_id} event=#${event.id} source=${event.source}`,
    );
  });
}

function takeQueuedReplies(sessionId) {
  return replies
    .filter((reply) => reply.session_id === sessionId && reply.status === "queued")
    .sort((a, b) => a.id - b.id);
}

// Stop 事件在离开模式下的处理：先把已排队的手机回复交给这一轮，
// 否则挂起等待手机回复（最多 10 分钟）。
async function resumeFromPhone(event) {
  if (!config.away.enabled) {
    return {};
  }

  const queued = takeQueuedReplies(event.session_id);
  if (queued.length) {
    for (const reply of queued) {
      setReplyStatus(reply, "held");
    }
    console.log(
      `[hold] 排队回复直接续聊 session=${event.session_id} count=${queued.length}`,
    );
    return blockDecision(queued.map((reply) => deliveryTextFor(reply)));
  }

  return await holdStopRequest(event);
}

const claudeProcesses = new Map(); // session_id -> child（claude -p 注入）
const launchedProcesses = new Map(); // session_id -> child（/api/sessions/new 起的 codex exec）
const workbuddyAcpSessions = new Map(); // session_id -> { port, connectionId }

// Claude：claude -p --resume <session> --model <model>。仅当调用方指定 model 时使用；
// 会话看起来正被桌面端占用（源文件 15 秒内仍在变化）就转为 queued，等下一次 Stop 钩子。
function injectClaudeReply(reply) {
  const binary = commandExists("claude");
  if (!binary) {
    setReplyStatus(reply, "failed", "本机没有 claude 命令");
    return;
  }
  const filePath = sessionFilePath("claude", reply.session_id);
  let busy = false;
  if (filePath) {
    try {
      busy = Date.now() - fs.statSync(filePath).mtimeMs < 15_000;
    } catch {
      busy = false;
    }
  }
  if (busy) {
    setReplyStatus(reply, "held", "Claude 会话可能正被桌面端占用，已排队等下一次 Stop 钩子");
    return;
  }

  const args = ["-p", deliveryTextFor(reply), "--resume", reply.session_id];
  if (reply.model) {
    args.push("--model", reply.model);
  }
  let child;
  try {
    child = spawn(binary, args, {
      cwd: reply.cwd && fs.existsSync(reply.cwd) ? reply.cwd : relayDir,
      stdio: "ignore",
      env: process.env,
      detached: true,
    });
  } catch (error) {
    setReplyStatus(reply, "failed", `无法启动 claude: ${error.message}`);
    return;
  }
  claudeProcesses.set(reply.session_id, child);
  child.on("exit", () => {
    if (claudeProcesses.get(reply.session_id) === child) {
      claudeProcesses.delete(reply.session_id);
    }
  });
  child.unref();
  setReplyStatus(reply, "injected", `已用 claude -p --resume 注入${reply.model ? `（model=${reply.model}）` : ""}`);
}

function stopSessionProcess(sessionId) {
  const codexState = codexThreads.get(sessionId);
  if (codexState?.child) {
    try {
      codexState.child.kill("SIGTERM");
    } catch {
      // 忽略
    }
    codexState.child = null;
    codexState.running = false;
    codexState.queue.length = 0;
    return { stopped: true, via: "codex-exec" };
  }
  const launched = launchedProcesses.get(sessionId);
  if (launched) {
    try {
      launched.kill("SIGTERM");
    } catch {
      // 忽略
    }
    launchedProcesses.delete(sessionId);
    return { stopped: true, via: "codex-exec" };
  }
  const claudeChild = claudeProcesses.get(sessionId);
  if (claudeChild) {
    try {
      claudeChild.kill("SIGTERM");
    } catch {
      // 忽略
    }
    claudeProcesses.delete(sessionId);
    return { stopped: true, via: "claude-p" };
  }
  const acp = workbuddyAcpSessions.get(sessionId);
  if (acp) {
    acpFire(acp.port, acp.connectionId, {
      jsonrpc: "2.0",
      id: 9,
      method: "session/cancel",
      params: { sessionId },
    });
    workbuddyAcpSessions.delete(sessionId);
    return { stopped: true, via: "acp-cancel" };
  }
  return { stopped: false };
}

function resolveInjectionCwd(event) {
  const cwd = typeof event.cwd === "string" ? event.cwd.trim() : "";
  if (cwd) {
    try {
      if (fs.statSync(cwd).isDirectory()) {
        return cwd;
      }
    } catch {
      // cwd 已不存在，回落到 relay 目录
    }
  }
  return relayDir;
}

// Codex 注入：优先用 `codex queue --resume`，它会走 Codex 自己的 thread store，
// 不会和桌面 App 已持有的 writer 冲突。旧版 CLI 才回退到 `codex exec resume`。
function injectCodexReply(reply, event) {
  const threadId = reply.session_id;
  if (!threadId) {
    setReplyStatus(reply, "failed", "Codex 事件缺少 thread id，无法注入");
    return;
  }

  const state = codexThreads.get(threadId) || { running: false, queue: [] };
  codexThreads.set(threadId, state);

  if (state.running) {
    state.queue.push({ reply, event });
    setReplyStatus(reply, "queued", "该 thread 正在执行上一轮注入，已排队");
    return;
  }

  runCodexInjection(state, reply, event);
}

function runCodexInjection(state, reply, event) {
  state.running = true;

  ensureCodexDaemon();
  const cwd = resolveInjectionCwd(event);
  const args = ["queue", "--thread", reply.session_id, "--message", deliveryTextFor(reply), "--resume"];
  if (cwd) {
    args.push("-C", cwd);
  }

  let child;
  try {
    child = spawn(codexBin, args, {
      cwd,
      stdio: ["ignore", "pipe", "pipe"],
      env: process.env,
    });
  } catch (error) {
    state.running = false;
    setReplyStatus(reply, "failed", `无法启动 codex queue: ${error.message}`);
    drainCodexQueue(state);
    return;
  }

  state.child = child;
  setReplyStatus(reply, "queued", "正在交给 Codex 原生队列");

  const killTimer = setTimeout(() => {
    try {
      child.kill("SIGKILL");
    } catch {
      // 忽略
    }
  }, CODEX_INJECT_TIMEOUT_MS);
  let handedOff = false;
  let finished = false;
  const probeTimer = setTimeout(() => {
    if (finished) {
      return;
    }
    handedOff = true;
    const modelNote = reply.model || reply.effort
      ? "；原生队列沿用该会话当前模型与强度"
      : "";
    setReplyStatus(
      reply,
      "injected",
      `已交给 Codex 原生队列，继续在会话中执行${modelNote}`,
    );
    console.log(`[reply] #${reply.id} Codex 原生队列已接收，回合在后台继续`);
  }, 800);

  let output = "";
  child.stdout.on("data", (chunk) => {
    if (output.length < 4000) {
      output += chunk.toString("utf8");
    }
  });
  child.stderr.on("data", (chunk) => {
    if (output.length < 4000) {
      output += chunk.toString("utf8");
    }
  });

  const finish = (code, error) => {
    if (finished) {
      return;
    }
    finished = true;
    clearTimeout(probeTimer);
    clearTimeout(killTimer);
    state.running = false;
    state.child = null;
    if (code !== 0) {
      const detail = (error?.message || output.trim() || "").slice(0, 300);
      if (/unrecognized subcommand|unknown command|unexpected argument/i.test(detail)) {
        console.log(`[reply] #${reply.id} 当前 codex 不支持 queue，回退 exec resume`);
        runLegacyCodexInjection(state, reply, event);
        return;
      }
      setReplyStatus(reply, "failed", `codex queue 注入失败（exit=${code}）${detail ? `: ${detail}` : ""}`);
    } else {
      const status = resolveCodexFinishStatus(handedOff, output);
      const modelNote = reply.model || reply.effort
        ? "；原生队列沿用该会话当前模型与强度"
        : "";
      if (status !== reply.status) {
        setReplyStatus(
          reply,
          status,
          status === "queued"
            ? `已排入 Codex 队列，等待会话恢复${modelNote}`
            : handedOff
              ? `已交给 Codex 原生队列，继续在会话中执行${modelNote}`
              : `已注入 Codex thread ${reply.session_id}${modelNote}`,
        );
      }
      console.log(`[reply] #${reply.id} Codex 原生队列已接收`);
    }
    drainCodexQueue(state);
  };

  child.on("error", (error) => finish(-1, error));
  child.on("close", (code) => finish(code ?? -1));
}

function ensureCodexDaemon() {
  if (codexDaemonReady || !codexBin) {
    return;
  }
  try {
    execFileSync(codexBin, ["app-server", "daemon", "version"], {
      stdio: "ignore",
      timeout: 3000,
    });
    codexDaemonReady = true;
    return;
  } catch {
    // 没有常驻 app-server；启动一次后再走原生队列。
  }
  try {
    execFileSync(codexBin, ["app-server", "daemon", "start"], {
      stdio: "ignore",
      timeout: 15000,
    });
    codexDaemonReady = true;
  } catch (error) {
    console.log(`[reply] Codex daemon 启动失败，消息仍会排队: ${error.message}`);
  }
}

function runLegacyCodexInjection(state, reply, event) {
  state.running = true;
  const args = ["exec", "resume", "--skip-git-repo-check"];
  if (reply.model) {
    args.push("-m", reply.model);
  }
  if (reply.effort) {
    args.push("-c", `model_reasoning_effort=${reply.effort}`);
  }
  args.push("--all", reply.session_id, deliveryTextFor(reply));

  let child;
  try {
    child = spawn(codexBin, args, {
      cwd: resolveInjectionCwd(event),
      stdio: ["ignore", "pipe", "pipe"],
      env: process.env,
    });
  } catch (error) {
    state.running = false;
    setReplyStatus(reply, "failed", `无法启动 codex: ${error.message}`);
    drainCodexQueue(state);
    return;
  }

  state.child = child;
  setReplyStatus(reply, "queued", "当前 Codex 使用兼容模式注入");
  const timer = setTimeout(() => {
    try {
      child.kill("SIGKILL");
    } catch {
      // 忽略
    }
  }, CODEX_INJECT_TIMEOUT_MS);

  let stderr = "";
  child.stdout.resume();
  child.stderr.on("data", (chunk) => {
    if (stderr.length < 4000) {
      stderr += chunk.toString("utf8");
    }
  });

  const finish = (code, error) => {
    clearTimeout(timer);
    state.running = false;
    state.child = null;
    if (code !== 0) {
      const detail = (error?.message || stderr.trim() || "").slice(0, 300);
      setReplyStatus(reply, "failed", `codex 注入失败（exit=${code}）${detail ? `: ${detail}` : ""}`);
    } else {
      setReplyStatus(reply, "injected", `已注入 Codex thread ${reply.session_id}`);
    }
    drainCodexQueue(state);
  };
  child.on("error", (error) => finish(-1, error));
  child.on("close", (code) => finish(code ?? -1));
}

function drainCodexQueue(state) {
  const next = state.queue.shift();
  if (!next) {
    return;
  }
  runCodexInjection(state, next.reply, next.event);
}

function sendJson(response, statusCode, payload) {
  const body = JSON.stringify(payload);
  response.writeHead(statusCode, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
    "Cache-Control": "no-store",
  });
  response.end(body);
}

function readBody(request) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;

    request.on("data", (chunk) => {
      size += chunk.length;
      if (size > 1024 * 1024) {
        const error = new Error("请求体过大");
        error.statusCode = 413;
        reject(error);
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });

    request.on("end", () => {
      if (size === 0) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
      } catch {
        const error = new Error("请求体不是合法 JSON");
        error.statusCode = 400;
        reject(error);
      }
    });

    request.on("error", reject);
  });
}

function isLoopback(request) {
  const address = request.socket.remoteAddress || "";
  return (
    address === "127.0.0.1" ||
    address === "::1" ||
    address === "::ffff:127.0.0.1"
  );
}

// 只放行本机与局域网来源（RFC1918 / 链路本地 / IPv6 ULA），公网来源一律拒绝。
function isLocalOrLanAddress(rawAddress) {
  const address = (rawAddress || "").trim().toLowerCase();
  if (!address) {
    return false;
  }
  if (address === "::1") {
    return true;
  }

  const mapped = address.startsWith("::ffff:") ? address.slice(7) : address;
  const octets = mapped.split(".").map((part) => Number(part));
  if (
    octets.length === 4 &&
    octets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255)
  ) {
    const [a, b] = octets;
    return (
      a === 127 ||
      a === 10 ||
      (a === 192 && b === 168) ||
      (a === 172 && b >= 16 && b <= 31) ||
      (a === 169 && b === 254)
    );
  }

  // IPv6：链路本地 fe80::/10 与唯一本地地址 fc00::/7。
  if (/^fe[89ab]/.test(address) || address.startsWith("fc") || address.startsWith("fd")) {
    return true;
  }
  return false;
}

function sendApk(request, response) {
  let stat;
  try {
    stat = fs.statSync(apkPath);
  } catch {
    sendJson(response, 404, { error: "APK 尚未构建" });
    return;
  }
  if (!stat.isFile()) {
    sendJson(response, 404, { error: "APK 尚未构建" });
    return;
  }

  response.writeHead(200, {
    "Content-Type": "application/vnd.android.package-archive",
    "Content-Length": stat.size,
    "Content-Disposition": 'attachment; filename="agent-inbox.apk"',
    "Cache-Control": "no-store",
  });

  if (request.method === "HEAD") {
    response.end();
    return;
  }

  const stream = fs.createReadStream(apkPath);
  stream.on("error", () => response.destroy());
  response.on("close", () => stream.destroy());
  stream.pipe(response);
}

function hasValidToken(request) {
  const authorization = request.headers.authorization || "";
  const prefix = "Bearer ";
  if (authorization.startsWith(prefix)) {
    const supplied = Buffer.from(authorization.slice(prefix.length));
    const expected = Buffer.from(config.token);
    if (supplied.length === expected.length && crypto.timingSafeEqual(supplied, expected)) {
      return true;
    }
    // 手机配对后拿到的是设备令牌，按设备逐个可撤销；配置文件里的老 token 继续有效
    const token = authorization.slice(prefix.length).trim();
    if (token.startsWith("hm_")) {
      try {
        if (verifyDeviceToken(token)) {
          return true;
        }
      } catch {
        /* 设备库坏了不影响主 token 鉴权 */
      }
    }
  }

  // 控制台页面：本机打开的页面带一个 HttpOnly cookie，免去在页面里出现 token。
  // 只在回环地址上认这个 cookie，局域网来源一律要走 Bearer。
  if (isLoopback(request)) {
    const cookie = request.headers.cookie || "";
    const match = /(?:^|;\s*)homing_local=([^;]+)/.exec(cookie);
    if (match) {
      const supplied = Buffer.from(decodeURIComponent(match[1]));
      const expected = Buffer.from(config.token);
      if (supplied.length === expected.length && crypto.timingSafeEqual(supplied, expected)) {
        return true;
      }
    }
  }
  return false;
}

// Claude Code 与 WorkBuddy 的 hook 载荷结构一致（hook_event_name + session_id + cwd），
// 因此共用同一套映射：Stop → reply（取 last_assistant_message），
// Notification → permission（permission_prompt）/ needs_input。
async function handleStopHook(body, source) {
  const label = sourceLabels[source];
  const hookEventName = body.hook_event_name;

  // Claude / WorkBuddy 的钩子载荷带 transcript_path，按 session 记下来给 /api/sessions/<id>/transcript 用。
  recordTranscript(body.session_id, source, body.transcript_path, body.cwd);

  if (hookEventName === "Stop") {
    // 先照常推送事件，再决定是否挂起等待手机回复。
    const event = addEvent({
      source,
      kind: "reply",
      session_id: body.session_id,
      cwd: body.cwd,
      title: `${label} 回复`,
      text: body.last_assistant_message,
    });
    return await resumeFromPhone(event);
  }

  if (hookEventName === "PermissionRequest") {
    const event = addEvent({
      source,
      kind: "permission",
      session_id: body.session_id,
      cwd: body.cwd,
      title: `${label} 请求授权`,
      text:
        typeof body.tool_name === "string"
          ? `${body.tool_name}：${shortJson(body.tool_input || body.input || "", 400)}`
          : body.message || body.reason || "需要授权",
    });
    return await holdApproval(event);
  }

  if (hookEventName === "Notification") {
    const event = addEvent({
      source,
      kind:
        body.notification_type === "permission_prompt"
          ? "permission"
          : "needs_input",
      session_id: body.session_id,
      cwd: body.cwd,
      title:
        body.notification_type === "permission_prompt"
          ? `${label} 请求权限`
          : `${label} 等待输入`,
      text: body.message,
    });
    if (body.notification_type === "permission_prompt") {
      return await holdApproval(event);
    }
  }

  return {};
}

/* ------------------------------------------------------------------ *
 * 附件与能力链接（/f/<32位 token>/<相对路径>）
 * ------------------------------------------------------------------ */

const MAX_ATTACHMENT_BYTES = 50 * 1024 * 1024;
const MAX_UPLOAD_BYTES = 50 * 1024 * 1024;
const MAX_ATTACHMENTS_PER_EVENT = 20;
const CAPABILITY_TTL_MS = 7 * 24 * 60 * 60 * 1000;
const capabilitiesPath = path.join(dataDir, "capabilities.json");
const capabilities = new Map();
loadCapabilities();
setTimeout(refreshCapabilitiesForEvents, 0);
const transcriptPaths = new Map(); // session_id -> { source, path, cwd, ts }
const codexRolloutCache = new Map(); // thread_id -> { path, foundAt }

const MIME_BY_EXT = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  svg: "image/svg+xml",
  bmp: "image/bmp",
  ico: "image/x-icon",
  avif: "image/avif",
  html: "text/html; charset=utf-8",
  htm: "text/html; charset=utf-8",
  css: "text/css; charset=utf-8",
  js: "text/javascript; charset=utf-8",
  mjs: "text/javascript; charset=utf-8",
  json: "application/json; charset=utf-8",
  txt: "text/plain; charset=utf-8",
  md: "text/markdown; charset=utf-8",
  csv: "text/csv; charset=utf-8",
  xml: "application/xml; charset=utf-8",
  yml: "text/yaml; charset=utf-8",
  yaml: "text/yaml; charset=utf-8",
  pdf: "application/pdf",
  mp4: "video/mp4",
  webm: "video/webm",
  mp3: "audio/mpeg",
  wav: "audio/wav",
  zip: "application/zip",
  jsonl: "application/json; charset=utf-8",
};

const KIND_BY_EXT = {
  png: "image",
  jpg: "image",
  jpeg: "image",
  gif: "image",
  webp: "image",
  svg: "image",
  bmp: "image",
  ico: "image",
  avif: "image",
  html: "html",
  htm: "html",
  pdf: "pdf",
};

const BLOCKED_DIR_SEGMENTS = new Set([
  ".ssh",
  ".gnupg",
  ".aws",
  ".azure",
  ".kube",
  ".docker",
  "keychains",
]);

function extensionOf(filePath) {
  return path.extname(filePath).replace(/^\./, "").toLowerCase();
}

function mimeFor(filePath) {
  return MIME_BY_EXT[extensionOf(filePath)] || "application/octet-stream";
}

function kindFor(filePath) {
  return KIND_BY_EXT[extensionOf(filePath)] || "file";
}

function loadCapabilities() {
  let raw;
  try {
    raw = JSON.parse(fs.readFileSync(capabilitiesPath, "utf8"));
  } catch {
    return;
  }
  const now = Date.now();
  for (const [token, value] of Object.entries(raw || {})) {
    if (
      value &&
      typeof value.root === "string" &&
      typeof value.entry === "string" &&
      typeof value.expiresAt === "number" &&
      value.expiresAt > now
    ) {
      capabilities.set(token, value);
    }
  }
}

function saveCapabilities() {
  const payload = {};
  for (const [token, value] of capabilities) {
    payload[token] = value;
  }
  fs.writeFileSync(capabilitiesPath, `${JSON.stringify(payload, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
}

function createCapability(root, entry) {
  const now = Date.now();
  for (const [token, value] of capabilities) {
    if (value.root === root && value.entry === entry && value.expiresAt > now) {
      return token;
    }
  }
  const token = crypto.randomBytes(16).toString("hex");
  capabilities.set(token, {
    root,
    entry,
    expiresAt: now + CAPABILITY_TTL_MS,
    created: new Date(now).toISOString(),
  });
  saveCapabilities();
  return token;
}

function tokenFromCapabilityUrl(url) {
  const match = /^\/f\/([0-9a-fA-F]{32})\//.exec(typeof url === "string" ? url : "");
  return match ? match[1].toLowerCase() : null;
}

// 中继重启后 capability 表可能缺项（旧版本遗留），按正文重新生成，保证历史事件的 /f 链接仍可用。
function refreshCapabilitiesForEvents() {
  let changed = 0;
  for (const event of events) {
    if (!Array.isArray(event.attachments) || !event.attachments.length) {
      continue;
    }
    const missing = event.attachments.some((attachment) => {
      const token = tokenFromCapabilityUrl(attachment?.url);
      return !token || !capabilities.has(token);
    });
    if (!missing) {
      continue;
    }
    const rebuilt = buildAttachments(event.text, event.cwd);
    if (!rebuilt.length) {
      continue;
    }
    event.attachments = rebuilt;
    changed += 1;
  }
  if (changed) {
    const body = `${events.map((event) => JSON.stringify(event)).join("\n")}\n`;
    fs.writeFileSync(eventsPath, body, "utf8");
    console.log(`[capability] 重新生成 ${changed} 条事件的附件链接`);
  }
}

function capabilityIsValid(token) {
  if (typeof token !== "string" || !/^[0-9a-f]{32}$/i.test(token)) {
    return false;
  }
  const value = capabilities.get(token.toLowerCase());
  return Boolean(value && value.expiresAt > Date.now());
}

// 凭据位置与敏感文件名一律不收录。
function isBlockedFile(realPath) {
  const lower = realPath.toLowerCase();
  const base = path.basename(lower);
  if (/token|secret|key|credential/.test(base)) {
    return true;
  }
  const segments = lower.split(path.sep);
  for (const segment of segments) {
    if (BLOCKED_DIR_SEGMENTS.has(segment)) {
      return true;
    }
  }
  if (lower.endsWith(`${path.sep}.codex${path.sep}auth.json`)) {
    return true;
  }
  if (lower.includes(`${path.sep}.claude${path.sep}`) && /credential|auth|secret|token|key|\.env/.test(base)) {
    return true;
  }
  return false;
}

function allowedAttachmentRoots(cwd) {
  const roots = [];
  for (const candidate of [cwd, os.homedir()]) {
    if (typeof candidate !== "string" || !candidate.trim()) {
      continue;
    }
    try {
      const real = fs.realpathSync(candidate);
      if (!roots.includes(real)) {
        roots.push(real);
      }
    } catch {
      // cwd 已不存在时忽略
    }
  }
  return roots;
}

function extractPathCandidates(text) {
  const found = new Set();
  const push = (value) => {
    const trimmed = (value || "").trim().replace(/^[<("']+|[>)"'.,;:，。；：）]+$/g, "");
    if (trimmed) {
      found.add(trimmed);
    }
  };

  for (const match of text.matchAll(/!\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    push(match[1]);
  }
  for (const match of text.matchAll(/\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    push(match[1]);
  }
  for (const match of text.matchAll(/`([^`\n]{1,300})`/g)) {
    push(match[1]);
  }
  for (const match of text.matchAll(/(?:^|[\s（(：:"'`])((?:~|\.{1,2})?\/[^\s"'`()<>\[\]，。；、）]+)/g)) {
    push(match[1]);
  }
  for (const match of text.matchAll(
    /(?:^|[\s（(：:"'`])([A-Za-z0-9._-]+(?:\/[A-Za-z0-9._ -]+)*\.[A-Za-z0-9]{1,8})(?=$|[\s"'`)，。；、）])/g,
  )) {
    push(match[1]);
  }
  return [...found];
}

function expandPathCandidate(raw) {
  let value = (raw || "").trim();
  if (!value) {
    return null;
  }
  value = value.replace(/^<|>$/g, "");
  if (/^(https?:|data:|mailto:|javascript:|#|\/\/)/i.test(value)) {
    return null;
  }
  value = value.replace(/^file:\/\//i, "");
  value = value.split("#")[0].split("?")[0];
  if (!value) {
    return null;
  }
  if (value === "~" || value.startsWith("~/")) {
    value = path.join(os.homedir(), value.slice(1));
  }
  return value;
}

function resolveAttachmentFile(value, cwd) {
  const baseDir = cwd && cwd.trim() ? cwd : relayDir;
  const absolute = path.isAbsolute(value) ? value : path.join(baseDir, value);
  let real;
  try {
    real = fs.realpathSync(absolute);
  } catch {
    return null;
  }
  let stat;
  try {
    stat = fs.statSync(real);
  } catch {
    return null;
  }
  if (!stat.isFile() || stat.size > MAX_ATTACHMENT_BYTES) {
    return null;
  }
  const roots = allowedAttachmentRoots(cwd);
  if (!roots.some((root) => real === root || real.startsWith(`${root}${path.sep}`))) {
    return null;
  }
  if (isBlockedFile(real)) {
    return null;
  }
  return { real, size: stat.size };
}

function buildAttachments(text, cwd, limit = MAX_ATTACHMENTS_PER_EVENT) {
  if (typeof text !== "string" || !text.trim()) {
    return [];
  }
  const attachments = [];
  const seen = new Set();
  for (const candidate of extractPathCandidates(text)) {
    if (attachments.length >= limit) {
      break;
    }
    const expanded = expandPathCandidate(candidate);
    if (!expanded) {
      continue;
    }
    const resolved = resolveAttachmentFile(expanded, cwd);
    if (!resolved || seen.has(resolved.real)) {
      continue;
    }
    seen.add(resolved.real);
    const dir = path.dirname(resolved.real);
    const name = path.basename(resolved.real);
    const token = createCapability(dir, name);
    attachments.push({
      id: crypto.randomBytes(8).toString("hex"),
      name,
      mime: mimeFor(name),
      size: resolved.size,
      kind: kindFor(name),
      url: `/f/${token}/${encodeURIComponent(name)}`,
    });
  }
  return attachments;
}

function localhostLinksIn(text) {
  const ports = new Set();
  const value = typeof text === "string" ? text : "";
  for (const match of value.matchAll(/(?:https?:\/\/)?(?:localhost|127\.0\.0\.1|\[::1\]):(\d{2,5})/gi)) {
    const port = Number(match[1]);
    if (Number.isInteger(port) && port > 0 && port < 65536) {
      ports.add(port);
    }
  }
  return [...ports].sort((a, b) => a - b);
}

function sendFileWithRange(request, response, filePath) {
  let stat;
  try {
    stat = fs.statSync(filePath);
  } catch {
    sendJson(response, 404, { error: "文件不存在" });
    return;
  }
  if (!stat.isFile()) {
    sendJson(response, 404, { error: "不是普通文件" });
    return;
  }

  const total = stat.size;
  const headers = {
    "Content-Type": mimeFor(filePath),
    "Accept-Ranges": "bytes",
    "Cache-Control": "private, max-age=60",
    "Content-Length": total,
  };

  let start = 0;
  let end = total === 0 ? 0 : total - 1;
  let status = 200;

  const rangeHeader = request.headers.range;
  if (typeof rangeHeader === "string") {
    const match = /^bytes=(\d*)-(\d*)$/.exec(rangeHeader.trim());
    if (match) {
      const hasStart = match[1] !== "";
      const hasEnd = match[2] !== "";
      if (hasStart) {
        start = Number(match[1]);
        end = hasEnd ? Math.min(Number(match[2]), total - 1) : total - 1;
      } else if (hasEnd) {
        start = Math.max(0, total - Number(match[2]));
        end = total - 1;
      }
      if (
        !Number.isFinite(start) ||
        !Number.isFinite(end) ||
        start > end ||
        start >= total
      ) {
        response.writeHead(416, { "Content-Range": `bytes */${total}` });
        response.end();
        return;
      }
      status = 206;
      headers["Content-Range"] = `bytes ${start}-${end}/${total}`;
      headers["Content-Length"] = end - start + 1;
    }
  }

  response.writeHead(status, headers);
  if (request.method === "HEAD" || total === 0) {
    response.end();
    return;
  }
  const stream = fs.createReadStream(filePath, { start, end });
  stream.on("error", () => response.destroy());
  response.on("close", () => stream.destroy());
  stream.pipe(response);
}

function serveCapability(request, response, url) {
  if (request.method !== "GET" && request.method !== "HEAD") {
    sendJson(response, 405, { error: "Method Not Allowed" });
    return;
  }
  if (!isLocalOrLanAddress(request.socket.remoteAddress)) {
    sendJson(response, 403, { error: "Forbidden" });
    return;
  }

  const match = /^\/f\/([0-9a-fA-F]{32})(?:\/(.*))?$/.exec(url.pathname);
  if (!match) {
    sendJson(response, 404, { error: "Not Found" });
    return;
  }

  const token = match[1].toLowerCase();
  const capability = capabilities.get(token);
  if (!capability || capability.expiresAt <= Date.now()) {
    if (capability) {
      capabilities.delete(token);
      saveCapabilities();
    }
    sendJson(response, 404, { error: "链接已失效" });
    return;
  }

  let relative = "";
  try {
    relative = decodeURIComponent(match[2] || "");
  } catch {
    sendJson(response, 400, { error: "路径编码非法" });
    return;
  }
  if (relative.includes("\0")) {
    sendJson(response, 400, { error: "路径非法" });
    return;
  }
  if (relative === "") {
    relative = capability.entry;
  }

  const segments = relative.split("/").filter((segment) => segment !== "" && segment !== ".");
  if (segments.some((segment) => segment === ".." || segment.includes("\\"))) {
    sendJson(response, 403, { error: "禁止路径穿越" });
    return;
  }

  const target = path.resolve(capability.root, segments.join("/"));
  if (target !== capability.root && !target.startsWith(`${capability.root}${path.sep}`)) {
    sendJson(response, 403, { error: "禁止路径穿越" });
    return;
  }

  let real;
  try {
    real = fs.realpathSync(target);
  } catch {
    sendJson(response, 404, { error: "文件不存在" });
    return;
  }
  if (real !== capability.root && !real.startsWith(`${capability.root}${path.sep}`)) {
    sendJson(response, 403, { error: "禁止路径穿越" });
    return;
  }

  let stat;
  try {
    stat = fs.statSync(real);
  } catch {
    sendJson(response, 404, { error: "文件不存在" });
    return;
  }
  if (stat.isDirectory()) {
    const indexFile = path.join(real, "index.html");
    if (fs.existsSync(indexFile)) {
      sendFileWithRange(request, response, indexFile);
      return;
    }
    sendJson(response, 404, { error: "目录没有 index.html" });
    return;
  }

  sendFileWithRange(request, response, real);
}

/* ------------------------------------------------------------------ *
 * 上传
 * ------------------------------------------------------------------ */

function sanitizeFileName(raw) {
  const base = path.basename((raw || "").replace(/\\/g, "/")).replace(/[\u0000-\u001f]/g, "");
  const cleaned = base.replace(/[^\p{L}\p{N}._-]+/gu, "_").replace(/^\.+/, "").slice(0, 120);
  return cleaned || "";
}

function timestampSlug() {
  const now = new Date();
  const pad = (value) => String(value).padStart(2, "0");
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
}

function uploadDirFor(event) {
  const cwd = typeof event?.cwd === "string" ? event.cwd.trim() : "";
  if (cwd) {
    try {
      if (fs.statSync(cwd).isDirectory()) {
        fs.accessSync(cwd, fs.constants.W_OK);
        return path.join(cwd, ".agent-inbox", "uploads");
      }
    } catch {
      // cwd 不可写，回落到主目录
    }
  }
  return path.join(os.homedir(), "AgentInbox", "uploads");
}

const clientLogPath = path.join(dataDir, "client-crash.jsonl");
const CLIENT_LOG_LIMIT = 256 * 1024;

function readRawBody(request, limit) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    let failed = false;
    request.on("data", (chunk) => {
      if (failed) {
        return;
      }
      size += chunk.length;
      if (size > limit) {
        failed = true;
        const error = new Error("请求体过大");
        error.statusCode = 413;
        reject(error);
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => {
      if (!failed) {
        resolve(Buffer.concat(chunks));
      }
    });
    request.on("error", (error) => {
      if (!failed) {
        failed = true;
        reject(error);
      }
    });
  });
}

function firstStackLine(stack) {
  const text = typeof stack === "string" ? stack.trim() : "";
  const line = text.split("\n").find((item) => item.trim()) || "";
  return clip(line.trim(), 200);
}

async function handleClientLog(request, response, url) {
  if (request.method === "POST") {
    let raw;
    try {
      raw = await readRawBody(request, CLIENT_LOG_LIMIT);
    } catch (error) {
      sendJson(response, error.statusCode || 400, { error: error.message });
      return;
    }

    let body;
    try {
      body = JSON.parse(raw.toString("utf8"));
    } catch {
      sendJson(response, 400, { error: "请求体不是合法 JSON" });
      return;
    }
    if (!body || typeof body !== "object") {
      sendJson(response, 400, { error: "请求体必须是 JSON 对象" });
      return;
    }

    const record = {
      received_at: new Date().toISOString(),
      app_version: typeof body.app_version === "string" ? body.app_version.slice(0, 64) : null,
      ts: typeof body.ts === "string" ? body.ts.slice(0, 64) : null,
      session_id: typeof body.session_id === "string" ? body.session_id.slice(0, 128) : null,
      device: typeof body.device === "string" ? body.device.slice(0, 200) : null,
      stack: typeof body.stack === "string" ? body.stack.slice(0, CLIENT_LOG_LIMIT) : "",
      bytes: raw.length,
      remote: request.socket.remoteAddress || null,
    };
    record.exception = firstStackLine(record.stack);

    appendJsonLine(clientLogPath, record);
    console.log(
      `[client-log] v=${record.app_version || "-"} ts=${record.ts || "-"} session=${record.session_id || "-"} bytes=${record.bytes} exception=${record.exception || "-"}`,
    );
    sendJson(response, 200, { ok: true, saved: true, bytes: record.bytes, exception: record.exception });
    return;
  }

  if (request.method === "GET") {
    const limitValue = Number(url.searchParams.get("limit") || "5");
    const limit = Number.isFinite(limitValue) ? Math.min(Math.max(Math.trunc(limitValue), 1), 50) : 5;
    const all = readJsonLines(clientLogPath);
    const entries = all.slice(-limit);
    sendJson(response, 200, { count: entries.length, total: all.length, entries });
    return;
  }

  sendJson(response, 405, { error: "Method Not Allowed" });
}

async function handleUpload(request, response, url) {
  const eventIdRaw = url.searchParams.get("event_id");
  const cwdParam = (url.searchParams.get("cwd") || "").trim();
  const name = sanitizeFileName(url.searchParams.get("name") || "upload.bin");
  if (!name) {
    sendJson(response, 400, { error: "文件名非法" });
    return;
  }

  let target = null;

  if (eventIdRaw !== null && eventIdRaw !== "") {
    const eventId = Number(eventIdRaw);
    if (!Number.isSafeInteger(eventId) || eventId <= 0) {
      sendJson(response, 400, { error: "event_id 非法" });
      return;
    }
    target = events.find((item) => item.id === eventId);
    if (!target) {
      sendJson(response, 404, { error: "事件不存在" });
      return;
    }
  } else if (cwdParam) {
    // 新建任务场景：还没有事件，按 cwd 落盘；只允许 /api/projects 里出现过的路径。
    const normalized = path.resolve(cwdParam);
    const allowed = listProjects().some((project) => path.resolve(project.cwd) === normalized);
    if (!allowed) {
      sendJson(response, 403, { error: "cwd 不在 /api/projects 列表内" });
      return;
    }
    let stat;
    try {
      stat = fs.statSync(normalized);
    } catch {
      sendJson(response, 400, { error: "cwd 不存在" });
      return;
    }
    if (!stat.isDirectory()) {
      sendJson(response, 400, { error: "cwd 不是目录" });
      return;
    }
    target = { cwd: normalized };
  } else {
    sendJson(response, 400, { error: "event_id 或 cwd 必须提供一个" });
    return;
  }

  const dir = uploadDirFor(target);
  fs.mkdirSync(dir, { recursive: true });

  const stampedName = `${timestampSlug()}-${name}`;
  const finalPath = path.join(dir, stampedName);
  const tempPath = `${finalPath}.part`;
  const out = fs.createWriteStream(tempPath);

  let size = 0;
  let overflow = false;
  let aborted = false;
  let writeFailed = false;

  // WriteStream 的 error 必须被消费，否则会在超限/中断时抛到进程顶层。
  out.on("error", () => {
    writeFailed = true;
  });

  await new Promise((resolve) => {
    request.on("data", (chunk) => {
      if (overflow || aborted || writeFailed) {
        return;
      }
      size += chunk.length;
      if (size > MAX_UPLOAD_BYTES) {
        overflow = true;
        resolve();
        return;
      }
      try {
        out.write(chunk);
      } catch {
        writeFailed = true;
        resolve();
      }
    });
    request.on("end", () => {
      if (overflow || aborted || writeFailed) {
        resolve();
        return;
      }
      out.end(() => resolve());
    });
    request.on("error", () => {
      aborted = true;
      resolve();
    });
    request.on("aborted", () => {
      aborted = true;
      resolve();
    });
  });

  if (overflow || aborted || writeFailed) {
    try {
      out.destroy();
    } catch {
      // 忽略
    }
    try {
      fs.unlinkSync(tempPath);
    } catch {
      // 忽略
    }
    // 让剩下的请求体被读掉，避免连接卡住。
    request.resume();
    if (overflow) {
      sendJson(response, 413, { error: "文件超过 50MB 上限" });
    } else if (!response.destroyed) {
      sendJson(response, 400, { error: "上传被中断" });
    }
    return;
  }

  try {
    fs.renameSync(tempPath, finalPath);
  } catch (error) {
    sendJson(response, 500, { error: `保存失败：${error.message}` });
    return;
  }

  sendJson(response, 200, {
    ok: true,
    path: finalPath,
    name: stampedName,
    size,
    mime: mimeFor(stampedName),
  });
}

/* ------------------------------------------------------------------ *
 * 完整对话（transcript）
 * ------------------------------------------------------------------ */

function recordTranscript(sessionId, source, transcriptPath, cwd) {
  if (typeof sessionId !== "string" || !sessionId.trim()) {
    return;
  }
  if (typeof transcriptPath !== "string" || !transcriptPath.trim()) {
    return;
  }
  transcriptPaths.set(sessionId, {
    source,
    path: transcriptPath,
    cwd: typeof cwd === "string" ? cwd : "",
    ts: new Date().toISOString(),
  });
}

function findCodexRollout(threadId) {
  if (typeof threadId !== "string" || !threadId.trim()) {
    return null;
  }
  const cached = codexRolloutCache.get(threadId);
  if (cached && Date.now() - cached.foundAt < 30_000 && fs.existsSync(cached.path)) {
    return cached.path;
  }

  const base = path.join(os.homedir(), ".codex", "sessions");
  let found = null;
  let foundMtime = 0;

  const walk = (dir, depth) => {
    if (depth > 4) {
      return;
    }
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full, depth + 1);
      } else if (entry.name.startsWith("rollout-") && entry.name.includes(threadId)) {
        try {
          const stat = fs.statSync(full);
          if (stat.mtimeMs > foundMtime) {
            foundMtime = stat.mtimeMs;
            found = full;
          }
        } catch {
          // 忽略
        }
      }
    }
  };

  walk(base, 0);
  if (found) {
    codexRolloutCache.set(threadId, { path: found, foundAt: Date.now() });
  }
  return found;
}

function normalizeContent(content) {
  if (typeof content === "string") {
    return content;
  }
  if (!Array.isArray(content)) {
    return "";
  }
  const parts = [];
  for (const block of content) {
    if (typeof block === "string") {
      parts.push(block);
      continue;
    }
    if (!block || typeof block !== "object") {
      continue;
    }
    if (
      (block.type === "text" || block.type === "input_text" || block.type === "output_text") &&
      typeof block.text === "string"
    ) {
      parts.push(block.text);
    }
  }
  return parts.join("\n");
}

const NOISE_PATTERN = /^(<skills_instructions>|<environment_context>|# AGENTS\.md instructions|<user_instructions>|<permissions|<INSTRUCTIONS|<environment|<tools>|<multi_agent_mode>|<system-reminder|<command-name|<command-message|<local-command)/i;

function parseTranscript(filePath, source, cwd) {
  const rows = readJsonLines(filePath);
  const messages = [];
  const seen = new Set();

  for (const row of rows) {
    if (!row || typeof row !== "object") {
      continue;
    }
    let role = null;
    let text = "";
    let ts = typeof row.timestamp === "string" ? row.timestamp : "";

    const payload = row.payload && typeof row.payload === "object" ? row.payload : null;
    if (payload && payload.type === "message") {
      role = payload.role === "assistant" ? "assistant" : payload.role === "user" ? "user" : null;
      text = normalizeContent(payload.content);
    } else if (row.type === "user" || row.type === "assistant") {
      role = row.type;
      text = normalizeContent(row.message?.content ?? row.content);
    } else if (row.type === "message" && (row.role || row.message?.role)) {
      const rawRole = row.role || row.message?.role;
      role = rawRole === "assistant" ? "assistant" : rawRole === "user" ? "user" : null;
      text = normalizeContent(row.content ?? row.message?.content);
    }

    if (!role) {
      continue;
    }
    const trimmed = text.trim();
    if (!trimmed) {
      continue;
    }
    if (role === "user" && NOISE_PATTERN.test(trimmed)) {
      continue;
    }

    // Codex 每次 resume 都会重发整段上下文，按 (role, text) 去重只保留首次出现的版本。
    const dedupeKey = `${role}\u0000${trimmed}`;
    if (seen.has(dedupeKey)) {
      continue;
    }
    seen.add(dedupeKey);

    messages.push({
      role,
      text: trimmed,
      ts,
      attachments: buildAttachments(trimmed, cwd, 10),
    });
  }

  return messages;
}

/* ------------------------------------------------------------------ *
 * 项目列表与新会话
 * ------------------------------------------------------------------ */

function listProjects() {
  const byCwd = new Map();
  for (const event of events) {
    const cwd = typeof event.cwd === "string" ? event.cwd.trim() : "";
    if (!cwd) {
      continue;
    }
    const entry = byCwd.get(cwd) || {
      cwd,
      project: event.project || projectFromCwd(cwd),
      count: 0,
      last_ts: "",
      sources: new Set(),
    };
    entry.count += 1;
    if (typeof event.source === "string") {
      entry.sources.add(event.source);
    }
    if (!entry.last_ts || String(event.ts) > entry.last_ts) {
      entry.last_ts = String(event.ts);
    }
    byCwd.set(cwd, entry);
  }
  return [...byCwd.values()]
    .sort((a, b) => (a.last_ts < b.last_ts ? 1 : -1))
    .map((entry) => ({
      cwd: entry.cwd,
      project: entry.project,
      count: entry.count,
      last_ts: entry.last_ts,
      sources: [...entry.sources],
    }));
}

function resolveBinaryPath(name, extraCandidates = []) {
  const candidates = [
    ...extraCandidates,
    path.join(os.homedir(), ".npm-global/bin", name),
    path.join(os.homedir(), ".local/bin", name),
    `/opt/homebrew/bin/${name}`,
    `/usr/local/bin/${name}`,
    `/usr/bin/${name}`,
  ];
  for (const candidate of candidates) {
    try {
      const stat = fs.statSync(candidate);
      if (stat.isFile()) {
        return candidate;
      }
    } catch {
      // 继续
    }
  }
  try {
    const output = execFileSync("/usr/bin/env", ["sh", "-c", `command -v ${name}`], {
      timeout: 3000,
      env: {
        ...process.env,
        PATH: `${process.env.PATH || ""}:/opt/homebrew/bin:/usr/local/bin:${path.join(os.homedir(), ".npm-global/bin")}`,
      },
    })
      .toString("utf8")
      .trim();
    return output || null;
  } catch {
    return null;
  }
}

function commandExists(name) {
  return resolveBinaryPath(name);
}

function startCodexSession(cwd, text) {
  return new Promise((resolve) => {
    const args = ["exec", "--json", "--skip-git-repo-check", "-C", cwd, text];
    let child;
    try {
      child = spawn(codexBin, args, {
        cwd,
        stdio: ["ignore", "pipe", "pipe"],
        env: process.env,
      });
    } catch (error) {
      resolve({ ok: false, error: `无法启动 codex：${error.message}` });
      return;
    }

    let buffer = "";
    let settled = false;
    const finish = (value) => {
      if (settled) {
        return;
      }
      settled = true;
      resolve(value);
    };

    const timer = setTimeout(() => finish({ ok: false, error: "启动 Codex 会话超时（未收到 thread id）" }), 25_000);

    child.stdout.on("data", (chunk) => {
      buffer += chunk.toString("utf8");
      let index;
      while ((index = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, index).trim();
        buffer = buffer.slice(index + 1);
        if (!line) {
          continue;
        }
        try {
          const payload = JSON.parse(line);
          if (payload.type === "thread.started" && payload.thread_id) {
            launchedProcesses.set(payload.thread_id, child);
            child.on("close", () => {
              if (launchedProcesses.get(payload.thread_id) === child) {
                launchedProcesses.delete(payload.thread_id);
              }
            });
            clearTimeout(timer);
            finish({ ok: true, session_id: payload.thread_id, status: "started" });
          }
        } catch {
          // 非 JSON 行忽略
        }
      }
    });
    child.stderr.on("data", () => {});
    child.on("error", (error) => {
      clearTimeout(timer);
      finish({ ok: false, error: error.message });
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      finish({ ok: code === 0, error: code === 0 ? "" : `codex 退出码 ${code}` });
    });
  });
}

function workbuddyAcpPorts() {
  let pids;
  try {
    pids = execFileSync("pgrep", ["-f", "WorkBuddy.app/Contents/MacOS/Electron"], { timeout: 3000 })
      .toString("utf8")
      .trim()
      .split("\n")
      .filter(Boolean);
  } catch {
    return [];
  }
  if (!pids.length) {
    return [];
  }
  let output;
  try {
    output = execFileSync(
      "lsof",
      ["-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-p", pids.join(",")],
      { timeout: 5000 },
    ).toString("utf8");
  } catch {
    return [];
  }
  const ports = new Set();
  for (const line of output.split("\n")) {
    const match = /127\.0\.0\.1:(\d+) \(LISTEN\)/.exec(line);
    if (match) {
      ports.add(Number(match[1]));
    }
  }
  return [...ports];
}

function httpJsonPost(port, pathname, body, headers = {}, timeout = 4000) {
  return new Promise((resolve) => {
    const data = Buffer.from(JSON.stringify(body));
    const request = httpRequest(
      {
        host: "127.0.0.1",
        port,
        path: pathname,
        method: "POST",
        headers: {
          "content-type": "application/json",
          "content-length": data.length,
          ...headers,
        },
        timeout,
      },
      (upstream) => {
        const chunks = [];
        upstream.on("data", (chunk) => chunks.push(chunk));
        upstream.on("end", () =>
          resolve({ status: upstream.statusCode || 0, body: Buffer.concat(chunks).toString("utf8") }),
        );
      },
    );
    request.on("timeout", () => {
      request.destroy();
      resolve({ status: 0, body: "" });
    });
    request.on("error", () => resolve({ status: 0, body: "" }));
    request.end(data);
  });
}

function acpCall(port, connectionId, payload, { timeout = 8000, waitForId = true } = {}) {
  return new Promise((resolve) => {
    const data = Buffer.from(JSON.stringify(payload));
    let done = false;
    const finish = (value) => {
      if (done) {
        return;
      }
      done = true;
      resolve(value);
    };

    const request = httpRequest(
      {
        host: "127.0.0.1",
        port,
        path: "/api/v1/acp",
        method: "POST",
        headers: {
          "content-type": "application/json",
          accept: "application/json, text/event-stream",
          "acp-connection-id": connectionId,
          "content-length": data.length,
        },
        timeout,
      },
      (upstream) => {
        let buffer = "";
        upstream.on("data", (chunk) => {
          buffer += chunk.toString("utf8");
          let index;
          while ((index = buffer.indexOf("\n")) >= 0) {
            const line = buffer.slice(0, index).trim();
            buffer = buffer.slice(index + 1);
            if (!line.startsWith("data:")) {
              continue;
            }
            let message;
            try {
              message = JSON.parse(line.slice(5).trim());
            } catch {
              continue;
            }
            if (waitForId && message.id === payload.id) {
              if (message.result) {
                finish({ ok: true, result: message.result });
              } else if (message.error) {
                finish({ ok: false, error: message.error.message || "ACP error" });
              }
              return;
            }
            if (!waitForId && message.method === "session/update" && message.params?.sessionId) {
              finish({ ok: true, result: { sessionId: message.params.sessionId } });
            }
          }
        });
        upstream.on("end", () => finish({ ok: false, error: "ACP 连接结束" }));
        upstream.on("error", () => finish({ ok: false, error: "ACP 连接错误" }));
      },
    );

    request.on("timeout", () => {
      request.destroy();
      finish({ ok: false, error: "ACP 超时" });
    });
    request.on("error", (error) => finish({ ok: false, error: error.message }));
    request.end(data);
  });
}

// session/new 的 sessionId 必须取 JSON-RPC 响应体，不能拿广播里的 session/update
// （update 可能是桌面端当前会话的，会把 id 认错）。
function acpSessionNew(port, connectionId, cwd, timeout = 25_000) {
  return new Promise((resolve) => {
    const payload = {
      jsonrpc: "2.0",
      id: 2,
      method: "session/new",
      params: { cwd, mcpServers: [] },
    };
    const data = Buffer.from(JSON.stringify(payload));
    let done = false;
    let lastUpdateSession = null;

    const finish = (value) => {
      if (done) {
        return;
      }
      done = true;
      resolve(value);
    };

    const request = httpRequest(
      {
        host: "127.0.0.1",
        port,
        path: "/api/v1/acp",
        method: "POST",
        headers: {
          "content-type": "application/json",
          accept: "application/json, text/event-stream",
          "acp-connection-id": connectionId,
          "content-length": data.length,
        },
        timeout,
      },
      (upstream) => {
        let buffer = "";
        upstream.on("data", (chunk) => {
          buffer += chunk.toString("utf8");
          let index;
          while ((index = buffer.indexOf("\n")) >= 0) {
            const line = buffer.slice(0, index).trim();
            buffer = buffer.slice(index + 1);
            if (!line.startsWith("data:")) {
              continue;
            }
            let message;
            try {
              message = JSON.parse(line.slice(5).trim());
            } catch {
              continue;
            }
            if (message.method === "session/update" && message.params?.sessionId) {
              lastUpdateSession = message.params.sessionId;
            }
            if (message.id === payload.id) {
              if (message.error) {
                finish({ ok: false, error: message.error.message || "ACP error" });
                return;
              }
              const result = message.result || {};
              const sessionId =
                result.sessionId || result.session_id || result.session?.id || lastUpdateSession;
              finish(
                sessionId
                  ? { ok: true, sessionId }
                  : { ok: false, error: "session/new 未返回 sessionId" },
              );
              return;
            }
          }
        });
        upstream.on("end", () =>
          finish(
            lastUpdateSession
              ? { ok: true, sessionId: lastUpdateSession }
              : { ok: false, error: "ACP 连接结束" },
          ),
        );
        upstream.on("error", () => finish({ ok: false, error: "ACP 连接错误" }));
      },
    );

    request.on("timeout", () => {
      request.destroy();
      finish(
        lastUpdateSession
          ? { ok: true, sessionId: lastUpdateSession }
          : { ok: false, error: "ACP 超时" },
      );
    });
    request.on("error", (error) => finish({ ok: false, error: error.message }));
    request.end(data);
  });
}

function acpFire(port, connectionId, payload) {
  try {
    const data = Buffer.from(JSON.stringify(payload));
    const request = httpRequest({
      host: "127.0.0.1",
      port,
      path: "/api/v1/acp",
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "application/json, text/event-stream",
        "acp-connection-id": connectionId,
        "content-length": data.length,
      },
      timeout: 2500,
    });
    request.on("response", (upstream) => upstream.resume());
    request.on("timeout", () => request.destroy());
    request.on("error", () => {});
    request.end(data);
  } catch {
    // 投递失败不影响主流程
  }
}

async function startWorkbuddySession(cwd, text) {
  const ports = workbuddyAcpPorts();
  if (!ports.length) {
    return { ok: false, error: "WorkBuddy 未运行，没有可用的 ACP 端口" };
  }

  for (const port of ports) {
    const connected = await httpJsonPost(
      port,
      "/api/v1/acp/connect",
      { clientInfo: { name: "agent-inbox", version: "0.1" } },
      {},
      3000,
    );
    if (connected.status !== 200) {
      continue;
    }
    let connectionId = null;
    try {
      connectionId = JSON.parse(connected.body).connectionId || null;
    } catch {
      connectionId = null;
    }
    if (!connectionId) {
      continue;
    }

    const initialized = await acpCall(port, connectionId, {
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: { protocolVersion: 1, clientInfo: { name: "agent-inbox", version: "0.1" } },
    });
    if (!initialized.ok) {
      continue;
    }

    const created = await acpSessionNew(port, connectionId, cwd);
    const sessionId = created.ok ? created.sessionId : null;
    if (!sessionId) {
      continue;
    }

    workbuddyAcpSessions.set(sessionId, { port, connectionId });
    acpFire(port, connectionId, {
      jsonrpc: "2.0",
      id: 3,
      method: "session/prompt",
      params: { sessionId, prompt: [{ type: "text", text }] },
    });

    // 该版本 WorkBuddy 的 ACP 是会话级的：session/new 返回的是当前活跃会话，
    // cwd 会被忽略，所以不能声称“新建成功”。
    return {
      ok: true,
      session_id: sessionId,
      status: "reused_active",
      warning: "WorkBuddy 当前版本不能新建会话，已发到当前活跃会话",
      port,
    };
  }

  return { ok: false, error: "未能建立 WorkBuddy ACP 连接" };
}

function startClaudeSession(cwd, text) {
  const binary = commandExists("claude") || resolveBinaryPath("claude");
  if (!binary) {
    return { ok: false, error: "本机没有 claude 命令" };
  }
  try {
    const child = spawn(binary, ["-p", text], {
      cwd,
      stdio: "ignore",
      env: process.env,
      detached: true,
    });
    child.unref();
    return { ok: true, session_id: null, status: "started", binary };
  } catch (error) {
    return { ok: false, error: error.message };
  }
}

/* ------------------------------------------------------------------ *
 * 本机端口代理（默认关闭）
 * ------------------------------------------------------------------ */

function proxyAuthorized(request, url) {
  if (hasValidToken(request)) {
    return true;
  }
  const cap = url.searchParams.get("cap") || url.searchParams.get("token");
  return capabilityIsValid(cap || "");
}

function handleProxy(request, response, port, restPath, url) {
  if (!config.proxy.enabled) {
    sendJson(response, 404, { error: "代理未开启" });
    return;
  }
  if (!config.proxy.ports.includes(port)) {
    sendJson(response, 403, { error: "端口不在白名单" });
    return;
  }
  if (!proxyAuthorized(request, url)) {
    sendJson(response, 401, { error: "Unauthorized" });
    return;
  }

  const headers = { ...request.headers };
  delete headers.authorization;
  headers.host = `127.0.0.1:${port}`;
  headers["x-forwarded-for"] = request.socket.remoteAddress || "";
  headers["x-forwarded-port"] = String(port);

  const upstream = httpRequest(
    {
      host: "127.0.0.1",
      port,
      method: request.method,
      path: `${restPath || "/"}${url.search || ""}`,
      headers,
      timeout: 30_000,
    },
    (target) => {
      const responseHeaders = { ...target.headers };
      delete responseHeaders["transfer-encoding"];
      response.writeHead(target.statusCode || 502, responseHeaders);
      target.pipe(response);
    },
  );

  upstream.on("timeout", () => {
    upstream.destroy();
    if (!response.headersSent) {
      sendJson(response, 504, { error: "上游超时" });
    }
  });
  upstream.on("error", (error) => {
    if (!response.headersSent) {
      sendJson(response, 502, { error: `上游连接失败：${error.message}` });
    } else {
      response.destroy();
    }
  });

  request.pipe(upstream);
}

// 三家实际可用的模型清单 + 能力矩阵（只读原生元数据）
const agentsCache = { at: 0, value: null };
const CODEX_EFFORTS = ["none", "minimal", "low", "medium", "high", "xhigh", "ultra", "max"];

function providerOfModelId(modelId) {
  if (typeof modelId !== "string" || !modelId) {
    return null;
  }
  const slash = modelId.indexOf("/");
  return slash > 0 ? modelId.slice(0, slash) : null;
}

function modelLabel(modelId) {
  const base = String(modelId || "").split("/").pop() || String(modelId || "");
  return base
    .split(/[-_.]+/)
    .filter(Boolean)
    .map((word) => (word.length <= 3 ? word.toUpperCase() : word.charAt(0).toUpperCase() + word.slice(1)))
    .join(" ");
}

function modelEntry(id, provider, isDefault, efforts = [], fallbackProvider = null) {
  return {
    id,
    label: modelLabel(id),
    provider: provider || providerOfModelId(id) || fallbackProvider || "unknown",
    is_default: Boolean(isDefault),
    default: Boolean(isDefault),
    efforts,
  };
}

function hookConfigured(filePath, urlFragment, eventName) {
  try {
    const text = fs.readFileSync(filePath, "utf8");
    return text.includes(urlFragment) && text.includes(eventName);
  } catch {
    return false;
  }
}

function codexModelList() {
  const collected = [];
  const seen = new Set();
  let defaultModel = null;
  let configText = "";
  try {
    configText = fs.readFileSync(path.join(os.homedir(), ".codex", "config.toml"), "utf8");
  } catch {
    configText = "";
  }
  const modelMatch = /^model\s*=\s*"([^"]+)"/m.exec(configText);
  if (modelMatch) {
    defaultModel = modelMatch[1];
  }
  const add = (id) => {
    if (typeof id !== "string" || !id || seen.has(id)) {
      return;
    }
    seen.add(id);
    collected.push(id);
  };
  add(defaultModel);
  let scanned = 0;
  for (const file of codexRolloutIndex().values()) {
    if (scanned >= 20) {
      break;
    }
    scanned += 1;
    try {
      add(codexRolloutMeta(file)?.model);
    } catch {
      // 忽略
    }
  }
  if (collected.length && !collected.includes(defaultModel)) {
    defaultModel = collected[0];
  }
  return {
    models: collected.map((id) => modelEntry(id, null, id === defaultModel, CODEX_EFFORTS, "openai")),
    defaultModel,
  };
}

function claudeModelList() {
  const collected = [];
  const seen = new Set();
  let defaultModel = null;
  for (const entry of claudeSessionFiles().slice(0, 30)) {
    try {
      const model = claudeFileMeta(entry)?.model;
      if (!model || model === "<synthetic>" || seen.has(model)) {
        continue;
      }
      seen.add(model);
      collected.push(model);
      if (!defaultModel) {
        defaultModel = model;
      }
    } catch {
      // 忽略
    }
  }
  return {
    models: collected.map((id) => modelEntry(id, "anthropic", id === defaultModel, [])),
    defaultModel,
  };
}

function workbuddyModelList() {
  const collected = [];
  const seen = new Set();
  const projects = path.join(os.homedir(), ".workbuddy", "projects");
  let files = [];
  try {
    for (const dir of fs.readdirSync(projects, { withFileTypes: true })) {
      if (!dir.isDirectory()) {
        continue;
      }
      const dirPath = path.join(projects, dir.name);
      for (const file of fs.readdirSync(dirPath)) {
        if (!file.endsWith(".jsonl")) {
          continue;
        }
        const full = path.join(dirPath, file);
        files.push({ path: full, mtimeMs: fs.statSync(full).mtimeMs });
      }
    }
  } catch {
    files = [];
  }
  files.sort((a, b) => b.mtimeMs - a.mtimeMs);
  for (const entry of files.slice(0, 15)) {
    let chunk = "";
    try {
      const stat = fs.statSync(entry.path);
      chunk = readFileSlice(entry.path, Math.max(0, stat.size - 262144), Math.min(stat.size, 262144));
    } catch {
      continue;
    }
    for (const match of chunk.matchAll(/"requestModelId"\s*:\s*"([^"]+)"/g)) {
      const id = match[1];
      if (!id || seen.has(id)) {
        continue;
      }
      seen.add(id);
      collected.push(id);
    }
    if (collected.length >= 12) {
      break;
    }
  }
  const defaultModel = collected[0] || null;
  return {
    models: collected.map((id) => modelEntry(id, providerOfModelId(id) || "workbuddy", id === defaultModel, [])),
    defaultModel,
  };
}

function workbuddyRunning() {
  try {
    execFileSync("pgrep", ["-f", "WorkBuddy.app/Contents/MacOS/Electron"], { timeout: 1500 });
    return true;
  } catch {
    return false;
  }
}

function listAgents() {
  if (agentsCache.value && Date.now() - agentsCache.at < 60_000) {
    return agentsCache.value;
  }

  const codex = codexModelList();
  const claude = claudeModelList();
  const workbuddy = workbuddyModelList();
  const claudeBin = commandExists("claude");
  const codexBinAvailable = Boolean(codexBin || commandExists("codex"));

  const agents = [
    {
      source: "codex",
      display_name: "Codex",
      available: codexBinAvailable,
      capabilities: {
        reply: true,
        new_session: codexBinAvailable,
        new_session_ready: codexBinAvailable,
        switch_model: true,
        effort: true,
        stop: true,
        approve: hookConfigured(
          path.join(os.homedir(), ".codex", "hooks.json"),
          "127.0.0.1:18787/hook/codex",
          "PermissionRequest",
        ),
        live: true,
      },
      models: codex.models,
      default_model: codex.defaultModel,
    },
    {
      source: "claude",
      display_name: "Claude Code",
      available: Boolean(claudeBin),
      capabilities: {
        reply: true,
        new_session: Boolean(claudeBin),
        new_session_ready: Boolean(claudeBin),
        switch_model: Boolean(claudeBin),
        effort: false,
        stop: Boolean(claudeBin),
        approve: hookConfigured(
          path.join(os.homedir(), ".claude", "settings.json"),
          "127.0.0.1:18787/hook/claude",
          "PermissionRequest",
        ),
        live: true,
      },
      models: claude.models,
      default_model: claude.defaultModel,
    },
    {
      source: "workbuddy",
      display_name: "WorkBuddy",
      available: workbuddyRunning(),
      capabilities: {
        reply: true,
        // WorkBuddy 5.9+ 已移除旧的 /api/v1/acp/connect 桥；旧桥即使存在
        // 也会复用当前活跃会话。未核实独立会话协议前不能声称可新建。
        new_session: false,
        new_session_ready: false,
        switch_model: false,
        effort: false,
        stop: false,
        approve: false,
        live: true,
      },
      models: workbuddy.models,
      default_model: workbuddy.defaultModel,
    },
  ];

  // v2：图标 / 安装状态 / 版本 / 推送信号 / 连接状态
  const nowTs = Date.now();
  const lastBySource = new Map();
  for (const ev of events) {
    const t = Date.parse(ev.ts || ev.at || "") || 0;
    if (!lastBySource.has(ev.source) || t > lastBySource.get(ev.source)) {
      lastBySource.set(ev.source, t);
    }
  }
  for (const a of agents) {
    const probe = agentProbe.get(a.source) || {};
    const installed = probe.installed ?? a.available;
    const lastAt = lastBySource.get(a.source) || 0;
    const recent = nowTs - lastAt < 24 * 3600 * 1000;
    const connected = Boolean(installed) && (recent || a.capabilities?.approve === true);
    a.icon = iconFor(a.source);
    a.installed = Boolean(installed);
    a.version = probe.version || null;
    a.push_signal = getScanner(a.source)?.pushSignal || "hook";
    a.status = !installed ? "missing" : connected ? "connected" : "pending";
    a.last_event_at = lastAt ? new Date(lastAt).toISOString() : null;
  }

  // 注册表里有、但旧 listAgents 没列出的 agent（opencode / zcode 等）也补上
  const listed = new Set(agents.map((a) => a.source));
  for (const [id, sc] of scanners) {
    if (listed.has(id)) continue;
    const probe = agentProbe.get(id) || {};
    agents.push({
      source: id,
      display_name: sc.name || id,
      available: Boolean(probe.installed),
      capabilities: {
        ...(sc.capabilities || {}),
        // Node 中继的 scanner 只负责读取；没有 /api/sessions/new 启动器
        // 就不能把 installed 当成可新建。
        new_session: false,
        new_session_ready: false,
      },
      models: [],
      default_model: null,
      icon: iconFor(id),
      installed: Boolean(probe.installed),
      version: probe.version || null,
      push_signal: sc.pushSignal || "poll",
      status: probe.installed ? "connected" : "missing",
      last_event_at: null,
    });
  }

  const value = { agents, generated_at: new Date().toISOString() };
  agentsCache.value = value;
  agentsCache.at = Date.now();
  return value;
}

function sessionSourceFor(sessionId) {
  if (!sessionId) {
    return null;
  }
  const known = [...events].reverse().find((event) => event.session_id === sessionId);
  if (known) {
    return known.source;
  }
  const recorded = transcriptPaths.get(sessionId);
  if (recorded?.source) {
    return recorded.source;
  }
  return inferSessionSource(sessionId);
}

function inferSessionSource(sessionId) {
  if (!sessionId) {
    return null;
  }
  const known = [...events].reverse().find((event) => event.session_id === sessionId);
  if (known) {
    return known.source;
  }
  try {
    if (codexTitles().has(sessionId) || codexRolloutIndex().has(sessionId)) {
      return "codex";
    }
  } catch {
    // 忽略
  }
  try {
    if (claudeSessionFiles().some((entry) => entry.sessionId === sessionId)) {
      return "claude";
    }
  } catch {
    // 忽略
  }
  try {
    if (workbuddySnapshot().has(sessionId)) {
      return "workbuddy";
    }
  } catch {
    // 忽略
  }
  return null;
}

function uploadedFilesNote(files) {
  const lines = files.map((file) => `- ${file}`);
  return `用户从手机上传了文件：\n${lines.join("\n")}`;
}

function normalizeReplyFiles(input) {
  if (!Array.isArray(input)) {
    return [];
  }
  const files = [];
  for (const item of input) {
    if (typeof item !== "string" || !item.trim()) {
      continue;
    }
    const absolute = path.resolve(item.trim());
    if (!path.isAbsolute(absolute)) {
      continue;
    }
    try {
      if (!fs.statSync(absolute).isFile()) {
        continue;
      }
    } catch {
      continue;
    }
    if (!files.includes(absolute)) {
      files.push(absolute);
    }
    if (files.length >= 20) {
      break;
    }
  }
  return files;
}

function handleTranscript(response, sessionId, limit) {
  const record = transcriptPaths.get(sessionId) || null;
  let source = record?.source || null;
  let filePath = null;
  let reason = null;

  if (record?.path && fs.existsSync(record.path)) {
    filePath = record.path;
  } else {
    const rollout = findCodexRollout(sessionId);
    if (rollout) {
      filePath = rollout;
      source = source || "codex";
    }
  }

  if (!filePath) {
    sendJson(response, 200, {
      session_id: sessionId,
      source,
      transcript_path: null,
      count: 0,
      messages: [],
      reason: "未找到该会话的转录文件（Codex 需 thread id，Claude/WorkBuddy 需先有 Stop 钩子记录 transcript_path）",
    });
    return;
  }

  if (!source) {
    source = filePath.includes(`${path.sep}.codex${path.sep}`) ? "codex" : "unknown";
  }

  const cwd =
    record?.cwd ||
    (events.find((event) => event.session_id === sessionId) || {}).cwd ||
    "";

  let messages = [];
  try {
    messages = parseTranscript(filePath, source, cwd);
  } catch (error) {
    reason = `解析失败：${error.message}`;
  }

  const sliced = messages.slice(-limit);
  sendJson(response, 200, {
    session_id: sessionId,
    source,
    transcript_path: filePath,
    count: sliced.length,
    messages: sliced,
    reason,
  });
}

/* ------------------------------------------------------------------ *
 * 会话元数据（GET /api/sessions）
 * 只读三家的原生会话文件，按 (path,size,mtime) 缓存，不碰任何凭据文件。
 * ------------------------------------------------------------------ */

const META_TTL_MS = 60_000;
const GIT_BRANCH_TTL_MS = 60_000;
const RUNNING_WINDOW_MS = 90_000;

const sessionMetaCache = new Map(); // `${source}:${id}` -> { at, value }
const gitBranchCache = new Map(); // cwd -> { at, branch }
const codexTitleIndex = { key: "", map: new Map() };
const codexRolloutIndexCache = { at: 0, map: new Map() };
const codexRolloutMetaCache = new Map(); // path -> { size, mtimeMs, meta }
const claudeFilesCache = { at: 0, entries: [] };
const claudeMetaCache = new Map(); // path -> { size, mtimeMs, meta }
const workbuddySnapshotCache = { key: "", map: new Map() };
const sessionSignatures = new Map();

function firstLine(text, limit = 120) {
  const value = typeof text === "string" ? text.trim() : "";
  if (!value) {
    return "";
  }
  const line = value.split("\n").find((item) => item.trim()) || "";
  return Array.from(line.trim()).slice(0, limit).join("");
}

function readFileSlice(filePath, start, length) {
  if (length <= 0) {
    return "";
  }
  let fd;
  try {
    fd = fs.openSync(filePath, "r");
    const buffer = Buffer.alloc(length);
    const read = fs.readSync(fd, buffer, 0, length, start);
    return buffer.subarray(0, read).toString("utf8");
  } catch {
    return "";
  } finally {
    if (fd !== undefined) {
      try {
        fs.closeSync(fd);
      } catch {
        // 忽略
      }
    }
  }
}

// Codex 的 rollout 头部常被几十 KB 的 instructions 占满，首条用户消息可能落在 150KB 之后。
const HEAD_BYTES = 320 * 1024;
const TAIL_BYTES = 192 * 1024;

function readFileHeadAndTail(filePath, size) {
  const head = readFileSlice(filePath, 0, Math.min(size, HEAD_BYTES));
  const tail =
    size > HEAD_BYTES
      ? readFileSlice(filePath, Math.max(0, size - TAIL_BYTES), Math.min(size, TAIL_BYTES))
      : head;
  return { head, tail };
}

// ---- Codex -------------------------------------------------------------

function codexTitles() {
  const file = path.join(os.homedir(), ".codex", "session_index.jsonl");
  let stat;
  try {
    stat = fs.statSync(file);
  } catch {
    return new Map();
  }
  const key = `${stat.mtimeMs}|${stat.size}`;
  if (codexTitleIndex.key === key) {
    return codexTitleIndex.map;
  }
  const map = new Map();
  for (const row of readJsonLines(file)) {
    if (!row || typeof row.id !== "string") {
      continue;
    }
    const updated = typeof row.updated_at === "string" ? row.updated_at : "";
    const previous = map.get(row.id);
    if (!previous || updated >= previous.updated_at) {
      map.set(row.id, {
        thread_name: typeof row.thread_name === "string" ? row.thread_name : "",
        updated_at: updated,
      });
    }
  }
  codexTitleIndex.key = key;
  codexTitleIndex.map = map;
  return map;
}

function codexRolloutIndex() {
  if (Date.now() - codexRolloutIndexCache.at < META_TTL_MS && codexRolloutIndexCache.map.size) {
    return codexRolloutIndexCache.map;
  }
  const map = new Map();
  const base = path.join(os.homedir(), ".codex", "sessions");
  const walk = (dir, depth) => {
    if (depth > 4) {
      return;
    }
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full, depth + 1);
        continue;
      }
      if (!entry.name.startsWith("rollout-") || !entry.name.endsWith(".jsonl")) {
        continue;
      }
      const match = /-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$/i.exec(
        entry.name,
      );
      if (match) {
        map.set(match[1], full);
      }
    }
  };
  walk(base, 0);
  codexRolloutIndexCache.at = Date.now();
  codexRolloutIndexCache.map = map;
  return map;
}

function codexRolloutMeta(filePath) {
  let stat;
  try {
    stat = fs.statSync(filePath);
  } catch {
    return null;
  }
  const cached = codexRolloutMetaCache.get(filePath);
  if (cached && cached.size === stat.size && cached.mtimeMs === stat.mtimeMs) {
    return cached.meta;
  }

  const meta = {
    cwd: null,
    branch: null,
    model: null,
    effort: null,
    permissionMode: null,
    contextUsage: null,
    originator: null,
    lastTs: null,
    lastPreview: null,
    firstPrompt: null,
  };
  const { head, tail } = readFileHeadAndTail(filePath, stat.size);

  for (const line of head.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      continue;
    }
    const payload = row?.payload && typeof row.payload === "object" ? row.payload : null;
    if (!payload) {
      continue;
    }
    if (row.type === "session_meta") {
      if (typeof payload.cwd === "string") {
        meta.cwd = payload.cwd;
      }
      if (payload.git && typeof payload.git.branch === "string") {
        meta.branch = payload.git.branch;
      }
      if (typeof payload.originator === "string") {
        meta.originator = payload.originator;
      }
    }
    if (
      !meta.firstPrompt &&
      payload.type === "message" &&
      payload.role === "user"
    ) {
      const text = normalizeContent(payload.content).trim();
      if (text && !NOISE_PATTERN.test(text)) {
        meta.firstPrompt = text;
      }
    }
  }

  for (const line of tail.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (typeof row.timestamp === "string") {
      meta.lastTs = row.timestamp;
    }
    const payload = row?.payload && typeof row.payload === "object" ? row.payload : null;
    if (!payload) {
      continue;
    }
    if (typeof payload.model === "string" && payload.model) {
      meta.model = payload.model;
    }
    if (typeof payload.effort === "string" && payload.effort) {
      meta.effort = payload.effort;
    }
    if (typeof payload.approval_policy === "string" && payload.approval_policy) {
      meta.permissionMode = payload.approval_policy;
    } else if (payload.sandbox_policy?.type) {
      meta.permissionMode = payload.sandbox_policy.type;
    } else if (payload.permission_profile?.type) {
      meta.permissionMode = payload.permission_profile.type;
    }
    if (payload.type === "token_count" && payload.info) {
      const usage = payload.info.last_token_usage || payload.info.total_token_usage || null;
      if (usage) {
        const used =
          typeof usage.total_tokens === "number"
            ? usage.total_tokens
            : (usage.input_tokens || 0) + (usage.cached_input_tokens || 0) + (usage.output_tokens || 0);
        meta.contextUsage = {
          used,
          limit: typeof payload.info.model_context_window === "number" ? payload.info.model_context_window : null,
        };
      }
    }
    if (
      payload.thread_settings &&
      typeof payload.thread_settings.model === "string" &&
      payload.thread_settings.model
    ) {
      meta.model = payload.thread_settings.model;
    }
    if (payload.type === "message" && payload.role === "assistant") {
      const text = normalizeContent(payload.content).trim();
      if (text) {
        meta.lastPreview = firstLine(text, 120);
      }
    }
    const item = payload.item;
    if (item && item.type === "agent_message" && typeof item.text === "string" && item.text.trim()) {
      meta.lastPreview = firstLine(item.text, 120);
    }
  }

  codexRolloutMetaCache.set(filePath, { size: stat.size, mtimeMs: stat.mtimeMs, meta });
  return meta;
}

// ---- Claude Code -------------------------------------------------------

function claudeSessionFiles() {
  if (Date.now() - claudeFilesCache.at < META_TTL_MS && claudeFilesCache.entries.length) {
    return claudeFilesCache.entries;
  }
  const entries = [];
  const base = path.join(os.homedir(), ".claude", "projects");
  let dirs = [];
  try {
    dirs = fs.readdirSync(base, { withFileTypes: true });
  } catch {
    dirs = [];
  }
  for (const dir of dirs) {
    if (!dir.isDirectory()) {
      continue;
    }
    const dirPath = path.join(base, dir.name);
    let files = [];
    try {
      files = fs.readdirSync(dirPath);
    } catch {
      continue;
    }
    for (const file of files) {
      if (!file.endsWith(".jsonl")) {
        continue;
      }
      const full = path.join(dirPath, file);
      let stat;
      try {
        stat = fs.statSync(full);
      } catch {
        continue;
      }
      entries.push({
        path: full,
        sessionId: file.replace(/\.jsonl$/, ""),
        dirName: dir.name,
        size: stat.size,
        mtimeMs: stat.mtimeMs,
      });
    }
  }
  entries.sort((a, b) => b.mtimeMs - a.mtimeMs);
  claudeFilesCache.at = Date.now();
  claudeFilesCache.entries = entries;
  return entries;
}

function claudeFileMeta(entry) {
  const cached = claudeMetaCache.get(entry.path);
  if (cached && cached.size === entry.size && cached.mtimeMs === entry.mtimeMs) {
    return cached.meta;
  }
  const meta = {
    cwd: null,
    branch: null,
    model: null,
    effort: null,
    permissionMode: null,
    contextUsage: null,
    customTitle: null,
    aiTitle: null,
    summary: null,
    firstPrompt: null,
    lastTs: null,
    lastPreview: null,
  };
  const { head, tail } = readFileHeadAndTail(entry.path, entry.size);

  const apply = (line, fromHead) => {
    const trimmed = line.trim();
    if (!trimmed) {
      return;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      return;
    }
    if (!row || typeof row !== "object") {
      return;
    }
    if (typeof row.cwd === "string" && row.cwd) {
      meta.cwd = row.cwd;
    }
    if (typeof row.gitBranch === "string" && row.gitBranch) {
      meta.branch = row.gitBranch;
    }
    if (typeof row.timestamp === "string" && row.timestamp) {
      meta.lastTs = row.timestamp;
    }
    if (row.type === "custom-title" && typeof row.customTitle === "string") {
      meta.customTitle = row.customTitle;
    }
    if (row.type === "ai-title" && typeof row.aiTitle === "string") {
      meta.aiTitle = row.aiTitle;
    }
    if (row.type === "summary" && typeof row.summary === "string") {
      meta.summary = row.summary;
    }
    if (typeof row.permissionMode === "string" && row.permissionMode) {
      meta.permissionMode = row.permissionMode;
    }
    if (typeof row.effort === "string" && row.effort) {
      meta.effort = row.effort;
    } else if (typeof row.perTurnEffort === "string" && row.perTurnEffort) {
      meta.effort = row.perTurnEffort;
    }
    const message = row.message && typeof row.message === "object" ? row.message : null;
    if (message) {
      if (typeof message.model === "string" && message.model) {
        meta.model = message.model;
      }
      if (message.usage && typeof message.usage === "object") {
        const usage = message.usage;
        const used =
          (usage.input_tokens || 0) +
          (usage.cache_read_input_tokens || 0) +
          (usage.cache_creation_input_tokens || 0) +
          (usage.output_tokens || 0);
        if (used > 0) {
          meta.contextUsage = { used, limit: null };
        }
      }
      const text = normalizeContent(message.content).trim();
      const role = message.role || row.type;
      if (fromHead && !meta.firstPrompt && role === "user" && text && !NOISE_PATTERN.test(text)) {
        meta.firstPrompt = text;
      }
      if (!fromHead && role === "assistant" && text) {
        meta.lastPreview = firstLine(text, 120);
      }
    }
  };

  for (const line of head.split("\n")) {
    apply(line, true);
  }
  if (tail !== head) {
    for (const line of tail.split("\n")) {
      apply(line, false);
    }
  }

  claudeMetaCache.set(entry.path, { size: entry.size, mtimeMs: entry.mtimeMs, meta });
  return meta;
}

// ---- WorkBuddy ---------------------------------------------------------

function workbuddySnapshot() {
  const base = path.join(os.homedir(), ".workbuddy");
  let dirs;
  try {
    dirs = fs.readdirSync(base, { withFileTypes: true });
  } catch {
    return new Map();
  }
  for (const dir of dirs) {
    if (!dir.isDirectory()) {
      continue;
    }
    const file = path.join(base, dir.name, "sidebar-list-snapshot.json");
    let stat;
    try {
      stat = fs.statSync(file);
    } catch {
      continue;
    }
    const key = `${file}|${stat.mtimeMs}|${stat.size}`;
    if (workbuddySnapshotCache.key === key) {
      return workbuddySnapshotCache.map;
    }
    let data;
    try {
      data = JSON.parse(fs.readFileSync(file, "utf8"));
    } catch {
      continue;
    }
    const map = new Map();
    for (const item of data.items || []) {
      if (item && typeof item.id === "string") {
        map.set(item.id, item);
      }
    }
    workbuddySnapshotCache.key = key;
    workbuddySnapshotCache.map = map;
    return map;
  }
  return new Map();
}

// ---- 公共字段推导 -------------------------------------------------------

function gitBranchFor(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return null;
  }
  const cached = gitBranchCache.get(cwd);
  if (cached && Date.now() - cached.at < GIT_BRANCH_TTL_MS) {
    return cached.branch;
  }
  let branch = null;
  try {
    const output = execFileSync(
      "git",
      ["-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"],
      { timeout: 1000, stdio: ["ignore", "pipe", "ignore"] },
    )
      .toString("utf8")
      .trim();
    if (output && output !== "HEAD") {
      branch = output;
    }
  } catch {
    branch = null;
  }
  gitBranchCache.set(cwd, { at: Date.now(), branch });
  return branch;
}

function workspaceLabel(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return null;
  }
  const parent = path.dirname(cwd.trim());
  if (!parent || parent === "." || parent === "/") {
    return path.basename(parent || "/") || null;
  }
  return path.basename(parent);
}

const nativeTitlePrefixPattern =
  /^(?:会话标题|會話標題|對話標題|标题|標題|conversation title|session title|title)\s*[：:]\s*/i;

function cleanAgentNativeTitle(value) {
  if (typeof value !== "string") {
    return "";
  }
  let title = value.trim();
  while (true) {
    const withoutHeading = title.replace(/^#{1,6}\s+/, "").trim();
    if (withoutHeading === title) {
      break;
    }
    title = withoutHeading;
  }
  while (true) {
    const withoutPrefix = title.replace(nativeTitlePrefixPattern, "").trim();
    if (withoutPrefix === title) {
      break;
    }
    title = withoutPrefix;
  }
  return title;
}

function sessionTitleFor({ source, sessionId, nativeTitle, nativeTitleSource, firstPrompt, cwd }) {
  const cleanTitle = (value) =>
    typeof value === "string" && value.trim() ? Array.from(value.trim()).slice(0, 200).join("") : "";
  // Only titles generated by the agent are cleaned. A title explicitly named
  // by the user can legitimately start with "Title:" or "标题：".
  const byNative = cleanTitle(
    !nativeTitleSource || nativeTitleSource === "agent"
      ? cleanAgentNativeTitle(nativeTitle)
      : nativeTitle,
  );
  if (byNative) {
    return { title: byNative, title_source: nativeTitleSource || "agent" };
  }
  const byPrompt = firstLine(firstPrompt, 40);
  if (byPrompt) {
    return { title: byPrompt, title_source: "first_prompt" };
  }
  const project = typeof cwd === "string" && cwd.trim() ? path.basename(cwd.trim()) : "";
  if (project) {
    return { title: project, title_source: "project" };
  }
  const shortId = String(sessionId || "").slice(0, 8) || "unknown";
  return { title: `${source}-${shortId}`, title_source: "fallback" };
}

function deriveSessionStatus({ lastKind, pendingInjection, lastTs, nativeState, now = Date.now() }) {
  if (lastKind === "permission") {
    return "permission";
  }
  if (lastKind === "needs_input") {
    return "needs_input";
  }
  if (lastKind === "error") {
    return "error";
  }
  if (nativeState === "error") {
    return "error";
  }
  if (pendingInjection) {
    return "running";
  }
  if (lastKind === "reply") {
    const ts = Date.parse(lastTs || "");
    if (Number.isFinite(ts) && now - ts < RUNNING_WINDOW_MS) {
      return "running";
    }
  }
  return "done";
}

// 单个会话的元数据（事件里的 session_meta 与 /api/sessions/<id> 共用）
function sessionMetaFor(source, sessionId, cwdHint) {
  const key = `${source}:${sessionId}`;
  const cached = sessionMetaCache.get(key);
  if (cached && Date.now() - cached.at < META_TTL_MS) {
    return cached.value;
  }

  let cwd = typeof cwdHint === "string" && cwdHint ? cwdHint : null;
  let nativeTitle = "";
  let nativeTitleSource = "agent";
  let firstPrompt = "";
  let branch = null;
  let model = null;
  let effort = null;
  let permissionMode = null;
  let contextUsage = null;
  let workspace = null;
  let nativeState = null;

  try {
    // v2：按注册表分派；每个 scanner 的 meta() 内部就是原 v1 的 source 分支
    const sc = getScanner(source);
    if (sc?.meta) {
      const m = sc.meta(sessionId, cwdHint) || {};
      cwd = cwd || m.cwd || null;
      nativeTitle = m.nativeTitle || "";
      nativeTitleSource = m.titleSource || "agent";
      firstPrompt = m.firstPrompt || "";
      branch = m.branch ?? null;
      model = m.model ?? null;
      effort = m.effort ?? null;
      permissionMode = m.permissionMode ?? null;
      contextUsage = m.contextUsage ?? null;
      workspace = m.workspace ?? null;
      nativeState = m.nativeState ?? null;
    }
  } catch {
    // 任何读取异常都降级，不影响事件入库
  }

  if (branch === "HEAD") {
    branch = null;
  }
  if (!branch) {
    branch = gitBranchFor(cwd);
  }

  const { title, title_source } = sessionTitleFor({
    source,
    sessionId,
    nativeTitle,
    nativeTitleSource,
    firstPrompt,
    cwd,
  });

  const value = {
    title,
    title_source,
    workspace: workspace || workspaceLabel(cwd),
    project: typeof cwd === "string" && cwd.trim() ? path.basename(cwd.trim()) : null,
    branch,
    // Claude 的 "<synthetic>" 是占位模型名，当作没有
    model: model && model !== "<synthetic>" ? model : null,
    effort: effort || null,
    permission_mode: permissionMode || null,
    context_usage: contextUsage || null,
    native_state: nativeState || null,
  };
  sessionMetaCache.set(key, { at: Date.now(), value });
  return value;
}

// 会话汇总列表
function listSessionSummaries({ limit = 100, source = null, status = null } = {}) {
  const relayIndex = new Map();

  for (const event of events) {
    if (!event.session_id) {
      continue;
    }
    const key = `${event.source}:${event.session_id}`;
    let entry = relayIndex.get(key);
    if (!entry) {
      entry = {
        session_id: event.session_id,
        source: event.source,
        cwd: "",
        event_count: 0,
        unread: 0,
        lastTs: "",
        lastKind: null,
        lastPreview: "",
        pendingInjection: false,
      };
      relayIndex.set(key, entry);
    }
    entry.event_count += 1;
    if (!event.read) {
      entry.unread += 1;
    }
    if (event.cwd) {
      entry.cwd = event.cwd;
    }
    if (!entry.lastTs || String(event.ts) >= entry.lastTs) {
      entry.lastTs = String(event.ts || "");
      entry.lastKind = event.kind;
      if (event.kind === "reply" && event.text) {
        entry.lastPreview = firstLine(event.text, 120);
      }
    }
  }

  for (const reply of replies) {
    if (reply.status !== "injected" && reply.status !== "held") {
      continue;
    }
    const key = `${(events.find((event) => event.id === reply.event_id) || {}).source}:${reply.session_id}`;
    const entry = relayIndex.get(key);
    if (entry && String(reply.ts || "") >= entry.lastTs) {
      entry.pendingInjection = true;
    }
  }

  const candidates = [];
  for (const [key, entry] of relayIndex) {
    candidates.push({ key, relay: entry, lastTs: entry.lastTs });
  }

  const titles = codexTitles();
  const rollouts = codexRolloutIndex();
  for (const [id, title] of titles) {
    const key = `codex:${id}`;
    if (relayIndex.has(key)) {
      continue;
    }
    candidates.push({ key, codexId: id, lastTs: title.updated_at || "" });
  }
  for (const id of rollouts.keys()) {
    const key = `codex:${id}`;
    if (relayIndex.has(key) || titles.has(id)) {
      continue;
    }
    let lastTs = "";
    try {
      lastTs = new Date(fs.statSync(rollouts.get(id)).mtimeMs).toISOString();
    } catch {
      lastTs = "";
    }
    candidates.push({ key, codexId: id, lastTs });
  }
  for (const entry of claudeSessionFiles()) {
    const key = `claude:${entry.sessionId}`;
    if (relayIndex.has(key)) {
      continue;
    }
    candidates.push({
      key,
      claudeEntry: entry,
      lastTs: new Date(entry.mtimeMs).toISOString(),
    });
  }
  for (const [id, item] of workbuddySnapshot()) {
    const key = `workbuddy:${id}`;
    if (relayIndex.has(key)) {
      continue;
    }
    const ms = Number(item.lastActivityAt || item.updatedAt || 0);
    candidates.push({
      key,
      workbuddyItem: item,
      lastTs: ms > 0 ? new Date(ms).toISOString() : "",
    });
  }

  candidates.sort((a, b) => (String(a.lastTs) < String(b.lastTs) ? 1 : -1));

  const results = [];
  const wideLimit = Math.max(limit, 200);   // 先放宽，合并 scanner 结果后再统一截断
  for (const candidate of candidates) {
    if (results.length >= wideLimit) {
      break;
    }
    let summary = null;
    try {
      summary = buildSessionSummary(candidate);
    } catch {
      summary = null;
    }
    if (!summary) {
      continue;
    }
    if (source && summary.source !== source) {
      continue;
    }
    if (status && summary.status !== status) {
      continue;
    }
    results.push(decorateSummary(summary));
  }

  // v2：把 scanner 原生发现的会话并进来（已在 candidates 里的跳过）
  try {
    const seen = new Set(results.map((r) => `${r.source}:${r.session_id}`));
    const extra = [];
    for (const row of scannerRows.rows) {
      const key = `${row.source}:${row.session_id}`;
      if (seen.has(key)) continue;
      if (source && row.source !== source) continue;
      seen.add(key);
      const meta = sessionMetaFor(row.source, row.session_id, row.cwd || null);
      extra.push(decorateSummary({
        session_id: row.session_id,
        source: row.source,
        title: row.title || meta.title,
        title_source: row.title_source || meta.title_source,
        workspace: meta.workspace,
        project: meta.project,
        branch: meta.branch,
        cwd: meta.cwd || row.cwd || null,
        model: meta.model || row.model || null,
        status: row.native_state === "error" ? "error" : "done",
        unread: 0,
        last_ts: row.last_ts,
        last_preview: null,
        event_count: 0,
      }));
    }
    if (process.env.HOMING_DEBUG) console.log(`[merge] scannerRows=${scannerRows.rows.length} extra=${extra.length} results=${results.length} limit=${limit}`);
    for (const e of extra) {
      if (status && e.status !== status) continue;
      results.push(e);
    }
  } catch (err) {
    console.log(`[scanner] 会话合并失败（已忽略）: ${err.message}`);
  }

  results.sort((a, b) => ((a.last_ts || "") < (b.last_ts || "") ? 1 : -1));
  return results.slice(0, limit);
}

// 列表/详情统一补：effort、permission_mode、context_usage、running、running_since
// ---- Homing v2：收件箱 ----------------------------------------------------
// waiting = 需要我处理（授权 / 等你回复 / 出错），running = 最近 5 分钟还在动，
// recent_done = 最近 24 小时完成。muted 与 archived 的会话不进 waiting。
function buildInbox({ now = Date.now() } = {}) {
  const latest = new Map();
  for (const ev of events) {
    if (!ev.session_id) continue;
    latest.set(`${ev.source}:${ev.session_id}`, ev);
  }
  const waiting = [];
  const running = [];
  const recentDone = [];
  const WAITING = new Set(["permission", "needs_input", "error"]);
  const DONE = new Set(["reply", "done", "stop", "turn_complete", "completed"]);

  for (const ev of latest.values()) {
    const st = sessionState.get(ev.source, ev.session_id);
    if (st.archived) continue;
    const ts = Date.parse(ev.ts || ev.at || "") || 0;
    const age = now - ts;
    const meta = sessionMetaFor(ev.source, ev.session_id, ev.cwd);
    const session = {
      session_id: ev.session_id,
      source: ev.source,
      title: meta.title,
      title_source: meta.title_source,
      project: meta.project,
      workspace: meta.workspace,
      branch: meta.branch,
      model: meta.model,
    };
    const item = {
      session,
      event_id: ev.id,
      kind: ev.kind,
      title: meta.title || ev.title || "",
      summary: String(ev.text || "").replace(/\s+/g, " ").trim().slice(0, 160),
      at: ev.ts || ev.at || null,
      pinned: st.pinned,
      read: st.read,
    };
    if (WAITING.has(ev.kind)) {
      if (st.muted || st.read) continue;
      waiting.push({
        ...item,
        kind: ev.kind === "permission" ? "approval" : ev.kind === "needs_input" ? "needs_reply" : "error",
      });
      continue;
    }
    if (age <= 5 * 60 * 1000) {
      running.push(item);
      continue;
    }
    if (DONE.has(ev.kind) && age <= 24 * 3600 * 1000) {
      recentDone.push(item);
    }
  }

  const byNewest = (a, b) => ((a.at || "") < (b.at || "") ? 1 : -1);
  const pinFirst = (a, b) => (a.pinned === b.pinned ? byNewest(a, b) : a.pinned ? -1 : 1);
  waiting.sort(pinFirst);
  running.sort(pinFirst);
  recentDone.sort(byNewest);

  return {
    waiting,
    running,
    recent_done: recentDone,
    counts: { waiting: waiting.length, running: running.length },
    generated_at: new Date(now).toISOString(),
  };
}

function decorateSummary(summary) {
  if (!summary) {
    return summary;
  }
  try {
    const meta = sessionMetaFor(summary.source, summary.session_id, summary.cwd || null);
    summary.effort = meta.effort ?? null;
    summary.permission_mode = meta.permission_mode ?? null;
    summary.context_usage = meta.context_usage ?? null;
  } catch {
    summary.effort = summary.effort ?? null;
    summary.permission_mode = summary.permission_mode ?? null;
    summary.context_usage = summary.context_usage ?? null;
  }
  summary.running = summary.status === "running";
  summary.running_since = summary.running ? summary.last_ts || null : null;
  // v2：治理状态 + 临时/空会话判定
  try {
    const st = sessionState.get(summary.source, summary.session_id);
    summary.read = st.read;
    summary.pinned = st.pinned;
    summary.muted = st.muted;
    summary.archived = st.archived;
  } catch {
    summary.read = false;
    summary.pinned = false;
    summary.muted = false;
    summary.archived = false;
  }
  summary.is_temp = isTempCwd(summary.cwd);
  summary.is_empty = isSessionEmpty(summary);
  return summary;
}

const emptyCache = new Map();   // "source:id" -> { at, value }
// 空会话：没有任何 user / assistant 条目（只有启动记录或系统消息）
function isSessionEmpty(summary) {
  const key = `${summary.source}:${summary.session_id}`;
  const hit = emptyCache.get(key);
  if (hit && Date.now() - hit.at < 60_000) return hit.value;
  const value = isSessionEmptyUncached(summary);
  emptyCache.set(key, { at: Date.now(), value });
  if (emptyCache.size > 2000) emptyCache.clear();
  return value;
}

function isSessionEmptyUncached(summary) {
  try {
    const sc = getScanner(summary.source);
    if (sc?.isEmptySession) {
      return sc.isEmptySession(summary.session_id) === true;
    }
    if (summary.event_count > 0) return false;
    const filePath = sessionFilePath(summary.source, summary.session_id);
    if (!filePath) return !summary.last_preview;
    const items = readRecentItems(summary.source, filePath, 12);
    if (!Array.isArray(items) || items.length === 0) return !summary.last_preview;
    return !items.some((it) => it.role === "user" || it.role === "assistant");
  } catch {
    return false;
  }
}

function buildSessionSummary(candidate) {
  const relay = candidate.relay || null;

  if (relay) {
    const meta = sessionMetaFor(relay.source, relay.session_id, relay.cwd);
    const nativeState =
      relay.source === "workbuddy" ? workbuddySnapshot().get(relay.session_id)?.state : null;
    return {
      session_id: relay.session_id,
      source: relay.source,
      title: meta.title,
      title_source: meta.title_source,
      workspace: meta.workspace,
      project: meta.project,
      branch: meta.branch,
      cwd: relay.cwd || null,
      model: meta.model,
      status: deriveSessionStatus({
        lastKind: relay.lastKind,
        pendingInjection: relay.pendingInjection,
        lastTs: relay.lastTs,
        nativeState,
      }),
      unread: relay.unread,
      last_ts: relay.lastTs || null,
      last_preview: relay.lastPreview || null,
      event_count: relay.event_count,
    };
  }

  if (candidate.codexId) {
    const title = codexTitles().get(candidate.codexId);
    const meta = sessionMetaFor("codex", candidate.codexId, null);
    return {
      session_id: candidate.codexId,
      source: "codex",
      title: meta.title,
      title_source: meta.title_source,
      workspace: meta.workspace,
      project: meta.project,
      branch: meta.branch,
      cwd: (() => {
        const rollout = codexRolloutIndex().get(candidate.codexId);
        const m = rollout ? codexRolloutMeta(rollout) : null;
        return m?.cwd || null;
      })(),
      model: meta.model,
      status: deriveSessionStatus({ lastKind: null, pendingInjection: false, lastTs: candidate.lastTs }),
      unread: 0,
      last_ts: candidate.lastTs || title?.updated_at || null,
      last_preview: (() => {
        const rollout = codexRolloutIndex().get(candidate.codexId);
        const m = rollout ? codexRolloutMeta(rollout) : null;
        return m?.lastPreview || null;
      })(),
      event_count: 0,
    };
  }

  if (candidate.claudeEntry) {
    const meta = sessionMetaFor("claude", candidate.claudeEntry.sessionId, null);
    return {
      session_id: candidate.claudeEntry.sessionId,
      source: "claude",
      title: meta.title,
      title_source: meta.title_source,
      workspace: meta.workspace,
      project: meta.project,
      branch: meta.branch,
      cwd: claudeFileMeta(candidate.claudeEntry).cwd || null,
      model: meta.model,
      status: deriveSessionStatus({ lastKind: null, pendingInjection: false, lastTs: candidate.lastTs }),
      unread: 0,
      last_ts: candidate.lastTs || null,
      last_preview: claudeFileMeta(candidate.claudeEntry).lastPreview || null,
      event_count: 0,
    };
  }

  if (candidate.workbuddyItem) {
    const item = candidate.workbuddyItem;
    const meta = sessionMetaFor("workbuddy", item.id, null);
    return {
      session_id: item.id,
      source: "workbuddy",
      title: meta.title,
      title_source: meta.title_source,
      workspace: meta.workspace,
      project: meta.project,
      branch: meta.branch,
      cwd: (item.space && item.space.name) || null,
      model: meta.model,
      status: deriveSessionStatus({
        lastKind: null,
        pendingInjection: false,
        lastTs: candidate.lastTs,
        nativeState: item.state,
      }),
      unread: 0,
      last_ts: candidate.lastTs || null,
      last_preview: null,
      event_count: 0,
    };
  }

  return null;
}

// 单会话查询：只解析这一个会话，不做全量扫描。
function findSessionSummary(sessionId) {
  if (typeof sessionId !== "string" || !sessionId.trim()) {
    return null;
  }

  const relayEvents = events.filter((event) => event.session_id === sessionId);
  if (relayEvents.length) {
    const source = relayEvents[relayEvents.length - 1].source;
    const entry = {
      session_id: sessionId,
      source,
      cwd: "",
      event_count: 0,
      unread: 0,
      lastTs: "",
      lastKind: null,
      lastPreview: "",
      pendingInjection: false,
    };
    for (const event of relayEvents) {
      entry.event_count += 1;
      if (!event.read) {
        entry.unread += 1;
      }
      if (event.cwd) {
        entry.cwd = event.cwd;
      }
      if (!entry.lastTs || String(event.ts) >= entry.lastTs) {
        entry.lastTs = String(event.ts || "");
        entry.lastKind = event.kind;
        if (event.kind === "reply" && event.text) {
          entry.lastPreview = firstLine(event.text, 120);
        }
      }
    }
    return buildSessionSummary({
      key: `${source}:${sessionId}`,
      relay: entry,
      lastTs: entry.lastTs,
    });
  }

  if (codexTitles().has(sessionId) || codexRolloutIndex().has(sessionId)) {
    return buildSessionSummary({ key: `codex:${sessionId}`, codexId: sessionId, lastTs: "" });
  }

  const claudeEntry = claudeSessionFiles().find((item) => item.sessionId === sessionId);
  if (claudeEntry) {
    return buildSessionSummary({
      key: `claude:${sessionId}`,
      claudeEntry,
      lastTs: new Date(claudeEntry.mtimeMs).toISOString(),
    });
  }

  const wbItem = workbuddySnapshot().get(sessionId);
  if (wbItem) {
    const ms = Number(wbItem.lastActivityAt || wbItem.updatedAt || 0);
    return buildSessionSummary({
      key: `workbuddy:${sessionId}`,
      workbuddyItem: wbItem,
      lastTs: ms > 0 ? new Date(ms).toISOString() : "",
    });
  }

  return null;
}

// 新建任务时 session_id 还拿不到的来源（Claude CLI）：等首个钩子事件到达后补发 event: session。
const pendingLaunches = new Map(); // request_id -> { source, cwd, at, text }

// 授权挂起：PermissionRequest 类钩子最多等 10 分钟，手机 /api/approvals 决定后返回。
const pendingApprovals = new Map(); // event_id -> { resolve, timer, settled }

function holdApproval(event) {
  return new Promise((resolve) => {
    const record = { resolve, settled: false, timer: null };
    record.timer = setTimeout(() => {
      if (record.settled) {
        return;
      }
      record.settled = true;
      pendingApprovals.delete(event.id);
      console.log(`[approval] 超时 event=#${event.id}`);
      try {
        broadcastItem(event.session_id, approvalItemFor(event, "timeout"));
      } catch {
        // 忽略
      }
      resolve({});
    }, HOLD_TIMEOUT_MS);
    pendingApprovals.set(event.id, record);
    console.log(`[approval] 挂起 event=#${event.id} session=${event.session_id}`);
    try {
      broadcastItem(event.session_id, approvalItemFor(event, "pending"));
    } catch {
      // 推送失败不影响挂起
    }
  });
}

function resolveApproval(eventId, decision) {
  const record = pendingApprovals.get(eventId);
  if (!record || record.settled) {
    return false;
  }
  record.settled = true;
  clearTimeout(record.timer);
  pendingApprovals.delete(eventId);
  record.resolve({ decision, reason: decision === "allow" ? "手机批准" : "手机拒绝" });
  console.log(`[approval] event=#${eventId} -> ${decision}`);
  try {
    const event = events.find((item) => item.id === eventId);
    if (event) {
      broadcastItem(event.session_id, approvalItemFor(event, decision === "allow" ? "allowed" : "denied"));
    }
  } catch {
    // 忽略
  }
  return true;
}

function claimLaunchForEvent(event) {
  if (!event || !event.session_id) {
    return;
  }
  for (const [requestId, record] of pendingLaunches) {
    if (record.source !== event.source) {
      continue;
    }
    let cwdMatches = false;
    try {
      cwdMatches =
        Boolean(record.cwd && event.cwd) &&
        path.resolve(record.cwd) === path.resolve(event.cwd);
    } catch {
      cwdMatches = record.cwd === event.cwd;
    }
    if (!cwdMatches) {
      continue;
    }
    pendingLaunches.delete(requestId);
    console.log(`[launch] 关联 request=${requestId} session=${event.session_id}`);
    setTimeout(() => {
      try {
        const summary = decorateSummary(findSessionSummary(event.session_id));
        if (summary) {
          broadcastSessionUpdate({ ...summary, request_id: requestId });
        }
      } catch {
        // 补发失败不影响主流程
      }
    }, 300).unref?.();
    return;
  }
}

function broadcastSessionUpdate(session) {
  const payload = JSON.stringify(session);
  for (const client of sseClients) {
    if (client.destroyed || client.writableEnded) {
      continue;
    }
    if (client.__sessionFilter && client.__sessionFilter !== session.session_id) {
      continue;
    }
    client.write(`event: session\ndata: ${payload}\n\n`);
  }
}

// 会话元数据变化时推送 event: session（首次只建立基线，不刷屏）
let sessionBaselineReady = false;
function refreshSessionSignatures() {
  let list = [];
  try {
    list = listSessionSummaries({ limit: 100 });
  } catch (error) {
    console.error("[sessions] 刷新失败:", error?.message || error);
    return;
  }
  const seen = new Set();
  for (const session of list) {
    const key = `${session.source}:${session.session_id}`;
    seen.add(key);
    const signature = JSON.stringify([
      session.title,
      session.title_source,
      session.workspace,
      session.project,
      session.branch,
      session.model,
      session.status,
      session.unread,
      session.last_ts,
    ]);
    if (sessionSignatures.get(key) === signature) {
      continue;
    }
    sessionSignatures.set(key, signature);
    if (sessionBaselineReady) {
      broadcastSessionUpdate(session);
    }
  }
  for (const key of [...sessionSignatures.keys()]) {
    if (!seen.has(key)) {
      sessionSignatures.delete(key);
    }
  }
  sessionBaselineReady = true;
}

setInterval(() => {
  const cutoff = Date.now() - 15 * 60 * 1000;
  for (const [requestId, record] of pendingLaunches) {
    if (record.at < cutoff) {
      pendingLaunches.delete(requestId);
    }
  }
}, 5 * 60_000).unref?.();

const SESSION_REFRESH_TIMER = setInterval(refreshSessionSignatures, 60_000);
SESSION_REFRESH_TIMER.unref?.();
setTimeout(refreshSessionSignatures, 2000).unref?.();

/* ------------------------------------------------------------------ *
 * 会话条目（items）
 * 约定结构：type 只取 user|assistant|reasoning|tool_call|file_change|plan|approval|error|system
 * seq / role / command 等保留为附加字段。
 * ------------------------------------------------------------------ */

const ITEM_TEXT_LIMIT = 4000;
const REASONING_LIMIT = 300;
const TOOL_OUTPUT_TAIL = 800;
const TAIL_WINDOW = 2 * 1024 * 1024;
const TAILERS = new Map(); // session_id -> tailer

function clip(value, limit) {
  const text = typeof value === "string" ? value : "";
  return Array.from(text).slice(0, limit).join("");
}

function shortJson(value, limit = 300) {
  try {
    return clip(typeof value === "string" ? value : JSON.stringify(value), limit);
  } catch {
    return "";
  }
}

function tailOf(value, limit = TOOL_OUTPUT_TAIL) {
  const text = typeof value === "string" ? value : shortJson(value, limit * 2);
  return Array.from(text).slice(-limit).join("");
}

function countPatchLines(section, sign) {
  let count = 0;
  for (const line of section.split("\n")) {
    if (line.startsWith(sign) && !line.startsWith(`${sign}${sign}${sign}`)) {
      count += 1;
    }
  }
  return count;
}

// *** Begin Patch / *** Update File: path ... → files:[{path, added, removed, diff}]
function parsePatchFiles(patchText) {
  const text = typeof patchText === "string" ? patchText : "";
  const files = [];
  const pattern = /^\*\*\* (Add|Update|Delete) File: (.+)$/gm;
  const matches = [...text.matchAll(pattern)];
  for (let index = 0; index < matches.length; index += 1) {
    const match = matches[index];
    const start = match.index + match[0].length;
    const end = index + 1 < matches.length ? matches[index + 1].index : text.length;
    const section = text.slice(start, end).replace(/^\*\*\* End Patch.*$/m, "");
    files.push({
      path: match[2].trim(),
      added: countPatchLines(section, "+"),
      removed: countPatchLines(section, "-"),
      diff: clip(section.trim(), 1500),
    });
  }
  return files.slice(0, 20);
}

function parsePlanInput(inputText) {
  let parsed = null;
  try {
    parsed = typeof inputText === "string" ? JSON.parse(inputText) : inputText;
  } catch {
    parsed = null;
  }
  const rawPlan = parsed?.plan || parsed?.steps || (Array.isArray(parsed) ? parsed : null);
  if (!Array.isArray(rawPlan)) {
    return null;
  }
  return rawPlan
    .map((entry) => {
      if (!entry || typeof entry !== "object") {
        return null;
      }
      const step = entry.step || entry.title || entry.text || "";
      if (!step) {
        return null;
      }
      return { step: clip(String(step), 300), status: String(entry.status || "pending") };
    })
    .filter(Boolean)
    .slice(0, 50);
}

function claudeFileChange(name, input) {
  const filePath = input?.file_path || input?.path || "";
  if (name === "Edit") {
    const oldText = String(input?.old_string || "");
    const newText = String(input?.new_string || "");
    return [
      {
        path: filePath,
        added: newText ? newText.split("\n").length : 0,
        removed: oldText ? oldText.split("\n").length : 0,
        diff: clip(oldText && newText ? `- ${oldText}\n+ ${newText}` : `+ ${newText}`, 1500),
      },
    ];
  }
  if (name === "Write" || name === "MultiEdit") {
    const content = String(input?.content || input?.new_string || "");
    const edits = Array.isArray(input?.edits) ? input.edits : null;
    if (edits) {
      return edits
        .map((edit) => ({
          path: edit.file_path || filePath,
          added: String(edit.new_string || "").split("\n").length,
          removed: String(edit.old_string || "").split("\n").length,
          diff: clip(`- ${edit.old_string || ""}\n+ ${edit.new_string || ""}`, 1500),
        }))
        .slice(0, 20);
    }
    return [
      {
        path: filePath,
        added: content ? content.split("\n").length : 0,
        removed: 0,
        diff: clip(content, 1500),
      },
    ];
  }
  return null;
}

function codexCommandFromArgs(argumentsText) {
  try {
    const parsed = JSON.parse(argumentsText);
    if (parsed && typeof parsed.cmd === "string") {
      return parsed.cmd;
    }
    if (parsed && Array.isArray(parsed.command)) {
      return parsed.command.join(" ");
    }
  } catch {
    // 不是 JSON 就原样给出
  }
  return clip(String(argumentsText || ""), 300);
}

function newItemCtx() {
  return { calls: new Map(), byCall: new Map(), seq: 0, model: null };
}

function nextItemId(ctx, source) {
  ctx.seq += 1;
  return `${source}-i${ctx.seq}`;
}

function toolStatus(value) {
  const status = String(value || "").toLowerCase();
  if (status === "failed" || status === "error") {
    return "failed";
  }
  if (status === "running" || status === "in_progress" || status === "in-progress") {
    return "running";
  }
  return "done";
}

function extractExitCode(text) {
  const match = /(?:Process exited with code|exit code|Exit code:?)\s*(-?\d+)/i.exec(String(text || ""));
  return match ? Number(match[1]) : null;
}

// 返回 0..n 个约定结构的 item（不含 ts；id / seq 已填）
function extractItems(source, row, ctx) {
  const items = [];
  if (!row || typeof row !== "object") {
    return items;
  }

  if (source === "codex") {
    const payload = row.payload && typeof row.payload === "object" ? row.payload : null;
    if (!payload) {
      return items;
    }
    if (row.type === "turn_context" && typeof payload.model === "string") {
      ctx.model = payload.model;
    }
    if (row.type === "event_msg" && payload.type === "thread_settings_applied") {
      const model = payload.thread_settings?.model;
      if (typeof model === "string") {
        ctx.model = model;
      }
    }

    if (payload.type === "message") {
      const text = normalizeContent(payload.content).trim();
      if (text && !(payload.role === "user" && NOISE_PATTERN.test(text))) {
        items.push({
          id: nextItemId(ctx, source),
          seq: ctx.seq,
          type: payload.role === "assistant" ? "assistant" : "user",
          role: payload.role === "assistant" ? "assistant" : "user",
          text: clip(text, ITEM_TEXT_LIMIT),
          model: ctx.model,
        });
      }
      return items;
    }

    if (payload.type === "reasoning") {
      const summary = Array.isArray(payload.summary)
        ? payload.summary.map((part) => part?.text || "").join("\n")
        : "";
      const text = clip(summary.trim() || normalizeContent(payload.content), ITEM_TEXT_LIMIT);
      if (text) {
        items.push({
          id: nextItemId(ctx, source),
          seq: ctx.seq,
          type: "reasoning",
          role: "assistant",
          text,
          model: ctx.model,
        });
      }
      return items;
    }

    if (payload.type === "function_call") {
      const id = `${source}-tool-${payload.call_id}`;
      ctx.calls.set(payload.call_id, id);
      const command = codexCommandFromArgs(payload.arguments);
      ctx.byCall.set(payload.call_id, {
        id,
        type: "tool_call",
        name: payload.name || "shell",
        files: null,
        plan: null,
      });
      items.push({
        id,
        seq: ctx.seq,
        type: "tool_call",
        role: "assistant",
        command,
        tool: {
          name: payload.name || "shell",
          input_summary: command,
          output_tail: null,
          exit_code: null,
          status: toolStatus(payload.status === "completed" ? "done" : payload.status),
        },
        model: ctx.model,
      });
      return items;
    }

    if (payload.type === "function_call_output") {
      const known = ctx.byCall.get(payload.call_id) || null;
      const id = known?.id || ctx.calls.get(payload.call_id) || `${source}-tool-${payload.call_id}`;
      const output = String(payload.output || "");
      const exitCode = extractExitCode(output);
      items.push({
        id,
        seq: ctx.seq,
        type: known?.type || "tool_call",
        role: "tool",
        exit_code: exitCode,
        ...(known?.files ? { files: known.files } : {}),
        ...(known?.plan ? { plan: known.plan } : {}),
        tool: {
          name: known?.name || "shell",
          input_summary: null,
          output_tail: tailOf(output),
          exit_code: exitCode,
          status: exitCode === 0 || exitCode === null ? "done" : "failed",
        },
        model: ctx.model,
      });
      return items;
    }

    if (payload.type === "custom_tool_call") {
      const name = payload.name || "tool";
      const id = `${source}-tool-${payload.call_id}`;
      ctx.calls.set(payload.call_id, id);
      if (name === "apply_patch") {
        const files = parsePatchFiles(payload.input);
        ctx.byCall.set(payload.call_id, { id, type: "file_change", name, files, plan: null });
        items.push({
          id,
          seq: ctx.seq,
          type: "file_change",
          role: "assistant",
          files,
          tool: { name, input_summary: "apply_patch", output_tail: null, exit_code: null, status: "running" },
          model: ctx.model,
        });
      } else if (name === "update_plan") {
        const plan = parsePlanInput(payload.input) || [];
        ctx.byCall.set(payload.call_id, { id, type: "plan", name, files: null, plan });
        items.push({
          id,
          seq: ctx.seq,
          type: "plan",
          role: "assistant",
          plan,
          tool: { name, input_summary: "update_plan", output_tail: null, exit_code: null, status: "running" },
          model: ctx.model,
        });
      } else {
        ctx.byCall.set(payload.call_id, { id, type: "tool_call", name, files: null, plan: null });
        items.push({
          id,
          seq: ctx.seq,
          type: "tool_call",
          role: "assistant",
          tool: {
            name,
            input_summary: clip(String(payload.input || ""), 300),
            output_tail: null,
            exit_code: null,
            status: "running",
          },
          model: ctx.model,
        });
      }
      return items;
    }

    if (payload.type === "custom_tool_call_output") {
      const known = ctx.byCall.get(payload.call_id) || null;
      const id = known?.id || ctx.calls.get(payload.call_id) || `${source}-tool-${payload.call_id}`;
      const output = String(payload.output || "");
      const exitCode = extractExitCode(output);
      items.push({
        id,
        seq: ctx.seq,
        type: known?.type || "tool_call",
        role: "tool",
        exit_code: exitCode,
        ...(known?.files ? { files: known.files } : {}),
        ...(known?.plan ? { plan: known.plan } : {}),
        tool: {
          name: known?.name || "tool",
          input_summary: null,
          output_tail: tailOf(output),
          exit_code: exitCode,
          status: exitCode === 0 || exitCode === null ? "done" : "failed",
        },
        model: ctx.model,
      });
      return items;
    }

    return items;
  }

  // ---- Claude Code / WorkBuddy ----
  const rowType = row.type;
  const message = row.message && typeof row.message === "object" ? row.message : null;
  const rowModel =
    typeof message?.model === "string"
      ? message.model
      : typeof row.providerData?.model === "string"
        ? row.providerData.model
        : null;
  if (rowModel) {
    ctx.model = rowModel;
  }
  const model = ctx.model;

  if (rowType === "reasoning") {
    const text = clip(normalizeContent(row.content ?? row.text ?? ""), REASONING_LIMIT);
    if (text) {
      items.push({ id: nextItemId(ctx, source), seq: ctx.seq, type: "reasoning", role: "assistant", text, model });
    }
    return items;
  }

  if (rowType === "function_call") {
    const callId = row.callId || row.id;
    const id = `${source}-tool-${callId}`;
    ctx.calls.set(callId, id);
    const inputSummary = clip(String(row.arguments || row.input || ""), 300);
    ctx.byCall.set(callId, { id, type: "tool_call", name: row.name || "tool", files: null, plan: null });
    items.push({
      id,
      seq: ctx.seq,
      type: "tool_call",
      role: "assistant",
      command: inputSummary,
      tool: { name: row.name || "tool", input_summary: inputSummary, output_tail: null, exit_code: null, status: "running" },
      model,
    });
    return items;
  }

  if (rowType === "function_call_result") {
    const callId = row.callId || row.id;
    const known = ctx.byCall.get(callId) || null;
    const id = known?.id || ctx.calls.get(callId) || `${source}-tool-${callId}`;
    const outputText = normalizeContent(row.output) || shortJson(row.output, TOOL_OUTPUT_TAIL);
    items.push({
      id,
      seq: ctx.seq,
      type: known?.type || "tool_call",
      role: "tool",
      ...(known?.files ? { files: known.files } : {}),
      ...(known?.plan ? { plan: known.plan } : {}),
      tool: {
        name: known?.name || row.name || "tool",
        input_summary: null,
        output_tail: tailOf(outputText),
        exit_code: extractExitCode(outputText),
        status: toolStatus(row.status),
      },
      model,
    });
    return items;
  }

  const blocks = Array.isArray(message?.content)
    ? message.content
    : Array.isArray(row.content)
      ? row.content
      : typeof message?.content === "string"
        ? [{ type: "text", text: message.content }]
        : typeof row.content === "string"
          ? [{ type: "text", text: row.content }]
          : [];
  const role = message?.role || row.role || (rowType === "assistant" ? "assistant" : rowType === "user" ? "user" : null);

  for (const block of blocks) {
    if (typeof block === "string") {
      const text = block.trim();
      if (text) {
        items.push({
          id: nextItemId(ctx, source),
          seq: ctx.seq,
          type: role === "assistant" ? "assistant" : "user",
          role: role === "assistant" ? "assistant" : "user",
          text: clip(text, ITEM_TEXT_LIMIT),
          model,
        });
      }
      continue;
    }
    if (!block || typeof block !== "object") {
      continue;
    }
    if (block.type === "text" || block.type === "input_text" || block.type === "output_text") {
      const text = String(block.text || "").trim();
      if (text && !(role === "user" && NOISE_PATTERN.test(text))) {
        items.push({
          id: nextItemId(ctx, source),
          seq: ctx.seq,
          type: role === "assistant" ? "assistant" : "user",
          role: role === "assistant" ? "assistant" : "user",
          text: clip(text, ITEM_TEXT_LIMIT),
          model,
        });
      }
      continue;
    }
    if (block.type === "thinking") {
      const text = clip(String(block.thinking || block.text || ""), REASONING_LIMIT);
      if (text) {
        items.push({ id: nextItemId(ctx, source), seq: ctx.seq, type: "reasoning", role: "assistant", text, model });
      }
      continue;
    }
    if (block.type === "tool_use") {
      const id = `${source}-tool-${block.id}`;
      ctx.calls.set(block.id, id);
      const name = block.name || "tool";
      const input = block.input || {};
      const files = claudeFileChange(name, input);
      ctx.byCall.set(block.id, {
        id,
        type: files ? "file_change" : "tool_call",
        name,
        files: files || null,
        plan: null,
      });
      if (files) {
        items.push({
          id,
          seq: ctx.seq,
          type: "file_change",
          role: "assistant",
          files,
          tool: { name, input_summary: files.map((file) => file.path).join(", "), output_tail: null, exit_code: null, status: "running" },
          model,
        });
      } else {
        const summary = name === "Bash" ? String(input.command || "") : shortJson(input, 300);
        items.push({
          id,
          seq: ctx.seq,
          type: "tool_call",
          role: "assistant",
          command: name === "Bash" ? summary : undefined,
          tool: { name, input_summary: clip(summary, 300), output_tail: null, exit_code: null, status: "running" },
          model,
        });
      }
      continue;
    }
    if (block.type === "tool_result") {
      const known = ctx.byCall.get(block.tool_use_id) || null;
      const id = known?.id || ctx.calls.get(block.tool_use_id) || `${source}-tool-${block.tool_use_id}`;
      const content = normalizeContent(block.content) || shortJson(block.content, TOOL_OUTPUT_TAIL);
      items.push({
        id,
        seq: ctx.seq,
        type: known?.type || "tool_call",
        role: "tool",
        ...(known?.files ? { files: known.files } : {}),
        ...(known?.plan ? { plan: known.plan } : {}),
        tool: {
          name: known?.name || "tool",
          input_summary: null,
          output_tail: tailOf(content),
          exit_code: extractExitCode(content),
          status: block.is_error ? "failed" : "done",
        },
        model,
      });
      continue;
    }
  }

  return items;
}

function itemTimestamp(source, row) {
  const raw = source === "codex" ? row.timestamp : row.timestamp ?? row.ts;
  if (typeof raw === "number") {
    return new Date(raw).toISOString();
  }
  if (typeof raw === "string" && raw) {
    return raw;
  }
  return null;
}

function turnMarker(source, row) {
  const payload = row?.payload && typeof row.payload === "object" ? row.payload : {};
  if (source === "codex") {
    if (row.type === "event_msg" && payload.type === "task_started") {
      return "started";
    }
    if (row.type === "event_msg" && payload.type === "task_complete") {
      return "finished";
    }
    if (row.type === "event_msg" && payload.type === "error") {
      return "failed";
    }
    return null;
  }
  if (row?.type === "error" || row?.isApiErrorMessage) {
    return "failed";
  }
  if (row?.type === "user" || (row?.type === "message" && row?.role === "user")) {
    return "started";
  }
  if (row?.type === "assistant" || (row?.type === "message" && row?.role === "assistant")) {
    return "finished";
  }
  return null;
}

function readRecentItems(source, filePath, limit) {
  let stat;
  try {
    stat = fs.statSync(filePath);
  } catch {
    return null;
  }
  const size = stat.size;
  const start = Math.max(0, size - TAIL_WINDOW);
  const chunk = readFileSlice(filePath, start, size - start);
  if (!chunk) {
    return { items: [], turns: [] };
  }
  const lines = chunk.split("\n");
  if (start > 0) {
    lines.shift();
  }
  const ctx = newItemCtx();
  const byId = new Map();
  const order = [];
  const turns = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      continue;
    }
    const turn = turnMarker(source, row);
    if (turn) {
      turns.push({ state: turn, ts: itemTimestamp(source, row), seq: ctx.seq });
    }
    for (const item of extractItems(source, row, ctx)) {
      const withTs = { ts: itemTimestamp(source, row), ...item };
      if (byId.has(withTs.id)) {
        const previous = byId.get(withTs.id);
        const merged = { ...previous, ...withTs };
        merged.type = previous.type;
        merged.files = withTs.files?.length ? withTs.files : previous.files;
        merged.plan = withTs.plan?.length ? withTs.plan : previous.plan;
        merged.tool = { ...(previous.tool || {}), ...(withTs.tool || {}) };
        if (!merged.tool.input_summary) {
          merged.tool.input_summary = previous.tool?.input_summary ?? null;
        }
        if ((!merged.tool.name || merged.tool.name === "tool") && previous.tool?.name) {
          merged.tool.name = previous.tool.name;
        }
        byId.set(withTs.id, merged);
      } else {
        byId.set(withTs.id, withTs);
        order.push(withTs.id);
      }
    }
  }
  const items = order.map((id) => byId.get(id));
  return { items: items.slice(-limit), turns: turns.slice(-50), total: items.length };
}

function sessionFilePath(source, sessionId) {
  if (source === "codex") {
    return codexRolloutIndex().get(sessionId) || findCodexRollout(sessionId) || null;
  }
  const recorded = transcriptPaths.get(sessionId);
  if (recorded?.path) {
    try {
      if (fs.statSync(recorded.path).isFile()) {
        return recorded.path;
      }
    } catch {
      // 继续找
    }
  }
  if (source === "claude") {
    const entry = claudeSessionFiles().find((item) => item.sessionId === sessionId);
    return entry ? entry.path : null;
  }
  if (source === "workbuddy") {
    const projects = path.join(os.homedir(), ".workbuddy", "projects");
    let dirs = [];
    try {
      dirs = fs.readdirSync(projects, { withFileTypes: true });
    } catch {
      return null;
    }
    for (const dir of dirs) {
      if (!dir.isDirectory()) {
        continue;
      }
      const candidate = path.join(projects, dir.name, `${sessionId}.jsonl`);
      try {
        if (fs.statSync(candidate).isFile()) {
          return candidate;
        }
      } catch {
        // 继续
      }
    }
  }
  return null;
}

function sseWriteFiltered(response, chunk, sessionId) {
  if (response.destroyed || response.writableEnded) {
    return;
  }
  if (response.__sessionFilter && response.__sessionFilter !== sessionId) {
    return;
  }
  response.write(chunk);
}

function broadcastItem(sessionId, item) {
  const chunk = `event: item\ndata: ${JSON.stringify({ session_id: sessionId, item })}\n\n`;
  for (const client of sseClients) {
    if (
      item?.type === "reasoning" &&
      client.__includeReasoning !== true
    ) {
      continue;
    }
    sseWriteFiltered(client, chunk, sessionId);
  }
}

function broadcastTurn(sessionId, state) {
  const chunk = `event: turn\ndata: ${JSON.stringify({ session_id: sessionId, state, ts: new Date().toISOString() })}\n\n`;
  for (const client of sseClients) {
    sseWriteFiltered(client, chunk, sessionId);
  }
}

// 挂起中的授权请求 → approval 条目（带 event_id）
function approvalItemFor(event, status = "pending") {
  return {
    id: `approval-${event.id}`,
    seq: 0,
    ts: event.ts,
    type: "approval",
    role: "system",
    event_id: event.id,
    status,
    title: event.title,
    text: clip(event.text || "", 600),
    model: null,
  };
}

function pendingApprovalItems(sessionId) {
  const out = [];
  for (const eventId of pendingApprovals.keys()) {
    const event = events.find((item) => item.id === eventId);
    if (event && event.session_id === sessionId) {
      out.push(approvalItemFor(event, "pending"));
    }
  }
  return out;
}

function ensureTailer(sessionId, source) {
  if (!sessionId || !source) {
    return null;
  }
  const existing = TAILERS.get(sessionId);
  if (existing) {
    existing.at = Date.now();
    return existing;
  }
  const filePath = sessionFilePath(source, sessionId);
  if (!filePath) {
    return null;
  }
  let offset = 0;
  try {
    offset = fs.statSync(filePath).size;
  } catch {
    return null;
  }
  const tailer = { sessionId, source, path: filePath, offset, at: Date.now(), ctx: newItemCtx() };
  TAILERS.set(sessionId, tailer);
  return tailer;
}

function tickTailers() {
  const cutoff = Date.now() - 10 * 60 * 1000;
  for (const [sessionId, tailer] of TAILERS) {
    if (tailer.at < cutoff) {
      TAILERS.delete(sessionId);
      continue;
    }
    let stat;
    try {
      stat = fs.statSync(tailer.path);
    } catch {
      TAILERS.delete(sessionId);
      continue;
    }
    if (stat.size <= tailer.offset) {
      continue;
    }
    const chunk = readFileSlice(tailer.path, tailer.offset, stat.size - tailer.offset);
    if (!chunk) {
      continue;
    }
    const keepFrom = chunk.lastIndexOf("\n");
    if (keepFrom < 0) {
      continue;
    }
    const complete = chunk.slice(0, keepFrom + 1);
    tailer.offset += Buffer.byteLength(complete, "utf8");
    tailer.at = Date.now();
    for (const line of complete.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) {
        continue;
      }
      let row;
      try {
        row = JSON.parse(trimmed);
      } catch {
        continue;
      }
      const turn = turnMarker(tailer.source, row);
      if (turn) {
        broadcastTurn(sessionId, turn);
      }
      for (const item of extractItems(tailer.source, row, tailer.ctx)) {
        broadcastItem(sessionId, { ts: itemTimestamp(tailer.source, row), ...item });
      }
    }
  }
}

const TAILER_TIMER = setInterval(tickTailers, 2000);
TAILER_TIMER.unref?.();

async function handleRequest(request, response) {
  const url = new URL(request.url || "/", "http://localhost");

  if (url.pathname === "/health") {
    if (request.method !== "GET") {
      sendJson(response, 405, { error: "Method Not Allowed" });
      return;
    }
    sendJson(response, 200, { ok: true });
    return;
  }

  if (url.pathname === "/apk") {
    if (request.method !== "GET" && request.method !== "HEAD") {
      sendJson(response, 405, { error: "Method Not Allowed" });
      return;
    }
    if (!isLocalOrLanAddress(request.socket.remoteAddress)) {
      sendJson(response, 403, { error: "Forbidden" });
      return;
    }
    sendApk(request, response);
    return;
  }

  if (url.pathname.startsWith("/f/")) {
    serveCapability(request, response, url);
    return;
  }

  // 本机控制台：/console/*、配对与设备、日志、统计（必须在下面的 /api/* 统一鉴权之前，
  // 因为 /api/pair/claim 是给手机扫码用的免鉴权接口，靠一次性令牌守卫）
  if (await consoleApp.handle(request, response, url)) {
    return;
  }

  const proxyMatch = /^\/p\/(\d{1,5})(\/.*)?$/.exec(url.pathname);
  if (proxyMatch) {
    handleProxy(request, response, Number(proxyMatch[1]), proxyMatch[2] || "/", url);
    return;
  }

  if (url.pathname.startsWith("/hook/")) {
    if (!isLoopback(request)) {
      sendJson(response, 403, { error: "Forbidden" });
      return;
    }
    if (request.method !== "POST") {
      sendJson(response, 405, { error: "Method Not Allowed" });
      return;
    }

    const body = await readBody(request);
    let decision = {};
    if (url.pathname === "/hook/claude") {
      decision = await handleStopHook(body, "claude");
    } else if (url.pathname === "/hook/workbuddy") {
      decision = await handleStopHook(body, "workbuddy");
    } else if (url.pathname === "/hook/codex") {
      if (body.hook_event_name === "PermissionRequest") {
        // Codex 的 PermissionRequest 钩子（需用户在 hooks.json 里自行添加并信任）
        decision = await handleStopHook(body, "codex");
      } else {
      if (body.type && body.type !== "agent-turn-complete") {
        sendJson(response, 200, {});
        return;
      }
      addEvent({
        source: "codex",
        kind: "reply",
        session_id: body["thread-id"],
        cwd: body.cwd,
        title: "Codex 回复",
        text: body["last-assistant-message"],
      });
      }
    } else if (url.pathname === "/hook/generic") {
      addEvent({
        source: body.source,
        kind: body.kind,
        session_id: body.session_id,
        cwd: body.cwd,
        title: body.title,
        text: body.text,
      });
    } else {
      sendJson(response, 404, { error: "Not Found" });
      return;
    }

    sendJson(response, 200, decision || {});
    return;
  }

  if (url.pathname.startsWith("/api/")) {
    if (!hasValidToken(request)) {
      sendJson(response, 401, { error: "Unauthorized" });
      return;
    }

    if (url.pathname === "/api/events") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      const sinceValue = Number(url.searchParams.get("since") || "0");
      const since = Number.isFinite(sinceValue) ? sinceValue : 0;
      sendJson(
        response,
        200,
        events.filter((event) => event.id > since),
      );
      return;
    }

    if (url.pathname === "/api/stream") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }

      response.writeHead(200, {
        "Content-Type": "text/event-stream; charset=utf-8",
        "Cache-Control": "no-cache, no-transform",
        Connection: "keep-alive",
        "X-Accel-Buffering": "no",
      });
      response.flushHeaders?.();

      const sessionFilter = (url.searchParams.get("session_id") || "").trim();
      response.__includeReasoning = ["1", "true", "yes", "on"].includes(
        (url.searchParams.get("include_reasoning") || "").trim().toLowerCase(),
      );
      if (sessionFilter) {
        response.__sessionFilter = sessionFilter;
        const filterSource = sessionSourceFor(sessionFilter);
        if (filterSource) {
          try {
            ensureTailer(sessionFilter, filterSource);
          } catch {
            // 允许没有源头文件
          }
        }
      }

      for (const event of events) {
        if (!event.read && (!sessionFilter || event.session_id === sessionFilter)) {
          sendEvent(response, event);
        }
      }

      sseClients.add(response);

      // 连接审计：记录来源地址、连接与断开时间，便于判断手机是否连过。
      const remoteAddress = request.socket.remoteAddress || "unknown";
      const connectedAt = Date.now();
      const remote = isLoopback(request) ? "本机" : "局域网/远端";
      console.log(
        `[stream] 连接 ${remoteAddress} (${remote}) @ ${new Date(connectedAt).toISOString()}`,
      );

      const pingTimer = setInterval(() => {
        if (!response.destroyed && !response.writableEnded) {
          response.write(":ping\n");
        }
      }, 25_000);

      let cleaned = false;
      const cleanup = () => {
        if (cleaned) {
          return;
        }
        cleaned = true;
        clearInterval(pingTimer);
        sseClients.delete(response);
        const seconds = Math.round((Date.now() - connectedAt) / 1000);
        console.log(
          `[stream] 断开 ${remoteAddress} (${remote}) 持续 ${seconds}s @ ${new Date().toISOString()}`,
        );
      };
      request.on("close", cleanup);
      response.on("close", cleanup);
      return;
    }

    if (url.pathname === "/api/read") {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }

      const body = await readBody(request);
      const ids = new Set(
        Array.isArray(body.ids)
          ? body.ids.filter((id) => Number.isSafeInteger(id))
          : [],
      );
      let updated = 0;
      for (const event of events) {
        if (ids.has(event.id) && !event.read) {
          event.read = true;
          updated += 1;
        }
      }
      sendJson(response, 200, { ok: true, updated });
      return;
    }

    if (url.pathname === "/api/reply") {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }

      const body = await readBody(request);

      const text = typeof body.text === "string" ? body.text.trim() : "";
      if (!text) {
        sendJson(response, 400, { error: "回复内容不能为空" });
        return;
      }

      const files = normalizeReplyFiles(body.files);

      // 两种定位方式：event_id（旧），或 session_id + source（会话在 /api/sessions 里但没有新事件）。
      let event = null;
      if (body.event_id !== undefined && body.event_id !== null && body.event_id !== "") {
        event = events.find((item) => item.id === Number(body.event_id));
        if (!event) {
          sendJson(response, 404, { error: "事件不存在" });
          return;
        }
      }

      const requestedSessionId =
        typeof body.session_id === "string" ? body.session_id.trim() : "";
      const sessionId = event ? event.session_id : requestedSessionId;
      if (!sessionId) {
        sendJson(response, 400, { error: "event_id 或 session_id 必须提供一个" });
        return;
      }

      let source = event ? event.source : typeof body.source === "string" ? body.source : "";
      if (!validSources.has(source)) {
        source = inferSessionSource(sessionId) || "";
      }
      if (!validSources.has(source)) {
        sendJson(response, 400, { error: "source 只能是 claude、codex 或 workbuddy" });
        return;
      }

      let cwd = event?.cwd || "";
      if (!cwd) {
        try {
          cwd = findSessionSummary(sessionId)?.cwd || "";
        } catch {
          cwd = "";
        }
      }

      const model = typeof body.model === "string" && body.model.trim() ? body.model.trim().slice(0, 80) : null;
      const effortRaw = typeof body.effort === "string" ? body.effort.trim().toLowerCase() : "";
      const effort = ["none", "minimal", "low", "medium", "high", "xhigh", "ultra", "max"].includes(effortRaw)
        ? effortRaw
        : null;

      const reply = {
        id: nextReplyId++,
        event_id: event ? event.id : null,
        session_id: sessionId,
        source,
        cwd,
        model,
        effort,
        text: truncate(text),
        files,
        ts: new Date().toISOString(),
        injected: false,
        status: "queued",
        message: "",
      };
      replies.push(reply);
      if (event) {
        event.read = true;
      }

      if (source === "codex") {
        // Codex：直接注入该 thread 并触发新一轮（没有事件时用 /api/sessions 里的 cwd）。
        injectCodexReply(reply, { source, session_id: sessionId, cwd });
      } else if (source === "claude" && reply.model) {
        injectClaudeReply(reply);
      } else {
        const hold = firstPendingHold(sessionId);
        if (hold) {
          setReplyStatus(reply, "held");
          settleHold(hold, blockDecision([deliveryTextFor(reply)]));
        } else {
          setReplyStatus(reply, "queued");
        }
        // WorkBuddy 该版本 ACP 没有切换模型的接口：照常发送，但明确回报不支持。
        if (source === "workbuddy" && reply.model) {
          setReplyStatus(
            reply,
            "switch_model_unsupported",
            `WorkBuddy 当前版本 ACP 不支持切换模型（请求 ${reply.model}），已按当前模型发送`,
          );
        }
      }

      sendJson(response, 200, {
        ok: true,
        reply_id: reply.id,
        status: reply.status,
        message: reply.message,
        files: reply.files || [],
      });
      return;
    }

    if (url.pathname === "/api/replies") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }

      const sessionId = url.searchParams.get("session_id") || "";
      const list = replies
        .filter((reply) => !sessionId || reply.session_id === sessionId)
        .sort((a, b) => a.id - b.id)
        .map((reply) => ({
          reply_id: reply.id,
          event_id: reply.event_id,
          session_id: reply.session_id,
          status: reply.status,
          message: reply.message || statusMessages[reply.status] || "",
          ts: reply.ts,
          text: reply.text,
          files: Array.isArray(reply.files) ? reply.files : [],
        }));
      sendJson(response, 200, {
        session_id: sessionId,
        count: list.length,
        replies: list,
      });
      return;
    }

    if (url.pathname === "/api/away") {
      if (request.method === "GET") {
        sendJson(response, 200, { enabled: config.away.enabled });
        return;
      }
      if (request.method === "POST") {
        const body = await readBody(request);
        if (typeof body.enabled !== "boolean") {
          sendJson(response, 400, { error: "enabled 必须是布尔值" });
          return;
        }
        config.away.enabled = body.enabled;
        writeConfig(config);
        console.log(`[away] 离开模式${config.away.enabled ? "开启" : "关闭"}`);

        // 关闭离开模式时立刻放行所有挂起中的钩子，避免它们空等到 10 分钟超时。
        if (!config.away.enabled) {
          let released = 0;
          for (const [sessionId, list] of [...pendingHolds.entries()]) {
            for (const hold of [...list]) {
              if (!hold.settled) {
                settleHold(hold, {});
                released += 1;
              }
            }
            pendingHolds.delete(sessionId);
          }
          if (released) {
            console.log(`[hold] 关闭离开模式，放行 ${released} 个挂起请求`);
          }
        }

        sendJson(response, 200, { ok: true, enabled: config.away.enabled });
        return;
      }
      sendJson(response, 405, { error: "Method Not Allowed" });
      return;
    }

    if (url.pathname === "/api/upload") {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      await handleUpload(request, response, url);
      return;
    }

    if (url.pathname === "/api/projects") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      const projects = listProjects();
      sendJson(response, 200, { count: projects.length, projects });
      return;
    }

    if (url.pathname === "/api/client-log") {
      await handleClientLog(request, response, url);
      return;
    }

    if (url.pathname === "/api/inbox") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      sendJson(response, 200, buildInbox());
      return;
    }

    if (url.pathname === "/api/agents") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      sendJson(response, 200, listAgents());
      return;
    }

    if (url.pathname === "/api/sessions") {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      const limitValue = Number(url.searchParams.get("limit") || "100");
      const limit = Number.isFinite(limitValue)
        ? Math.min(Math.max(Math.trunc(limitValue), 1), 500)
        : 100;
      const source = url.searchParams.get("source");
      const status = url.searchParams.get("status");
      const includeEmpty = url.searchParams.get("include_empty") === "1";
      let sessions = listSessionSummaries({
        limit,
        source: source && source !== "all" ? source : null,
        status: status && status !== "any" ? status : null,
      });
      // v2：默认不返回空会话；pinned 置顶，其余按最后活动时间倒序
      if (!includeEmpty) {
        sessions = sessions.filter((s) => !s.is_empty);
      }
      sessions.sort((a, b) => {
        if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
        return (a.last_ts || "") < (b.last_ts || "") ? 1 : -1;
      });
      sendJson(response, 200, {
        sessions,
        count: sessions.length,
        next_cursor: null,
        generated_at: new Date().toISOString(),
      });
      return;
    }

    const sessionMatch = /^\/api\/sessions\/([^/]+)$/.exec(url.pathname);
    if (sessionMatch && sessionMatch[1] !== "new") {
      let sessionId;
      try {
        sessionId = decodeURIComponent(sessionMatch[1]);
      } catch {
        sendJson(response, 400, { error: "session_id 编码非法" });
        return;
      }
      // v2：治理状态 PATCH（read / pinned / muted / archived）
      if (request.method === "PATCH") {
        const body = await readBody(request);
        const patch = {};
        for (const field of ["read", "pinned", "muted", "archived"]) {
          if (Object.prototype.hasOwnProperty.call(body, field)) patch[field] = body[field] === true;
        }
        if (Object.keys(patch).length === 0) {
          sendJson(response, 400, { error: "body 需含 read / pinned / muted / archived 之一" });
          return;
        }
        const source = sessionSourceFor(sessionId) || "unknown";
        const state = sessionState.patch(source, sessionId, patch);
        const session = decorateSummary(findSessionSummary(sessionId));
        sendJson(response, 200, {
          session_id: sessionId,
          source,
          state,
          session: session || null,
        });
        return;
      }
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      try {
        sessionId2 = decodeURIComponent(sessionMatch[1]);
      } catch {
        sendJson(response, 400, { error: "session_id 编码非法" });
        return;
      }
      const session = decorateSummary(findSessionSummary(sessionId2));
      if (!session) {
        sendJson(response, 404, { error: "会话不存在" });
        return;
      }
      sendJson(response, 200, session);
      return;
    }

    if (url.pathname === "/api/sessions/new") {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      const body = await readBody(request);
      const source = body.source;
      if (!["codex", "workbuddy", "claude"].includes(source)) {
        sendJson(response, 409, {
          ok: false,
          code: "agent_not_driveable",
          error: "这个 Agent 目前只能读取会话，还没有可用的新会话驱动",
        });
        return;
      }
      if (source === "workbuddy") {
        sendJson(response, 409, {
          ok: false,
          code: "new_session_not_ready",
          error: "WorkBuddy 当前版本只能读取和回复，暂不支持安全地新建独立会话",
        });
        return;
      }
      const cwd = typeof body.cwd === "string" ? body.cwd.trim() : "";
      if (!cwd) {
        sendJson(response, 400, { error: "cwd 不能为空" });
        return;
      }
      let dirStat;
      try {
        dirStat = fs.statSync(cwd);
      } catch {
        sendJson(response, 400, { error: "cwd 不存在" });
        return;
      }
      if (!dirStat.isDirectory()) {
        sendJson(response, 400, { error: "cwd 不是目录" });
        return;
      }
      const text = typeof body.text === "string" ? body.text.trim() : "";
      if (!text) {
        sendJson(response, 400, { error: "text 不能为空" });
        return;
      }
      const files = normalizeReplyFiles(body.files);
      const finalText = files.length ? `${text}\n\n${uploadedFilesNote(files)}` : text;

      let result;
      if (source === "codex") {
        result = await startCodexSession(cwd, finalText);
      } else {
        result = startClaudeSession(cwd, finalText);
      }

      const payload = { ...result, cwd };
      // Claude CLI 先拿不到 session_id：发一个 request_id，等首个钩子事件到达后用
      // SSE event: session（带 request_id）补发，手机据此把任务和后续会话对应起来。
      if (result.ok && !result.session_id) {
        const requestId = crypto.randomBytes(8).toString("hex");
        pendingLaunches.set(requestId, { source, cwd, at: Date.now(), text: finalText });
        payload.request_id = requestId;
      }

      sendJson(response, result.ok ? 200 : 501, payload);
      return;
    }

    const itemsMatch = /^\/api\/sessions\/([^/]+)\/items$/.exec(url.pathname);
    if (itemsMatch) {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      let sessionId;
      try {
        sessionId = decodeURIComponent(itemsMatch[1]);
      } catch {
        sendJson(response, 400, { error: "session_id 编码非法" });
        return;
      }
      const limitValue = Number(url.searchParams.get("limit") || "200");
      const limit = Number.isFinite(limitValue)
        ? Math.min(Math.max(Math.trunc(limitValue), 1), 1000)
        : 200;
      const source = sessionSourceFor(sessionId);
      if (!source) {
        sendJson(response, 200, { session_id: sessionId, source: null, count: 0, items: [], turns: [], reason: "未知会话来源" });
        return;
      }
      const filePath = sessionFilePath(source, sessionId);
      if (!filePath) {
        sendJson(response, 200, { session_id: sessionId, source, count: 0, items: [], turns: [], reason: "未找到该会话的源头文件" });
        return;
      }
      const parsed = readRecentItems(source, filePath, limit);
      // 挂起中的授权请求补一条 approval 条目，手机端据此直接调 /api/approvals/<event_id>
      const includeReasoning = ["1", "true", "yes", "on"].includes(
        (url.searchParams.get("include_reasoning") || "").trim().toLowerCase(),
      );
      const items = [...(parsed?.items || []), ...pendingApprovalItems(sessionId)]
        .filter((item) => includeReasoning || item?.type !== "reasoning");
      sendJson(response, 200, {
        session_id: sessionId,
        source,
        file: filePath,
        count: items.length,
        items,
        turns: parsed?.turns || [],
        reason: parsed ? null : "解析失败",
      });
      return;
    }

    const stopMatch = /^\/api\/sessions\/([^/]+)\/stop$/.exec(url.pathname);
    if (stopMatch) {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      let sessionId;
      try {
        sessionId = decodeURIComponent(stopMatch[1]);
      } catch {
        sendJson(response, 400, { error: "session_id 编码非法" });
        return;
      }
      const result = stopSessionProcess(sessionId);
      if (!result.stopped) {
        sendJson(response, 409, { ok: false, error: "unsupported", message: "该会话不是由中继启动的进程，无法停止" });
        return;
      }
      sendJson(response, 200, { ok: true, session_id: sessionId, via: result.via });
      return;
    }

    const approvalMatch = /^\/api\/approvals\/(\d+)$/.exec(url.pathname);
    if (approvalMatch) {
      if (request.method !== "POST") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      const body = await readBody(request);
      const decision = typeof body.decision === "string" ? body.decision.trim().toLowerCase() : "";
      if (decision !== "allow" && decision !== "deny") {
        sendJson(response, 400, { error: "decision 只能是 allow 或 deny" });
        return;
      }
      const eventId = Number(approvalMatch[1]);
      if (!resolveApproval(eventId, decision)) {
        sendJson(response, 404, { ok: false, error: "not_pending", message: "该授权请求没有挂起或已处理" });
        return;
      }
      sendJson(response, 200, { ok: true, event_id: eventId, decision });
      return;
    }

    const transcriptMatch = /^\/api\/sessions\/([^/]+)\/transcript$/.exec(url.pathname);
    if (transcriptMatch) {
      if (request.method !== "GET") {
        sendJson(response, 405, { error: "Method Not Allowed" });
        return;
      }
      let sessionId;
      try {
        sessionId = decodeURIComponent(transcriptMatch[1]);
      } catch {
        sendJson(response, 400, { error: "session_id 编码非法" });
        return;
      }
      const limitValue = Number(url.searchParams.get("limit") || "200");
      const limit = Number.isFinite(limitValue)
        ? Math.min(Math.max(Math.trunc(limitValue), 1), 1000)
        : 200;
      handleTranscript(response, sessionId, limit);
      return;
    }

    sendJson(response, 404, { error: "Not Found" });
    return;
  }

  sendJson(response, 404, { error: "Not Found" });
}

const server = createServer((request, response) => {
  // 访问日志：只记 /api、/f、/p、/health，字段为 状态码 / 响应字节数 / 耗时
  const startedAtNs = process.hrtime.bigint();
  let sentBytes = 0;
  let pathname = request.url || "/";
  try {
    pathname = new URL(request.url || "/", "http://localhost").pathname;
  } catch {
    // 保留原样
  }
  const shouldLog =
    pathname === "/health" || /^\/(api|f|p)(\/|$)/.test(pathname) || pathname.startsWith("/api/");

  if (shouldLog) {
    const originalWrite = response.write.bind(response);
    response.write = function write(chunk, encoding, callback) {
      if (chunk) {
        sentBytes += typeof chunk === "string" ? Buffer.byteLength(chunk) : chunk.length;
      }
      return originalWrite(chunk, encoding, callback);
    };
    const originalEnd = response.end.bind(response);
    response.end = function end(chunk, encoding, callback) {
      if (chunk && typeof chunk !== "function") {
        sentBytes += typeof chunk === "string" ? Buffer.byteLength(chunk) : chunk.length;
      }
      return originalEnd(chunk, encoding, callback);
    };
    response.on("finish", () => {
      const ms = Number((process.hrtime.bigint() - startedAtNs) / 1_000_000n);
      const status = response.statusCode || 0;
      const tag = status >= 500 ? "[access][error]" : "[access]";
      console.log(
        `${tag} ${new Date().toISOString()} ${request.method} ${pathname} ${status} ${sentBytes}B ${ms}ms`,
      );
    });
  }

  handleRequest(request, response).catch((error) => {
    if (!response.headersSent) {
      sendJson(response, error.statusCode || 500, {
        error: error.statusCode ? error.message : "Internal Server Error",
      });
    } else {
      response.destroy();
    }
    if (!error.statusCode) {
      console.error(error);
    }
  });
});

// Stop 钩子在离开模式下最长挂起 10 分钟，不能被默认的 5 分钟请求超时打断。
server.requestTimeout = 0;
server.headersTimeout = 0;
server.timeout = 0;

// 启动后探测各 agent 的安装状态（5 分钟一次），供 /api/agents 的 installed/version 使用
async function probeAgents() {
  for (const [id, sc] of scanners) {
    try {
      const info = (await sc.detect?.()) || {};
      agentProbe.set(id, { installed: Boolean(info.installed), version: info.version || null, detail: info.detail || null });
    } catch {
      agentProbe.set(id, { installed: false, version: null, detail: null });
    }
  }
  agentsCache.value = null;
  agentsCache.at = 0;
}

// v2：把各 scanner 的原生会话列表（没有事件也能发现）拉进缓存
async function refreshScannerSessions() {
  const rows = [];
  for (const [id, sc] of scanners) {
    if (typeof sc.listSessions !== "function") continue;
    try {
      const list = await sc.listSessions({ limit: 80 });
      if (Array.isArray(list)) {
        for (const r of list) rows.push({ ...r, source: r.source || id });
      }
    } catch (err) {
      console.log(`[scanner] ${id}.listSessions 失败（已忽略）: ${err.message}`);
    }
  }
  scannerRows.rows = rows;
  scannerRows.at = Date.now();
}
probeAgents();
refreshScannerSessions();
const scanTimer = setInterval(() => {
  refreshScannerSessions().catch(() => {});
}, 60 * 1000);
if (scanTimer.unref) scanTimer.unref();
const probeTimer = setInterval(probeAgents, 5 * 60 * 1000);
if (probeTimer.unref) probeTimer.unref();

server.listen(port, host, () => {
  const addresses = [];
  for (const interfaces of Object.values(os.networkInterfaces())) {
    for (const address of interfaces || []) {
      if (address.family === "IPv4" && !address.internal) {
        addresses.push(address.address);
      }
    }
  }

  console.log("Agent Push Relay 已启动");
  console.log(`监听: http://${host}:${port}`);
  if (addresses.length === 0) {
    console.log("局域网 IPv4: 未找到");
  } else {
    for (const address of addresses) {
      console.log(`局域网 IPv4: ${address}:${port}`);
    }
  }
  console.log(`离开模式: ${config.away.enabled ? "开启" : "关闭"}`);
  console.log(
    `端口代理: ${config.proxy.enabled ? `开启（白名单 ${config.proxy.ports.join(",") || "空"}）` : "关闭"}`,
  );
  console.log(`能力链接: ${capabilities.size} 个有效 token（7 天有效）`);
  console.log(
    `Codex 注入命令: ${codexBin} queue --thread <thread> --message <text> --resume（旧版回退 exec resume）`,
  );
  console.log(`控制台: http://127.0.0.1:${port}/console`);
  console.log(`数据目录: ${platform.dataDir}`);
  platform.writeRuntimeInfo({
    pid: process.pid,
    port,
    packaged: platform.isPackaged,
    console_dir: consoleApp.consoleDir(),
    data_dir: platform.dataDir,
    started_at: new Date().toISOString(),
  });
  console.log(`Token: ${config.token}`);
});

server.on("error", (error) => {
  console.error("Relay 启动失败:", error);
  process.exitCode = 1;
});

function shutdown() {
  try {
    consoleApp.stats.flush();
  } catch {
    /* 忽略 */
  }
  for (const list of pendingHolds.values()) {
    for (const hold of [...list]) {
      settleHold(hold, {});
    }
  }
  pendingHolds.clear();
  for (const client of sseClients) {
    client.end();
  }
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(0), 1000).unref();
}

process.on("uncaughtException", (error) => {
  console.error("[relay] 未捕获异常（已忽略，服务继续）:", error?.message || error);
});

process.on("unhandledRejection", (reason) => {
  console.error("[relay] 未处理的 Promise 拒绝（已忽略）:", reason?.message || reason);
});

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
