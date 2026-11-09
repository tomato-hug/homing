// Homing relay · scanners 共用工具
// 从 v1 server.mjs 原样搬迁（行为不变），供各 scanner import。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

export const META_TTL_MS = 60_000;
export const HEAD_BYTES = 320 * 1024;
export const TAIL_BYTES = 192 * 1024;

// ---- 通用读取 -----------------------------------------------------------

export function readJsonLines(filePath) {
  if (!fs.existsSync(filePath)) {
    return [];
  }

  const records = [];
  for (const line of fs.readFileSync(filePath, "utf8").split("\n")) {
    if (!line.trim()) {
      continue;
    }
    try {
      records.push(JSON.parse(line));
    } catch {
      // A damaged trailing line should not prevent the relay from starting.
    }
  }
  return records;
}

export function truncate(value, limit = 4000) {
  return Array.from(typeof value === "string" ? value : "").slice(0, limit).join("");
}

export function projectFromCwd(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return "未命名项目";
  }

  const parts = cwd
    .trim()
    .replace(/[\\/]+$/, "")
    .split(/[\\/]/)
    .filter(Boolean);
  return parts.at(-1) || cwd.trim();
}

export const NOISE_PATTERN = /^(<skills_instructions>|<environment_context>|# AGENTS\.md instructions|<user_instructions>|<permissions|<INSTRUCTIONS|<environment|<tools>|<multi_agent_mode>|<system-reminder|<command-name|<command-message|<local-command)/i;


export function normalizeContent(content) {
  if (typeof content === "string") {
    return content;
  }
  if (!Array.isArray(content)) {
    return "";
  }
  const parts = [];
  for (const block of content) {
    if (typeof block === "string") {
      parts.push(block);
      continue;
    }
    if (!block || typeof block !== "object") {
      continue;
    }
    if (
      (block.type === "text" || block.type === "input_text" || block.type === "output_text") &&
      typeof block.text === "string"
    ) {
      parts.push(block.text);
    }
  }
  return parts.join("\n");
}

// ---- 文件切片与首行 -----------------------------------------------------

export function firstLine(text, limit = 120) {
  const value = typeof text === "string" ? text.trim() : "";
  if (!value) {
    return "";
  }
  const line = value.split("\n").find((item) => item.trim()) || "";
  return Array.from(line.trim()).slice(0, limit).join("");
}

export function readFileSlice(filePath, start, length) {
  if (length <= 0) {
    return "";
  }
  let fd;
  try {
    fd = fs.openSync(filePath, "r");
    const buffer = Buffer.alloc(length);
    const read = fs.readSync(fd, buffer, 0, length, start);
    return buffer.subarray(0, read).toString("utf8");
  } catch {
    return "";
  } finally {
    if (fd !== undefined) {
      try {
        fs.closeSync(fd);
      } catch {
        // 忽略
      }
    }
  }
}

export function readFileHeadAndTail(filePath, size) {
  const head = readFileSlice(filePath, 0, Math.min(size, HEAD_BYTES));
  const tail =
    size > HEAD_BYTES
      ? readFileSlice(filePath, Math.max(0, size - TAIL_BYTES), Math.min(size, TAIL_BYTES))
      : head;
  return { head, tail };
}

// ---- Codex -------------------------------------------------------------

// ---- 公共字段推导 -------------------------------------------------------

const gitBranchCache = new Map(); // cwd -> { at, branch }


export function gitBranchFor(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return null;
  }
  const cached = gitBranchCache.get(cwd);
  if (cached && Date.now() - cached.at < GIT_BRANCH_TTL_MS) {
    return cached.branch;
  }
  let branch = null;
  try {
    const output = execFileSync(
      "git",
      ["-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"],
      { timeout: 1000, stdio: ["ignore", "pipe", "ignore"] },
    )
      .toString("utf8")
      .trim();
    if (output && output !== "HEAD") {
      branch = output;
    }
  } catch {
    branch = null;
  }
  gitBranchCache.set(cwd, { at: Date.now(), branch });
  return branch;
}

export function workspaceLabel(cwd) {
  if (typeof cwd !== "string" || !cwd.trim()) {
    return null;
  }
  const parent = path.dirname(cwd.trim());
  if (!parent || parent === "." || parent === "/") {
    return path.basename(parent || "/") || null;
  }
  return path.basename(parent);
}

export function sessionTitleFor({ source, sessionId, nativeTitle, nativeTitleSource, firstPrompt, cwd }) {
  const cleanTitle = (value) =>
    typeof value === "string" && value.trim() ? Array.from(value.trim()).slice(0, 200).join("") : "";
  const byNative = cleanTitle(nativeTitle);
  if (byNative) {
    return { title: byNative, title_source: nativeTitleSource || "agent" };
  }
  const byPrompt = firstLine(firstPrompt, 40);
  if (byPrompt) {
    return { title: byPrompt, title_source: "first_prompt" };
  }
  const project = typeof cwd === "string" && cwd.trim() ? path.basename(cwd.trim()) : "";
  if (project) {
    return { title: project, title_source: "project" };
  }
  const shortId = String(sessionId || "").slice(0, 8) || "unknown";
  return { title: `${source}-${shortId}`, title_source: "fallback" };
}

export function deriveSessionStatus({ lastKind, pendingInjection, lastTs, nativeState, now = Date.now() }) {
  if (lastKind === "permission") {
    return "permission";
  }
  if (lastKind === "needs_input") {
    return "needs_input";
  }
  if (lastKind === "error") {
    return "error";
  }
  if (nativeState === "error") {
    return "error";
  }
  if (pendingInjection) {
    return "running";
  }
  if (lastKind === "reply") {
    const ts = Date.parse(lastTs || "");
    if (Number.isFinite(ts) && now - ts < RUNNING_WINDOW_MS) {
      return "running";
    }
  }
  return "done";
}

export { fs, os, path, execFileSync };
