package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

type fakeLauncher struct {
	targets map[string]bool
	result  StartResult
	calls   []string
	starts  []StartOptions
}

func (f *fakeLauncher) CanDispatch(target string) bool {
	return f.targets[target]
}

func (f *fakeLauncher) Start(_ context.Context, options StartOptions) StartResult {
	f.calls = append(f.calls, options.Target+"|"+options.Cwd+"|"+options.Text)
	f.starts = append(f.starts, options)
	return f.result
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(platform.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDispatchStartsChildAndPersistsRelationship(t *testing.T) {
	s := testStore(t)
	cwd := t.TempDir()
	launcher := &fakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  StartResult{SessionID: "demo-claude-child", Status: "started"},
	}
	service := Service{
		Store:    s,
		Launcher: launcher,
		NewID:    func() string { return "dsp_demo_1" },
	}
	eventID := int64(7)
	dispatch, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-codex-parent",
		ParentSource:    "codex",
		ParentEventID:   &eventID,
		TargetAgent:     "Claude",
		Cwd:             cwd,
		Text:            "检查登录流程",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Status != "started" || dispatch.TargetSessionID != "demo-claude-child" {
		t.Fatalf("dispatch = %+v", dispatch)
	}
	if dispatch.ParentSessionID != "demo-codex-parent" || dispatch.ParentSource != "codex" {
		t.Fatalf("parent relationship = %+v", dispatch)
	}
	if len(launcher.calls) != 1 || launcher.calls[0] != "claude|"+cwd+"|检查登录流程" {
		t.Fatalf("launcher calls = %v", launcher.calls)
	}
	if launcher.starts[0].Preset != "do" || launcher.starts[0].Workspace != "auto" {
		t.Fatalf("launcher options = %+v", launcher.starts[0])
	}

	reopened, err := store.Open(platform.NewPaths(s.Paths.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	persisted := reopened.Dispatches()
	if len(persisted) != 1 || persisted[0].ID != "dsp_demo_1" ||
		persisted[0].TargetSessionID != "demo-claude-child" {
		t.Fatalf("persisted = %+v", persisted)
	}
}

func TestDispatchPassesPresetAndWorkspaceToLauncher(t *testing.T) {
	s := testStore(t)
	launcher := &fakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  StartResult{SessionID: "demo-claude-child", Status: "started"},
	}
	service := Service{Store: s, Launcher: launcher}
	_, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		Cwd:             t.TempDir(),
		Text:            "检查登录流程",
		Preset:          "review",
		Workspace:       "readonly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v", launcher.starts)
	}
	if launcher.starts[0].Preset != "review" || launcher.starts[0].Workspace != "readonly" {
		t.Fatalf("start options = %+v", launcher.starts[0])
	}
}

func TestDispatchHandoffAlwaysReturnsNone(t *testing.T) {
	s := testStore(t)
	launcher := &fakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  StartResult{SessionID: "demo-claude-child", Status: "started"},
	}
	service := Service{Store: s, Launcher: launcher}
	dispatch, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		Cwd:             t.TempDir(),
		Text:            "接管登录修复",
		Preset:          "handoff",
		Return:          "inject",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Return != "none" {
		t.Fatalf("handoff return = %q, want none", dispatch.Return)
	}
	if dispatch.Message == "" {
		t.Fatal("handoff should record that ownership moved")
	}
}

func TestDispatchWaitsForLateSessionAndBinds(t *testing.T) {
	s := testStore(t)
	cwd := t.TempDir()
	launcher := &fakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  StartResult{RequestID: "demo-request-1", Status: "started"},
	}
	service := Service{
		Store:    s,
		Launcher: launcher,
		NewID:    func() string { return "dsp_demo_2" },
	}
	dispatch, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-codex-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		Cwd:             cwd,
		Text:            "检查登录流程",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Status != "waiting_session" || dispatch.RequestID != "demo-request-1" {
		t.Fatalf("dispatch = %+v", dispatch)
	}
	bound, ok := s.BindDispatchTarget("demo-request-1", "claude", "demo-claude-late")
	if !ok {
		t.Fatal("late session did not bind")
	}
	if bound.Status != "started" || bound.TargetSessionID != "demo-claude-late" {
		t.Fatalf("bound = %+v", bound)
	}
}

