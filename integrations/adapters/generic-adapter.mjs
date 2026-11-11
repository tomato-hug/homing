// 最小脚本适配器。保存到 ~/.homing/adapters/my-agent.mjs。
// 运行器会用 node（没有就 bun）在隔离子进程里调用这里导出的函数。

export const meta = { reads: ["~/my-agent/sessions"] };
export const id = "my-agent";
export const name = "My Agent";
export const icon = "generic";
export const pushSignal = "poll";
export const capabilities = { sessions: true, reply: false };

export async function detect() {
  return { installed: true, version: null, detail: "示例适配器" };
}

export async function listSessions({ limit }) {
  // 返回 [{ session_id, title, last_ts, cwd, model }]
  return [];
}

export async function readItems(sessionId, { limit }) {
  // 返回 [{ role, text, at }]
  return [];
}
