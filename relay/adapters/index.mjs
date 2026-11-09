// Homing relay · 自定义 agent 接入层的对外入口
// 只导出两个东西（接线方需要的全部）：
//   registerAdapterRoutes(app)  —— 注册 /api/adapters* 与 /api/ingest*
//   loadCustomScanners()        —— 返回清单 + 脚本适配器转成的 Scanner，与内置同接口
//
// server.mjs 接线时大致是：
//   import { registerAdapterRoutes, loadCustomScanners } from "./adapters/index.mjs";
//   const custom = await loadCustomScanners({ dataDir, emit: (e) => addEvent(e), log });
//   for (const sc of custom.scanners) scanners.set(sc.id, sc);
//   registerAdapterRoutes(app, custom.runtime);
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createAdapterRuntime, registerAdapterRoutes, dispatchAdapterRoute } from "./routes.mjs";
import { loadManifests } from "./manifest.mjs";
import { loadScriptAdapters } from "./scripted.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_DATA_DIR = path.join(HERE, "..", "data");

let runtime = null;
let lastLoad = { at: 0, scanners: [], errors: [] };

export function adapterRuntime({ dataDir = DEFAULT_DATA_DIR, emit, log } = {}) {
  if (!runtime) {
    runtime = createAdapterRuntime({
      dataDir,
      emit: emit || (() => ({ id: null })),
      log: log || (() => {}),
    });
  }
  return runtime;
}

// 加载清单 + 脚本适配器，转成 Scanner 列表。
// 结果缓存 15s；加 { force: true } 可强制重读。
export async function loadCustomScanners({ dataDir, emit, log, force = false, builtin = [] } = {}) {
  const rt = adapterRuntime({ dataDir, emit, log });
  if (Array.isArray(builtin) && builtin.length) {
    rt.setBuiltin(builtin.map((b) => ({
      id: b.id, name: b.name, icon: b.icon, pushSignal: b.pushSignal,
    })));
  }
  if (!force && Date.now() - lastLoad.at < 15_000) return { ...lastLoad, runtime: rt };

  const manifests = loadManifests();
  const scripts = await loadScriptAdapters();
  const scanners = [...manifests.scanners, ...scripts.scanners];
  const errors = [
    ...manifests.errors.map((e) => ({ ...e, kind: "manifest" })),
    ...scripts.errors.map((e) => ({ ...e, kind: "script" })),
  ];
  lastLoad = { at: Date.now(), scanners, errors };
  return { scanners, errors, runtime: rt, dirs: { manifests: manifests.dir, scripts: scripts.dir } };
}

export { registerAdapterRoutes, dispatchAdapterRoute };