func TestDispatchRejectsInvalidRequestsWithoutLaunching(t *testing.T) {
	cwd := t.TempDir()
	missingCwd := filepath.Join(cwd, "missing")
	tests := []struct {
		name   string
		req    Request
		code   string
		status int
	}{
		{
			name: "unsupported agent",
			req: Request{
				ParentSessionID: "demo-parent", TargetAgent: "kimi", Cwd: cwd, Text: "demo",
			},
			code: "agent_not_dispatchable", status: 409,
		},
		{
			name: "missing text",
			req: Request{
				ParentSessionID: "demo-parent", TargetAgent: "claude", Cwd: cwd,
			},
			code: "text_required", status: 400,
		},
		{
			name: "missing parent",
			req: Request{
				TargetAgent: "claude", Cwd: cwd, Text: "demo",
			},
			code: "parent_required", status: 400,
		},
		{
			name: "missing cwd",
			req: Request{
				ParentSessionID: "demo-parent", TargetAgent: "claude", Cwd: missingCwd, Text: "demo",
			},
			code: "cwd_invalid", status: 400,
		},
		{
			name: "unsupported mode",
			req: Request{
				ParentSessionID: "demo-parent", TargetAgent: "claude", Cwd: cwd, Text: "demo", Mode: "serial",
			},
			code: "mode_unsupported", status: 400,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			launcher := &fakeLauncher{targets: map[string]bool{"claude": true}}
			service := Service{Store: s, Launcher: launcher}
			_, err := service.Dispatch(context.Background(), tc.req)
			var typed *Error
			if !errors.As(err, &typed) {
				t.Fatalf("error = %v, want orchestrator error", err)
			}
			if typed.Code != tc.code || typed.Status != tc.status {
				t.Fatalf("error = %+v, want %s/%d", typed, tc.code, tc.status)
			}
			if len(launcher.calls) != 0 {
				t.Fatalf("launcher was called for invalid request: %v", launcher.calls)
			}
		})
	}
}

func TestDispatchRecordsLaunchFailure(t *testing.T) {
	s := testStore(t)
	service := Service{
		Store: s,
		Launcher: &fakeLauncher{
			targets: map[string]bool{"claude": true},
			result:  StartResult{Status: "failed", Message: "本机没有 claude 命令"},
		},
		NewID: func() string { return "dsp_demo_failed" },
	}
	_, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		Cwd:             t.TempDir(),
		Text:            "demo",
	})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "launch_failed" {
		t.Fatalf("error = %+v", err)
	}
	records := s.Dispatches()
	if len(records) != 1 || records[0].Status != "failed" {
		t.Fatalf("records = %+v", records)
	}
}

func TestDispatchEnforcesConcurrencyLimits(t *testing.T) {
	cwd := t.TempDir()
	t.Run("parent has five active children", func(t *testing.T) {
		s := testStore(t)
		for i := 0; i < 5; i++ {
			_, err := s.SaveDispatch(store.Dispatch{
				ID: "dsp_existing_" + string(rune('a'+i)), ParentSessionID: "demo-parent",
				TargetAgent: "claude", TargetSessionID: "demo-child-" + string(rune('a'+i)),
				Cwd: cwd, Text: "demo", Workspace: "worktree", Preset: "do", Status: "running",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		service := Service{
			Store: s, Launcher: &fakeLauncher{targets: map[string]bool{"claude": true}},
			NewID: func() string { return "dsp_limit" },
		}
		_, err := service.Dispatch(context.Background(), Request{
			ParentSessionID: "demo-parent", TargetAgent: "claude", Cwd: cwd,
			Text: "demo", Preset: "do", Workspace: "worktree",
		})
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "parent_dispatch_limit" || typed.Status != 429 {
			t.Fatalf("error = %+v", err)
		}
	})

	t.Run("two writable children share cwd", func(t *testing.T) {
		s := testStore(t)
		for i := 0; i < 2; i++ {
			_, err := s.SaveDispatch(store.Dispatch{
				ID: "dsp_shared_" + string(rune('a'+i)), ParentSessionID: "demo-parent",
				TargetAgent: "claude", TargetSessionID: "demo-shared-" + string(rune('a'+i)),
				Cwd: cwd, Text: "demo", Workspace: "shared", Preset: "do", Status: "running",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		service := Service{
			Store: s, Launcher: &fakeLauncher{targets: map[string]bool{"claude": true}},
			NewID: func() string { return "dsp_limit" },
		}
		_, err := service.Dispatch(context.Background(), Request{
			ParentSessionID: "demo-parent", TargetAgent: "claude", Cwd: cwd,
			Text: "demo", Preset: "do", Workspace: "shared",
		})
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "cwd_dispatch_limit" || typed.Status != 429 {
			t.Fatalf("error = %+v", err)
		}
	})
}

func TestDispatchRejectsChildAsParentAndKeepsBranch(t *testing.T) {
	s := testStore(t)
	cwd := t.TempDir()
	if _, err := s.SaveDispatch(store.Dispatch{
		ID: "dsp_parent", ParentSessionID: "demo-root", TargetAgent: "claude",
		TargetSessionID: "demo-child", Cwd: cwd, Text: "demo", Status: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	service := Service{
		Store: s, Launcher: &fakeLauncher{targets: map[string]bool{"claude": true}},
		NewID: func() string { return "dsp_nested" },
	}
	_, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-child", TargetAgent: "claude", Cwd: cwd, Text: "demo",
	})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "nested_dispatch_unsupported" {
		t.Fatalf("error = %+v", err)
	}

	service.NewID = func() string { return "dsp_branch" }
	dispatch, err := service.Dispatch(context.Background(), Request{
		ParentSessionID: "demo-root", TargetAgent: "claude", Cwd: cwd, Text: "demo",
		Preset: "do", Workspace: "worktree", Branch: "homing/dsp_branch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Branch != "homing/dsp_branch" {
		t.Fatalf("branch = %q", dispatch.Branch)
	}
}
