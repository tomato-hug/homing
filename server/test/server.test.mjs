import assert from "node:assert/strict";
import { once } from "node:events";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import WebSocket from "ws";
import * as protocol from "@homing/protocol-js";
import { createHomingServer } from "../src/server.mjs";

const HOST_SEED = "homing-test-host-seed-0000000001";
const CLIENT_SEED = "homing-test-client-seed-00000001";

await protocol.ready();

class Peer {
  constructor(ws) {
    this.ws = ws;
    this.messages = [];
    this.waiters = [];
    ws.on("message", (raw) => {
      let message;
      try {
        message = JSON.parse(raw.toString());
      } catch {
        return;
      }
      const waiterIndex = this.waiters.findIndex((waiter) => waiter.predicate(message));
      if (waiterIndex >= 0) {
        const [waiter] = this.waiters.splice(waiterIndex, 1);
        clearTimeout(waiter.timer);
        waiter.resolve(message);
      } else {
        this.messages.push(message);
      }
    });
  }

  send(message) {
    this.ws.send(JSON.stringify(message));
  }

  next(predicate, timeoutMs = 2_000) {
    const index = this.messages.findIndex(predicate);
    if (index >= 0) return Promise.resolve(this.messages.splice(index, 1)[0]);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        const waiterIndex = this.waiters.findIndex((waiter) => waiter.timer === timer);
        if (waiterIndex >= 0) this.waiters.splice(waiterIndex, 1);
        reject(new Error("timed out waiting for websocket message"));
      }, timeoutMs);
      this.waiters.push({ predicate, resolve, timer });
    });
  }

  has(predicate) {
    return this.messages.some(predicate);
  }

  async close() {
    if (this.ws.readyState === WebSocket.CLOSED) return;
    this.ws.close();
    await Promise.race([
      once(this.ws, "close"),
      new Promise((resolve) => setTimeout(resolve, 300)),
    ]);
  }
}

async function connectPeer(url, seed) {
  const keypair = protocol.keypairFromSeed(seed);
  const pub = protocol.toBase64(keypair.publicKey);
  const deviceId = protocol.deviceIdFromPublicKey(keypair.publicKey);
  const ws = new WebSocket(url);
  const peer = new Peer(ws);
  await once(ws, "open");
  const challenge = await peer.next((message) => message.t === "challenge");
  const ts = new Date().toISOString();
  const signature = protocol.helloSignature(keypair.secretKey, challenge.nonce, ts);
  peer.send({
    t: "hello",
    device_id: deviceId,
    pub,
    ts,
    sig: protocol.toBase64(signature),
  });
  const answer = await peer.next((message) => message.t === "hello.ok" || message.t === "error");
  if (answer.t === "error") throw new Error(`${answer.code}: ${answer.message}`);
  return { peer, keypair, pub, deviceId };
}

async function pair(host, client, mode = "token") {
  host.peer.send({ t: "pair.start" });
  const started = await host.peer.next((message) => message.t === "pair.start.ok");
  const join = {
    t: "pair.join",
    pub: client.pub,
    name: "测试手机",
    platform: "android",
  };
  if (mode === "token") join.token = started.pair_token;
  else join.code6 = started.code6;
  client.peer.send(join);
  const request = await host.peer.next((message) => message.t === "pair.request");
  const waiting = await client.peer.next((message) => message.t === "pair.waiting");
  host.peer.send({ t: "pair.accept", client_id: client.deviceId });
  const accepted = await client.peer.next((message) => message.t === "pair.accepted");
  const ok = await host.peer.next((message) => message.t === "pair.ok");
  return { started, request, waiting, accepted, ok };
}

async function withServer(options, fn) {
  const server = await createHomingServer(options);
  try {
    return await fn(server);
  } finally {
    await server.close();
  }
}

test("pairing works through both the QR token and the 6-digit code", async () => {
  await withServer({ port: 0 }, async (server) => {
    const host = await connectPeer(server.url, HOST_SEED);
    const client = await connectPeer(server.url, CLIENT_SEED);
    const tokenPair = await pair(host, client, "token");
    assert.equal(tokenPair.request.client_id, client.deviceId);
    assert.equal(
      protocol.verifyHostFingerprint(
        protocol.fromBase64(tokenPair.waiting.host_pub),
        tokenPair.waiting.host_fingerprint,
      ),
      true,
    );
    assert.equal(
      protocol.verifyHostFingerprint(
        protocol.fromBase64(tokenPair.waiting.host_pub),
        "0000000000000000",
      ),
      false,
    );
    assert.equal(server.db.isPaired(host.deviceId, client.deviceId), true);

    const codePair = await pair(host, client, "code");
    assert.equal(codePair.accepted.host_id, host.deviceId);
    assert.equal(server.db.isPaired(host.deviceId, client.deviceId), true);
    await host.peer.close();
    await client.peer.close();
  });
});

test("a wrong 6-digit code is limited to five attempts per IP", async () => {
  await withServer({ port: 0 }, async (server) => {
    const host = await connectPeer(server.url, HOST_SEED);
    const client = await connectPeer(server.url, CLIENT_SEED);
    host.peer.send({ t: "pair.start" });
    const started = await host.peer.next((message) => message.t === "pair.start.ok");
    const wrong = String((Number(started.code6) + 1) % 1_000_000).padStart(6, "0");
    for (let attempt = 0; attempt < 5; attempt += 1) {
      client.peer.send({
        t: "pair.join",
        code6: wrong,
        pub: client.pub,
        name: "测试手机",
        platform: "android",
      });
      const answer = await client.peer.next((message) => message.t === "error");
      assert.ok(["invalid_code", "too_many_attempts"].includes(answer.code));
    }
    client.peer.send({
      t: "pair.join",
      code6: started.code6,
      pub: client.pub,
      name: "测试手机",
      platform: "android",
    });
    const blocked = await client.peer.next((message) => message.t === "error");
    assert.equal(blocked.code, "too_many_attempts");
    assert.equal(host.peer.has((message) => message.t === "pair.request"), false);
    await host.peer.close();
    await client.peer.close();
  });
});

