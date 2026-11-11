package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/store"
)

func TestHomeAttentionAndWatermark(t *testing.T) {
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/home", nil)

	first, err := s.AppendEvent(store.Event{
		Source: "codex", Kind: "permission", SessionID: "demo-home-1",
		Cwd: "/Users/demo/Projects/demo-home", Title: "Codex 请求授权", Text: "Bash：ls",
	})
	if err != nil {
		t.Fatal(err)
	}
	home := d.buildHome(request)
	if len(home.Attention) != 1 || home.Attention[0].Reason != "approval" {
		t.Fatalf("attention = %+v, want one approval", home.Attention)
	}
	if home.Counts.NeedsYou != 1 || home.Attention[0].EventID != first.ID {
		t.Fatalf("home counts/event = %+v / %d", home.Counts, home.Attention[0].EventID)
	}
	if home.Attention[0].Approval == nil || home.Attention[0].Approval.Command == "" {
		t.Fatal("approval command is empty")
	}

	// Opening the session advances the watermark; the card leaves the home page.
	if _, err := s.PatchSession("codex", "demo-home-1", map[string]bool{"read": true}); err != nil {
		t.Fatal(err)
	}
	home = d.buildHome(request)
	if len(home.Attention) != 0 {
		t.Fatalf("attention after read = %+v, want empty", home.Attention)
	}

	// A new waiting event on the same session must surface it again (G4).
	if _, err := s.AppendEvent(store.Event{
		Source: "codex", Kind: "needs_input", SessionID: "demo-home-1",
		Cwd: "/Users/demo/Projects/demo-home", Title: "Codex 等待输入", Text: "选 A 还是 B？",
	}); err != nil {
		t.Fatal(err)
	}
	home = d.buildHome(request)
	if len(home.Attention) != 1 || home.Attention[0].Reason != "needs_reply" {
		t.Fatalf("attention after new event = %+v, want needs_reply", home.Attention)
	}
}

func TestHomeAwayOnlyShowsNewActivityAndKeepsFinishedInResume(t *testing.T) {
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/home", nil)

	if _, err := s.AppendEvent(store.Event{
		Source: "claude", Kind: "done", SessionID: "demo-home-done",
		Cwd: "/Users/demo/Projects/demo-home", Title: "完成", Text: "测试通过",
	}); err != nil {
		t.Fatal(err)
	}
	home := d.buildHome(request)
	if len(home.Attention) != 0 {
		t.Fatalf("not away should not put done in attention: %+v", home.Attention)
	}
	if len(home.Resume) != 1 || home.Resume[0].SessionID != "demo-home-done" {
		t.Fatalf("resume = %+v, want the finished session", home.Resume)
	}

	if _, err := s.StartAway("manual", "demo-phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(store.Event{
		Source: "claude", Kind: "done", SessionID: "demo-home-away",
		Cwd: "/Users/demo/Projects/demo-home", Title: "离开期间完成", Text: "跑完了",
	}); err != nil {
		t.Fatal(err)
	}
	home = d.buildHome(request)
	if len(home.Attention) != 1 || !home.Attention[0].SinceAway || home.Attention[0].Reason != "done" {
		t.Fatalf("away attention = %+v", home.Attention)
	}
	if home.Counts.AwayNew != 1 {
		t.Fatalf("away_new = %d, want 1", home.Counts.AwayNew)
	}
}

func TestHomeClearedAwayPeriodDoesNotCaptureLaterDoneEvents(t *testing.T) {
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	period, err := s.StartAway("manual", "demo-phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(store.Event{
		Source: "claude", Kind: "done", SessionID: "demo-home-away-cleared",
		Cwd: "/Users/demo/Projects/demo-home", Title: "离开期间完成", Text: "已查看",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EndAway(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ClearAwayPeriod(period.ID); err != nil || !ok {
		t.Fatalf("clear away period: ok=%v err=%v", ok, err)
	}
	if _, err := s.AppendEvent(store.Event{
		Source: "codex", Kind: "done", SessionID: "demo-home-after-clear",
		Cwd: "/Users/demo/Projects/demo-home", Title: "普通完成", Text: "不需要关注",
	}); err != nil {
		t.Fatal(err)
	}

	home := d.buildHome(httptest.NewRequest(http.MethodGet, "/api/home", nil))
	if home.Away.Period != nil {
		t.Fatalf("cleared away period should not be exposed by /api/home: %+v", home.Away.Period)
	}
	if len(home.Attention) != 0 {
		t.Fatalf("cleared away period leaked into attention: %+v", home.Attention)
	}
	found := false
	for _, summary := range home.Resume {
		if summary.SessionID == "demo-home-after-clear" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ordinary done event did not fall back to resume: %+v", home.Resume)
	}
}

func TestHomeSettingsSwitchToAllUnread(t *testing.T) {
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(store.Event{
		Source: "codex", Kind: "done", SessionID: "demo-home-unread",
		Cwd: "/Users/demo/Projects/demo-home", Title: "完成", Text: "完成",
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/home", nil)
	if home := d.buildHome(request); len(home.Attention) != 0 {
		t.Fatalf("away scope should hide the event: %+v", home.Attention)
	}
	if err := s.SetHomeNewActivity("unread"); err != nil {
		t.Fatal(err)
	}
	home := d.buildHome(request)
	if len(home.Attention) != 1 || home.Attention[0].Reason != "done" {
		t.Fatalf("unread scope attention = %+v", home.Attention)
	}
}

func TestHomeSettingsEnableCollabInject(t *testing.T) {
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/home/settings",
		strings.NewReader(`{"collab_return":"inject"}`),
	)
	recorder := httptest.NewRecorder()
	d.handleHomeSettings(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		OK       bool         `json:"ok"`
		Settings HomeSettings `json:"settings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Settings.CollabReturn != "inject" {
		t.Fatalf("payload = %+v", payload)
	}
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.CollabReturn(); got != "inject" {
		t.Fatalf("persisted collab return = %q", got)
	}

	request = httptest.NewRequest(
		http.MethodPatch,
		"/api/home/settings",
		strings.NewReader(`{"collab_return":"always"}`),
	)
	recorder = httptest.NewRecorder()
	d.handleHomeSettings(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHomeResumePrefersLastUserActivity(t *testing.T) {
	olderOwner := "2026-11-09T10:00:00.000Z"
	newerAgent := "2026-11-10T08:00:00.000Z"
	lastUser := "2026-11-10T09:00:00.000Z"
	resume := []Summary{
		{SessionID: "agent-ran-later", LastTS: &newerAgent},
		{SessionID: "user-replied-later", LastTS: &olderOwner, LastUserTS: &lastUser},
	}
	if resumeActivityTS(resume[1]) <= resumeActivityTS(resume[0]) {
		t.Fatalf("last_user_ts did not outrank a newer agent event: %+v", resume)
	}
	withoutUserActivity := Summary{SessionID: "fallback", LastTS: &newerAgent}
	if got := resumeActivityTS(withoutUserActivity); got != newerAgent {
		t.Fatalf("fallback = %q, want last_ts %q", got, newerAgent)
	}
}
