# Self-hosting the Homing relay

`cmd/homing-server` is the Go cloud relay for Homing v1. It speaks the same
`/v1/ws` protocol as `homing serve`; clients do not need a different mode for
cloud and LAN.

> **Deploying in Mainland China while managing proxy-equipped workstations?**
> See the dedicated [Mainland China Public Relay & Split-Horizon Proxy Guide](china-relay-and-proxy.md) for 1C1G cloud VM sizing, ICP/TLS setup, `HTTP_PROXY` isolation, `198.18.0.0/15` TUN Fake-IP automatic bypass, and Clash / Mihomo / Surge `DIRECT` rules.

## Requirements

- Go 1.26 or newer when building from source (the release binary is static and
  needs no runtime)
- A persistent directory for SQLite
- TLS termination in front of the service for `wss://`
- Optional Firebase service-account JSON for wake-up pushes

The relay stores only device public keys, pairing relationships, encrypted
envelopes, timestamps and push tokens. Message payloads are encrypted by the
devices before they reach the server.

## Run with Docker

Build from the repository root:

```sh
docker build -f server/Dockerfile -t homing-server .
```

Start it with a named volume:

```sh
docker run -d \
  --name homing-server \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v homing-data:/data \
  homing-server
```

Check health:

```sh
curl http://127.0.0.1:8080/health
# {"ok":true,"service":"homing-server","version":1}
```

## Run without Docker

Build from the repository root:

```sh
go build -o homing-server ./cmd/homing-server
```

Run it directly (`--addr` and `--db` override the environment):

```sh
DB_PATH="$HOME/.homing/homing.sqlite" PORT=8080 ./homing-server
```

The binary is pure Go (`CGO_ENABLED=0`) and can be cross-compiled for Linux:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o homing-server-linux ./cmd/homing-server
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP/WebSocket listen port |
| `HOST` | `0.0.0.0` | Listen address |
| `DB_PATH` | `./data/homing.sqlite` | SQLite file |
| `HOMING_FCM_CREDENTIALS` | empty | Path to a Firebase service-account JSON file |
| `HOMING_TRUSTED_PROXIES` | empty | Comma-separated reverse-proxy IPs/CIDRs; loopback is always trusted |
| `HOMING_AUTH_MODE` | `open` | `open` (zero-registration self-host) or `managed` (official public relay requiring Relay Keys, quotas, `/account` & `/admin`) |
| `HOMING_REGISTRATION_MODE` | `open` | Initial policy in `managed` mode: `open`, `invite` (redeem/invite code required), or `closed` |
| `HOMING_ADMIN_TOKEN` | empty | Enables the `/admin` dashboard & `/v1/admin/*` management APIs (ban/unban, quotas, sponsor code generator) |
| `HOMING_SMTP_HOST` / `PORT` / `USER` / `PASS` / `FROM` | empty | Enables Email 6-digit OTP registration/login on `/account` (`465` implicit TLS or `587` STARTTLS) |
| `HOMING_GITHUB_CLIENT_ID` / `HOMING_GITHUB_CLIENT_SECRET` | empty | Enables GitHub OAuth 1-click registration/login on `/account` (`HOMING_PUBLIC_BASE_URL` optional) |
| `HOMING_FREE_WEEKLY_MINUTES` | `1200` (20 h) | `managed` mode only: weekly remote-usage allowance for free accounts. `0` disables the limit |
| `HOMING_SPONSOR_WEEKLY_MINUTES` | `0` (unlimited) | Same allowance for Sponsor accounts |

### Free-tier limits on a managed relay

| Limit | Free | Sponsor | Notes |
| --- | --- | --- | --- |
| Computers per account | 2 | 8 | |
| Paired phones per account | 4 | 20 | |
| Single encrypted message | 512 KB | 25 MB | Larger files: LAN or self-hosted relay |
| Relayed traffic per day | 64 MB | 2 GB | Resets 00:00 UTC |
| Remote usage per week | 20 h | unlimited | Resets Monday 00:00 China Standard Time (UTC+8) |

"Remote usage" counts each minute in which a paired **phone sends** encrypted
traffic (opening sessions, replying, approving). The desktop's idle
connection, keep-alive pings and pushes to the phone never count, so
notifications keep arriving after the weekly allowance is used up; only
phone-initiated actions are refused with `weekly_time_exceeded` until the
reset. Administrators can raise one account's allowance with
`PATCH /v1/admin/accounts/{id}` and `{"weekly_minutes": N}` (or switch it to
Sponsor). Failed admin logins are limited to 10 per 15 minutes per address,
and verification e-mails to 10 per hour per address.

