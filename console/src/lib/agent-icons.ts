import codex from "@lobehub/icons-static-svg/icons/codex-color.svg?raw";
import claude from "@lobehub/icons-static-svg/icons/claudecode-color.svg?raw";
import workbuddy from "@lobehub/icons-static-svg/icons/workbuddy-color.svg?raw";
import kimi from "@lobehub/icons-static-svg/icons/kimi-color.svg?raw";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg?raw";
import deepseek from "@lobehub/icons-static-svg/icons/deepseek-color.svg?raw";
import openclaw from "@lobehub/icons-static-svg/icons/openclaw-color.svg?raw";
import codebuddy from "@lobehub/icons-static-svg/icons/codebuddy-color.svg?raw";
import gemini from "@lobehub/icons-static-svg/icons/geminicli-color.svg?raw";
import hermes from "@lobehub/icons-static-svg/icons/hermesagent.svg?raw";
import cursor from "@lobehub/icons-static-svg/icons/cursor.svg?raw";
import goose from "@lobehub/icons-static-svg/icons/goose.svg?raw";
import cline from "@lobehub/icons-static-svg/icons/cline.svg?raw";
import roo from "@lobehub/icons-static-svg/icons/roocode.svg?raw";
import qwen from "@lobehub/icons-static-svg/icons/qwen-color.svg?raw";
import amp from "@lobehub/icons-static-svg/icons/amp.svg?raw";
import copilot from "@lobehub/icons-static-svg/icons/githubcopilot.svg?raw";
import openhands from "@lobehub/icons-static-svg/icons/openhands-color.svg?raw";
import antigravity from "@lobehub/icons-static-svg/icons/antigravity-color.svg?raw";
import trae from "@lobehub/icons-static-svg/icons/trae-color.svg?raw";
import qoder from "@lobehub/icons-static-svg/icons/qoder-color.svg?raw";
import windsurf from "@lobehub/icons-static-svg/icons/windsurf.svg?raw";
import kiro from "@lobehub/icons-static-svg/icons/kiro-color.svg?raw";
import kilocode from "@lobehub/icons-static-svg/icons/kilocode.svg?raw";
import junie from "@lobehub/icons-static-svg/icons/junie-color.svg?raw";
import minimax from "@lobehub/icons-static-svg/icons/minimax-color.svg?raw";

export interface AgentLogo {
  svg: string;
  color?: string;
  mono?: boolean;
}

const mark = (body: string, background: string, foreground: string): string =>
  `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect width="24" height="24" rx="4.5" fill="${background}"/>${body.replaceAll("CURRENT", foreground)}</svg>`;

const monogram = (text: string, background: string, foreground: string): string =>
  mark(
    `<text x="12" y="12.2" text-anchor="middle" dominant-baseline="central" font-family="ui-monospace, SFMono-Regular, Menlo, monospace" font-size="${
      text.length > 2 ? 6.3 : text.length > 1 ? 8.5 : 11
    }" font-weight="700" letter-spacing="-.04em" fill="CURRENT">${text}</text>`,
    background,
    foreground,
  );

