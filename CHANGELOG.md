# Changelog

## 0.11.5-beta.1 — release candidate

- Android groups notifications into phone-local conversations, starts context at the first received message, and loads earlier context in ten-round pages on request.
- Inbox clearing and conversation removal affect only phone display, with undo support. Added the in-app link browser.
- Event synchronization uses bounded pages and negotiated compression over encrypted RPC. Older clients retain the unpaged response format.
- WebSocket control frames remain responsive during slower API calls. Android resumes synchronization when a connection returns and refreshes the conversation left open.
- Failed explicit permission options now report failure instead of sending an ordinary chat reply.
- Fixed Android JSON-null history notices and switching the active computer after selection was already persisted.
- Retains meeting room chat, Agent mentions, pause / resume, minutes and confirmed action dispatch from the preceding development builds.

See the Beta guide and acceptance record for the distinction between tested behavior, runtime prerequisites and pending checks. This entry does not imply that a public GitHub release has already been published.
