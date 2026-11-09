# Self-hosting the Homing relay

`cmd/homing-server` is the Go cloud relay for Homing v1. It speaks the same
`/v1/ws` protocol as `homing serve`; clients do not need a different mode for
cloud and LAN.

## Requirements

- Go 1.24 or newer when building from source (the release binary is static and
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

## Pairing

Start the local service first, listening on an address the phone can reach:

```sh
homing serve --addr 0.0.0.0:18788
```

Then, in another terminal, let the CLI create a single-use token and print the
QR code:

```sh
homing pair --serve http://127.0.0.1:18788
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
