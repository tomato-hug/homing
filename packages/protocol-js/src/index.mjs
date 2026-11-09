import { readFileSync } from "node:fs";
import { randomInt } from "node:crypto";
import sodium from "libsodium-wrappers-sumo";
import { base32Lower, bytesToHex, hexToBytes } from "./base32.mjs";
import { ulid } from "./crockford.mjs";

export { base32Lower, bytesToHex, hexToBytes, ulid };

const WORD_LIST = readFileSync(
  new URL("./bip39-chinese-simplified.txt", import.meta.url),
  "utf8",
)
  .trim()
  .split(/\r?\n/);

if (WORD_LIST.length !== 2048) {
  throw new Error(`BIP39 word list must contain 2048 words, got ${WORD_LIST.length}`);
}

export const BIP39_CHINESE_SIMPLIFIED = Object.freeze(WORD_LIST);

export async function ready() {
  await sodium.ready;
}

export function randomBytes(length) {
  return sodium.randombytes_buf(length);
}

export function toBase64(bytes) {
  return sodium.to_base64(bytes, sodium.base64_variants.ORIGINAL);
}

export function fromBase64(value) {
  return sodium.from_base64(value, sodium.base64_variants.ORIGINAL);
}

export function utf8(value) {
  return new TextEncoder().encode(value);
}

export function keypairFromSeed(seed) {
  const seedBytes = typeof seed === "string" ? utf8(seed) : seed;
  if (seedBytes.length !== sodium.crypto_sign_SEEDBYTES) {
    throw new Error("Ed25519 seed must be 32 bytes");
  }
  const pair = sodium.crypto_sign_seed_keypair(seedBytes);
  return {
    publicKey: pair.publicKey,
    secretKey: pair.privateKey ?? pair.secretKey,
  };
}

export function curveKeys(publicKey, secretKey) {
  return {
    publicKey: sodium.crypto_sign_ed25519_pk_to_curve25519(publicKey),
    secretKey: sodium.crypto_sign_ed25519_sk_to_curve25519(secretKey),
  };
}

export function deviceIdFromPublicKey(publicKey) {
  return base32Lower(sodium.crypto_generichash(16, publicKey));
}

export function fingerprintFromPublicKey(publicKey) {
  return bytesToHex(sodium.crypto_generichash(8, publicKey));
}

export function verifyHostFingerprint(publicKey, expectedFingerprint) {
  return fingerprintFromPublicKey(publicKey) === String(expectedFingerprint).toLowerCase();
}

export function sasWords(hostPublicKey, clientPublicKey) {
  const digest = sodium.crypto_generichash(
    8,
    new Uint8Array([...hostPublicKey, ...clientPublicKey]),
  );
  let bits = 0n;
  for (const byte of digest.slice(0, 6)) {
    bits = (bits << 8n) | BigInt(byte);
  }
  bits >>= 4n;
  return [0, 1, 2, 3].map((index) => {
    const shift = BigInt((3 - index) * 11);
    return WORD_LIST[Number((bits >> shift) & 0x7ffn)];
  });
}

export function helloSignature(secretKey, nonceBase64, ts) {
  const material = new Uint8Array([...fromBase64(nonceBase64), ...utf8(ts)]);
  return sodium.crypto_sign_detached(material, secretKey);
}

export function verifyHelloSignature(publicKey, nonceBase64, ts, signature) {
  const material = new Uint8Array([...fromBase64(nonceBase64), ...utf8(ts)]);
  return sodium.crypto_sign_verify_detached(signature, material, publicKey);
}

export function createEnvelope({
  from,
  to,
  payload,
  senderSecretKey,
  senderPublicKey,
  recipientPublicKey,
  id = ulid(randomBytes(10)),
  ts = new Date().toISOString(),
  nonce = null,
}) {
  const nonceBytes = nonce ?? randomBytes(24);
  if (nonceBytes.length !== 24) {
    throw new Error("crypto_box nonce must be 24 bytes");
  }
  const senderCurve = curveKeys(senderPublicKey, senderSecretKey);
  const recipientCurvePublic = sodium.crypto_sign_ed25519_pk_to_curve25519(recipientPublicKey);
  const ciphertext = sodium.crypto_box_easy(
    utf8(JSON.stringify(payload)),
    nonceBytes,
    recipientCurvePublic,
    senderCurve.secretKey,
  );
  return {
    v: 1,
    t: "msg",
    id,
    from,
    to,
    ts,
    nonce: toBase64(nonceBytes),
    ct: toBase64(ciphertext),
  };
}

export function openEnvelope(envelope, { recipientSecretKey, senderPublicKey }) {
  if (envelope?.v !== 1 || envelope?.t !== "msg") {
    throw new Error("unsupported envelope");
  }
  const recipientCurve = sodium.crypto_sign_ed25519_sk_to_curve25519(recipientSecretKey);
  const senderCurvePublic = sodium.crypto_sign_ed25519_pk_to_curve25519(senderPublicKey);
  const clear = sodium.crypto_box_open_easy(
    fromBase64(envelope.ct),
    fromBase64(envelope.nonce),
    senderCurvePublic,
    recipientCurve,
  );
  return JSON.parse(new TextDecoder().decode(clear));
}

export function randomPairToken() {
  return sodium.to_hex(randomBytes(16));
}

export function randomCode6() {
  return String(randomInt(0, 1_000_000)).padStart(6, "0");
}

export function pairUri({ server, token, hostId, fingerprint }) {
  const params = new URLSearchParams({
    s: server,
    t: token,
    h: hostId,
    fp: fingerprint,
  });
  return `homing://pair?${params.toString()}`;
}

export function parsePairUri(value) {
  const url = new URL(value);
  if (url.protocol !== "homing:" || url.hostname !== "pair") {
    throw new Error("不是 Homing 配对二维码");
  }
  const server = url.searchParams.get("s");
  const token = url.searchParams.get("t");
  const hostId = url.searchParams.get("h");
  const fingerprint = url.searchParams.get("fp");
  if (!server || !token || !hostId || !fingerprint) {
    throw new Error("配对二维码字段不完整");
  }
  return { server, token, hostId, fingerprint };
}

export function normalizeWebSocketUrl(server) {
  const url = new URL(server);
  if (url.protocol === "https:") url.protocol = "wss:";
  else if (url.protocol === "http:") url.protocol = "ws:";
  if (url.protocol !== "ws:" && url.protocol !== "wss:") {
    throw new Error("服务器地址必须使用 ws、wss、http 或 https");
  }
  if (!url.pathname || url.pathname === "/") url.pathname = "/v1/ws";
  return url.toString();
}

export { sodium };
