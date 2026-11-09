const ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

function encode(value, length) {
  let remaining = value;
  let output = "";
  for (let index = 0; index < length; index += 1) {
    output = ALPHABET[Number(remaining & 31n)] + output;
    remaining >>= 5n;
  }
  return output;
}

function bytesToBigInt(bytes) {
  let value = 0n;
  for (const byte of bytes) {
    value = (value << 8n) | BigInt(byte);
  }
  return value;
}

export function ulid(randomBytes, now = Date.now()) {
  const timestamp = BigInt(now) & ((1n << 48n) - 1n);
  return encode(timestamp, 10) + encode(bytesToBigInt(randomBytes), 16);
}
