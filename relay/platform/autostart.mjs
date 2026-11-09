// Homing · 开机自启（给纯命令行版 homingd 用；桌面 App 自己用 tauri-plugin-autostart）
// Mac：写一个用户级 LaunchAgent。Windows：当前用户登录启动项（HKCU\...\Run），不做系统服务。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { VERSION, isMac, isWindows, paths, readJson, writeJson } from "./index.mjs";

const MAC_LABEL = "dev.homing.homingd";
const WIN_RUN_KEY = "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run";
const WIN_VALUE = "Homing";

function macPlistPath() {
  return path.join(os.homedir(), "Library", "LaunchAgents", `${MAC_LABEL}.plist`);
}

/** 老的、由前身版本装的 launchd 服务；桌面上如果已经有它，就不再重复拉一个 homingd */
export function legacyRelayAgent() {
  const plist = path.join(os.homedir(), "Library", "LaunchAgents", "dev.agentpush.relay.plist");
  return fs.existsSync(plist) ? plist : null;
}

function plistBody({ program, args, workdir, port }) {
  const argXml = [program, ...args].map((a) => `\t\t<string>${a}</string>`).join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
\t<key>Label</key>
\t<string>${MAC_LABEL}</string>
\t<key>ProgramArguments</key>
\t<array>
${argXml}
\t</array>
\t<key>WorkingDirectory</key>
\t<string>${workdir}</string>
\t<key>EnvironmentVariables</key>
\t<dict>
\t\t<key>PORT</key>
\t\t<string>${port}</string>
\t\t<key>PATH</key>
\t\t<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
\t</dict>
\t<key>RunAtLoad</key>
\t<true/>
\t<key>KeepAlive</key>
\t<true/>
\t<key>ProcessType</key>
\t<string>Background</string>
\t<key>StandardOutPath</key>
\t<string>${paths.log}</string>
\t<key>StandardErrorPath</key>
\t<string>${paths.log}</string>
</dict>
</plist>
`;
}

function readAutostartState() {
  const file = path.join(paths.data, "autostart.json");
  return readJson(file, { enabled: false, backend: null, updated_at: null }) || { enabled: false };
}

function saveAutostartState(state) {
  writeJson(path.join(paths.data, "autostart.json"), { ...state, updated_at: new Date().toISOString() });
}

export function getAutostart() {
  if (isMac) {
    const plist = macPlistPath();
    if (fs.existsSync(plist)) return { enabled: true, backend: "launchagent", target: plist };
    const legacy = legacyRelayAgent();
    if (legacy) return { enabled: true, backend: "launchagent-legacy", target: legacy };
    return { enabled: false, backend: "launchagent", target: plist };
  }
  if (isWindows) {
    try {
      const out = execFileSync("reg", ["query", WIN_RUN_KEY, "/v", WIN_VALUE], { encoding: "utf8" });
      const match = /REG_SZ\s+(.+)$/m.exec(out);
      return { enabled: true, backend: "run-key", target: match ? match[1].trim() : WIN_RUN_KEY };
    } catch {
      return { enabled: false, backend: "run-key", target: WIN_RUN_KEY };
    }
  }
  return { enabled: false, backend: "none", target: null };
}

/**
 * @param {boolean} enabled
 * @param {{execPath?: string, scriptPath?: string, port?: number}} options
 *   execPath：单文件 homingd；scriptPath：node 模式下的 server.mjs
 */
export function setAutostart(enabled, { execPath = process.execPath, scriptPath = null, port = 18787 } = {}) {
  if (isMac) {
    const plist = macPlistPath();
    if (!enabled) {
      try {
        if (fs.existsSync(plist)) fs.unlinkSync(plist);
      } catch (err) {
        return { ok: false, error: `删除 LaunchAgent 失败：${err.message}` };
      }
      saveAutostartState({ enabled: false });
      return { ok: true, enabled: false, backend: "launchagent", note: "已移除 LaunchAgent，下次登录不再自启" };
    }
    const args = scriptPath ? [scriptPath] : [];
    const program = scriptPath ? execPath : execPath;
    const body = plistBody({ program, args, workdir: paths.data, port });
    try {
      fs.mkdirSync(path.dirname(plist), { recursive: true });
      fs.writeFileSync(plist, body, "utf8");
    } catch (err) {
      return { ok: false, error: `写入 LaunchAgent 失败：${err.message}` };
    }
    // 立刻加载会跟当前进程抢端口，所以只写文件，不动 launchctl
    saveAutostartState({ enabled: true });
    return { ok: true, enabled: true, backend: "launchagent", note: "已写入 LaunchAgent，下次登录自动启动" };
  }

  if (isWindows) {
    const command = scriptPath ? `"${execPath}" "${scriptPath}"` : `"${execPath}"`;
    try {
      if (enabled) execFileSync("reg", ["add", WIN_RUN_KEY, "/v", WIN_VALUE, "/t", "REG_SZ", "/d", command, "/f"]);
      else execFileSync("reg", ["delete", WIN_RUN_KEY, "/v", WIN_VALUE, "/f"]);
    } catch (err) {
      if (!enabled) {
        saveAutostartState({ enabled: false });
        return { ok: true, enabled: false, backend: "run-key", note: "启动项本来就不存在" };
      }
      return { ok: false, error: `写注册表失败：${err.message}` };
    }
    saveAutostartState({ enabled });
    return { ok: true, enabled, backend: "run-key", note: enabled ? "已加入当前用户登录启动项" : "已从登录启动项移除" };
  }

  return { ok: false, error: `暂不支持在 ${process.platform} 上设置开机自启`, backend: "none" };
}

export const autostartInfo = () => ({ ...getAutostart(), version: VERSION, legacy: legacyRelayAgent() });
