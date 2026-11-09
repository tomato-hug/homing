// Homing · 自定义接入清单（~/.homing/agents/<id>.yaml）
//
// 这一层只做「清单文件的读写与最小校验」，真正的会话读取、事件归一化、
// /api/ingest、脚本适配器加载由 relay 侧（DSH）接上；控制台的三个接口
// （GET/POST /api/adapters、POST /api/adapters/test）先按约定形状占位。
//
// 清单故意做成人能看懂的窄格式：两级缩进 + 标量，不引入 YAML 依赖。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export const ADAPTER_ROOT = path.join(os.homedir(), ".homing", "agents");
export const FORMATS = ["jsonl", "json", "sqlite", "markdown", "log"];
export const PUSH_METHODS = ["hook", "watch", "poll"];

function ensureRoot() {
  try {
    fs.mkdirSync(ADAPTER_ROOT, { recursive: true, mode: 0o700 });
  } catch {
    /* 建不出来时由调用方报错 */
  }
}

export function expandHome(input) {
  const text = String(input || "").trim();
  if (text === "~") return os.homedir();
  if (text.startsWith("~/") || text.startsWith("~\\")) return path.join(os.homedir(), text.slice(2));
  return text;
}

/** ~ 打头的路径在清单里更好读，写盘时换回去 */
export function tildify(input) {
  const home = os.homedir();
  const text = String(input || "");
  return text.startsWith(home) ? `~${text.slice(home.length)}` : text;
}

export function slugId(input) {
  const raw = String(input || "").trim().toLowerCase();
  const ascii = raw.replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  if (ascii) return ascii;
  // 中文名拿不出 ASCII 时用名字的哈希，别用时间戳：同一个名字要多份保存落在同一个文件里
  let hash = 0;
  for (const ch of raw) hash = (hash * 31 + ch.codePointAt(0)) % 0xffffffff;
  return `adapter-${hash.toString(36)}`;
}

// ---------------------------------------------------------------- 极简 YAML

