package relayapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/tomato-hug/homing/internal/api"
)

type memResponse struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newMemResponse() *memResponse {
	return &memResponse{header: make(http.Header), code: http.StatusOK}
}

func (m *memResponse) Header() http.Header         { return m.header }
func (m *memResponse) Write(b []byte) (int, error) { return m.body.Write(b) }
func (m *memResponse) WriteHeader(statusCode int)  { m.code = statusCode }

func invokeLocalGET(mux *http.ServeMux, base *http.Request, pathWithQuery string) (int, json.RawMessage) {
	parsed, err := url.ParseRequestURI(pathWithQuery)
	if err != nil {
		return http.StatusInternalServerError, nil
	}
	sub := base.Clone(base.Context())
	sub.Method = http.MethodGet
	sub.URL = parsed
	sub.RequestURI = pathWithQuery
	sub.Body = http.NoBody
	sub.ContentLength = 0

	rec := newMemResponse()
	mux.ServeHTTP(rec, sub)
	raw := bytes.TrimSpace(rec.body.Bytes())
	if len(raw) == 0 || !json.Valid(raw) {
		return rec.code, nil
	}
	copied := make([]byte, len(raw))
	copy(copied, raw)
	return rec.code, json.RawMessage(copied)
}

// mobileBootstrapHandler aggregates the mobile startup/reconnect sync into a
// single HTTP / encrypted-RPC call, cutting 7-9 round trips down to 1 RTT.
func mobileBootstrapHandler(mux *http.ServeMux) handlerFunc {
	return func(_ *api.Deps, w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		since, _ := strconv.ParseInt(strings.TrimSpace(query.Get("since")), 10, 64)
		if since < 0 {
			since = 0
		}
		sessionLimit, err := strconv.Atoi(strings.TrimSpace(query.Get("session_limit")))
		if err != nil || sessionLimit < 1 {
			sessionLimit = 120
		}
		if sessionLimit > 500 {
			sessionLimit = 500
		}
		includeMeta := query.Get("include_meta") == "1" || strings.EqualFold(query.Get("include_meta"), "true")

		type callResult struct {
			code int
			body json.RawMessage
		}
		var (
			wg           sync.WaitGroup
			eventsRes    callResult
			sessionsRes  callResult
			projectsRes  callResult
			awayRes      callResult
			inboxRes     callResult
			homeRes      callResult
			dispatchRes  callResult
			agentsRes    callResult
			catalogRes   callResult
		)

		run := func(dst *callResult, path string) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, body := invokeLocalGET(mux, r, path)
				*dst = callResult{code: code, body: body}
			}()
		}

		run(&eventsRes, "/api/events?since="+strconv.FormatInt(since, 10)+"&limit=32")
		run(&sessionsRes, "/api/sessions?limit="+strconv.Itoa(sessionLimit))
		run(&projectsRes, "/api/projects")
		run(&awayRes, "/api/away")
		run(&inboxRes, "/api/inbox")
		run(&homeRes, "/api/home")
		run(&dispatchRes, "/api/dispatches")
		if includeMeta {
			run(&agentsRes, "/api/agents")
			run(&catalogRes, "/api/agent-catalog")
		}
		wg.Wait()

		if eventsRes.code != http.StatusOK || eventsRes.body == nil {
			status := eventsRes.code
			if status < 400 {
				status = http.StatusInternalServerError
			}
			api.WriteError(w, status, "bootstrap_events_failed", "同步事件失败")
			return
		}

		out := map[string]any{
			"ok":     true,
			"events": eventsRes.body,
		}
		if sessionsRes.code == http.StatusOK && sessionsRes.body != nil {
			out["sessions"] = sessionsRes.body
			out["sessions_ok"] = true
		} else {
			out["sessions_ok"] = false
		}
		if projectsRes.code == http.StatusOK && projectsRes.body != nil {
			out["projects"] = projectsRes.body
		}
		if awayRes.code == http.StatusOK && awayRes.body != nil {
			out["away"] = awayRes.body
		}
		if inboxRes.code == http.StatusOK && inboxRes.body != nil {
			out["inbox"] = inboxRes.body
		}
		if homeRes.code == http.StatusOK && homeRes.body != nil {
			out["home"] = homeRes.body
		}
		if dispatchRes.code == http.StatusOK && dispatchRes.body != nil {
			out["dispatches"] = dispatchRes.body
		}
		if includeMeta {
			if agentsRes.code == http.StatusOK && agentsRes.body != nil {
				out["agents"] = agentsRes.body
			}
			if catalogRes.code == http.StatusOK && catalogRes.body != nil {
				out["agent_catalog"] = catalogRes.body
			}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}
