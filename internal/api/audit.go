package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

var auditAllowedKeys = map[string]bool{
	"session_id":        true,
	"dispatch_id":       true,
	"resource_id":       true,
	"parent_session_id": true,
	"target":            true,
	"preset":            true,
	"cwd":               true,
	"event_id":          true,
	"decision":          true,
	"stopped":           true,
	"kind":              true,
	"branch":            true,
	"room_id":           true,
	"participants":      true,
}

// Audit appends one security-relevant action to audit.jsonl. Callers pass only
// identifiers and action names; request bodies and agent output never enter
// this file.
func (d *Deps) Audit(r *http.Request, action string, fields map[string]any) {
	d.recordAuditFlow(r, action, fields)
	entry := map[string]any{
		"at":     time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"action": action,
		"device": d.auditDevice(r),
	}
	if r != nil {
		entry["ip"] = r.RemoteAddr
	}
	for key, value := range fields {
		if !auditAllowedKeys[key] {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				entry[key] = typed
			}
		case int, int64, bool:
			entry[key] = typed
		}
	}
	paths := platform.NewPaths(d.DataDir)
	file, err := os.OpenFile(paths.Audit, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = file.Write(append(raw, '\n'))
}

func (d *Deps) auditDevice(r *http.Request) string {
	if r == nil {
		return "local"
	}
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		supplied := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if s, ok := d.store(); ok && strings.HasPrefix(supplied, "hm_") {
			if device := deviceForToken(s, supplied); device != nil {
				return device.ID
			}
		}
	}
	return "local"
}
