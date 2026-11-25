# Homing Beta installation and release notes

This Beta is for early adopters who can run a computer service and install an Android APK. The native Mac package has a separate signing/notarization status; inspect its build manifest. Check the release's actual asset list and verification record; source-tree files alone do not mean a public release exists.

## Package types

| Package | Purpose |
| --- | --- |
| `Homing-*-macos-*.app.zip` | Native Mac app; see the [Mac guide](macos-native.md) and verify its actual signing / notarization status |
| `homing-*-darwin-arm64.tar.gz` | Apple Silicon computer binary, license and instructions; run `homing serve` for the service and web console |
| `homing-server-*-linux-*.tar.gz` | Self-hosted relay binary; requires persistent storage and HTTPS/WSS termination |
| `Homing-*-release.apk` | APK signed with the maintained release key, intended for new Beta installations or upgrades from the same key |
| `Homing-*-debug.apk` | Development / physical-test APK; use only with a matching debug installation |
| `SHA256SUMS` and `BUILD-INFO.json` | Download integrity and build provenance; hashes alone do not establish publisher identity |

Verify downloads before installation:

```sh
shasum -a 256 -c SHA256SUMS
```

The project has not established a notarized Mac DMG release in this Beta. Do not assume an old `Homing.app` is the same binary as a newly deployed `homing serve` service. The web console can manage the service without opening a separate Mac app.

## Computer service

Build or extract the binary, then run:

```sh
./homing serve --addr 127.0.0.1:18787
```

Open `http://127.0.0.1:18787/console`. Install and authenticate the Agent runtimes you intend to use, and verify their capabilities in the Agent page. The service does not include model subscriptions or credentials. A shell login can have environment variables that a macOS LaunchAgent does not inherit; validate the runtime from the actual service environment.

For remote use, configure your own relay address and pair the phone as described in [self-host.md](self-host.md). A Mac management window is not required, but the computer must remain awake, online and running the service. A relay cannot execute Agent work while that computer is off.

Before upgrading a running service, stop it in an agreed window and back up the binary, startup configuration and complete data directory consistently, including SQLite state. Preserve device identity and pairing files. Start the candidate, verify local health and encrypted public requests, and restore the backup if startup or the required checks fail. Never run two writers against the same data directory.

## Android installation and upgrade

Minimum version: Android 8.0 (API 26). Install the APK, grant notification permission when requested, and pair with the computer. OEM background and battery settings can affect delivery.

For an existing installation, compare signing identities before using `adb install -r`. A release APK cannot upgrade a debug APK signed with a different key. Do not uninstall an existing app merely to make a signature mismatch disappear: uninstalling removes its local state and pairings. Plan a separate migration if changing signing channels.

Local release signing uses `android/scripts/build-release.sh` with a private keystore and passwords supplied through the documented environment variables or the local Keychain integration. Keys and passwords are not distributed with the source. Debug builds from another machine or CI may have a different debug key.

## Acceptance checklist

- New pairing with matching verification words; existing pairings survive an update.
- Mobile data only, with no LAN route or ADB reverse: receive a new synthetic message and reply to a new test session.
- One conversation for repeated notifications; initial context starts at receipt; ten earlier rounds can be fetched and survive a process restart.
- Inbox clear and conversation remove / undo affect only the phone; computer data stays unchanged.
- HTTPS links open in the in-app browser and return to the conversation.
- After a temporary network outage, missed events and the open conversation catch up without re-pairing.
- Meeting controls and action dispatch use new temporary sessions and projects; verify the resulting artifact, not just the API response.

The redacted [2026-11-25 acceptance record](homing-v3/beta-acceptance-2026-11-25.md) documents the current rollout.

Record the exact commit, APK signing channel, server version, device / OS, network route and observed results. Keep credentials, native conversation logs and screenshots private; publish only redacted evidence.

## Known limits

- Background instant delivery is not guaranteed on every Android vendor. FCM requires deployment configuration; other vendor push adapters remain placeholders. Reconnection catch-up and background wake-up are separate acceptance items.
- Agent availability depends on local runtime versions, authentication, model availability and permissions. A successful ACP handshake alone does not prove a useful model reply or tool execution.
- Some long-running tool states still need broader physical-device testing. Broader Android versions, Windows desktop packaging and Apple notarization remain outside this Beta's verified surface; native Mac checks are documented separately.
- Public opening of a repository requires review of its entire history, issue attachments, generated artifacts and maintainer settings. A scan of only the current diff is insufficient. Enable private vulnerability reporting before inviting public security reports.
