# Mobile conversation context implementation

**Goal:** Deliver grouped mobile conversations, receipt-based context windows with explicit ten-round history, phone-local cleanup, and usable in-app browsing; verify public relay and Mac management UI.

**Spec:** ../specs/2026-11-21-mobile-conversation-context-design.md

**Architecture:** Keep Compose/AgentStore and existing native session APIs. Add phone-local conversation boundaries and a backward-compatible, source-bound Go history query. Reuse BrowserScreen. Independent browser, backend patch and read-only Mac QA tasks run separately; root integrates the mobile store and UI.

## Global constraints

- No remote read/archive/delete changes for the user's cleanup controls.
- Preserve other contributors' dirty files; only stage owned changes.
- Backend ownership handoff requires a concrete tested patch first.
- No tests mutate real sessions. Same local debug signer for physical installation.
- Reader retention may be unknown: never label an unproven boundary as absolute history start.

## Review focus

- Identical session IDs on different sources/computers stay separate in boundaries and grouping.
- Initial synchronization or late event replay cannot silently move a previously established reading anchor backward.
- A delayed history response must not restore a different computer's data after switching hosts.
- Inbox clear followed by new activity must reveal new content without changing Mac metadata.
- Browser navigation/redirects must not forward Homing credentials to an external origin.

## Task 1: Public and Mac verification

Run opt-in TestPublicRelaySmoke and normal TLS health. Open an isolated local console browser, inspect only synthetic meetings/tasks, close that browser and verify the resident service remains available. Record installed native desktop version and any UI/API parity gaps separately.

## Task 2: History API patch

Files: isolated copies of internal/api/sessions.go plus new history helper/tests. Preserve legacy /items requests. New request uses source + anchor_event_id; history adds opaque before + turns=10. Return stable item_key, anchor_status, before_cursor, history_status, rounds_returned and explicit limitations. Test exact ten user-bounded rounds, timestamp-only anchors, corrupt/cross-session cursors, missing retention, unsupported readers and budgets. Prepare patch for ownership approval, then apply only those files.

## Task 3: Phone-local conversation state

Create PhoneConversations.kt and tests. Persist first receipt and cleanup watermarks keyed by computer/source/session. Add state to AgentUiState and integrate AgentStore receipt ingestion and home aggregation. Tests cover restart persistence, duplicate events, out-of-order receipt, independent hosts/sources, clear/undo and new-message visibility. Ensure cleanup tests observe zero transport mutations.

## Task 4: Mobile history and reading flow

Extend SessionItemsResult/SessionItemsState and AgentApi for history fields. AgentStore opens anchored context, merges pages by item_key, retains older loaded pages on refresh, and rejects stale host/anchor responses. SessionScreen shows origin boundary and an explicit load-10-rounds button with progress/retry/unavailable states; preserve scroll position on prepend. Test initial privacy boundary, paging, live-item deduplication and refresh retention.

## Task 5: Inbox and session controls

Group home entries by computer/source/session. Add visible clear control and undo; add local session removal and restore using Task3 operations, not existing remote archive/read operations. Tests cover multiple messages per conversation, mixed sources, cleanup after search/filter and remote-state invariance.

## Task 6: In-app browsing

BrowserScreen plus focused policy helpers/tests. Verify system Back history vs close-to-conversation, initial/address/popup navigation policy, reload/error UI, strict proxy origin/path authorization and redirect handling. Root integrates any missing conversation link callback and localhost capability copy.

## Task 7: Integration and delivery

Run focused regression tests, Android full unit suite/debug build, relevant backend tests/build and gitleaks. Install only after matching debug certificate; repeat real phone/public flows if connected. Review the combined diff, commit only owned paths, push the existing development branch and run exact-commit CI. Record unavailable physical/background scenarios explicitly.

## Execution evidence

The user authorized ownership handoff of the three API files; the patch is applied and the isolated paired Mac host is updated. Local Android verification passes all 255 tests and debug assembly; API tests, history race checks, Go build and headless full tests pass. Native-reader HTTP integration passes 19 checks with 23 synthetic rounds and 10/10/2 round pages. Public real-host encrypted history RPC and temporary-peer revocation also pass. Final commit/push and exact-commit CI are recorded in the delivery result. Physical Android remains pending because ADB has no connected device.
