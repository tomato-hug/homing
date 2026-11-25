# Native Mac acceptance — 2026-11-25

Source: `39a4793305ec7d1f6bca21ef3ad505fe8e8df84a` (`homing-v3`). Native application and resident candidate: `0.11.6-beta.1`, bundle version `0.11.6 (20)`, Apple Silicon, minimum macOS 13. Tested locally on macOS 26.3.1.

## Completed

- The installed `/Applications/Homing.app` was upgraded from 0.7.0 after preserving the complete old bundle. Developer ID Application signature and secure timestamp verified. This build is not Apple-notarized.
- The installed app opens and authenticates the existing 18787 Go service. Quitting the attached app leaves the same resident process healthy and does not alter phone pairing.
- Isolated native tests cover form entry/clearing, permission selection, fresh Codex session creation, immediate navigation to its confirmed identity, transcript display, and a real queued follow-up reply. All prompts used new synthetic conversations in a temporary project.
- Main-window hide/reopen and repeated launch were checked; the second instance exits successfully. Tray pause/resume is covered against the actual local API in a native unit test.
- A restricted GUI-style PATH reproduces missing user-installed interpreters. The runtime now discovers installed standard user/Homebrew directories without reading shell startup scripts or changing Agent credentials. Claude CLI 2.1.90 and Hermes ACP checks pass. Model completion was verified with Codex, not every catalog Agent.
- WorkBuddy new-session creation returns 409 before invoking its bridge. Existing user conversations were not used for message tests.
- Signed headless candidate runs separately on an isolated data profile. Over the public relay: WSS 101, signed hello, pairing identity/SAS, encrypted version/connection RPC, receipt-anchored history, and an earlier-history request all pass. The two-round fixture returned its two available earlier rounds when ten were requested. Temporary pairing was revoked; its private key was removed; pre-existing peers were unchanged.

## Automated checks

- `go build ./...` on macOS; native tests for the entry point, GUI, platform and Codex integration.
- `go vet -tags headless ./...` and `go test -tags headless ./...`.
- Race tests for Codex startup and runtime paths; silent startup timeout, continued execution after identity announcement, and short-lived session identity regression tests.
- TypeScript build, mention checks and new-session selection checks.
- Staged gitleaks scan: no findings.
- [Mac native CI](https://github.com/tomato-hug/homing/actions/runs/45571196210): passed, including native packaging.
- [Full CI](https://github.com/tomato-hug/homing/actions/runs/45571277864): Go, protocol, relay and Android tests/APK build passed.

## Deployment boundary

At this checkpoint, the installed native shell is updated, while the production 18787 resident service remains `0.11.5-dev+19ca106.sync2`. The user required confirmation before that service is switched. The candidate, read-only preflight, consistent backup plan and automatic startup/cloud-connection rollback are ready; activation is pending confirmation. Consequently, the daily window still receives the old embedded console until the resident service is updated.

The current production encrypted probe confirmed connection, 537 events across paginated responses, and the refresh endpoints. Its older fixed-fixture history assertion failed; a direct check still returned a matched receipt anchor and the later messages. This was not recorded as a full pass. The fresh isolated candidate fixture described above passed the current history contract.

Android was absent from ADB during this checkpoint. No new physical-phone regression or notarization is claimed. Existing Android 0.11.5-beta.1 remains the installed mobile release from the previous acceptance round.

## Artifacts

The private draft `0.11.6-beta.1` contains the Developer ID signed app ZIP, separate signed resident executable archive, build provenance, checksums, guide and license. Seven uploaded asset digests were compared with the local files. It is a draft prerelease, not a public stable release. See [Mac native guide](../macos-native.md) for startup ownership, Agent capability boundaries and packaging instructions.
