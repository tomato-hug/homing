// Package cloud connects homing to a Homing relay server (cloud or self-hosted)
// and serves LAN-direct phones on /v1/ws: it unwraps rpc payloads into local /api
// calls and pushes events back, all end-to-end encrypted with internal/protocol.
//
// Owner: Codex.
package cloud
