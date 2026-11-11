import assert from "node:assert/strict";
import test from "node:test";
import {
  mergeReplyRecord,
  oldestQueuedReplyBeforeEvent,
  queuedReplyIDsConfirmedByEvents,
  resolveCodexFinishStatus,
  shouldIgnoreReplyStatusDowngrade,
} from "./reply-state.mjs";

test("a delivered reply is never downgraded to queued", () => {
  assert.equal(shouldIgnoreReplyStatusDowngrade("injected", "queued"), true);
  assert.equal(shouldIgnoreReplyStatusDowngrade("held", "queued"), true);
  assert.equal(shouldIgnoreReplyStatusDowngrade("queued", "queued"), false);
  assert.equal(shouldIgnoreReplyStatusDowngrade("queued", "injected"), false);
});

test("loading replies repairs historical injected-then-queued records", () => {
  const injected = {
    id: 20,
    status: "injected",
    message: "已交给 Codex 原生队列，继续在会话中执行",
    injected: true,
  };
  const staleQueued = {
    id: 20,
    status: "queued",
    message: "已排入 Codex 队列，等待会话恢复",
    injected: false,
  };
  const merged = mergeReplyRecord(injected, staleQueued);
  assert.equal(merged.status, "injected");
  assert.equal(merged.message, injected.message);
  assert.equal(merged.injected, true);
});

test("a warning after the handoff probe does not undo delivery", () => {
  assert.equal(resolveCodexFinishStatus(true, "warning: failed to resume"), "injected");
  assert.equal(resolveCodexFinishStatus(false, "warning: failed to resume"), "queued");
  assert.equal(resolveCodexFinishStatus(false, "queued"), "injected");
});

test("an agent reply event confirms queued messages one at a time", () => {
  const replies = [
    { id: 18, session_id: "demo", status: "queued", ts: "2026-11-11T15:32:56.000Z" },
    { id: 19, session_id: "demo", status: "queued", ts: "2026-11-11T15:36:46.000Z" },
    { id: 20, session_id: "demo", status: "injected", ts: "2026-11-11T15:37:00.000Z" },
  ];
  const events = [
    { id: 129, session_id: "demo", kind: "reply", ts: "2026-11-11T15:33:13.000Z" },
    { id: 130, session_id: "demo", kind: "reply", ts: "2026-11-11T15:43:51.000Z" },
  ];

  assert.deepEqual(queuedReplyIDsConfirmedByEvents(replies, events), [18, 19]);
  assert.equal(oldestQueuedReplyBeforeEvent(replies, events[0])?.id, 18);
});
