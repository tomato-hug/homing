// Homing · 配对与设备
// 一次性的配对 offer（5 分钟、6 位码）→ 手机 claim → 换发长期设备令牌。
// 令牌只存 sha256，明文只在 claim 的响应里出现一次，任何接口都不会再回显。

import crypto from "node:crypto";
import { paths, readJson, writeJson, lanUrls, hostName, platformLabel, VERSION, PORT } from "./index.mjs";

const OFFER_TTL_MS = 5 * 60_000;
const MAX_ATTEMPTS = 12;
const MAX_DEVICES = 24;

function now() {
  return Date.now();
}

function hash(token) {
  return crypto.createHash("sha256").update(String(token)).digest("hex");
}

function newDeviceToken() {
  return `hm_${crypto.randomBytes(32).toString("base64url")}`;
}

function newPairToken() {
  return crypto.randomBytes(16).toString("hex");
}

function pairCode() {
  // 6 位数字，首位非 0，避免和「000000」这种一眼假的码混淆
  const n = crypto.randomInt(100000, 1000000);
  return String(n);
}

function normalize(value) {
  return String(value ?? "").replace(/\s|-/g, "").trim();
}

function load() {
  const state = readJson(paths.devices, null);
  if (!state || typeof state !== "object") {
    return { version: 1, devices: [], offers: [] };
  }
  if (!Array.isArray(state.devices)) state.devices = [];
  if (!Array.isArray(state.offers)) state.offers = [];
  return state;
}

function save(state) {
  writeJson(paths.devices, state);
}

function pruneOffers(state) {
  const cutoff = now() - 24 * 3600_000;
  state.offers = state.offers.filter((o) => (o.created_ms || 0) > cutoff);
}

function publicOffer(offer, { includeToken = false } = {}) {
  if (!offer) return null;
  const expired = offer.expires_ms <= now();
  return {
    offer_id: offer.id,
    code: offer.code,
    status: offer.claimed_ms ? "claimed" : expired ? "expired" : "waiting",
    created_at: new Date(offer.created_ms).toISOString(),
    expires_at: new Date(offer.expires_ms).toISOString(),
    ttl_s: Math.max(0, Math.round((offer.expires_ms - now()) / 1000)),
    lan_url: offer.lan_url || null,
    lan_urls: offer.lan_urls || [],
    qr_payload: offer.qr_payload || null,
    claimed_device_id: offer.claimed_device_id || null,
    ...(includeToken ? { token: offer.token } : {}),
  };
}

function publicDevice(device) {
  const online = device.last_seen_ms ? now() - device.last_seen_ms < 90_000 : false;
  return {
    device_id: device.id,
    name: device.name || "手机",
    platform: device.platform || "android",
    app_version: device.app_version || null,
    created_at: device.created_at || null,
    last_seen_at: device.last_seen_ms ? new Date(device.last_seen_ms).toISOString() : null,
    online,
    paired_via: device.paired_via || "qr",
    last_ip: device.last_ip || null,
  };
}

/** QR 内容：局域网地址 + 一次性配对令牌。手机解析这个串就够了。 */
function buildQrPayload(token, lanUrl) {
  let host = "127.0.0.1:18787";
  try {
    host = new URL(lanUrl).host;
  } catch {
    /* 用默认值 */
  }
  return `homing://pair?v=1&host=${encodeURIComponent(host)}&token=${token}`;
}

export function createOffer({ lanUrl = null, ttlMs = OFFER_TTL_MS } = {}) {
  const state = load();
  pruneOffers(state);
  const urls = lanUrls(PORT);
  const chosen = lanUrl || urls[0] || `http://127.0.0.1:${PORT}`;
  const token = newPairToken();

  // 6 位码在「还没过期的 offer」里必须唯一，否则手机可能配错电脑
  const live = new Set(state.offers.filter((o) => o.expires_ms > now() && !o.claimed_ms).map((o) => o.code));
  let code = pairCode();
  for (let i = 0; i < 20 && live.has(code); i += 1) code = pairCode();

  const offer = {
    id: crypto.randomBytes(8).toString("hex"),
    token,
    code,
    created_ms: now(),
    expires_ms: now() + Math.min(Math.max(ttlMs, 30_000), OFFER_TTL_MS * 4),
    claimed_ms: null,
    claimed_device_id: null,
    attempts: 0,
    lan_url: chosen,
    lan_urls: urls,
    qr_payload: buildQrPayload(token, chosen),
  };
  // 新 offer 上线后，旧的未使用 offer 作废，避免同时有多个可用码
  state.offers = state.offers.filter((o) => o.claimed_ms);
  state.offers.push(offer);
  save(state);
  return publicOffer(offer, { includeToken: true });
}

export function getOffer(id) {
  const state = load();
  const offer = state.offers.find((o) => o.id === id);
  return publicOffer(offer);
}