function parseScalar(raw) {
  const text = raw.trim();
  if (text === "" || text === "null" || text === "~") return null;
  if (text === "true") return true;
  if (text === "false") return false;
  if (/^-?\d+$/.test(text)) return Number(text);
  if (
    (text.startsWith('"') && text.endsWith('"')) ||
    (text.startsWith("'") && text.endsWith("'"))
  ) {
    return text.slice(1, -1).replace(/\\"/g, '"');
  }
  return text;
}

export function parseYaml(text) {
  const root = {};
  let current = root;
  for (const rawLine of String(text || "").split(/\r?\n/)) {
    if (!rawLine.trim() || rawLine.trim().startsWith("#")) continue;
    const indent = rawLine.match(/^\s*/)[0].length;
    const line = rawLine.trim();
    const sep = line.indexOf(":");
    if (sep < 0) continue;
    const key = line.slice(0, sep).trim();
    const rest = line.slice(sep + 1);
    if (indent === 0) {
      if (rest.trim() === "") {
        root[key] = root[key] && typeof root[key] === "object" ? root[key] : {};
        current = root[key];
      } else {
        root[key] = parseScalar(rest);
        current = root;
      }
    } else {
      current[key] = parseScalar(rest);
    }
  }
  return root;
}

function quote(value) {
  if (value === null || value === undefined) return "null";
  const text = String(value);
  if (text === "" || /^[\s#&*?|>%@`]/.test(text) || /: |\s#/.test(text)) {
    return `"${text.replace(/"/g, '\\"')}"`;
  }
  return text;
}

export function yamlFor(manifest) {
  const m = manifest;
  return [
    `# Homing 接入清单 · 由控制台生成，可以手改`,
    `# 文件位置：${tildify(path.join(ADAPTER_ROOT, `${m.id}.yaml`))}`,
    `id: ${quote(m.id)}`,
    `name: ${quote(m.name)}`,
    `source:`,
    `  kind: ${quote(m.source?.kind || "file")}`,
    `  path: ${quote(tildify(m.source?.path || ""))}`,
    `  format: ${quote(m.source?.format || "jsonl")}`,
    `push:`,
    `  method: ${quote(m.push?.method || "poll")}`,
    `reply:`,
    `  mode: ${quote(m.reply?.mode || "command")}`,
    `  command: ${quote(m.reply?.command || "")}`,
    `created_at: ${quote(m.created_at || new Date().toISOString())}`,
    ``,
  ].join("\n");
}

// ---------------------------------------------------------------- 清单

export function normalizeManifest(input = {}) {
  const name = String(input.name || "").trim();
  const id = slugId(input.id || input.name || "");
  const sourcePath = String(input.source?.path || "").trim();
  const format = FORMATS.includes(input.source?.format) ? input.source.format : "jsonl";
  const method = PUSH_METHODS.includes(input.push?.method) ? input.push.method : "poll";
  return {
    id,
    name: name || id,
    source: {
      kind: input.source?.kind === "process" ? "process" : "file",
      path: sourcePath,
      format,
    },
    push: { method },
    reply: {
      mode: input.reply?.mode === "none" ? "none" : "command",
      command: String(input.reply?.command || "").trim(),
    },
    created_at: input.created_at || new Date().toISOString(),
  };
}

export function validateManifest(manifest) {
  const errors = [];
  if (!manifest.name) errors.push("名称不能为空");
  if (!manifest.source.path) errors.push("文件位置不能为空");
  if (manifest.source.kind === "file" && manifest.source.path) {
    const full = expandHome(manifest.source.path);
    if (!fs.existsSync(full)) errors.push(`文件位置不存在：${full}`);
  }
  if (manifest.reply.mode === "command" && !manifest.reply.command) {
    errors.push("回复命令不能为空（也可以把回复方式改成「不回复」）");
  }
  return errors;
}

export function fileFor(id) {
  return path.join(ADAPTER_ROOT, `${slugId(id)}.yaml`);
}

export function saveManifest(input) {
  ensureRoot();
  const manifest = normalizeManifest(input);
  const errors = validateManifest(manifest);
  if (errors.length) return { ok: false, errors };
  fs.writeFileSync(fileFor(manifest.id), yamlFor(manifest), "utf8");
  return { ok: true, adapter: manifest, file: fileFor(manifest.id), yaml: yamlFor(manifest) };
}

export function removeManifest(id) {
  const file = fileFor(id);
  try {
    fs.unlinkSync(file);
    return { ok: true, removed: file };
  } catch {
    return { ok: false, error: "没有这个清单文件" };
  }
}

export function listManifests() {
  ensureRoot();
  let names = [];
  try {
    names = fs.readdirSync(ADAPTER_ROOT).filter((name) => name.endsWith(".yaml") || name.endsWith(".yml"));
  } catch {
    return [];
  }
  const out = [];
  for (const name of names) {
    const file = path.join(ADAPTER_ROOT, name);
    try {
      const manifest = normalizeManifest(parseYaml(fs.readFileSync(file, "utf8")));
      out.push({ ...manifest, file, size: fs.statSync(file).size, yaml: fs.readFileSync(file, "utf8") });
    } catch (err) {
      out.push({
        id: name.replace(/\.ya?ml$/, ""),
        name: name,
        file,
        broken: true,
        error: err.message,
      });
    }
  }
  return out.sort((a, b) => String(a.id).localeCompare(String(b.id)));
}

// ---------------------------------------------------------------- 测试读取（占位）

/**
 * 「测试读取」只验证「路径在不在、长什么样」，不解析记录。
 * 真正的记录解析由 relay 的 ingest 做，这里先给出可判断的样本。
 */
export function testRead({ path: inputPath, format = "jsonl", limit = 3 } = {}) {
  const target = expandHome(inputPath);
  if (!target) return { ok: false, error: "先填文件位置" };
  let stat;
  try {
    stat = fs.statSync(target);
  } catch {
    return { ok: false, error: `路径不存在：${tildify(target)}`, checked: target };
  }

  const files = [];
  const collect = (file) => {
    try {
      const st = fs.statSync(file);
      if (st.isFile()) files.push({ file, size: st.size, mtime: st.mtime.toISOString() });
    } catch {
      /* 跳过 */
    }
  };
  if (stat.isDirectory()) {
    let entries = [];
    try {
      entries = fs.readdirSync(target);
    } catch (err) {
      return { ok: false, error: `目录读不了：${err.message}`, checked: target };
    }
    for (const entry of entries.slice(0, 200)) collect(path.join(target, entry));
  } else {
    collect(target);
  }

  files.sort((a, b) => String(b.mtime).localeCompare(String(a.mtime)));
  const samples = [];
  for (const item of files.slice(0, limit)) {
    try {
      const fd = fs.openSync(item.file, "r");
      const buffer = Buffer.alloc(Math.min(2048, item.size));
      fs.readSync(fd, buffer, 0, buffer.length, 0);
      fs.closeSync(fd);
      const firstLine = buffer.toString("utf8").split(/\r?\n/).find((line) => line.trim()) || "";
      samples.push({ file: tildify(item.file), size: item.size, preview: firstLine.slice(0, 220) });
    } catch {
      /* 读不了就跳过这条样本 */
    }
  }

  return {
    ok: files.length > 0,
    format,
    checked: tildify(target),
    is_dir: stat.isDirectory(),
    file_count: files.length,
    total_size: files.reduce((sum, item) => sum + item.size, 0),
    samples,
    error: files.length ? null : "路径在，但里面没有可读文件",
    note: "占位实现：只验证路径与文件形状，记录解析等 relay 的 ingest 接上后才会出现。",
  };
}
