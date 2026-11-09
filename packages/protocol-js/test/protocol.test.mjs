import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import * as protocol from "../src/index.mjs";

const vectors = JSON.parse(
  readFileSync(new URL("../test-vectors.json", import.meta.url), "utf8"),
);

test("cross-language device identity, curve keys and SAS vectors", async () => {
  await protocol.ready();
  const host = protocol.keypairFromSeed(vectors.host.seed);
  const client = protocol.keypairFromSeed(vectors.client.seed);
  assert.equal(protocol.bytesToHex(host.publicKey), vectors.host.publicKeyHex);
  assert.equal(protocol.bytesToHex(client.publicKey), vectors.client.publicKeyHex);
  assert.equal(protocol.bytesToHex(protocol.curveKeys(host.publicKey, host.secretKey).publicKey), vectors.host.curvePublicKeyHex);
  assert.equal(protocol.bytesToHex(protocol.curveKeys(client.publicKey, client.secretKey).publicKey), vectors.client.curvePublicKeyHex);
  assert.equal(protocol.deviceIdFromPublicKey(host.publicKey), vectors.host.deviceId);
  assert.equal(protocol.fingerprintFromPublicKey(host.publicKey), vectors.host.fingerprint);
  assert.equal(protocol.deviceIdFromPublicKey(client.publicKey), vectors.client.deviceId);
  assert.deepEqual(protocol.sasWords(host.publicKey, client.publicKey), vectors.sas.words);
  assert.equal(
    protocol.verifyHostFingerprint(host.publicKey, vectors.host.fingerprint),
    true,
  );
  assert.equal(
    protocol.verifyHostFingerprint(host.publicKey, "0000000000000000"),
    false,
  );
});

test("hello signature vector verifies and rejects tampering", async () => {
  await protocol.ready();
  const host = protocol.keypairFromSeed(vectors.host.seed);
  const signature = protocol.hexToBytes(vectors.hello.signatureHex);
  assert.equal(
    Buffer.from(signature).toString("hex"),
    vectors.hello.signatureHex,
  );
  assert.equal(
    protocol.verifyHelloSignature(
      host.publicKey,
      vectors.hello.nonce,
      vectors.hello.ts,
      signature,
    ),
    true,
  );
  assert.equal(
    protocol.verifyHelloSignature(
      host.publicKey,
      vectors.hello.nonce,
      `${vectors.hello.ts}x`,
      signature,
    ),
    false,
  );
});

test("envelope vector decrypts and re-encrypts byte for byte", async () => {
  await protocol.ready();
  const host = protocol.keypairFromSeed(vectors.host.seed);
  const client = protocol.keypairFromSeed(vectors.client.seed);
  assert.deepEqual(
    protocol.openEnvelope(vectors.envelope, {
      recipientSecretKey: client.secretKey,
      senderPublicKey: host.publicKey,
    }),
    vectors.envelope.payload,
  );
  const rebuilt = protocol.createEnvelope({
    from: vectors.envelope.from,
    to: vectors.envelope.to,
    payload: vectors.envelope.payload,
    senderSecretKey: host.secretKey,
    senderPublicKey: host.publicKey,
    recipientPublicKey: client.publicKey,
    id: vectors.envelope.id,
    ts: vectors.envelope.ts,
    nonce: protocol.fromBase64(vectors.envelope.nonce),
  });
  assert.equal(rebuilt.ct, vectors.envelope.ct);
  assert.equal(rebuilt.nonce, vectors.envelope.nonce);
});

test("tampered ciphertext is rejected", async () => {
  await protocol.ready();
  const host = protocol.keypairFromSeed(vectors.host.seed);
  const client = protocol.keypairFromSeed(vectors.client.seed);
  const tampered = {
    ...vectors.envelope,
    ct: `${vectors.envelope.ct.slice(0, -4)}AAAA`,
  };
  assert.throws(() =>
    protocol.openEnvelope(tampered, {
      recipientSecretKey: client.secretKey,
      senderPublicKey: host.publicKey,
    }),
  );
});

test("pairing URI and code helpers", async () => {
  await protocol.ready();
  const uri = protocol.pairUri({
    server: "wss://relay.example.test/v1/ws",
    token: "demo-pair-token",
    hostId: "demo-host",
    fingerprint: "0011223344556677",
  });
  assert.deepEqual(protocol.parsePairUri(uri), {
    server: "wss://relay.example.test/v1/ws",
    token: "demo-pair-token",
    hostId: "demo-host",
    fingerprint: "0011223344556677",
  });
  assert.match(protocol.randomCode6(), /^\d{6}$/);
  assert.match(protocol.randomPairToken(), /^[0-9a-f]{32}$/);
  assert.equal(
    protocol.normalizeWebSocketUrl("https://relay.example.test"),
    "wss://relay.example.test/v1/ws",
  );
});
