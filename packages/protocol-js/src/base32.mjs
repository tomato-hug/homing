const ALPHABET = "abcdefghijklmnopqrstuvwxyz234567";

export function base32Lower(bytes) {
  let value = 0n;
  let bits = 0n;
  let output = "";
  for (const byte of bytes) {
    value = (value << 8n) | BigInt(byte);
    bits += 8n;
    while (bits >= 5n) {
      const shift = bits - 5n;
      output += ALPHABET[Number((value >> shift) & 31n)];
      bits -= 5n;
      value &= (1n << bits) - 1n;
    }
  }
  if (bits > 0n) {
    output += ALPHABET[Number((value << (5n - bits)) & 31n)];
  }
  return output;
}

export function bytesToHex(bytes) {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

export function hexToBytes(hex) {
  if (typeof hex !== "string" || hex.length % 2 !== 0 || !/^[0-9a-f]*$/i.test(hex)) {
    throw new Error("invalid hex");
  }
  return Uint8Array.from(hex.match(/.{2}/g) ?? [], (value) => Number.parseInt(value, 16));
}
