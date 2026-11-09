// Package api serves the local HTTP interface of homing: /api/* (field-for-field
// compatible with the Node relay), the SSE stream, /v1/ws for LAN-direct phones,
// and the console pages under /console.
//
// Other packages add their routes from init() with Register, so nobody needs to
// edit this package to plug in (internal/adapter adds /api/ingest and
// /api/adapters, internal/cloud adds /v1/ws, ...).
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"

	"github.com/tomato-hug/homing/internal/store"
)

// Deps is what route handlers get. Grow it as packages need more; keep it a
// plain struct so tests can build one by hand.
type Deps struct {
	Log     *slog.Logger
	DataDir string
	Version string

	// The store is opened on first use: DataDir is enough to build a Deps, and
	// a test that mounts one route group never touches the rest of the files.
	storeOnce  sync.Once
	storeValue *store.Store
	storeErr   error
}

// RouteFunc mounts routes on mux. Use Go 1.22 patterns, e.g. "GET /api/agents".
type RouteFunc func(mux *http.ServeMux, d *Deps)

type route struct {
	name  string
	order int
	fn    RouteFunc
}

var (
	mu     sync.Mutex
	routes []route
)

// Register adds a group of routes. Lower order mounts first; ties keep
// registration order.
func Register(name string, order int, fn RouteFunc) {
	mu.Lock()
	defer mu.Unlock()
	routes = append(routes, route{name, order, fn})
}

// NewMux builds the full handler.
func NewMux(d *Deps) *http.ServeMux {
	mu.Lock()
	rs := append([]route(nil), routes...)
	mu.Unlock()
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].order < rs[j].order })

	mux := http.NewServeMux()
	for _, r := range rs {
		r.fn(mux, d)
	}
	return mux
}

// WriteJSON writes v with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func init() {
	Register("health", 0, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "impl": "go", "version": d.Version})
		})
	})
}
