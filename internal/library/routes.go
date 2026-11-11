package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/platform"
)

func init() {
	api.Register("library", 46, func(mux *http.ServeMux, d *api.Deps) {
		mux.HandleFunc("GET /api/library", libraryRead(d, handleLibrary))
		mux.HandleFunc("GET /api/library/sources", libraryRead(d, handleSources))
		mux.HandleFunc("GET /api/library/audit", libraryRead(d, handleAudit))
		mux.HandleFunc("GET /api/library/resources/{id}", libraryRead(d, handleResource))
		mux.HandleFunc("POST /api/library/inspect", libraryRead(d, handleInspect))
		mux.HandleFunc("POST /api/library/plan", libraryRead(d, handlePlan))
		mux.HandleFunc("POST /api/library/install", libraryWrite(d, handleInstall))
		mux.HandleFunc("DELETE /api/library/resources/{id}", libraryWrite(d, handleRemove))
		mux.HandleFunc("GET /api/library/sources/available", libraryRead(d, handleSources))
		mux.HandleFunc("POST /api/library/remote/plan", libraryRead(d, handleRemotePlan))
		mux.HandleFunc("POST /api/library/remote/install", libraryWrite(d, handleRemoteInstall))
		mux.HandleFunc("GET /api/library/installations", libraryRead(d, handleInstallations))
		mux.HandleFunc("POST /api/library/installations/{id}/update/plan", libraryRead(d, handleUpdatePlan))
		mux.HandleFunc("POST /api/library/installations/{id}/update", libraryWrite(d, handleUpdateInstall))
		mux.HandleFunc("POST /api/library/installations/{id}/rollback", libraryWrite(d, handleRollback))
		mux.HandleFunc("GET /api/library/packs", libraryRead(d, handlePacks))
		mux.HandleFunc("POST /api/library/packs/plan", libraryRead(d, handlePackPlan))
		mux.HandleFunc("POST /api/library/packs/install", libraryWrite(d, handlePackInstall))
		mux.HandleFunc("POST /api/library/packs/{id}/rollback", libraryWrite(d, handlePackRollback))
		mux.HandleFunc("POST /api/library/publish/check", libraryRead(d, handlePublishCheck))
	})
}

type libraryHandler func(*api.Deps, http.ResponseWriter, *http.Request)

func libraryRead(d *api.Deps, fn libraryHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.Authorized(r) {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
			return
		}
		if !d.HasScope(r, "read") {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{
				"error": "该设备没有 read 权限", "code": "scope_denied",
			})
			return
		}
		fn(d, w, r)
	}
}

func libraryWrite(d *api.Deps, fn libraryHandler) http.HandlerFunc {
	return libraryRead(d, func(d *api.Deps, w http.ResponseWriter, r *http.Request) {
		if !d.RequireScope(w, r, "drive") {
			return
		}
		if !d.CheckRate(w, r, "library_write", 20, time.Minute) {
			return
		}
		fn(d, w, r)
	})
}

func homeDir() (string, error) {
	home := platform.Home()
	if strings.TrimSpace(home) == "" {
		return "", errors.New("无法确定用户主目录")
	}
	return home, nil
}

func handleLibrary(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	snapshot := Scan(home)
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	kind := normalizeKind(r.URL.Query().Get("kind"))
	agent := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("agent")))
	if query != "" || kind != "" || agent != "" {
		filtered := make([]Resource, 0, len(snapshot.Resources))
		for _, item := range snapshot.Resources {
			if kind != "" && item.Kind != kind {
				continue
			}
			if query != "" {
				haystack := strings.ToLower(strings.Join([]string{item.Name, item.Description, item.Source}, " "))
				if !strings.Contains(haystack, query) {
					continue
				}
			}
			if agent != "" {
				found := false
				for _, target := range item.Targets {
					if target.Agent == agent {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		snapshot.Resources = filtered
	}
	api.WriteJSON(w, http.StatusOK, snapshot)
}

func handleSources(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	snapshot := Scan(home)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"sources": snapshot.Sources, "count": len(snapshot.Sources),
		"providers": RemoteProviders(), "generated_at": snapshot.GeneratedAt,
	})
}

func handleResource(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	detail, err := Detail(home, id)
	if errors.Is(err, ErrNotFound) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "资源不存在"})
		return
	}
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, detail)
}

func handleInspect(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	inspection, err := Inspect(textField(body, "path"), textField(body, "kind"))
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "inspection": inspection})
}

func handlePlan(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	plan, err := BuildImportPlan(home, textField(body, "path"), textField(body, "kind"), textField(body, "name"))
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, plan)
}

func handleInstall(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	dryRun, _ := body["dry_run"].(bool)
	if dryRun {
		handlePlan(d, w, r)
		return
	}
	plan, err := Install(home, textField(body, "path"), textField(body, "kind"), textField(body, "name"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrUnsafePath) || errors.Is(err, ErrDestination) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "plan": plan})
		return
	}
	installedID := resourceID(plan.Kind, plan.Name)
	d.Audit(r, "library_import", map[string]any{
		"resource_id": installedID,
		"kind":        plan.Kind,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "plan": plan,
		"resource_id": installedID,
	})
}

