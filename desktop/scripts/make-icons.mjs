// 从 pigeon.json 的路径生成桌面图标：托盘模板图、App 图标（.icns / .ico）。
// 不依赖任何图像库：自己解析 M/L/C/Z，扫描线填充，zlib 写 PNG。
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

const OUT = path.join(import.meta.dirname, "..", "src-tauri", "icons");
fs.mkdirSync(OUT, { recursive: true });

// pigeon.json 里的四层（坐标系 120×120，整体平移 (4,6)）
const TRANSLATE = [4, 6];
const LAYERS = [
  { d: "M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z", fill: "#E2E8F0" },
  { d: "M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z", fill: "#E2E8F0" },
  { d: "M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z", fill: "#2DD4BF" },
  { d: "M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z", fill: "#94A3B8" },
];
const EYE = { cx: 78, cy: 34, r: 2.4, fill: "#0F172A" };
const SILHOUETTE = "M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z";

// ---------------------------------------------------------------- 路径解析

function parsePath(d) {
  const tokens = d.match(/[MLCZmlcz]|-?\d*\.?\d+/g) || [];
  const contours = [];
  let current = null;
  let cursor = [0, 0];
  let i = 0;
  const num = () => Number(tokens[i++]);
  while (i < tokens.length) {
    const cmd = tokens[i++];
    if (cmd === "M" || cmd === "m") {
      const x = num();
      const y = num();
      cursor = cmd === "m" ? [cursor[0] + x, cursor[1] + y] : [x, y];
      current = [cursor.slice()];
      contours.push(current);
    } else if (cmd === "L" || cmd === "l") {
      const x = num();
      const y = num();
      cursor = cmd === "l" ? [cursor[0] + x, cursor[1] + y] : [x, y];
      current.push(cursor.slice());
    } else if (cmd === "C" || cmd === "c") {
      const pts = [num(), num(), num(), num(), num(), num()];
      const [x1, y1, x2, y2, x3, y3] = cmd === "c"
        ? pts.map((v, k) => v + cursor[k % 2])
        : pts;
      const start = cursor.slice();
      const STEPS = 16;
      for (let s = 1; s <= STEPS; s += 1) {
        const t = s / STEPS;
        const mt = 1 - t;
        const x = mt * mt * mt * start[0] + 3 * mt * mt * t * x1 + 3 * mt * t * t * x2 + t * t * t * x3;
        const y = mt * mt * mt * start[1] + 3 * mt * mt * t * y1 + 3 * mt * t * t * y2 + t * t * t * y3;
        current.push([x, y]);
      }
      cursor = [x3, y3];
    } else if (cmd === "Z" || cmd === "z") {
      // 闭合：起点已经在 contours 里，不需要额外动作
    }
  }
  return contours.map((c) => c.map(([x, y]) => [x + TRANSLATE[0], y + TRANSLATE[1]]));
}

const compiled = LAYERS.map((layer) => ({ fill: layer.fill, contours: parsePath(layer.d) }));
const silhouette = [parsePath(SILHOUETTE)];

// ---------------------------------------------------------------- 光栅化

