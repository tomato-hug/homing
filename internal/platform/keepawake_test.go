package platform

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestKeepAwakeControllerLifecycle(t *testing.T) {
	dir := t.TempDir()
	paths := NewPaths(dir)

	var starts atomic.Int32
	var stops atomic.Int32
	ctrl := NewKeepAwakeController(
		paths,
		func(d time.Duration) (func(), error) {
			if d <= 0 {
				t.Fatalf("expected positive duration, got %v", d)
			}
			starts.Add(1)
			return func() { stops.Add(1) }, nil
		},
		func() bool { return true },
	)
	defer ctrl.Close()

	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	ctrl.now = func() time.Time { return base }

	// Idle and not away -> inactive
	st := ctrl.Sync(false)
	if st.Active || !st.AutoOnAway {
		t.Fatalf("expected inactive with auto_on_away=true, got %+v", st)
	}

	// Entering away mode activates caffeinate automatically
	st = ctrl.Sync(true)
	if !st.Active || st.Reason != "away" {
		t.Fatalf("expected active on away, got %+v", st)
	}
	if starts.Load() != 1 {
		t.Fatalf("expected 1 start, got %d", starts.Load())
	}

	// Leaving away mode stops caffeinate
	st = ctrl.Sync(false)
	if st.Active || stops.Load() != 1 {
		t.Fatalf("expected stopped after leaving away, active=%v stops=%d", st.Active, stops.Load())
	}

	// Manual toggle overrides even when not away
	tr := true
	mins := 60
	st = ctrl.Update(KeepAwakePatch{Enabled: &tr, MaxMinutes: &mins}, false)
	if !st.Active || !st.Enabled || st.Reason != "manual" || st.MaxMinutes != 60 {
		t.Fatalf("expected manual active 60m, got %+v", st)
	}

	// Advancing past max_minutes expires manual hold
	ctrl.now = func() time.Time { return base.Add(61 * time.Minute) }
	st = ctrl.Sync(false)
	if st.Active || st.Enabled {
		t.Fatalf("expected expired manual hold, got %+v", st)
	}
}
