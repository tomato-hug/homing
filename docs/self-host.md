# Self-hosting the Homing relay

`server/` is the cloud relay for Homing v1. It speaks the same `/v1/ws`
protocol as the local `homingd`; clients do not need a different mode for
cloud and LAN.

## Requirements

- Node.js 22 or newer
- A persistent directory for SQLite
- TLS termination in front of the service for `wss://`
- Optional FCM credentials if you want wake-up pushes

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

```sh
cd server
npm ci --omit=dev
DB_PATH="$HOME/.homing/homing.sqlite" PORT=8080 node index.mjs
```

Configuration:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP/WebSocket listen port |
| `HOST` | `0.0.0.0` | Listen address |
| `DB_PATH` | `server/data/homing.sqlite` | SQLite file |
| `FCM_SERVER_KEY` | empty | Optional legacy FCM server key for wake pushes |

Without `FCM_SERVER_KEY`, offline messages are still queued and delivered
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