function hexToRgb(hex) {
  const n = Number.parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** 对一组轮廓做扫描线填充，返回 Float32Array 覆盖率（size×size，0..1） */
function coverage(contours, size, viewBox = 120) {
  const cov = new Float32Array(size * size);
  const scale = size / viewBox;
  const SS = 4; // 每个像素 4 条采样行
  const edges = [];
  for (const contour of contours) {
    for (let i = 0; i < contour.length; i += 1) {
      const a = contour[i];
      const b = contour[(i + 1) % contour.length];
      if (a[1] === b[1]) continue;
      edges.push([a, b]);
    }
  }
  for (let row = 0; row < size * SS; row += 1) {
    const y = (row + 0.5) / SS / scale;
    const xs = [];
    for (const [a, b] of edges) {
      const [x1, y1] = a;
      const [x2, y2] = b;
      if (y >= Math.min(y1, y2) && y < Math.max(y1, y2)) {
        xs.push(x1 + ((y - y1) / (y2 - y1)) * (x2 - x1));
      }
    }
    if (xs.length < 2) continue;
    xs.sort((p, q) => p - q);
    for (let k = 0; k + 1 < xs.length; k += 2) {
      const startX = xs[k] * scale;
      const endX = xs[k + 1] * scale;
      if (endX <= startX) continue;
      const firstPx = Math.floor(startX);
      const lastPx = Math.min(size - 1, Math.floor(endX));
      const py = Math.floor(row / SS);
      for (let px = firstPx; px <= lastPx; px += 1) {
        const left = Math.max(startX, px);
        const right = Math.min(endX, px + 1);
        if (right > left) cov[py * size + px] += (right - left) / SS;
      }
    }
  }
  for (let i = 0; i < cov.length; i += 1) cov[i] = Math.min(1, cov[i]);
  return cov;
}

/** 圆角矩形覆盖率（用来画 App 图标的底板） */
function roundedRectCoverage(size, radiusRatio = 0.225) {
  const cov = new Float32Array(size * size);
  const r = size * radiusRatio;
  const SS = 4;
  for (let py = 0; py < size; py += 1) {
    for (let px = 0; px < size; px += 1) {
      let hit = 0;
      for (let sy = 0; sy < SS; sy += 1) {
        for (let sx = 0; sx < SS; sx += 1) {
          const x = px + (sx + 0.5) / SS;
          const y = py + (sy + 0.5) / SS;
          const cx = Math.min(Math.max(x, r), size - r);
          const cy = Math.min(Math.max(y, r), size - r);
          const dx = x - cx;
          const dy = y - cy;
          if (dx * dx + dy * dy <= r * r) hit += 1;
        }
      }
      cov[py * size + px] = hit / (SS * SS);
    }
  }
  return cov;
}

function ellipseCoverage(size, cx, cy, radius, viewBox = 120) {
  const cov = new Float32Array(size * size);
  const scale = size / viewBox;
  const SS = 4;
  for (let py = 0; py < size; py += 1) {
    for (let px = 0; px < size; px += 1) {
      let hit = 0;
      for (let sy = 0; sy < SS; sy += 1) {
        for (let sx = 0; sx < SS; sx += 1) {
          const x = ((px + (sx + 0.5) / SS) / scale - (cx + TRANSLATE[0])) * scale;
          const y = ((py + (sy + 0.5) / SS) / scale - (cy + TRANSLATE[1])) * scale;
          if (x * x + y * y <= (radius * scale) ** 2) hit += 1;
        }
      }
      cov[py * size + px] = hit / (SS * SS);
    }
  }
  return cov;
}

// ---------------------------------------------------------------- PNG

function crc32(buf) {
  let c;
  const table = crc32.table || (crc32.table = (() => {
    const t = new Uint32Array(256);
    for (let n = 0; n < 256; n += 1) {
      c = n;
      for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      t[n] = c >>> 0;
    }
    return t;
  })());
  let crc = 0xffffffff;
  for (let i = 0; i < buf.length; i += 1) crc = table[(crc ^ buf[i]) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

function encodePng(width, height, rgba) {
  const raw = Buffer.alloc((width * 4 + 1) * height);
  for (let y = 0; y < height; y += 1) {
    raw[y * (width * 4 + 1)] = 0;
    rgba.copy(raw, y * (width * 4 + 1) + 1, y * width * 4, (y + 1) * width * 4);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

function blank(size) {
  return Buffer.alloc(size * size * 4);
}

function paint(buf, size, cov, [r, g, b], alpha = 1) {
  for (let i = 0; i < cov.length; i += 1) {
    const a = cov[i] * alpha;
    if (a <= 0) continue;
    const o = i * 4;
    const dstA = buf[o + 3] / 255;
    const outA = a + dstA * (1 - a);
    if (outA <= 0) continue;
    buf[o] = Math.round((r * a + buf[o] * dstA * (1 - a)) / outA);
    buf[o + 1] = Math.round((g * a + buf[o + 1] * dstA * (1 - a)) / outA);
    buf[o + 2] = Math.round((b * a + buf[o + 2] * dstA * (1 - a)) / outA);
    buf[o + 3] = Math.round(outA * 255);
  }
}

// ---------------------------------------------------------------- 生成

function silhouetteMask(size) {
  return coverage(silhouette[0], size);
}

/** 托盘 / 菜单栏：纯黑 + alpha，Mac 上会自动按模板图反色 */
function templateIcon(size) {
  const buf = blank(size);
  paint(buf, size, silhouetteMask(size), [0, 0, 0], 1);
  return encodePng(size, size, buf);
}

/** App 图标：深底圆角 + 灰鸽四层 */
function appIcon(size) {
  const buf = blank(size);
  paint(buf, size, roundedRectCoverage(size), hexToRgb("#0F172A"), 1);
  for (const layer of compiled) {
    paint(buf, size, coverage(layer.contours, size), hexToRgb(layer.fill), 1);
  }
  paint(buf, size, ellipseCoverage(size, EYE.cx, EYE.cy, EYE.r), hexToRgb(EYE.fill), 1);
  return encodePng(size, size, buf);
}

const written = [];
function write(name, data) {
  const file = path.join(OUT, name);
  fs.writeFileSync(file, data);
  written.push(name);
}

// 托盘（模板图标）
write("tray-template.png", templateIcon(32));
write("tray-template@2x.png", templateIcon(64));

// App 图标
for (const size of [32, 64, 128, 256, 512, 1024]) {
  write(`${size}x${size}.png`, appIcon(size));
}
write("icon.png", appIcon(512));
write("128x128.png", appIcon(128));
write("128x128@2x.png", appIcon(256));

// .icns（用系统的 iconutil）
const iconset = path.join(OUT, "icon.iconset");
fs.rmSync(iconset, { recursive: true, force: true });
fs.mkdirSync(iconset, { recursive: true });
for (const [base, scale] of [
  ["16", 1],
  ["16", 2],
  ["32", 1],
  ["32", 2],
  ["128", 1],
  ["128", 2],
  ["256", 1],
  ["256", 2],
  ["512", 1],
  ["512", 2],
]) {
  const px = Number(base) * scale;
  const name = scale === 1 ? `icon_${base}x${base}.png` : `icon_${base}x${base}@2x.png`;
  fs.writeFileSync(path.join(iconset, name), appIcon(px));
}

// .ico（Windows，PNG 压缩条目）
function buildIco(sizes) {
  const images = sizes.map((s) => ({ size: s, data: appIcon(s) }));
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(images.length, 4);
  let offset = 6 + images.length * 16;
  const entries = [];
  for (const image of images) {
    const e = Buffer.alloc(16);
    e[0] = image.size >= 256 ? 0 : image.size;
    e[1] = image.size >= 256 ? 0 : image.size;
    e[2] = 0;
    e[3] = 0;
    e.writeUInt16LE(1, 4);
    e.writeUInt16LE(32, 6);
    e.writeUInt32LE(image.data.length, 8);
    e.writeUInt32LE(offset, 12);
    offset += image.data.length;
    entries.push(e);
  }
  return Buffer.concat([header, ...entries, ...images.map((i) => i.data)]);
}
write("icon.ico", buildIco([16, 32, 48, 64, 128, 256]));

console.log(`图标已生成（${written.length} 个）：${written.join(", ")}`);
console.log(`iconset 目录：${iconset}`);
