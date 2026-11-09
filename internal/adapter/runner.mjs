// Homing · 脚本适配器的子进程执行体（Go 版内嵌，与 Node 版语义相同）
// 协议：stdin 一行 JSON {"op":"...","args":{...}} → stdout 一行 JSON 结果。
// 由 internal/adapter 用 node --permission --allow-fs-read=<适配器> runner.mjs 启动。
// 只 import 适配器、调一个方法、把结果写出来；不碰文件、不连网络。
import path from "node:path";
import { pathToFileURL } from "node:url";

function readStdin() {
  return new Promise((resolve, reject) => {
    let buf = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (d) => { buf += d; });
    process.stdin.on("end", () => resolve(buf));
    process.stdin.on("error", reject);
  });
}

const file = process.argv[2];
if (!file) {
  process.stderr.write("usage: runner.mjs <adapter.mjs> (payload on stdin)\n");
  process.exit(2);
}

let payload;
try {
  const raw = await readStdin();
  const line = raw.split("\n").find((l) => l.trim());
  payload = JSON.parse(line || "{}");
} catch (err) {
  process.stderr.write(`payload 不是 JSON: ${err.message}\n`);
  process.exit(2);
}

const { op, args = {} } = payload;

try {
  const mod = await import(pathToFileURL(path.resolve(file)).href);
  const api = mod.default || mod;
  let result;

  switch (op) {
    case "meta":
      result = {
        id: api.id,
        name: api.name,
        icon: api.icon,
        pushSignal: api.pushSignal,
        capabilities: api.capabilities,
        reads: (mod.meta && mod.meta.reads) || api.reads || [],
      };
      break;
    case "detect":
      result = typeof api.detect === "function" ? await api.detect() : { installed: false };
      break;
    case "listSessions":
      if (typeof api.listSessions !== "function") throw new Error("适配器未导出 listSessions");
      result = { sessions: await api.listSessions({ limit: args.limit || 200 }) };
      break;
    case "readItems":
      if (typeof api.readItems !== "function") throw new Error("适配器未导出 readItems");
      result = { items: await api.readItems(args.sessionID, { limit: args.limit || 100 }) };
      break;
    case "reply":
      if (typeof api.reply !== "function") throw new Error("适配器未导出 reply");
      result = await api.reply(args.sessionID, args.text, args.opts || {});
      break;
    case "models":
      result = typeof api.models === "function" ? await api.models() : { models: [], defaultModel: null };
      break;
    default:
      throw new Error(`未知 op: ${op}`);
  }

  process.stdout.write(JSON.stringify(result ?? {}) + "\n");
} catch (err) {
  process.stderr.write(`适配器执行失败: ${err?.message || err}\n`);
  process.exit(1);
}
