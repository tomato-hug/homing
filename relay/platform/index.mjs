// Homing · 跨平台平台层
// 只回答「这台机器上东西该放哪、怎么开机自启、agent 的家目录在哪」，
// 不掺业务逻辑。Mac / Windows / Linux 三套差异都收敛在这里。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const VERSION = "0.7.0";
export const PRODUCT = "Homing";
export const PORT = Number(process.env.PORT || 18787);
export const isWindows = process.platform === "win32";
export const isMac = process.platform === "darwin";
export const isLinux = !isWindows && !isMac;

// 打包成单文件时，build 脚本会把 process.env.HOMING_PACKAGED 直接替换成 "1"，
// 于是这里拿到的是常量，不依赖运行时环境变量。
const PACKAGED = process.env.HOMING_PACKAGED === "1";

/**
 * relay 目录（开发时是仓库里的 relay/，打包成单文件后是 exe 所在目录）。
 * 本文件在 relay/platform/ 下，所以开发时往上一层。
 * 顺序：环境变量 → 打包模式（可执行文件目录） → ESM 的 import.meta.url → 可执行文件目录。
 */
export const relayDir = (() => {
  if (process.env.HOMING_RELAY_DIR) return path.resolve(process.env.HOMING_RELAY_DIR);
  if (PACKAGED) return path.dirname(process.execPath);
  try {
    const here = import.meta.url;
    if (here) return path.dirname(path.dirname(fileURLToPath(here)));
  } catch {
    /* 打包成 CJS 后没有 import.meta */
  }
  return path.dirname(process.execPath);
})();

/** 单文件可执行？ */
export const isPackaged = PACKAGED;

/** 数据目录：Mac ~/Library/Application Support/Homing，Windows %APPDATA%\Homing */
export function defaultDataDir() {
  if (process.env.HOMING_DATA_DIR) return path.resolve(process.env.HOMING_DATA_DIR);
  if (isMac) return path.join(os.homedir(), "Library", "Application Support", "Homing");
  if (isWindows) {
    const appData = process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming");
    return path.join(appData, "Homing");
  }
  const xdg = process.env.XDG_DATA_HOME || path.join(os.homedir(), ".local", "share");
  return path.join(xdg, "homing");
}

export const dataDir = defaultDataDir();
export const dirs = {
  data: dataDir,
  logs: path.join(dataDir, "logs"),
  console: path.join(dataDir, "console"),
};

for (const dir of Object.values(dirs)) {
  try {
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  } catch {
    /* 目录建不出来时由各调用方自己报错 */
  }
}

export const paths = {
  config: path.join(dataDir, "config.json"),
  devices: path.join(dataDir, "devices.json"),
  pause: path.join(dataDir, "push-pause.json"),
  sessionState: path.join(dataDir, "session-state.json"),
  events: path.join(dataDir, "events.jsonl"),
  replies: path.join(dataDir, "replies.jsonl"),
  capabilities: path.join(dataDir, "capabilities.json"),
  clientCrash: path.join(dataDir, "client-crash.jsonl"),
  log: process.env.HOMING_LOG_PATH || path.join(dataDir, "homingd.log"),
  app: path.join(dataDir, "app.json"),
};

/**
 * 从旧的 relay/data（仓库内）搬过来。
 * 只在目标不存在时复制，绝不覆盖、绝不删除原文件，
 * 这样已经在用的 token / 配对关系不会因为升级而失效。
 */
export function migrateLegacyData({ legacyRelayDir = null, log = () => {} } = {}) {
  const legacy = legacyRelayDir || relayDir;
  const pairs = [
    [path.join(legacy, "config.json"), paths.config],
    [path.join(legacy, "data", "events.jsonl"), paths.events],
    [path.join(legacy, "data", "replies.jsonl"), paths.replies],
    [path.join(legacy, "data", "session-state.json"), paths.sessionState],
    [path.join(legacy, "data", "capabilities.json"), paths.capabilities],
    [path.join(legacy, "data", "client-crash.jsonl"), paths.clientCrash],
  ];
  const copied = [];
  for (const [from, to] of pairs) {
    try {
      if (!fs.existsSync(from) || fs.existsSync(to)) continue;
      fs.mkdirSync(path.dirname(to), { recursive: true });
      fs.copyFileSync(from, to);
      copied.push(path.basename(to));
    } catch (err) {
      log(`迁移 ${path.basename(to)} 失败：${err.message}`);
    }
  }
  if (copied.length) log(`已从 ${legacy} 迁移：${copied.join(", ")}`);
  return copied;
}

/**
 * 本机局域网 IPv4。
 * 198.18.0.0/15 是代理软件 TUN 网卡常用的保留段（手机上根本连不到），排到最后并在选地址时跳过；
 * 169.254 是没拿到 DHCP 的自分配地址，同样排最后。
 */
export function lanAddresses({ includeTunnel = false } = {}) {
  const found = [];
  for (const list of Object.values(os.networkInterfaces())) {
    for (const addr of list || []) {
      if (addr.family !== "IPv4" || addr.internal) continue;
      const tunnel = /^198\.1[89]\./.test(addr.address);
      const linkLocal = addr.address.startsWith("169.254.");
      if (tunnel && !includeTunnel) continue;
      found.push({
        address: addr.address,
        netmask: addr.netmask,
        tunnel,
        linkLocal,
        // 家庭 / 办公室最常见的 192.168、10、172.16–31 排前面
        rank: linkLocal ? 3 : tunnel ? 2 : /^(192\.168|10\.|172\.(1[6-9]|2\d|3[01])\.)/.test(addr.address) ? 0 : 1,
      });
    }
  }
  found.sort((a, b) => a.rank - b.rank);
  return found.map((item) => item.address);
}

export function lanUrls(port = PORT) {
  return lanAddresses().map((ip) => `http://${ip}:${port}`);
}

export function localUrl(port = PORT) {
  return `http://127.0.0.1:${port}`;
}

export function hostName() {
  try {
    return os.hostname().replace(/\.local$/, "");
  } catch {
    return "本机";
  }
}

/** 机器可读的平台名，给手机的「我的 › 添加电脑」显示 */
export function platformLabel() {
  if (isMac) return "Mac";
  if (isWindows) return "Windows";
  return "Linux";
}

export function readJson(file, fallback = null) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return fallback;
  }
}

/** 原子写：先写 tmp 再 rename，坏文件不会留下半截 */
export function writeJson(file, value, { mode = 0o600 } = {}) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp-${process.pid}-${Date.now()}`;
  fs.writeFileSync(tmp, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode });
  fs.renameSync(tmp, file);
}

export function writeText(file, text, { mode = 0o600 } = {}) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp-${process.pid}-${Date.now()}`;
  fs.writeFileSync(tmp, text, { encoding: "utf8", mode });
  fs.renameSync(tmp, file);
}

/** server.mjs 启动时用来把「运行期信息」写给桌面壳读（sidecar 模式下窗口标题/托盘会用到） */
export function writeRuntimeInfo(info) {
  try {
    writeJson(paths.app, { ...info, version: VERSION, updated_at: new Date().toISOString() });
  } catch {
    /* 写不进去不影响服务 */
  }
}

export { os };
export default {
  VERSION,
  PRODUCT,
  PORT,
  isWindows,
  isMac,
  isLinux,
  relayDir,
  isPackaged,
  dataDir,
  dirs,
  paths,
  defaultDataDir,
  migrateLegacyData,
  lanAddresses,
  lanUrls,
  localUrl,
  hostName,
  platformLabel,
  readJson,
  writeJson,
  writeText,
  writeRuntimeInfo,
};