### Dual-Mode Official Relay (`HOMING_AUTH_MODE=managed`)

- **Self-hosted (`HOMING_AUTH_MODE=open`, default)**: No accounts, no login, zero lock-in.
- **Official Public Relay (`HOMING_AUTH_MODE=managed`)**:
  - Users visit `https://<relay-domain>/account` to sign in via **Email 6-digit OTP** or **GitHub OAuth** and obtain a computer **Relay Key (`hrk_...`)**.
  - Only the **Computer** enters the `hrk_...` key once (in Desktop Console `连接 › 云中转设置` or `HOMING_RELAY_KEY`). **Phones require zero registration** — scanning the computer's 5-minute pairing QR automatically binds the phone under that account's device quota while keeping payloads 100% E2E encrypted (`Ed25519 + NaCl Box`).
  - Operators visit `https://<relay-domain>/admin` (authenticated with `HOMING_ADMIN_TOKEN`) to view live online devices & daily relayed bandwidth, toggle registration mode (`open` / `invite` / `closed`), ban abusive accounts, and batch-generate `HOMING-XXXX-XXXX` **Sponsor Redeem Codes**.

Forwarded client addresses are honored only when the immediate connection is
from loopback or an explicitly trusted proxy. At a public-facing proxy, replace
untrusted `X-Forwarded-For` values with the connecting client's address. This
keeps six-digit pairing-code limits independent for different phones.

With Docker bridge networking, a host-side proxy may arrive at the container
from the bridge gateway rather than loopback. Confirm that source address in
your deployment and pass only that proxy IP with
`-e HOMING_TRUSTED_PROXIES=ACTUAL_PROXY_IP` to `docker run`. Do not trust an
entire public network to make forwarding work.

Each host can keep up to three unused pairing invitations. Creating another
replaces its oldest unused invitation; generate a fresh QR if an older one no
longer works.

The relay uses the FCM HTTP v1 API. `HOMING_FCM_CREDENTIALS` points at the
service-account JSON downloaded from the Firebase console; the relay reads
`project_id` from that file and obtains an OAuth2 token for the
`https://www.googleapis.com/auth/firebase.messaging` scope, refreshing it when
it is close to expiry. Wake messages contain only `data.type=wake` and never
any session content. Tokens that FCM reports as `UNREGISTERED` (or 404) are
deleted automatically.

Without `HOMING_FCM_CREDENTIALS`, offline messages are still queued and delivered
when the phone reconnects; only the wake-up notification is skipped.
The `push.register` message also accepts `oppo`, `xiaomi`, `huawei`, `honor`
and `vivo` tokens, but those adapters are currently placeholders.

## Caddy TLS reverse proxy

Caddy obtains and renews certificates automatically:

```caddyfile
homing.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

Then use this URL in the app and in pairing QR codes:

```text
wss://homing.example.com/v1/ws
```

The reverse proxy must forward WebSocket upgrades. Caddy does this by
default. If you use nginx, set `proxy_http_version 1.1` and forward the
`Upgrade` and `Connection` headers.

### When TLS sometimes works but public connections fail

Check the endpoint from an external network as well as from the server.
A successful server-local health check or one WebSocket `101` response does
not establish reliable public reconnect behavior. Run the opt-in public
relay smoke test described below before marking that transport accepted.

If external TLS connections reset or return EOF, inspect the plain HTTP
response without following redirects. Compare it with the redirect configured
in your proxy. For Tencent-hosted deployments, a redirect to
`dnspod.qcloud.com/static/webblock.html` is an access-block signal. Check the
domain's filing/access-registration status with Tencent; see its
[filing guidance](https://cloud.tencent.com/document/product/243/19631).
Obtaining a DNS-01 certificate does not clear a separate provider access block.

Keep certificate validation enabled. Also verify the renewal method: a manually
completed DNS challenge alone does not establish unattended certificate renewal.

## Pairing

Start the local service first, listening on an address the phone can reach:

```sh
homing serve --addr 0.0.0.0:18787
```

For cloud/self-hosted pairing, first set the relay address in the console
(`连接 › 中转与局域网`) or write it into `<data-dir>/cloud/peers.json` before
restarting the service:

```json
{"version":1,"server":"wss://homing.example.com/v1/ws","peers":{}}
```

The console's `PUT /api/cloud/config` endpoint updates the same file and swaps
the background connector without restarting `homing serve`. It is loopback-only
and requires the console cookie or local owner token.

Then, in another terminal, let the CLI create a single-use token and print the
QR code:

```sh
homing pair --serve http://127.0.0.1:18787
```

`homing pair` asks `homing serve` over a loopback-only control endpoint, prints
the QR code, the 6-digit code and the four verification words, and waits for
you to approve or reject the phone. If that `homing serve` process already has
a cloud relay configured (and its connector is connected), the QR uses the
cloud address automatically; otherwise it uses the LAN address. Pass
`--lan-ip <address>` if the detected LAN IPv4 is not the one the phone should
use.

1. The host connects and calls `pair.start`.
2. The host renders `homing://pair?s=<server>&t=<pair_token>&h=<host_id>&fp=<fingerprint>` as a QR code.
3. The phone scans it, or the user enters the 6-digit code instead.
4. The phone verifies the host fingerprint or compares the four-word SAS
   with the computer, then the host sends `pair.accept`.
