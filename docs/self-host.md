# Self-hosting the Homing relay

`cmd/homing-server` is the Go cloud relay for Homing v1. It speaks the same
`/v1/ws` protocol as `homing serve`; clients do not need a different mode for
cloud and LAN.

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
