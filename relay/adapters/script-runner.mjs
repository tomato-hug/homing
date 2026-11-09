// Homing relay · 脚本适配器的子进程执行体
// 由 scripted.mjs spawn：node --permission --allow-fs-read=<adapter> script-runner.mjs <file> <payloadJson>
// 只负责「import 脚本 → 调一个方法 → 把结果 JSON 打到 stdout」。
// 不写任何文件；出错时把原因写到 stderr 并以非 0 退出。
import path from "node:path";
import { pathToFileURL } from "node:url";

const [file, payloadRaw] = process.argv.slice(2);
if (!file || !payloadRaw) {
  process.stderr.write("usage: script-runner.mjs <adapter.mjs> <json>\n");
  process.exit(2);
}

let payload;
try {
  payload = JSON.parse(payloadRaw);
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
    case "meta": {
      result = {
        meta: {
          id: api.id,
          name: api.name,
          icon: api.icon,
          pushSignal: api.pushSignal,
          capabilities: api.capabilities,
          reads: mod.meta?.reads || api.reads || [],
        },
      };
      break;
    }
    case "detect":
      result = typeof api.detect === "function" ? await api.detect() : { installed: false };
      break;
    case "listSessions":
      if (typeof api.listSessions !== "function") throw new Error("适配器未导出 listSessions");
      result = { sessions: await api.listSessions({ limit: args.limit || 200 }) };
      break;
    case "readItems":
      if (typeof api.readItems !== "function") throw new Error("适配器未导出 readItems");
      result = { items: await api.readItems(args.sessionId, { limit: args.limit || 100 }) };
      break;
    case "reply":
      if (typeof api.reply !== "function") throw new Error("适配器未导出 reply");
      result = await api.reply(args.sessionId, args.text, args.opts || {});
      break;
    case "models":
      result = typeof api.models === "function" ? await api.models() : { models: [], defaultModel: null };
      break;
    default:
      throw new Error(`未知 op: ${op}`);
  }

  process.stdout.write(JSON.stringify(result ?? {}));
} catch (err) {
  process.stderr.write(`适配器执行失败: ${err?.message || err}\n`);
  process.exit(1);
}
