package api

import (
	"context"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

// This file exposes the two pieces other packages need from api without
// widening the route handlers: the shared store, and the session_meta object
// that Node attaches to every event. Keeping them here (instead of exporting
// metaFor wholesale) lets the internal sessionMeta struct stay unexported.

// Store is the process-wide store for this Deps, opened on first use. Unlike
// the internal store() helper it reports why opening failed.
func (d *Deps) Store() (*store.Store, error) {
	d.storeOnce.Do(func() {
		d.storeValue, d.storeErr = store.Open(platform.NewPaths(d.DataDir))
	})
	if d.storeErr != nil {
		return nil, d.storeErr
	}
	return d.storeValue, nil
}

// Close releases the store and the optional full-text index. The service
// command calls it after the HTTP server has drained, so SQLite checkpoints its
// WAL instead of relying on process teardown.
func (d *Deps) Close() error {
	d.searchMu.Lock()
	if d.searchValue != nil {
		_ = d.searchValue.Close()
		d.searchValue = nil
	}
	d.searchMu.Unlock()
	if d.storeValue != nil {
		return d.storeValue.Close()
	}
	return nil
}

// SessionMetaFor builds the session_meta object Node stores on each event:
// title, workspace, project, branch, model, effort, permission mode, context
// ring and the agent's native state. Absent values are nil, which marshals to
// null exactly like the Node relay.
func (d *Deps) SessionMetaFor(source, sessionID, cwdHint string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.SessionMetaMap(ctx, source, sessionID, cwdHint)
}

// Summaries lists sessions exactly like GET /api/sessions: event-log sessions
// first, then whatever the agents found on disk, pinned first then by activity.
// source and status are optional filters ("" means no filter). It exists so the
// CLI does not have to make an HTTP round trip to its own server.
func (d *Deps) Summaries(ctx context.Context, limit int, source, status string) []Summary {
	if limit <= 0 {
		limit = 100
	}
	return d.summaries(ctx, limit, source, status)
}

// SessionMetaMap is SessionMetaFor with a caller-supplied context, so handlers
// can reuse the request context instead of a fresh timeout.
func (d *Deps) SessionMetaMap(ctx context.Context, source, sessionID, cwdHint string) map[string]any {
	m := d.metaFor(ctx, source, sessionID, cwdHint)
	out := map[string]any{
		"title":           m.Title,
		"title_source":    m.TitleSource,
		"workspace":       nil,
		"project":         nil,
		"branch":          nil,
		"model":           nil,
		"effort":          nil,
		"permission_mode": nil,
		"context_usage":   nil,
		"native_state":    nil,
	}
	if m.Workspace != "" {
		out["workspace"] = m.Workspace
	}
	if m.Project != "" {
		out["project"] = m.Project
	}
	if m.Branch != "" {
		out["branch"] = m.Branch
	}
	if m.Model != "" {
		out["model"] = m.Model
	}
	if m.Effort != "" {
		out["effort"] = m.Effort
	}
	if m.PermissionMode != "" {
		out["permission_mode"] = m.PermissionMode
	}
	if m.ContextUsage != nil {
		out["context_usage"] = m.ContextUsage
	}
	if m.NativeState != "" {
		out["native_state"] = m.NativeState
	}
	return out
}