const custom: Record<string, AgentLogo> = {
  zcode: {
    svg: mark('<path d="M6.8 6.4h10.4L10.6 14h6.6v3.6H6.8L13.4 10H6.8z" fill="CURRENT"/>', "#0F2A2A", "#5EEAD4"),
  },
  zed: {
    svg: '<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="currentColor"><path d="M2.25 1.5a.75.75 0 0 0-.75.75v16.5H0V2.25A2.25 2.25 0 0 1 2.25 0h20.095c1.002 0 1.504 1.212.795 1.92L10.764 14.298h3.486V12.75h1.5v1.922a1.125 1.125 0 0 1-1.125 1.125H9.264l-2.578 2.578h11.689V9h1.5v9.375a1.5 1.5 0 0 1-1.5 1.5H5.185L2.562 22.5H21.75a.75.75 0 0 0 .75-.75V5.25H24v16.5A2.25 2.25 0 0 1 21.75 24H1.655C.653 24 .151 22.788.86 22.08L13.19 9.75H9.75v1.5h-1.5V9.375A1.125 1.125 0 0 1 9.375 8.25h5.314l2.625-2.625H5.625V15h-1.5V5.625a1.5 1.5 0 0 1 1.5-1.5h13.19L21.438 1.5z"/></svg>',
    color: "#F8FAFC",
    mono: true,
  },
  aider: {
    svg: mark(
      '<path d="M4.8 17.6 13.1 5.8a1.4 1.4 0 0 1 2.3 1.6l-8.3 11.8z" fill="CURRENT"/><path d="m14.7 5.3 1.1-.7.8 1.2-1 .7zm2.6 2 1.2-.4.4 1.3-1.2.4z" fill="CURRENT"/>',
      "#2E1065",
      "#E9D5FF",
    ),
  },
  continue: {
    svg: mark(
      '<path d="M17.7 8.1A6.8 6.8 0 1 0 18.4 14" fill="none" stroke="CURRENT" stroke-width="2.3" stroke-linecap="round"/><path d="M18.4 5.8v4.4H14" fill="none" stroke="CURRENT" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/><circle cx="10.8" cy="12" r="1.4" fill="CURRENT"/>',
      "#7C2D12",
      "#FED7AA",
    ),
  },
  crush: {
    svg: mark(
      '<path d="M5 9.4c2.4 0 2.4 2.1 4.7 2.1s2.4-2.1 4.7-2.1 2.4 2.1 4.7 2.1M5 14.1c2.4 0 2.4 2.1 4.7 2.1s2.4-2.1 4.7-2.1 2.4 2.1 4.7 2.1" fill="none" stroke="CURRENT" stroke-width="1.8" stroke-linecap="round"/>',
      "#500724",
      "#FDA4AF",
    ),
  },
  droid: {
    svg: mark(
      '<rect x="6.7" y="8.1" width="10.6" height="8.4" rx="2.2" fill="CURRENT"/><path d="M9.4 8.1 7.8 5.2M14.6 8.1l1.6-2.9M9.6 12h.1M14.3 12h.1M9.2 20.2v-2.2M14.8 20.2v-2.2" stroke="CURRENT" stroke-width="1.7" stroke-linecap="round"/>',
      "#172554",
      "#93C5FD",
    ),
  },
  warp: {
    svg: '<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="currentColor"><path d="M12.035 2.723h9.253A2.712 2.712 0 0 1 24 5.435v10.529a2.712 2.712 0 0 1-2.712 2.713H8.047Zm-1.681 2.6L6.766 19.677h5.598l-.399 1.6H2.712A2.712 2.712 0 0 1 0 18.565V8.036a2.712 2.712 0 0 1 2.712-2.712Z"/></svg>',
    color: "#67E8F9",
    mono: true,
  },
  nanobot: { svg: monogram("NB", "#082F49", "#7DD3FC") },
  "reasonix-desktop": { svg: monogram("Rx", "#3B0764", "#E9D5FF") },
  pig: { svg: monogram("PiG", "#1C1917", "#FDA4AF") },
  bub: { svg: monogram("Bu", "#052E16", "#86EFAC") },
  type4me: { svg: monogram("4", "#0C4A6E", "#E0F2FE") },
  cowork: { svg: monogram("协", "#422006", "#FDE68A") },
  opencodereview: { svg: monogram("OCR", "#1E1B4B", "#C7D2FE") },
  pink: { svg: monogram("Pi", "#500724", "#F9A8D4") },
  wispterm: { svg: monogram(">_", "#042F2E", "#5EEAD4") },
  citebox: { svg: monogram("[]", "#0F172A", "#CBD5E1") },
  "wisp-science": { svg: monogram("Ws", "#1E1B4B", "#A5B4FC") },
  kaku: { svg: monogram("K", "#3F1D0B", "#FDBA74") },
  eta: { svg: monogram("η", "#2E1065", "#D8B4FE") },
  "rhino9-ai": { svg: monogram("Rh", "#052E16", "#86EFAC") },
  "nowledge-mem": { svg: monogram("N", "#1C1917", "#E7E5E4") },
  "deveco-code": { svg: monogram("DV", "#0C2A4A", "#93C5FD") },
  pi: { svg: monogram("π", "#2D1B0E", "#FDE68A") },
  mothx: { svg: monogram("M", "#2A0A18", "#F9A8D4") },
  penguin: { svg: monogram("Pg", "#0C4A6E", "#BAE6FD") },
  omo: { svg: monogram("OMO", "#28103B", "#F0ABFC") },
  omp: { svg: monogram("omp", "#172554", "#BFDBFE") },
  atomcode: { svg: monogram("At", "#0F172A", "#67E8F9") },
  snow: { svg: monogram("Sn", "#0C4A6E", "#E0F2FE") },
};

const imported: Record<string, AgentLogo> = {
  codex: { svg: codex },
  claude: { svg: claude },
  workbuddy: { svg: workbuddy },
  kimi: { svg: kimi, color: "#1783FF" },
  "kimi-desktop": { svg: kimi, color: "#1783FF" },
  opencode: { svg: opencode, color: "#F8FAFC", mono: true },
  dsh: { svg: deepseek },
  openclaw: { svg: openclaw },
  codebuddy: { svg: codebuddy },
  "gemini-cli": { svg: gemini },
  hermes: { svg: hermes, color: "#E2E8F0", mono: true },
  cursor: { svg: cursor, color: "#F8FAFC", mono: true },
  goose: { svg: goose, color: "#FB923C", mono: true },
  cline: { svg: cline, color: "#38BDF8", mono: true },
  "roo-code": { svg: roo, color: "#A78BFA", mono: true },
  "qwen-code": { svg: qwen },
  amp: { svg: amp, color: "#F8FAFC", mono: true },
  "copilot-cli": { svg: copilot, color: "#F8FAFC", mono: true },
  openhands: { svg: openhands },
  antigravity: { svg: antigravity },
  trae: { svg: trae },
  qoder: { svg: qoder, color: "#A78BFA", mono: true },
  windsurf: { svg: windsurf, color: "#22D3EE", mono: true },
  kiro: { svg: kiro },
  kilo: { svg: kilocode, color: "#F8FAFC", mono: true },
  junie: { svg: junie },
  "minimax-design": { svg: minimax },
};

export const AGENT_LOGOS: Record<string, AgentLogo> = { ...imported, ...custom };

export function agentLogo(id: string): AgentLogo | null {
  return AGENT_LOGOS[id] || null;
}
