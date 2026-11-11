package awayauto

import (
	"context"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

type fakeProbe struct {
	locked bool
	idle   time.Duration
	err    error
}

func (f *fakeProbe) State(context.Context) (bool, time.Duration, error) {
	return f.locked, f.idle, f.err
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(platform.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestAutoAwayDisabledDoesNothing(t *testing.T) {
	st := testStore(t)
	probe := &fakeProbe{idle: time.Hour}
	m := &Monitor{Store: st, Probe: probe, Now: time.Now}
	if action, err := m.Evaluate(context.Background()); err != nil || action != "" {
		t.Fatalf("action=%q err=%v", action, err)
	}
	if st.Away() {
		t.Fatal("away mode must stay off by default")
	}
}

func TestLockedThresholdStartsAndUnlockEnds(t *testing.T) {
	st := testStore(t)
	if err := st.SetAutoAway(true, 5); err != nil {
		t.Fatal(err)
	}
	probe := &fakeProbe{locked: true}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	m := &Monitor{Store: st, Probe: probe, Now: func() time.Time { return now }}

	if action, err := m.Evaluate(context.Background()); err != nil || action != "" {
		t.Fatalf("first lock action=%q err=%v", action, err)
	}
	now = now.Add(4 * time.Minute)
	if action, _ := m.Evaluate(context.Background()); action != "" {
		t.Fatalf("away started too early: %q", action)
	}
	now = now.Add(time.Minute)
	if action, err := m.Evaluate(context.Background()); err != nil || action != "started" {
		t.Fatalf("start action=%q err=%v", action, err)
	}
	current, ok := st.CurrentAwayPeriod()
	if !ok || current.Source != "auto_lock" {
		t.Fatalf("current=%+v ok=%v", current, ok)
	}

	probe.locked = false
	if action, err := m.Evaluate(context.Background()); err != nil || action != "ended" {
		t.Fatalf("end action=%q err=%v", action, err)
	}
	if st.Away() {
		t.Fatal("unlocking must end the automatic period")
	}
}

func TestIdleThresholdStartsAway(t *testing.T) {
	st := testStore(t)
	if err := st.SetAutoAway(true, 3); err != nil {
		t.Fatal(err)
	}
	probe := &fakeProbe{idle: 3 * time.Minute}
	m := &Monitor{Store: st, Probe: probe, Now: time.Now}
	if action, err := m.Evaluate(context.Background()); err != nil || action != "started" {
		t.Fatalf("action=%q err=%v", action, err)
	}
}

func TestDisablingDoesNotEndManualAway(t *testing.T) {
	st := testStore(t)
	if _, err := st.StartAway("manual", "demo-phone"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAutoAway(false, 5); err != nil {
		t.Fatal(err)
	}
	m := &Monitor{Store: st, Probe: &fakeProbe{}, Now: time.Now}
	if action, err := m.Evaluate(context.Background()); err != nil || action != "" {
		t.Fatalf("action=%q err=%v", action, err)
	}
	if !st.Away() {
		t.Fatal("manual away must not be ended by the automatic monitor")
	}
}

func TestDisablingEndsExistingAutoAway(t *testing.T) {
	st := testStore(t)
	if _, err := st.StartAway("auto_lock", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAutoAway(false, 5); err != nil {
		t.Fatal(err)
	}
	m := &Monitor{Store: st, Probe: &fakeProbe{}, Now: time.Now}
	if action, err := m.Evaluate(context.Background()); err != nil || action != "disabled" {
		t.Fatalf("action=%q err=%v", action, err)
	}
	if st.Away() {
		t.Fatal("disabling auto-away should close an automatic period")
	}
}

func TestParseHIDIdle(t *testing.T) {
	idle, err := parseHIDIdle([]byte(`"HIDIdleTime" = 1500000000`))
	if err != nil {
		t.Fatal(err)
	}
	if idle != 1500*time.Millisecond {
		t.Fatalf("idle = %v", idle)
	}
	if _, err := parseHIDIdle([]byte("nothing")); err == nil {
		t.Fatal("missing HIDIdleTime should be an error")
	}
	if !parseLocked([]byte(`"CGSSessionScreenIsLocked" = Yes`)) {
		t.Fatal("locked marker not detected")
	}
}
