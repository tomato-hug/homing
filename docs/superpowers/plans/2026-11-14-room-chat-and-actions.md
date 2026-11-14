# Room Chat and Actions Implementation Plan

**Goal:** Make Android rooms usable as a group chat with explicit mentions, reliable message status, and user-confirmed follow-up dispatch.

**Architecture:** Retain the R1 round-robin engine and add an atomic message+optional mention operation, keyed by a client-generated id for safe retry and reconciliation. Android preserves drafts and distinguishes sending, delivered, rejected, and unknown outcomes. Extract actionable lines from minutes as suggestions; confirmed dispatches are durably bound to stable identities before launching a separate agent task and their results return to the room. Never replay ambiguous launches.

**Tech stack:** Go relay API, Kotlin/Compose Android, existing orchestrator and transport; no new dependencies.

**Constraints:** Existing user and agent sessions are not used for tests. Preserve other authors' work and existing service. R1 remains read-only; writing actions use isolated worktrees. Real-phone and public relay tests remain deferred.

1. 完成 — RED/GREEN: validate atomic `@` slot selection and message deduplication under pause, completion, errors and retries; retain existing call rules.
2. 完成 — RED/GREEN: add Android mention chooser, message delivery states and per-room draft; verify cancellation/unknown outcome and polling reconciliation without replay.
3. 完成 — RED/GREEN: extract and edit minutes action suggestions, confirm target/task/worktree, create one durable dispatch, surface status and one result in room, and test crash/retry and errors.
4. 本机验证完成；远端 CI 在推送后跟进 — Run Go build/test/vet/race, Android tests/APK, staged gitleaks and clean-source verification. Record pending physical/public tests; commit owned paths and push existing development branch.
