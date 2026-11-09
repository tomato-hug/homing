// Homing · 每个 agent 在控制台里的接入设置（data/agents/<id>.json）
//
// 只存「控制台表达的态度」：要不要推手机、允不允许读会话、推送方式、回复方式、默认模型。
// 真正生效的开关在各自的配置文件里（钩子、监听、轮询），这里不碰那些文件。

import fs from "node:fs";
import path from "node:path";
import { paths, readJson, writeJson } from "./index.mjs";

export const AGENT_IDS = ["codex", "claude", "workbuddy", "opencode", "zcode"];
export const PUSH_SIGNALS = ["hook", "watch", "poll"];
export const REPLY_MODES = ["auto", "queue", "off"];

function dir() {
  const target = path.join(paths.config, "..", "agents");
  try {
    fs.mkdirSync(target, { recursive: true, mode: 0o700 });
  } catch {
    /* 建不出来时由调用方报错 */
  }
  return target;
}

function fileFor(id) {
  return path.join(dir(), `${id}.json`);
}

export function defaultsFor(id) {
  return {
    id,
    connected: false,
    push_to_phone: false,
    read_sessions: true,
    push_signal: id === "claude" || id === "workbuddy" ? "hook" : "poll",
    reply_mode: "auto",
    model: null,
    order: null,
    updated_at: null,
  };
}

export function read(id) {
  const stored = readJson(fileFor(id), null);
  if (!stored || typeof stored !== "object") return defaultsFor(id);
  return { ...defaultsFor(id), ...stored, id };
}

export function readAll() {
  const out = {};
  for (const id of AGENT_IDS) out[id] = read(id);
  return out;
}

export function patch(id, changes = {}) {
  const current = read(id);
  const next = { ...current };
  if (typeof changes.push_to_phone === "boolean") {
    // 关掉推送等于「不接入」
    next.push_to_phone = changes.push_to_phone;
    next.connected = changes.push_to_phone;
  }
  if (typeof changes.connected === "boolean") {
    next.connected = changes.connected;
    if (!changes.connected && changes.push_to_phone === undefined) next.push_to_phone = false;
  }
  if (typeof changes.read_sessions === "boolean") next.read_sessions = changes.read_sessions;
  if (PUSH_SIGNALS.includes(changes.push_signal)) next.push_signal = changes.push_signal;
  if (REPLY_MODES.includes(changes.reply_mode)) next.reply_mode = changes.reply_mode;
  if (changes.model === null || typeof changes.model === "string") {
    next.model = changes.model || null;
  }
  if (Number.isInteger(changes.order)) next.order = changes.order;
  next.updated_at = new Date().toISOString();
  writeJson(fileFor(id), next);
  return next;
}

/** 卡片顺序：settings 里没写 order 的排在后面，保持注册表顺序 */
export function orderedIds(registryIds = AGENT_IDS) {
  const stored = readAll();
  const withOrder = [];
  const rest = [];
  for (const id of registryIds) {
    const value = stored[id]?.order;
    if (Number.isInteger(value)) withOrder.push({ id, order: value });
    else rest.push(id);
  }
  withOrder.sort((a, b) => a.order - b.order);
  return [...withOrder.map((item) => item.id), ...rest];
}

export function setOrder(ids = []) {
  const clean = ids.filter((id) => AGENT_IDS.includes(id));
  clean.forEach((id, index) => {
    const current = read(id);
    writeJson(fileFor(id), { ...current, order: index, updated_at: new Date().toISOString() });
  });
  return { ok: true, order: clean };
}

export function settingsDir() {
  return dir();
}