5. Either side can revoke the pairing; the relay removes the pairing and
   queued messages in both directions.

Pairing codes and tokens expire after five minutes. A wrong 6-digit code is
limited to five attempts per source IP.

## Operations

- Back up the SQLite file and its directory.
- Keep port `8080` bound to loopback when using a reverse proxy.
- The relay is stateless apart from SQLite; a restart keeps devices,
  pairings, push tokens and queued envelopes.
- Queue limits are 500 envelopes per device and seven days.
- Use `docker logs homing-server` or the process stdout for health and
  connection logs. Never enable request-body logging in the reverse proxy:
  bodies contain encrypted envelopes, but logging them is unnecessary.

## Dedicated Linux systemd service

For a host already serving other applications, `deploy/homing-relay.service`
runs an independent, restricted service on **127.0.0.1:18789**. The database is
kept in the systemd-managed `/var/lib/homing-relay` directory. Do not expose
18789 in the cloud firewall; the HTTPS proxy on TCP 443 forwards `/v1/ws`.

Build and transfer a Linux binary together with `scripts/install-relay.sh` and
`deploy/homing-relay.service`, preserving those two directories. On the server:

```sh
sudo bash scripts/install-relay.sh ./homing-server EXPECTED_SHA256 GIT_REVISION
systemctl status homing-relay --no-pager
curl --noproxy '*' http://127.0.0.1:18789/health
```

The installer serializes installs, verifies a staged copy against the supplied
checksum and publishes each binary atomically. It checks service health and
restores the previous link, unit and activation state if installation fails.
It refuses foreign units, aliases, masks, drop-ins or differing service files. It
does not alter the web proxy, firewall, other applications or existing data.
Inspect those separately before deployment. The installer requires Bash,
systemd, Python 3, curl, `flock` (util-linux) and standard GNU core utilities.

Use `deploy/nginx-relay-location.conf` inside a dedicated, certificate-equipped
Nginx HTTPS server block. For Caddy, point `reverse_proxy` at 127.0.0.1:18789.
Check the proxy configuration before reloading it. Optional push configuration
can be set in `/etc/homing-relay.env`; keep credentials out of source control
and grant the relay service access only to the required credential file.

## Public deployment smoke check

Run the opt-in check from a trusted development machine with normal TLS
certificate validation:

```sh
HOMING_TEST_RELAY_URL=wss://homing.example.com/v1/ws \
  go test -tags headless ./internal/server -run '^TestPublicRelaySmoke$' \
  -count=1 -timeout=120s -v
```

It generates two new random identities and uses synthetic messages to verify
authenticated connections, rejection before pairing, token/code pairing,
encrypted delivery, offline replay/acknowledgement, reconnect and revocation.
The test rejects handshake redirects and checks both sender rejection and
recipient non-delivery. It attempts to revoke its pairing even after a failure;
two device public-key records remain
in the relay database. It does not access a user's device keys or conversations.
The public test is skipped unless its environment variable is explicitly set.
An Android physical-device/mobile-network check is still required separately.