func handleRemove(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	result, err := Remove(home, id, strings.TrimSpace(r.URL.Query().Get("target")))
	if errors.Is(err, ErrNotFound) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "资源不存在"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrUnsafePath) {
			status = http.StatusForbidden
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	d.Audit(r, "library_remove", map[string]any{"resource_id": id})
	api.WriteJSON(w, http.StatusOK, result)
}

func handleRemotePlan(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !d.CheckRate(w, r, "library_remote_plan", 20, time.Minute) {
		return
	}
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	ctx := r.Context()
	plan, err := PlanRemote(ctx, home, RemoteRequest{
		URL: textField(body, "url"), Kind: textField(body, "kind"),
		Name: textField(body, "name"), Ref: textField(body, "ref"),
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrDestination) || errors.Is(err, ErrUnsafePath) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, plan)
}

func handleRemoteInstall(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	plan, installation, err := InstallRemote(home, textField(body, "fetch_id"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrDestination) || errors.Is(err, ErrUnsafePath) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "plan": plan})
		return
	}
	d.Audit(r, "library_remote_install", map[string]any{
		"installation_id": installation.ID,
		"resource_id":     installation.ResourceID,
		"kind":            installation.Kind,
		"checksum":        installation.DownloadSHA256,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "plan": plan, "installation": installation,
	})
}

func handleInstallations(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items, err := Installations(home)
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"installations": items, "count": len(items)})
}

func handleRollback(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	installation, trash, err := RollbackInstallation(home, id)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	d.Audit(r, "library_rollback", map[string]any{
		"installation_id": installation.ID,
		"resource_id":     installation.ResourceID,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "installation": installation, "trash_path": trash,
	})
}

func handlePacks(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items, err := PackInstallations(home)
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"packs": items, "count": len(items)})
}

func handlePackPlan(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !d.CheckRate(w, r, "library_pack_plan", 20, time.Minute) {
		return
	}
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	plan, err := PlanPack(home, textField(body, "path"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrUnsafePath) || errors.Is(err, ErrPackConflict) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, plan)
}

func handlePackInstall(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	installation, err := InstallPack(home, textField(body, "plan_id"))
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ErrPackPlanMissing):
			status = http.StatusGone
		case errors.Is(err, ErrPackPlanStale), errors.Is(err, ErrPackConflict),
			errors.Is(err, ErrUnsafePath):
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	d.Audit(r, "library_pack_install", map[string]any{
		"installation_id": installation.ID,
		"pack_id":         installation.PackID,
		"version":         installation.Version,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "installation": installation,
	})
}

func handlePackRollback(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	installation, trash, missing, err := RollbackPack(home, id)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	d.Audit(r, "library_pack_rollback", map[string]any{
		"installation_id": installation.ID,
		"pack_id":         installation.PackID,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "installation": installation,
		"trash_path": trash, "missing": missing,
	})
}

func handleUpdatePlan(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !d.CheckRate(w, r, "library_update_plan", 20, time.Minute) {
		return
	}
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	plan, err := PlanRemoteUpdate(r.Context(), home, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrUpdateMismatch) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, plan)
}

func handleUpdateInstall(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	home, err := homeDir()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	body := readBody(r)
	id := strings.TrimSpace(r.PathValue("id"))
	plan, installation, err := InstallRemoteUpdate(home, id, textField(body, "fetch_id"))
	if errors.Is(err, ErrNoRemoteUpdate) {
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "error": err.Error(), "code": "no_update", "plan": plan,
		})
		return
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrDestination) || errors.Is(err, ErrUnsafePath) ||
			errors.Is(err, ErrUpdateMismatch) {
			status = http.StatusConflict
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "plan": plan})
		return
	}
	d.Audit(r, "library_update", map[string]any{
		"installation_id": installation.ID,
		"resource_id":     installation.ResourceID,
		"kind":            installation.Kind,
		"version":         installation.Version,
		"checksum":        installation.DownloadSHA256,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "plan": plan, "installation": installation,
	})
}

func handlePublishCheck(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	check, err := CheckPublish(textField(body, "path"), textField(body, "kind"))
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, check)
}

func handleAudit(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	paths := platform.NewPaths(d.DataDir)
	file, err := os.Open(paths.Audit)
	if errors.Is(err, os.ErrNotExist) {
		api.WriteJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "count": 0})
		return
	}
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer file.Close()
	var all []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		action, _ := entry["action"].(string)
		if strings.HasPrefix(action, "library_") {
			all = append(all, entry)
		}
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, _ := all[i]["at"].(string)
		b, _ := all[j]["at"].(string)
		return a > b
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"entries": all, "count": len(all)})
}

func readBody(r *http.Request) map[string]any {
	out := map[string]any{}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&out)
	return out
}

func textField(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return value
}
