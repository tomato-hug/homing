# Cloud relay deployment and Android acceptance — 2026-11-20

Follow-up: [Cloudflare Tunnel and Android acceptance — 2026-11-21](cloud-tunnel-acceptance-2026-11-21.md)
records the new `homing.douqu.online` endpoint, which passed public protocol
checks. The interception findings below still describe the original direct route.

## Deployment result

The Homing relay backend is installed on the user's Tencent Lighthouse instance
`lhins-od17m1rd` (Ubuntu 22.04, Linux amd64). The dedicated domain certificate
and Nginx proxy are now verified. Public HTTPS/WSS acceptance remains blocked
by a Tencent domain-access interception response; the user confirmed that
filing/access registration is not yet complete. Do not treat backend deployment
or an occasional successful TLS connection as completed mobile-data acceptance.

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

## Public-network status — initial USB acceptance

Before testing, the phone's default network was validated cellular with Wi-Fi
off. The actual app checks above used the USB tunnel, so cellular transport is
still unverified. The public smoke test correctly refused the certificate for
`bafang.littlelove.fun` when connecting to `homing.littlelove.fun`; the mismatch
was also reproduced on the server using loopback plus the Homing SNI. Later
public probes still did not pass normal TLS validation. No certificate checks
were bypassed.

## Follow-up after the domain certificate was installed

The user handed over the completed DNS-01 certificate and proxy configuration.
Fresh checks verified a trusted certificate for `homing.littlelove.fun`, with
expiry `2027-02-19 02:42:41 UTC` (`2027-02-18 20:42:41 America/Chicago`).
The original certificate-name mismatch is resolved.

Read-only Tencent console checks found one Homing Nginx site, the required
WebSocket headers, forwarding to `127.0.0.1:18789`, and active Nginx/relay
services. Eight consecutive loopback HTTPS health requests and eight requests
from the server to the public domain all passed normal certificate validation.
No server, Nginx or firewall configuration was changed in this follow-up.

External access was not stable. The complete public smoke was attempted over
the Mac's normal route, explicit local proxy, a temporary phone-cellular
transport, and a separate existing proxy outbound. Some attempts authenticated,
paired and delivered an encrypted message, but failed during reconnect with
TLS/HTTP handshake EOF; none completed the full workflow. These failures remain
recorded rather than being retried until a pass or weakening TLS validation.

Android was connected and its default network was validated cellular, with
Wi-Fi disabled and no ADB reverse mapping during the public attempt. The phone
received the public pairing verification words, but host confirmation did not
complete reliably. Public phone pairing, meeting flows and action dispatch are
therefore **not passed**. The Mac's Wi-Fi gateway was the same phone, so the
initial Mac and phone routes were not independent network providers.

The decisive external probe pinned the known server IP while retaining the
Homing Host header:

```sh
curl --noproxy '*' \
  --resolve homing.littlelove.fun:80:159.75.228.160 \
  -sS -i --max-time 8 http://homing.littlelove.fun/health
```

It returned this response instead of the configured HTTPS redirect:

```http
HTTP/1.1 302 OK
Connection: Keep-Alive
Location: https://dnspod.qcloud.com/static/webblock.html?d=homing.littlelove.fun
```

The alternate proxy route returned the same interception location. This is
direct evidence of a provider access block, not just the earlier hypothesis
of a Mac proxy problem. The user subsequently confirmed that Tencent filing/
access registration is incomplete or in progress. Tencent's
[filing guidance](https://cloud.tencent.com/document/product/243/19631)
explains that registration follows the hosting provider. A DNS-01 certificate
does not resolve that separate provider-access prerequisite.

The certificate renewal file currently uses `authenticator = manual` and has
no `manual_auth_hook` or `deploy_hook` entry. Unattended renewal is not verified;
the domain owner must arrange a supported renewal workflow. The leftover TXT
challenge record was not changed, and no DNS credentials were requested or
stored by this acceptance work.

Temporary network diagnostic processes, phone executable/forwarding, and the
temporary copy of proxy credentials were removed. The isolated host was
returned to its USB test configuration. System proxy settings and services
on 8787/18787 were preserved. No product source fix was justified by this
network evidence.

## Remaining acceptance

1. Complete Tencent filing/access registration for this deployment and confirm
   the external interception has cleared. If registration is already approved
   but the block persists, use the captured response when contacting Tencent
   support. Any alternative hosting deployment needs its own target and scope.
2. Run `HOMING_TEST_RELAY_URL=wss://homing.littlelove.fun/v1/ws go test -tags headless ./internal/server -run '^TestPublicRelaySmoke$' -count=1 -timeout=120s -v`.
3. With the Android phone connected and unlocked, pair it with the isolated host
   through the public relay and repeat the accepted phone flows over that
   transport using fresh test sessions only.
4. Verify actual cellular operation, reconnect and foreground/background
   behavior. Push wake-up requires separate provider credentials; queue replay
   after reconnect and OS wake delivery are distinct checks.
5. Configure and verify certificate renewal before unattended operation.

Local evidence is in `output/homing-cloud-rollout-20261120/`. Its `private/`
subdirectory contains device backup and UI/log material and is excluded from
source control; do not publish it.
