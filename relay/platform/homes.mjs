// Homing · agent 家目录解析
// Mac / Linux：就是 os.homedir()。
// Windows：%USERPROFILE% 之外，还要看一眼 WSL 各发行版的家目录（\\wsl$\<发行版>\home\<用户>）。
// 这里只负责「找位置」，读不读得动由 scanner 决定。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { isWindows } from "./index.mjs";

export const AGENT_HOMES = {
  codex: { label: "Codex", paths: [".codex"], probe: [".codex/session_index.jsonl", ".codex/sessions", ".codex/hooks.json"] },
  claude: { label: "Claude Code", paths: [".claude"], probe: [".claude/projects", ".claude/settings.json"] },
  workbuddy: { label: "WorkBuddy", paths: [".workbuddy"], probe: [".workbuddy"] },
  opencode: { label: "OpenCode", paths: [".local/share/opencode"], probe: [".local/share/opencode/opencode.db"] },
  zcode: { label: "ZCode", paths: [".zcode"], probe: [".zcode/cli/db/db.sqlite"] },
};

export function primaryHome() {
  return os.homedir();
}

/**
 * WSL 里的家目录。\\wsl$ 是 UNC 路径，只有 Windows 才有；
 * 发行版名和用户名都可能不存在，所以整个函数允许返回空数组。
 */
export function wslHomes() {
  if (!isWindows) return [];
  const roots = ["\\\\wsl$\\", "\\\\wsl.localhost\\"];
  const out = [];
  for (const root of roots) {
    let distros = [];
    try {
      distros = fs.readdirSync(root, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name);
    } catch {
      continue;
    }
    for (const distro of distros) {
      const homeRoot = `${root}${distro}\\home`;
      let users = [];
      try {
        users = fs.readdirSync(homeRoot, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name);
      } catch {
        // 有的发行版只跑 root，家目录在 /root
        users = ["root"];
      }
      for (const user of users) {
        const dir = `${root}${distro}${user === "root" ? "\\root" : `\\home\\${user}`}`;
        out.push({ dir, distro, user });
      }
      if (out.length > 8) return out;
    }
    if (out.length) return out;
  }
  return out;
}

/** 所有可能放着 agent 数据的「家目录」 */
export function homeCandidates() {
  const list = [{ dir: primaryHome(), kind: "native" }];
  for (const item of wslHomes()) list.push({ dir: item.dir, kind: "wsl", distro: item.distro, user: item.user });
  return list;
}

/** 某个 agent 在某个家目录下的数据路径（界面上用等宽字显示这个） */
export function dataPathFor(agentId, home = primaryHome()) {
  const spec = AGENT_HOMES[agentId];
  if (!spec) return home;
  return path.join(home, spec.paths[0]);
}

/** 该 agent 是否在某个家目录下存在 */
export function existsIn(agentId, home) {
  const spec = AGENT_HOMES[agentId];
  if (!spec) return false;
  return spec.probe.some((rel) => {
    try {
      return fs.existsSync(path.join(home, ...rel.split("/")));
    } catch {
      return false;
    }
  });
}

/** 全局探测：{ codex: { native: true, wsl: [{distro, user, dir}] } } */
export function detectAll() {
  const homes = homeCandidates();
  const result = {};
  for (const id of Object.keys(AGENT_HOMES)) {
    const entry = { native: false, wsl: [] };
    for (const home of homes) {
      if (!existsIn(id, home.dir)) continue;
      if (home.kind === "native") entry.native = true;
      else entry.wsl.push({ distro: home.distro, user: home.user, dir: dataPathFor(id, home.dir) });
    }
    result[id] = entry;
  }
  return result;
}

/** 给 /api/agents 用的一行摘要 */
export function describeHome(agentId) {
  const native = dataPathFor(agentId, primaryHome());
  return {
    path: native,
    wsl: wslHomes()
      .filter((h) => existsIn(agentId, h.dir))
      .map((h) => ({ distro: h.distro, user: h.user, path: dataPathFor(agentId, h.dir) })),
  };
}
