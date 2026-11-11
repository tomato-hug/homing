export const DELIVERED_REPLY_STATUSES = new Set(["injected", "held"]);

export function shouldIgnoreReplyStatusDowngrade(currentStatus, nextStatus) {
  const current = typeof currentStatus === "string" ? currentStatus : "";
  return nextStatus === "queued" && DELIVERED_REPLY_STATUSES.has(current);
}

export function normalizeReplyRecord(record) {
  return {
    injected: false,
    ...record,
    status: typeof record?.status === "string" ? record.status : "queued",
  };
}

export function mergeReplyRecord(previous, next) {
  const normalized = normalizeReplyRecord(next);
  if (!previous) {
    return normalized;
  }
  if (!shouldIgnoreReplyStatusDowngrade(previous.status, normalized.status)) {
    return normalized;
  }
  return {
    ...normalized,
    status: previous.status,
    message: previous.message || normalized.message,
    injected: previous.injected === true,
  };
}

export function resolveCodexFinishStatus(handedOff, output) {
  if (handedOff) {
    return "injected";
  }
  return /failed to resume|warning:/i.test(output) ? "queued" : "injected";
}

export function oldestQueuedReplyBeforeEvent(replies, event) {
  if (event?.kind !== "reply" || !event.session_id) {
    return null;
  }
  return replies
    .filter(
      (reply) =>
        reply.status === "queued" &&
        reply.session_id === event.session_id &&
        String(reply.ts || "") <= String(event.ts || ""),
    )
    .sort((a, b) => a.id - b.id)[0] || null;
}

export function queuedReplyIDsConfirmedByEvents(replies, events) {
  const claimedEvents = new Set();
  const confirmed = [];
  for (const reply of [...replies].sort((a, b) => a.id - b.id)) {
    if (reply.status !== "queued") {
      continue;
    }
    const event = events
      .filter(
        (candidate) =>
          !claimedEvents.has(candidate.id) &&
          oldestQueuedReplyBeforeEvent([reply], candidate) !== null,
      )
      .sort((a, b) => a.id - b.id)[0];
    if (event) {
      claimedEvents.add(event.id);
      confirmed.push(reply.id);
    }
  }
  return confirmed;
}
