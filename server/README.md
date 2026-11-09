# Legacy Node relay

This directory is the historical Node implementation of the Homing relay. It
is kept only as a reference while the Go implementation in
`cmd/homing-server` is the supported server.

## Legacy FCM is retired

The Node implementation still contains the old `FCM_SERVER_KEY` path and posts
to `https://fcm.googleapis.com/fcm/send`. Google shut down that legacy HTTP
API in July 2024, so it no longer works and must not be used for new
deployments.

Use the Go server instead. It uses FCM HTTP v1 with a Google service account
loaded from `HOMING_FCM_CREDENTIALS` and sends only a content-free
`data.type=wake` message. See [`docs/self-host.md`](../docs/self-host.md) for
the current deployment, environment-variable, and Caddy instructions.

No runtime behavior in this directory was changed as part of this migration.