test("messages are only forwarded between paired devices", async () => {
  await withServer({ port: 0 }, async (server) => {
    const host = await connectPeer(server.url, HOST_SEED);
    const client = await connectPeer(server.url, CLIENT_SEED);
    const clientKeypair = protocol.keypairFromSeed(CLIENT_SEED);
    const envelope = protocol.createEnvelope({
      from: host.deviceId,
      to: client.deviceId,
      payload: { type: "event", name: "message", data: { text: "not paired" } },
      senderSecretKey: host.keypair.secretKey,
      senderPublicKey: host.keypair.publicKey,
      recipientPublicKey: clientKeypair.publicKey,
    });
    host.peer.send(envelope);
    const rejected = await host.peer.next((message) => message.t === "error");
    assert.equal(rejected.code, "not_paired");
    assert.equal(client.peer.has((message) => message.t === "msg"), false);
    await host.peer.close();
    await client.peer.close();
  });
});

test("offline messages are queued, replayed and removed by ack", async () => {
  let wakeCount = 0;
  await withServer(
    {
      port: 0,
      pushDispatcher: {
        async wake() {
          wakeCount += 1;
        },
      },
    },
    async (server) => {
      const host = await connectPeer(server.url, HOST_SEED);
      const client = await connectPeer(server.url, CLIENT_SEED);
      await pair(host, client, "token");
      await client.peer.close();
      await new Promise((resolve) => setTimeout(resolve, 100));
      const clientKeypair = protocol.keypairFromSeed(CLIENT_SEED);
      const envelope = protocol.createEnvelope({
        from: host.deviceId,
        to: client.deviceId,
        payload: { type: "event", name: "message", data: { text: "离线消息" } },
        senderSecretKey: host.keypair.secretKey,
        senderPublicKey: host.keypair.publicKey,
        recipientPublicKey: clientKeypair.publicKey,
      });
      host.peer.send(envelope);
      await new Promise((resolve) => setTimeout(resolve, 100));
      assert.equal(server.db.queueCount(client.deviceId), 1);
      assert.equal(wakeCount, 1);

      const reconnected = await connectPeer(server.url, CLIENT_SEED);
      const replayed = await reconnected.peer.next((message) => message.t === "msg");
      assert.equal(replayed.id, envelope.id);
      reconnected.peer.send({ t: "ack", id: envelope.id });
      await new Promise((resolve) => setTimeout(resolve, 100));
      assert.equal(server.db.queueCount(client.deviceId), 0);
      await reconnected.peer.close();
      await host.peer.close();
    },
  );
});

test("revoking a pairing removes the pairing and queued messages", async () => {
  await withServer({ port: 0 }, async (server) => {
    const host = await connectPeer(server.url, HOST_SEED);
    const client = await connectPeer(server.url, CLIENT_SEED);
    await pair(host, client, "token");
    await client.peer.close();
    await new Promise((resolve) => setTimeout(resolve, 100));
    const clientKeypair = protocol.keypairFromSeed(CLIENT_SEED);
    host.peer.send(protocol.createEnvelope({
      from: host.deviceId,
      to: client.deviceId,
      payload: { type: "event", name: "message", data: { text: "before revoke" } },
      senderSecretKey: host.keypair.secretKey,
      senderPublicKey: host.keypair.publicKey,
      recipientPublicKey: clientKeypair.publicKey,
    }));
    await new Promise((resolve) => setTimeout(resolve, 100));
    assert.equal(server.db.queueCount(client.deviceId), 1);
    host.peer.send({ t: "pair.revoke", peer_id: client.deviceId });
    const revoked = await host.peer.next((message) => message.t === "pair.revoked.ok");
    assert.equal(revoked.peer_id, client.deviceId);
    assert.equal(server.db.isPaired(host.deviceId, client.deviceId), false);
    assert.equal(server.db.queueCount(client.deviceId), 0);
    await host.peer.close();
  });
});

test("the sqlite database never contains plaintext payloads", async () => {
  const directory = mkdtempSync(join(tmpdir(), "homing-server-test-"));
  const dbPath = join(directory, "homing.sqlite");
  const marker = "HOMING_PLAINTEXT_MARKER_DO_NOT_PERSIST";
  try {
    await withServer({ port: 0, dbPath }, async (server) => {
      const host = await connectPeer(server.url, HOST_SEED);
      const client = await connectPeer(server.url, CLIENT_SEED);
      await pair(host, client, "token");
      await client.peer.close();
      await new Promise((resolve) => setTimeout(resolve, 100));
      const clientKeypair = protocol.keypairFromSeed(CLIENT_SEED);
      host.peer.send(protocol.createEnvelope({
        from: host.deviceId,
        to: client.deviceId,
        payload: { type: "event", name: "message", data: { text: marker } },
        senderSecretKey: host.keypair.secretKey,
        senderPublicKey: host.keypair.publicKey,
        recipientPublicKey: clientKeypair.publicKey,
      }));
      await new Promise((resolve) => setTimeout(resolve, 100));
      assert.equal(server.db.queueCount(client.deviceId), 1);
      await host.peer.close();
    });
    const bytes = readFileSync(dbPath);
    assert.equal(bytes.includes(Buffer.from(marker)), false);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
