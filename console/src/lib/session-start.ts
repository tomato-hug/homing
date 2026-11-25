import type { SessionSummary } from "./api";

export interface StartedSession {
  sessionID: string;
  source: string;
  cwd: string;
  text: string;
  model?: string | null;
  effort?: string | null;
  permissionMode?: string | null;
}

export function startedSessionSummary(sessions: SessionSummary[], started: StartedSession): SessionSummary {
  const indexed = sessions.find(session => session.session_id === started.sessionID && session.source === started.source);
  if (indexed) return indexed;
  // Native scanners can lag behind session/new. The confirmed launch identity
  // is enough to open the transcript; normal polling replaces this summary.
  return {
    session_id: started.sessionID,
    source: started.source,
    cwd: started.cwd,
    title: started.text.trim().split("\n")[0].slice(0, 80),
    title_source: "prompt",
    model: started.model || null,
    effort: started.effort || null,
    permission_mode: started.permissionMode || null,
    status: "running",
    running: true,
    last_ts: new Date().toISOString(),
  };
}
