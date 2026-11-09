import http from "node:http";
import { URL } from "node:url";
import { WebSocket, WebSocketServer } from "ws";
import {
  deviceIdFromPublicKey,
  fingerprintFromPublicKey,
  fromBase64,
  randomBytes,
  randomCode6,
  randomPairToken,
  ready,
  toBase64,
  verifyHelloSignature,
} from "@homing/protocol-js";
import { HomingDb } from "./db.mjs";
import { PushDispatcher, SUPPORTED_PROVIDERS } from "./push.mjs";

const CHALLENGE_TTL_MS = 15_000;
const PAIR_TTL_MS = 300_000;
const MAX_CODE_ATTEMPTS = 5;
const HELLO_CLOCK_SKEW_MS = 300_000;

function normalizeAddress(address) {
  if (!address) return "unknown";
  return address.startsWith("::ffff:") ? address.slice(7) : address;
}

function sendJson(ws, value) {
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(value));
    return true;
  }
  return false;
}

function error(ws, code, message) {
  sendJson(ws, { t: "error", code, message });
}

export async function createHomingServer({
  port = 0,
  host = "127.0.0.1",
  dbPath = ":memory:",
  logger = console,
  pushDispatcher = null,
} = {}) {
  await ready();
  const db = new HomingDb(dbPath);
  const push = pushDispatcher ?? new PushDispatcher({ db, logger });
  const httpServer = http.createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://localhost");
    if (request.method === "GET" && url.pathname === "/health") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ ok: true, service: "homing-server", version: 1 }));
      return;
    }
    response.writeHead(404, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ ok: false, error: "not_found" }));
  });
  const wss = new WebSocketServer({ noServer: true, maxPayload: 25 * 1024 * 1024 });
  const connections = new Map();
  const pairTokens = new Map();
  const codeIndex = new Map();
  const pendingPairs = new Map();
  const pairAttempts = new Map();

  const addConnection = (ws) => {
    let set = connections.get(ws.deviceId);
    if (!set) {
      set = new Set();
      connections.set(ws.deviceId, set);
    }
    set.add(ws);
  };

  const removeConnection = (ws) => {
    const set = connections.get(ws.deviceId);
    if (!set) return;
    set.delete(ws);
    if (set.size === 0) connections.delete(ws.deviceId);
  };

  const sendTo = (deviceId, value) => {
    const set = connections.get(deviceId);
    if (!set) return 0;
    let sent = 0;
    for (const socket of set) {
      if (sendJson(socket, value)) sent += 1;
    }
    return sent;
  };

  const now = () => Date.now();

  const prunePairTokens = () => {
    const timestamp = now();
    for (const [token, entry] of pairTokens) {
      if (entry.expiresAt <= timestamp) {
        pairTokens.delete(token);
        if (codeIndex.get(entry.code6) === token) codeIndex.delete(entry.code6);
      }
    }
  };

  const pairTokenForCode = (code6) => {
    prunePairTokens();
    const token = codeIndex.get(code6);
    if (!token) return null;
    const entry = pairTokens.get(token);
    if (!entry || entry.used || entry.expiresAt <= now()) return null;
    return { token, entry };
  };

  const registerPushToken = (deviceId, provider, token) => {
    if (!SUPPORTED_PROVIDERS.has(provider) || typeof token !== "string" || !token) {
      return false;
    }
    db.savePushToken(deviceId, provider, token);
    return true;
  };

  const flushQueue = (ws) => {
    for (const envelope of db.queuedFor(ws.deviceId)) {
      sendJson(ws, envelope);
    }
  };

  const cleanupPair = (token) => {
    const entry = pairTokens.get(token);
    if (entry && codeIndex.get(entry.code6) === token) codeIndex.delete(entry.code6);
    pairTokens.delete(token);
    for (const [key, pending] of pendingPairs) {
      if (pending.token === token) pendingPairs.delete(key);
    }
  };

  const handleHello = (ws, message) => {
    if (ws.deviceId) return error(ws, "already_authenticated", "设备已经完成认证");
    let pub;
    let signature;
    try {
      pub = fromBase64(message.pub);
      signature = fromBase64(message.sig);
    } catch {
      return error(ws, "invalid_hello", "公钥或签名不是有效 base64");
    }
    if (pub.length !== 32) return error(ws, "invalid_hello", "Ed25519 公钥必须是 32 字节");
    const deviceId = deviceIdFromPublicKey(pub);
    if (deviceId !== message.device_id) {
      return error(ws, "device_id_mismatch", "device_id 与公钥不匹配");
    }
    const ts = typeof message.ts === "string" ? message.ts : "";
    const timestamp = Date.parse(ts);
    if (!Number.isFinite(timestamp) || Math.abs(Date.now() - timestamp) > HELLO_CLOCK_SKEW_MS) {
      return error(ws, "stale_hello", "hello 时间戳超出允许范围");
    }
    let valid = false;
    try {
      valid = verifyHelloSignature(pub, ws.challengeNonce, ts, signature);
    } catch {
      valid = false;
    }
    if (!valid) return error(ws, "invalid_signature", "hello 签名校验失败");

    const pubBase64 = toBase64(pub);
    if (!db.registerDevice(deviceId, pubBase64)) {
      return error(ws, "device_key_changed", "device_id 已绑定另一个公钥");
    }
    ws.deviceId = deviceId;
    ws.pub = pubBase64;
    addConnection(ws);
    sendJson(ws, {
      t: "hello.ok",
      device_id: deviceId,
      fingerprint: fingerprintFromPublicKey(pub),
    });
    flushQueue(ws);
  };

  const handlePairStart = (ws) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    prunePairTokens();
    const token = randomPairToken();
    let code6 = randomCode6();
    while (codeIndex.has(code6)) code6 = randomCode6();
    const entry = {
      hostId: ws.deviceId,
      code6,
      expiresAt: now() + PAIR_TTL_MS,
      used: false,
    };
    pairTokens.set(token, entry);
    codeIndex.set(code6, token);
    sendJson(ws, { t: "pair.start.ok", pair_token: token, code6, expires_in: 300 });
  };

  const handlePairJoin = (ws, message) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    const ip = normalizeAddress(ws.socket?.remoteAddress);
    let token = typeof message.token === "string" ? message.token : null;
    if (!token && typeof message.code6 === "string") {
      const attempt = pairAttempts.get(ip);
      if (attempt && attempt.resetAt > now() && attempt.count >= MAX_CODE_ATTEMPTS) {
        return error(ws, "too_many_attempts", "配对码尝试次数过多，请重新生成");
      }
      const result = pairTokenForCode(message.code6);
      if (!result) {
        const current = pairAttempts.get(ip);
        const next = !current || current.resetAt <= now()
          ? { count: 1, resetAt: now() + PAIR_TTL_MS }
          : { ...current, count: current.count + 1 };
        pairAttempts.set(ip, next);
        return error(
          ws,
          next.count >= MAX_CODE_ATTEMPTS ? "too_many_attempts" : "invalid_code",
          next.count >= MAX_CODE_ATTEMPTS
            ? "配对码尝试次数过多，请重新生成"
            : "配对码不正确",
        );
      }
      token = result.token;
    }
    if (!token) return error(ws, "invalid_pair", "需要 pair_token 或 code6");

    prunePairTokens();
    const entry = pairTokens.get(token);
    if (!entry || entry.used || entry.expiresAt <= now()) {
      return error(ws, "invalid_pair_token", "配对 token 已失效");
    }
    if (entry.hostId === ws.deviceId) {
      return error(ws, "self_pair", "不能和自己配对");
    }
    if (!connections.has(entry.hostId)) {
      return error(ws, "host_offline", "电脑当前不在线");
    }
    let pub;
    try {
      pub = fromBase64(message.pub);
    } catch {
      return error(ws, "invalid_pub", "客户端公钥不是有效 base64");
    }
    if (pub.length !== 32) return error(ws, "invalid_pub", "客户端公钥必须是 32 字节");
    const clientId = deviceIdFromPublicKey(pub);
    if (message.client_id && message.client_id !== clientId) {
      return error(ws, "client_id_mismatch", "client_id 与公钥不匹配");
    }
    const host = db.getDevice(entry.hostId);
    if (!host) return error(ws, "host_missing", "电脑设备记录不存在");

    entry.used = true;
    const pending = {
      token,
      hostId: entry.hostId,
      hostPub: host.pub,
      clientId,
      clientPub: toBase64(pub),
      name: typeof message.name === "string" ? message.name.slice(0, 120) : "",
      platform: typeof message.platform === "string" ? message.platform.slice(0, 40) : "",
      expiresAt: entry.expiresAt,
    };
    pendingPairs.set(`${entry.hostId}:${clientId}`, pending);
    pendingPairs.set(`token:${token}`, pending);
    sendTo(entry.hostId, {
      t: "pair.request",
      client_id: clientId,
      pub: pending.clientPub,
      name: pending.name,
      platform: pending.platform,
      pair_token: token,
    });
    sendJson(ws, {
      t: "pair.waiting",
      host_id: entry.hostId,
      host_pub: host.pub,
      host_fingerprint: fingerprintFromPublicKey(fromBase64(host.pub)),
      expires_in: Math.max(0, Math.floor((entry.expiresAt - now()) / 1000)),
    });
  };

  const handlePairDecision = (ws, message, accept) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    const clientId = typeof message.client_id === "string" ? message.client_id : "";
    const pending = pendingPairs.get(`${ws.deviceId}:${clientId}`);
    if (!pending) return error(ws, "pair_not_pending", "没有等待处理的配对请求");
    if (pending.expiresAt <= now()) {
      cleanupPair(pending.token);
      return error(ws, "pair_expired", "配对请求已过期");
    }
    if (accept) {
      db.createPairing(pending.hostId, pending.clientId);
      sendJson(ws, { t: "pair.ok", client_id: clientId });
      sendTo(pending.clientId, {
        t: "pair.accepted",
        host_id: pending.hostId,
        host_pub: pending.hostPub,
        client_id: pending.clientId,
      });
    } else {
      sendJson(ws, { t: "pair.rejected", client_id: clientId });
      sendTo(pending.clientId, { t: "pair.rejected", host_id: pending.hostId });
    }
    cleanupPair(pending.token);
  };

  const handleRevoke = (ws, message) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    const peerId = typeof message.peer_id === "string" ? message.peer_id : "";
    if (!peerId || !db.isPaired(ws.deviceId, peerId)) {
      return error(ws, "not_paired", "没有与该设备的配对");
    }
    db.revokePairing(ws.deviceId, peerId);
    sendTo(peerId, { t: "pair.revoked", peer_id: ws.deviceId });
    sendJson(ws, { t: "pair.revoked.ok", peer_id: peerId });
  };

  const handleMessage = (ws, message) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    if (message.v !== 1 || message.t !== "msg") {
      return error(ws, "invalid_envelope", "不支持的消息封套");
    }
    if (message.from !== ws.deviceId) return error(ws, "from_mismatch", "from 与当前设备不一致");
    if (typeof message.to !== "string" || !message.to) {
      return error(ws, "invalid_envelope", "缺少收件设备");
    }
    if (!message.id || typeof message.id !== "string") {
      return error(ws, "invalid_envelope", "缺少消息 id");
    }
    if (!db.isPaired(ws.deviceId, message.to)) {
      return error(ws, "not_paired", "只能给已配对设备发消息");
    }
    const forwarded = sendTo(message.to, message);
    if (forwarded === 0) {
      db.enqueue(message);
      void push.wake(message.to).catch((pushError) => {
        logger.warn?.(`[push] wake failed: ${pushError.message}`);
      });
    }
  };

  const handleAck = (ws, message) => {
    if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
    if (typeof message.id !== "string") return error(ws, "invalid_ack", "缺少消息 id");
    db.ack(ws.deviceId, message.id);
  };

  wss.on("connection", (ws, request) => {
    ws.challengeNonce = toBase64(randomBytes(32));
    ws.isAlive = true;
    ws.socket = request.socket;
    ws.on("pong", () => {
      ws.isAlive = true;
    });
    const challengeTimer = setTimeout(() => {
      if (!ws.deviceId) ws.close(4001, "hello timeout");
    }, CHALLENGE_TTL_MS);
    challengeTimer.unref?.();
    sendJson(ws, { t: "challenge", nonce: ws.challengeNonce });

    ws.on("message", (raw) => {
      let message;
      try {
        message = JSON.parse(raw.toString());
      } catch {
        return error(ws, "invalid_json", "消息不是有效 JSON");
      }
      const type = message?.t;
      try {
        if (type === "hello") return handleHello(ws, message);
        if (!ws.deviceId) return error(ws, "not_authenticated", "请先完成 hello");
        switch (type) {
          case "pair.start":
            return handlePairStart(ws);
          case "pair.join":
            return handlePairJoin(ws, message);
          case "pair.accept":
            return handlePairDecision(ws, message, true);
          case "pair.reject":
            return handlePairDecision(ws, message, false);
          case "pair.revoke":
            return handleRevoke(ws, message);
          case "msg":
            return handleMessage(ws, message);
          case "ack":
            return handleAck(ws, message);
          case "push.register":
            if (!registerPushToken(ws.deviceId, message.provider, message.token)) {
              return error(ws, "invalid_push", "推送 provider 或 token 无效");
            }
            return sendJson(ws, { t: "push.registered", provider: message.provider });
          case "ping":
            return sendJson(ws, { t: "pong", ts: new Date().toISOString() });
          case "pong":
            return;
          default:
            return error(ws, "unknown_type", `未知消息类型: ${String(type)}`);
        }
      } catch (handlerError) {
        logger.error?.(`[ws] handler failed: ${handlerError.stack ?? handlerError.message}`);
        error(ws, "internal_error", "服务器处理消息失败");
      }
    });
    ws.on("close", () => {
      clearTimeout(challengeTimer);
      removeConnection(ws);
    });
    ws.on("error", (socketError) => {
      logger.warn?.(`[ws] socket error: ${socketError.message}`);
    });
  });

  httpServer.on("upgrade", (request, socket, head) => {
    const url = new URL(request.url ?? "/", "http://localhost");
    if (url.pathname !== "/v1/ws") {
      socket.write("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n");
      socket.destroy();
      return;
    }
    wss.handleUpgrade(request, socket, head, (ws) => {
      wss.emit("connection", ws, request);
    });
  });

  const heartbeat = setInterval(() => {
    for (const ws of wss.clients) {
      if (!ws.isAlive) {
        ws.terminate();
        continue;
      }
      ws.isAlive = false;
      try {
        ws.ping();
      } catch {
        ws.terminate();
      }
    }
  }, 25_000);
  heartbeat.unref?.();

  const cleanup = setInterval(() => {
    prunePairTokens();
    const timestamp = now();
    for (const [key, pending] of pendingPairs) {
      if (pending.expiresAt <= timestamp) {
        pendingPairs.delete(key);
      }
    }
    for (const [ip, attempt] of pairAttempts) {
      if (attempt.resetAt <= timestamp) pairAttempts.delete(ip);
    }
  }, 30_000);
  cleanup.unref?.();

  await new Promise((resolve, reject) => {
    httpServer.once("error", reject);
    httpServer.listen(port, host, () => {
      httpServer.off("error", reject);
      resolve();
    });
  });
  const address = httpServer.address();
  const actualPort = typeof address === "object" && address ? address.port : port;

  return {
    db,
    push,
    httpServer,
    wss,
    port: actualPort,
    url: `ws://127.0.0.1:${actualPort}/v1/ws`,
    async close() {
      clearInterval(heartbeat);
      clearInterval(cleanup);
      for (const ws of wss.clients) ws.terminate();
      await new Promise((resolve) => wss.close(resolve));
      await new Promise((resolve) => httpServer.close(resolve));
      db.close();
    },
  };
}
