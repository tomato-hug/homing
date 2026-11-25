//go:build ((darwin && !ios) || (linux && !android) || windows) && !headless

package gui

import (
    "net/http/httptest"
    "testing"

    "github.com/tomato-hug/homing/internal/api"
    "github.com/tomato-hug/homing/internal/platform"
)

func TestTrayPauseAndResumeUseResidentServiceState(t *testing.T) {
    dir := t.TempDir()
    deps := &api.Deps{DataDir: dir, Version: "demo-desktop"}
    t.Cleanup(func() { _ = deps.Close() })
    server := httptest.NewServer(api.NewMux(deps))
    t.Cleanup(server.Close)
    store, err := deps.Store()
    if err != nil { t.Fatal(err) }
    if err := setPause(server.URL, store.Token(), true); err != nil { t.Fatal(err) }
    if state := platform.GetPause(platform.NewPaths(dir)); !state.Paused { t.Fatal("tray did not pause notifications") }
    if err := setPause(server.URL, store.Token(), false); err != nil { t.Fatal(err) }
    if state := platform.GetPause(platform.NewPaths(dir)); state.Paused { t.Fatal("tray did not resume notifications") }
}
