# Cloud tunnel and Android acceptance — 2026-11-21

## Public endpoint

The user configured Cloudflare Tunnel at `wss://homing.douqu.online/v1/ws`,
forwarding to the existing relay at `127.0.0.1:18789`. The original
`homing.littlelove.fun` route was left unchanged. Its earlier Tencent domain
interception result remains recorded in [the previous report](cloud-rollout-2026-11-20.md).

Fresh external verification against the tunnel passed normal TLS validation:

- `GET https://homing.douqu.online/health`: HTTP 200, `ok=true`, `service=homing-server`.
- Public protocol smoke: authenticated handshake and rejection of unpaired
  traffic; token pairing and encrypted delivery; offline replay, acknowledgement
  and reconnect; revocation preventing delivery; six-digit-code pairing.
- Latest full public smoke duration: 27.74 seconds.

These protocol checks exercised the existing remote backend. The SQLite fix
below has **not yet been deployed** to that backend; passing this smoke does not
establish that concurrent production queue writes are fixed.

## Reproduced defects and corrections

1. **Go connector heartbeat lock inversion.** `Ping` waits for the read loop to
   consume its pong. Holding the application write mutex while waiting can block
   that same reader when an application ping precedes the pong. Removed the
   outer mutex around `Ping`; the WebSocket library serializes control writes.
   A real WebSocket regression failed before the fix and passed with the race
   detector afterward.
2. **Concurrent SQLite queue writes.** File-backed databases used an unrestricted
   connection pool, while connection-local PRAGMAs were initialized only once.
   Eight concurrent enqueue/acknowledge workers reproduced `SQLITE_BUSY`.
   A single database connection serializes the queue transactions and receipt
   writes. The regression verifies every acknowledgement and an empty final queue.
   The live relay emitted `internal_error` during phone testing; remote logs are
   still needed to confirm whether those particular errors had this cause.
3. **Android reconnect state without a reconnect.** A relay `error` frame only
   changed the listener state, leaving the socket open and requests blocked by
   the disconnected UI state. It now invalidates the attempt, fails outstanding
   requests and closes the socket through the normal reconnect path. Tests cover
   errors before and after `hello.ok`. Mutation requests retain their existing
   no-automatic-replay behavior.
4. **Consumed Android pairing links replayed on recreation.** Activity recreation
   read the original launch intent again. Pending pairing navigation is now saved
   in Activity state, including its consumed/null state. Lifecycle tests verify
   consumed links stay consumed, unconsumed links survive and fresh intents work.

## Automated checks

- Clean candidate: `go build ./...`, `go vet -tags headless ./...`,
  `go test -tags headless ./... -count=1 -timeout=300s` — passed.
- Server/cloud/protocol: `go test -race -tags headless` — passed.
- Android: `:app:testDebugUnitTest :app:assembleDebug` — passed;
  191 tests, zero failures/errors/skips.
- Debug APK version 17 (`0.11.5-dev`), matching the installed Android Debug
  certificate; APK and app data backed up privately before replacement.
- APK SHA256: `edf8c4f984205d66a00a07b593383aa9a7f5ba14bc3f93ddfa8db8d803c781fe`.

## Physical-device evidence

Device: OnePlus Ace 2 (PHK110), Android 16. Wi-Fi was off, mobile data enabled,
and ADB reverse mappings empty. The phone used the public tunnel; USB supplied
ADB control only. All agent input targeted new synthetic test sessions and a
fresh repository under `/tmp`.

- Public pairing completed with matching four-word SAS.
- Phone group chat sent `@Hermes` with a synthetic marker; delivered state and
  exactly one persisted message were verified.
- Pause, next-speaker selection and resume succeeded from the phone.
- Real Codex and Hermes spoke; the room ended with minutes and two suggested
  action items. Hermes used the existing isolated acceptance runtime and sandbox.
- Updated debug APK installed successfully with existing pairing retained.

- The phone dispatched Codex into an independent worktree. It created
  `demo-tunnel-result.txt` containing exactly `demo-tunnel-ok` plus a newline.
- The first Hermes action read the expected README, but its final response
  incorrectly retained the meeting-stage wait-for-confirmation restriction.
  This was treated as failed task acceptance despite the runner completing.
- A synthetic pairing deep link was consumed, then the phone was rotated to
  landscape and back. It stayed in the meeting instead of reopening pairing;
  original rotation preferences were restored.
- Stopping only the isolated host produced a visible request timeout. After
  restarting it, the phone loaded the new meeting without re-pairing; the
  background/foreground transition also retained the selected computer.

## Confirmed action execution boundary

With the user's explicit coordination, this change also owns only
`internal/relayapi/rooms_actions.go` and its new prompt regression test.
The original prompt appended the old discussion topic after the confirmed task,
so a real Hermes run continued to follow the old “discussion only / wait for
confirmation” instruction. The new prompt labels that text as historical
background, then gives the confirmed task and its execution scope. It explicitly
preserves the task's own read-only and acceptance constraints and excludes other
actions. A dispatch-level regression failed before this change and passed after.
A fresh real Codex/Hermes room exercised the corrected path. Its confirmed
Hermes action read the README in a new worktree, reported the required marker
on a standalone final line, and left both the action worktree and source
repository clean. Actual tool output was checked independently of the summary.

The native task acceptance passed. The phone eventually displayed the corrected
Hermes result after the isolated host was restarted. However, an earlier
45-second foreground observation still showed a request timeout and stale action
state. A captured host stack was idle in WebSocket reading, rather than blocked
inside the local API. No cause is claimed for this remaining transport symptom.
A separate observation lasting over five minutes recorded no host WebSocket
disconnect or relay error; that does not establish end-to-end RPC stability.
Remote backend deployment and repeat acceptance without manual recovery remain
pending.
Do not treat this report as completed production acceptance.

## Deployment boundary

The currently verified remote backend remains the deployment documented in the
previous report. A Linux amd64 candidate with the SQLite correction has been
built and packaged with the checksum-verifying, rollback-capable relay installer.
Remote installation and post-update concurrency verification remain pending.
Browser-based management access was stopped by the computer-use URL restriction;
no remote service or tunnel configuration was changed in this round.

Gitleaks scanning of the owned source set passed. The patch, local test logs,
private physical-device evidence and Linux package are retained under
`output/homing-cloud-tunnel-20261121/` (ignored by Git). Other agents' working-tree
changes are outside this change set. The release manifest records the exact
committed source revision and binary checksums after packaging.
