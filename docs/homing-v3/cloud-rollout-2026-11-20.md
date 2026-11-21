# Cloud relay deployment and Android acceptance — 2026-11-20

## Deployment result

The Homing relay backend is installed on the user's Tencent Lighthouse instance
`lhins-od17m1rd` (Ubuntu 22.04, Linux amd64). Public HTTPS/WSS acceptance is still
pending the dedicated domain certificate. Do not treat backend deployment as
completed mobile-data acceptance.

- Source revision: `3fa2c97e3f71aef16b1d8bf484e2ff37a74c28d6` on `homing-v3`.
- Service: `homing-relay.service`, active and enabled.
- Backend listener: `127.0.0.1:18789`, HTTP/WS, loopback only.
- Intended public endpoint: `wss://homing.littlelove.fun/v1/ws`, TCP 443.
- Release: `/opt/homing-relay/releases/3fa2c97e3f71`; `current` points there.
- Database directory: `/var/lib/homing-relay`, managed by systemd.
- Deployed binary SHA256: `f95577acc158ec0ddc24f761fecc9d77a0f3fa398c411cc06587c2430904f84a`.

The uploaded archive and installed binary checksums matched the locally built
artifact. `/health` returned `ok=true`, `service=homing-server`. A deliberate
upgrade to an invalid test executable failed validation and restored the
previous working release. A subsequent normal restart passed health checks;
the service remained enabled. Nginx stayed active and the existing Bafang
HTTPS health endpoint returned HTTP 200 through the server's loopback proxy.
No existing application service or proxy configuration was changed by this
rollout. Domain/TLS configuration is being handled separately by the user's
other assistant.

## Code and automated verification

The release includes trusted-proxy source selection, a per-host limit of three
unused pairing invitations, a dedicated systemd unit, a verified/atomic install
script, an Nginx location example and an opt-in public encrypted relay smoke
check. The installer rejects foreign units, aliases, masks and drop-ins;
serializes installs; and restores the previous link/unit/activation state on
failure. It verifies the copied bytes before atomically publishing a release.

Fresh checks against an isolated source archive passed:

- `go build ./...`
- `go vet -tags headless ./...`
- `go test -tags headless ./... -count=1 -timeout=300s`
- `go test -race -tags headless ./internal/server/... ./internal/cloud/... ./internal/protocol/... -count=1 -timeout=180s`
- `python3 scripts/tests/test_install_relay.py` (temporary filesystem and fake systemd; actual Linux restart/rollback tested separately above)
- `bash -n scripts/install-relay.sh`
- Gitleaks candidate-directory and staged-diff scans: no leaks.

Regressions first reproduced proxy-client limit collisions, invitation replacement,
multiple forwarded-header trust boundaries, HTTP/TLS redirect following and
installer conflict/interrupted-copy/first-install rollback failures. A deliberately
mutated relay that both rejected and delivered an envelope was caught by the
recipient-side smoke assertion; restoring the candidate passed. Public smoke
connections now reject all handshake redirects and retain normal TLS validation.

CI run: https://github.com/tomato-hug/homing/actions/runs/44856502766
All four CI jobs passed for the deployed source revision: Go backend, protocol vectors, Node relay, and Android unit tests/debug APK.

## Android physical acceptance

The OnePlus Ace 2 (PHK110), Android 16, completed this round through **USB ADB
reverse to an isolated loopback host**. These are physical-device results, not
public mobile-data or Wi-Fi/LAN results.

- Installed debug APK version 17 (`0.11.5-dev`) over version 15, with matching
  debug signing certificate. Previous APK and app data were backed up privately.
- APK SHA256: `c990ea255ad8f6a4f402691f277e4650e46f22fea78aaf7948ace33ded91614a`.
- Four-word SAS matched on phone and host; encrypted pairing completed.
- Opened the meeting list/group chat, selected the next speaker from the phone,
  sent `@Hermes` and verified the delivered state plus a single persisted message.
- Stopped only the isolated host to test disconnect behavior. The phone retained
  its draft and showed an uncertain delivery state. Background/foreground retained
  the pending message. After restoring the host, checking that the message was
  absent and confirming in the UI, resending produced exactly one server message.
- Phone controls successfully resumed, paused and ended the second meeting;
  backend state matched the UI, and minutes were generated.
- Two new real Codex + Hermes meetings ended with minutes. Hermes used its
  installed runtime with temporary ACP 0.9.0 dependencies and a separate config/
  database under `/tmp`; its provider was DeepSeek (`deepseek-flash`, discovered
  from the configured provider). A macOS sandbox denied reading the user home
  and limited writes to the synthetic test tree. Both restrictions were probed.
- From the phone's minutes, dispatched one action to Codex and one to Hermes.
  Both completed in distinct worktrees. Codex created the expected synthetic
  file with its exact single-line content; Hermes tool evidence contained the
  original test README and its result included the expected marker. The source
  test repository stayed unchanged. The phone also opened the completed Hermes
  task conversation and its recorded tool steps from the action card.

The test host registers the real Hermes manifest only in a temporary acceptance
binary. It does not change the user's adapter configuration or add Hermes setup
to the production release. All native test sessions were newly created for the
synthetic `/tmp` workspace; no messages were sent into existing conversations.
Local services on 8787 and 18787 remained running.

## Public-network status

Before testing, the phone's default network was validated cellular with Wi-Fi
off. The actual app checks above used the USB tunnel, so cellular transport is
still unverified. The public smoke test correctly refused the certificate for
`bafang.littlelove.fun` when connecting to `homing.littlelove.fun`; the mismatch
was also reproduced on the server using loopback plus the Homing SNI. Later
public probes still did not pass normal TLS validation. No certificate checks
were bypassed.

## Remaining acceptance

1. Finish the dedicated domain certificate and TLS proxy, then confirm normal
   certificate validation succeeds for `homing.littlelove.fun`.
2. Run `HOMING_TEST_RELAY_URL=wss://homing.littlelove.fun/v1/ws go test -tags headless ./internal/server -run '^TestPublicRelaySmoke$' -count=1 -timeout=120s -v`.
3. With the Android phone connected and unlocked, pair it with the isolated host
   through the public relay and repeat the accepted phone flows over that
   transport using fresh test sessions only.
4. Verify actual cellular operation, reconnect and foreground/background
   behavior. Push wake-up requires separate provider credentials; queue replay
   after reconnect and OS wake delivery are distinct checks.

Local evidence is in `output/homing-cloud-rollout-20261120/`. Its `private/`
subdirectory contains device backup and UI/log material and is excluded from
source control; do not publish it.
