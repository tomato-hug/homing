// Homing · 暂停推送
// 托盘菜单「暂停推送 1 小时」和控制台通知页共用一份状态，落在 <data>/push-pause.json。
// notify.mjs 在出通知前问一句 isPaused()，暂停期间事件照常落库、照常进收件箱，只是不推送。

import { paths, readJson, writeJson } from "./index.mjs";

const MAX_MINUTES = 24 * 60;

export function getPause() {
  const state = readJson(paths.pause, null);
  if (!state || typeof state.until_ms !== "number") {
    return { paused: false, until: null, minutes: 0, reason: null, set_at: null };
  }
  const paused = state.until_ms > Date.now();
  return {
    paused,
    until: new Date(state.until_ms).toISOString(),
    until_ms: state.until_ms,
    minutes: Math.max(0, Math.round((state.until_ms - Date.now()) / 60000)),
    reason: state.reason || null,
    set_at: state.set_at || null,
  };
}

export function isPaused() {
  return getPause().paused;
}

export function setPause(minutes, reason = null) {
  const value = Math.trunc(Number(minutes) || 0);
  if (value <= 0) {
    writeJson(paths.pause, { until_ms: 0, reason: null, set_at: new Date().toISOString() });
    return getPause();
  }
  const clamped = Math.min(value, MAX_MINUTES);
  const until = Date.now() + clamped * 60_000;
  writeJson(paths.pause, { until_ms: until, reason, set_at: new Date().toISOString() });
  return getPause();
}

export function clearPause() {
  return setPause(0);
}