export function latestOffer() {
  const state = load();
  const offer = state.offers
    .filter((o) => o.expires_ms > now() && !o.claimed_ms)
    .sort((a, b) => b.created_ms - a.created_ms)[0];
  return publicOffer(offer);
}

export function findOfferByCode(code) {
  const state = load();
  return state.offers.find((o) => o.code === normalize(code) && !o.claimed_ms) || null;
}

/**
 * 手机端配对。token 或 code 二选一。
 * @returns {{ok: true, device: object, device_token: string, server: object} | {ok: false, status: number, error: string}}
 */
export function claim({ token = null, code = null, device = {}, ip = null } = {}) {
  const state = load();
  pruneOffers(state);
  const cleanToken = normalize(token);
  const cleanCode = normalize(code);
  if (!cleanToken && !cleanCode) {
    return { ok: false, status: 400, error: "需要 token 或 code" };
  }

  const offer = cleanToken
    ? state.offers.find((o) => o.token === cleanToken)
    : state.offers.find((o) => o.code === cleanCode);

  if (!offer) {
    return { ok: false, status: 404, error: "配对码无效，请在电脑上换一个" };
  }
  if (offer.claimed_ms) {
    // 同一个 offer 重复使用：直接说清楚，不要静默成功
    return { ok: false, status: 409, error: "这个配对码已经用过了，请在电脑上换一个" };
  }
  if (offer.expires_ms <= now()) {
    return { ok: false, status: 410, error: "配对码已过期（5 分钟内有效），请换一个" };
  }
  offer.attempts = (offer.attempts || 0) + 1;
  if (offer.attempts > MAX_ATTEMPTS) {
    save(state);
    return { ok: false, status: 429, error: "尝试次数过多，请重新开始配对" };
  }

  const deviceToken = newDeviceToken();
  const record = {
    id: crypto.randomBytes(6).toString("hex"),
    name: String(device.name || "").trim().slice(0, 60) || "手机",
    platform: String(device.platform || "android").slice(0, 20),
    app_version: device.app_version ? String(device.app_version).slice(0, 32) : null,
    token_hash: hash(deviceToken),
    created_at: new Date().toISOString(),
    created_ms: now(),
    last_seen_ms: now(),
    last_ip: ip,
    paired_via: cleanToken ? "qr" : "code",
    offer_id: offer.id,
  };

  state.devices.push(record);
  // 设备太多时丢掉最久没见过的（只丢令牌，不动任何会话数据）
  if (state.devices.length > MAX_DEVICES) {
    state.devices.sort((a, b) => (b.last_seen_ms || 0) - (a.last_seen_ms || 0));
    state.devices = state.devices.slice(0, MAX_DEVICES);
  }
  offer.claimed_ms = now();
  offer.claimed_device_id = record.id;
  save(state);

  return {
    ok: true,
    device: publicDevice(record),
    device_token: deviceToken,
    server: {
      name: hostName(),
      platform: platformLabel(),
      lan_url: offer.lan_url,
      lan_urls: offer.lan_urls || [],
      version: VERSION,
      token_type: "device",
    },
  };
}

export function listDevices() {
  const state = load();
  return state.devices
    .slice()
    .sort((a, b) => (b.last_seen_ms || 0) - (a.last_seen_ms || 0))
    .map(publicDevice);
}

export function revokeDevice(id) {
  const state = load();
  const before = state.devices.length;
  state.devices = state.devices.filter((d) => d.id !== id);
  if (state.devices.length === before) return { ok: false, error: "没有这台设备" };
  save(state);
  return { ok: true, device_id: id };
}

export function renameDevice(id, name) {
  const state = load();
  const device = state.devices.find((d) => d.id === id);
  if (!device) return { ok: false, error: "没有这台设备" };
  device.name = String(name || "").trim().slice(0, 60) || device.name;
  save(state);
  return { ok: true, device: publicDevice(device) };
}

/** 设备令牌 → 设备记录（/api/* 鉴权时用） */
export function verifyDeviceToken(token) {
  const raw = String(token || "").trim();
  if (!raw.startsWith("hm_")) return null;
  const state = load();
  const hashed = hash(raw);
  const device = state.devices.find((d) => d.token_hash === hashed);
  if (!device) return null;
  return publicDevice(device);
}

export function heartbeat(deviceId, { app_version = null, ip = null } = {}) {
  const state = load();
  const device = state.devices.find((d) => d.id === deviceId);
  if (!device) return null;
  device.last_seen_ms = now();
  if (app_version) device.app_version = String(app_version).slice(0, 32);
  if (ip) device.last_ip = ip;
  save(state);
  return publicDevice(device);
}

export function touchByToken(token, { app_version = null, ip = null } = {}) {
  const device = verifyDeviceToken(token);
  if (!device) return null;
  return heartbeat(device.device_id, { app_version, ip });
}

export const pairingConfig = { OFFER_TTL_MS, MAX_ATTEMPTS, MAX_DEVICES };
