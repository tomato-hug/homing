# Mac native application

Homing uses a Go/Wails native shell with the same console and Agent API as the resident service. The current native package targets macOS 13 or later. Validate the CPU architecture and signature of the actual download; CI artifacts are ad hoc signed and are not notarized.

## Install and update

1. Verify `SHA256SUMS` and `BUILD-INFO.json` from the release.
2. Quit the old Homing window and back up `Homing.app` before replacing it in Applications. Preserve the data directory and existing paired devices.
3. Open Homing from Finder. The app authenticates an existing local Go service using the same data profile; it refuses foreign or unauthenticated services. If no service exists, the app starts its own.
4. Check the service version at the top left. The menu bar menu reports the native shell version. Replacing the app does not replace a separately installed `homing serve` binary.
5. Upgrade a resident service separately during an agreed restart window, using a complete data/binary/startup backup and a rollback check. Do not run two writers against a data directory.

Closing the main window hides it; opening the app again brings back the main window without the tray panel. Repeated launches use one native instance for the same service/profile. Quitting an attached window leaves the resident service and phone connection running. Quitting an app that owns its service stops that service.

A Developer ID signature and Apple notarization are separate checks. `codesign --verify --deep --strict Homing.app` verifies the signature; it does not prove notarization. Do not describe a signed but unnotarized package as a public notarized release. Do not disable Gatekeeper or remove quarantine as an installation workaround.

## Agent setup and capabilities

Install and authenticate the Agent CLI before starting work. Finder and launchd do not read interactive shell startup files. Homing adds existing standard user/Homebrew command directories to PATH, preserving explicit PATH precedence, so a CLI installed through npm can find its Node interpreter. It does not read shell startup scripts, copy credentials or alter Agent configuration.

| Agent | Native integration boundary |
| --- | --- |
| Codex | Fresh sessions, transcript, native queued replies and stop for supported CLI versions; a silent launch fails after 60 seconds instead of leaving the form pending forever |
| Claude Code | Uses the installed CLI and existing login; command/interpreter discovery is checked separately from a real model response |
| WorkBuddy | Existing-session integration only; no new-session or dispatch action is exposed without a safe independent launcher |
| ACP Agents, including Hermes | Need the CLI, ACP dependency and a configured model/provider; a successful protocol check alone does not establish model readiness |
| Other catalog entries | Capabilities are per installed adapter and runtime, not guaranteed by appearing in the catalog |

The new-session dialog lists Agents advertising a usable new-session capability. Enter a project directory and task, choose permissions, and start. It opens the confirmed session immediately even when the background list scanner has not indexed it yet. Loading the list must not switch to another Agent with the same session identifier.

Authentication, model access, network reachability and storage permissions remain runtime requirements. If startup fails, inspect the specific CLI error. External-volume access may require macOS permission for the actual app. Do not grant broader access merely to hide an unrelated error.

## Build

On a Mac with Go, Node.js and Xcode Command Line Tools:

```sh
npm ci --prefix console
python3 scripts/package-macos.py \
  --version 0.11.6-beta.1 --build-number 20 \
  --commit "$(git rev-parse HEAD)" --output /tmp/homing-native-package
```

Use a clean source snapshot for release packaging. The output directory must not already contain the app. Without `--identity`, the script uses an ad hoc signature for local/CI testing. A maintainer can supply their Developer ID identity to enable hardened runtime and a secure timestamp. Notarization and stapling are separate release steps; this script records `notarized: false`.

The package contains an icon, numeric bundle version, minimum OS version, local-network usage description, the Go executable, an app ZIP, checksums and build provenance. The `Mac native` workflow builds and tests the native shell and packages an ad hoc artifact without requiring signing credentials.

## Verification

Run the native and headless checks, frontend checks, and a real new Codex conversation in a temporary project. Check typing/clearing the new-session form, selected session identity, a follow-up reply, main-window hide/reopen, repeated launch, tray pause/resume, and quitting an attached window while the resident service stays healthy. Keep test logs and identifiers private; publish only synthetic/redacted evidence.
