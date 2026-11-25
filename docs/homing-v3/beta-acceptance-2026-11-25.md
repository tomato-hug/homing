# Beta rollout acceptance — 2026-11-25

This record separates physical-device observations from automated checks. Test conversations and action projects were newly created under a temporary directory. No messages were sent into existing user conversations.

## Deployment

- The resident Mac service on 18787 was switched with explicit approval to `0.11.5-dev+19ca106.sync2` and connected to the configured public WSS relay.
- The binary, startup configuration and consistent data snapshot were backed up before the switch. Startup and encrypted public API checks passed; existing host identity and pairings were retained.
- The service runs independently of the mounted development source. The separate 8787 service was untouched. The old graphical `Homing.app` was not replaced.
- The latest debug APK is also served by the local `/apk` download endpoint. Updating that file does not restart the resident service.

## Automated verification

- Isolated Go build, headless vet / full tests, and cloud / API race tests passed. The shared working tree also passed `go build ./...` without incorporating unrelated edits into the candidate binary.
- Android passed all 267 tests, debug assembly and lint with zero errors (existing warnings remain). Explicit permission failures and both ordinary / conflated reconnect transitions are covered.
- A public encrypted probe delivered all 600 synthetic preview events through bounded pages. A separate production snapshot delivered all 522 events present at probe time and verified refresh endpoints and receipt-bound history.
- Legacy unpaged event responses and uncompressed RPC remain compatible. New clients negotiate compressed bodies; decoding is bounded. A blocked API handler no longer prevents WebSocket control-frame processing.
- Gitleaks found no matches in the pre-release Git history or the scoped implementation/document files. This does not replace a manual privacy and third-party licensing review before making the repository public.

## Android physical observations

Device: OnePlus Ace 2 / PHK110, Android 16. The phone used mobile data with Wi-Fi off and no ADB reverse tunnel. Installation preserved app data and the existing debug signing identity.

| Flow | Observed result |
| --- | --- |
| Public pairing | New host pairing completed with matching verification words; production pairing survived upgrades |
| Conversation grouping | Repeated notifications for the same native Codex session appeared in one conversation |
| Context boundary | Initial display started at the received message; earlier native turns appeared only after explicitly requesting ten rounds |
| Persistence | Loaded earlier turns survived process termination and APK replacement without duplicate item IDs |
| Phone-local cleanup | Inbox clear / undo and conversation hide / undo worked; Mac events and read flags stayed unchanged |
| Links | An HTTPS link opened in the in-app browser and returned to the conversation |
| Reconnect | A native Codex reply generated during a roughly 30-second outage (and a second 22-second outage on the final APK) appeared in the still-open conversation after mobile data returned, without manual refresh or re-pairing |
| Meeting chat | A new Codex / Hermes meeting displayed real native statements; a phone `@hermes` message reached the host and showed delivered state |
| Meeting control | Phone resume, pause, next-speaker selection and ending for minutes were exercised; host state confirmed the phone's pause |
| Minutes and dispatch | Minutes produced one action; the phone selected Hermes and confirmed dispatch into a separate worktree |
| Action result | `demo-result.txt` contained exactly `demo-beta-action-ok` followed by LF: 20 bytes, SHA-256 `1064d44792ccd95ed818f5d5a1bd42a1c78516a40abea7be30b0756197b5f8fd`; the original project stayed clean |
| Dispatch retry | Repeating the same confirmed action returned the existing dispatch identity |
| Result return | The phone showed the completed action and its result |

The Hermes installation needed its declared ACP dependency. The resident environment lacked a configured provider; the live multi-Agent regression used an isolated Hermes profile and an isolated Homing service inheriting an existing credential in memory. No model credentials were added to the production LaunchAgent. The temporary drive and public pairing were removed after acceptance, the preview process was stopped, and the phone was returned to its original production pairing. This test does not claim that an unconfigured production Hermes runtime is ready for use.

## Permission boundary found during testing

An expired native permission request returned `404 not_pending`. The Android client previously converted a failed selected permission option into an ordinary “allow” chat reply. The fix preserves the explicit option semantics: report failure and refresh state instead of sending a replacement chat message. The reproduction failed before the fix; both expired-request and unavailable-service regressions passed afterward. The complete 267-test suite passed with isolated ViewModel test stores and schedulers.

## Release limits

The Android release APK is built and signature-checked separately; it is not installed over the user's debug app. Mac notarization / DMG distribution, broader device coverage and guaranteed background wake-up are not claimed. The repository remains private; preparing Beta assets does not change its visibility or publish a stable release. Raw transcripts, pairing material, screenshots and local backups remain outside version control.
