// Package awayauto starts and ends away periods from macOS screen-lock and HID
// idle signals. It is disabled by default and uses an injectable probe so the
// decision logic can be tested without reading the real console.
package awayauto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/store"
)

// Probe returns whether the screen is locked and how long the HID has been idle.
type Probe interface {
	State(ctx context.Context) (locked bool, idle time.Duration, err error)
}

// Monitor evaluates the automatic-away rule on a fixed interval.
type Monitor struct {
	Store    *store.Store
	Probe    Probe
	Interval time.Duration
	Now      func() time.Time
	Log      *slog.Logger

	lockedSince time.Time
}

// Available reports whether this build has a real desktop probe.
func Available() bool {
	return runtime.GOOS == "darwin"
}

// DefaultProbe reads macOS HID idle time and the console lock flag. It never
// opens a window or asks for accessibility permissions.
func DefaultProbe() Probe { return systemProbe{} }

func New(st *store.Store, log *slog.Logger) *Monitor {
	return &Monitor{
		Store: st, Probe: DefaultProbe(), Interval: 15 * time.Second,
		Now: time.Now, Log: log,
	}
}

// Run evaluates immediately and then on every interval until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	if m == nil || m.Store == nil || m.Probe == nil {
		return
	}
	if m.Interval <= 0 {
		m.Interval = 15 * time.Second
	}
	if m.Now == nil {
		m.Now = time.Now
	}
	m.evaluateLogged(ctx)
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.evaluateLogged(ctx)
		}
	}
}

func (m *Monitor) evaluateLogged(ctx context.Context) {
	action, err := m.Evaluate(ctx)
	if err != nil && m.Log != nil {
		m.Log.Warn("自动离开检测失败", "err", err)
		return
	}
	if action != "" && m.Log != nil {
		m.Log.Info("自动离开状态变更", "action", action)
	}
}

// Evaluate performs one deterministic step. It returns "started", "ended",
// "disabled" or "" so tests can assert the transition without sleeping.
func (m *Monitor) Evaluate(ctx context.Context) (string, error) {
	if m == nil || m.Store == nil || m.Probe == nil {
		return "", errors.New("automatic away monitor is not configured")
	}
	enabled, minutes := m.Store.AutoAwaySettings()
	current, hasCurrent := m.Store.CurrentAwayPeriod()
	if !enabled {
		m.lockedSince = time.Time{}
		if hasCurrent && current.Source == "auto_lock" {
			return "disabled", m.endAutoAway(current)
		}
		return "", nil
	}
	locked, idle, err := m.Probe.State(ctx)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	threshold := time.Duration(minutes) * time.Minute
	if locked {
		if m.lockedSince.IsZero() {
			m.lockedSince = now
		}
	} else {
		m.lockedSince = time.Time{}
	}
	shouldBeAway := idle >= threshold ||
		(locked && !m.lockedSince.IsZero() && now.Sub(m.lockedSince) >= threshold)

	if shouldBeAway {
		if !hasCurrent {
			period, startErr := m.Store.StartAway("auto_lock", "")
			if startErr != nil {
				return "", startErr
			}
			api.PublishNamed("away", "", map[string]any{
				"enabled": true, "period": period, "source": "auto_lock",
			})
			return "started", nil
		}
		return "", nil
	}
	if hasCurrent && current.Source == "auto_lock" {
		return "ended", m.endAutoAway(current)
	}
	return "", nil
}

func (m *Monitor) endAutoAway(period store.AwayPeriod) error {
	ended, ok, err := m.Store.EndAway()
	if err != nil {
		return err
	}
	if ok {
		period = ended
	}
	api.RunAwayEndHooks()
	api.PublishNamed("away", "", map[string]any{
		"enabled": false, "period": period, "source": "auto_lock",
	})
	return nil
}

type systemProbe struct{}

func (systemProbe) State(ctx context.Context) (bool, time.Duration, error) {
	idleOut, err := commandOutput(ctx, "/usr/sbin/ioreg", "ioreg",
		"-c", "IOHIDSystem", "-r", "-d", "1")
	if err != nil {
		return false, 0, err
	}
	idle, err := parseHIDIdle(idleOut)
	if err != nil {
		return false, 0, err
	}
	lockOut, _ := commandOutput(ctx, "/usr/sbin/ioreg", "ioreg", "-n", "Root", "-d", "1")
	return parseLocked(lockOut), idle, nil
}

func commandOutput(ctx context.Context, absolute, fallback string, args ...string) ([]byte, error) {
	binary := absolute
	if _, err := exec.LookPath(absolute); err != nil {
		binary = fallback
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, binary, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fallback, err)
	}
	return out, nil
}

var hidIdlePattern = regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)

func parseHIDIdle(output []byte) (time.Duration, error) {
	match := hidIdlePattern.FindSubmatch(output)
	if len(match) != 2 {
		return 0, errors.New("找不到 HIDIdleTime")
	}
	nanos, err := strconv.ParseInt(string(match[1]), 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(nanos), nil
}

func parseLocked(output []byte) bool {
	return bytes.Contains(output, []byte("CGSSessionScreenIsLocked=Yes")) ||
		bytes.Contains(output, []byte(`"CGSSessionScreenIsLocked" = Yes`))
}
