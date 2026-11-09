// Package adapter turns custom agents into agent.Agent values: YAML manifests in
// ~/.homing/agents, script adapters in ~/.homing/adapters, and the event-push API
// (/api/ingest, /api/ingest/tokens, /api/adapters...). It plugs in from init()
// via agent.RegisterProvider and api.Register; see docs/go-architecture.md.
//
// Owner: DSH.
package adapter
