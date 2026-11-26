package platform

import (
	"sync"
	"time"
)

const (
	defaultKeepAwakeMinutes = 240
	minKeepAwakeMinutes     = 15
	maxKeepAwakeMinutes     = 24 * 60
)

type keepAwakeFile struct {
	Enabled    bool   `json:"enabled"`
	UntilMs    int64  `json:"until_ms,omitempty"`
	AutoOnAway *bool  `json:"auto_on_away,omitempty"`
	MaxMinutes int    `json:"max_minutes,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// KeepAwakeState reports macOS idle-sleep prevention settings and live status.
type KeepAwakeState struct {
	Available  bool    `json:"available"`
	Active     bool    `json:"active"`
	Enabled    bool    `json:"enabled"`
	AutoOnAway bool    `json:"auto_on_away"`
	MaxMinutes int     `json:"max_minutes"`
	Until      *string `json:"until,omitempty"`
	UntilMs    int64   `json:"until_ms,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// KeepAwakePatch updates one or more keep-awake preferences.
type KeepAwakePatch struct {
	Enabled    *bool
	AutoOnAway *bool
	MaxMinutes *int
}

// KeepAwakeRunner starts a bounded idle-sleep assertion and returns a stop func.
type KeepAwakeRunner func(duration time.Duration) (stop func(), err error)

// KeepAwakeController manages a single caffeinate assertion per dataDir.
type KeepAwakeController struct {
	paths     Paths
	runner    KeepAwakeRunner
	available func() bool
	now       func() time.Time

	mu          sync.Mutex
	stopCurrent func()
	activeUntil time.Time
	activeWhy   string
	awayStart   time.Time
}

var keepAwakeRegistry = struct {
	sync.Mutex
	byDir map[string]*KeepAwakeController
}{byDir: map[string]*KeepAwakeController{}}

// KeepAwakeFor returns the shared KeepAwakeController for dataDir.
func KeepAwakeFor(dataDir string) *KeepAwakeController {
	keepAwakeRegistry.Lock()
	defer keepAwakeRegistry.Unlock()
	if existing := keepAwakeRegistry.byDir[dataDir]; existing != nil {
		return existing
	}
	ctrl := NewKeepAwakeController(NewPaths(dataDir), nil, nil)
	keepAwakeRegistry.byDir[dataDir] = ctrl
	return ctrl
}

// NewKeepAwakeController constructs a controller (tests may inject runner/available).
func NewKeepAwakeController(
	paths Paths,
	runner KeepAwakeRunner,
	available func() bool,
) *KeepAwakeController {
	if runner == nil {
		runner = defaultKeepAwakeRunner
	}
	if available == nil {
		available = defaultKeepAwakeAvailable
	}
	return &KeepAwakeController{
		paths:     paths,
		runner:    runner,
		available: available,
		now:       time.Now,
	}
}

func clampKeepAwakeMinutes(v int) int {
	if v <= 0 {
		return defaultKeepAwakeMinutes
	}
	if v < minKeepAwakeMinutes {
		return minKeepAwakeMinutes
	}
	if v > maxKeepAwakeMinutes {
		return maxKeepAwakeMinutes
	}
	return v
}

func (c *KeepAwakeController) loadFileLocked() keepAwakeFile {
	var f keepAwakeFile
	_ = ReadJSON(c.paths.KeepAwake, &f)
	if f.AutoOnAway == nil {
		defaultTrue := true
		f.AutoOnAway = &defaultTrue
	}
	f.MaxMinutes = clampKeepAwakeMinutes(f.MaxMinutes)
	return f
}

// Sync reconciles the live caffeinate process with persisted settings + away state.
func (c *KeepAwakeController) Sync(away bool) KeepAwakeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syncLocked(c.loadFileLocked(), away)
}

// Update applies a preference patch, persists it, and reconciles the process.
func (c *KeepAwakeController) Update(patch KeepAwakePatch, away bool) KeepAwakeState {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	f := c.loadFileLocked()
	if patch.MaxMinutes != nil {
		f.MaxMinutes = clampKeepAwakeMinutes(*patch.MaxMinutes)
	}
	if patch.AutoOnAway != nil {
		val := *patch.AutoOnAway
		f.AutoOnAway = &val
	}
	if patch.Enabled != nil {
		f.Enabled = *patch.Enabled
		if f.Enabled {
			f.UntilMs = now.Add(time.Duration(f.MaxMinutes) * time.Minute).UnixMilli()
		} else {
			f.UntilMs = 0
		}
	} else if f.Enabled && patch.MaxMinutes != nil {
		f.UntilMs = now.Add(time.Duration(f.MaxMinutes) * time.Minute).UnixMilli()
	}
	f.UpdatedAt = now.UTC().Format("2006-01-02T15:04:05.000Z")
	_ = WriteJSON(c.paths.KeepAwake, f)
	return c.syncLocked(f, away)
}

// Close terminates any active caffeinate child process immediately.
func (c *KeepAwakeController) Close() {
	c.mu.Lock()
	stop := c.stopCurrent
	c.stopCurrent = nil
	c.activeUntil = time.Time{}
	c.activeWhy = ""
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (c *KeepAwakeController) syncLocked(f keepAwakeFile, away bool) KeepAwakeState {
	now := c.now()
	avail := c.available()
	autoOnAway := f.AutoOnAway == nil || *f.AutoOnAway
	maxDur := time.Duration(clampKeepAwakeMinutes(f.MaxMinutes)) * time.Minute

	if !away {
		c.awayStart = time.Time{}
	} else if c.awayStart.IsZero() {
		c.awayStart = now
	}

	manualActive := f.Enabled && f.UntilMs > now.UnixMilli()
	if f.Enabled && !manualActive {
		f.Enabled = false
		f.UntilMs = 0
		_ = WriteJSON(c.paths.KeepAwake, f)
	}

	var wantWhy string
	var wantUntil time.Time
	switch {
	case manualActive:
		wantWhy = "manual"
		wantUntil = time.UnixMilli(f.UntilMs)
	case away && autoOnAway:
		candidate := c.awayStart.Add(maxDur)
		if candidate.After(now) {
			wantWhy = "away"
			wantUntil = candidate
		}
	}

	if !avail || wantWhy == "" || !wantUntil.After(now) {
		if c.stopCurrent != nil {
			c.stopCurrent()
			c.stopCurrent = nil
		}
		c.activeUntil = time.Time{}
		c.activeWhy = ""
		return KeepAwakeState{
			Available:  avail,
			Active:     false,
			Enabled:    manualActive,
			AutoOnAway: autoOnAway,
			MaxMinutes: f.MaxMinutes,
		}
	}

	needRestart := c.stopCurrent == nil ||
		c.activeWhy != wantWhy ||
		!c.activeUntil.After(now.Add(5*time.Second)) ||
		absDuration(c.activeUntil.Sub(wantUntil)) > 10*time.Second

	if needRestart {
		if c.stopCurrent != nil {
			c.stopCurrent()
			c.stopCurrent = nil
		}
		stop, err := c.runner(wantUntil.Sub(now))
		if err == nil {
			c.stopCurrent = stop
			c.activeUntil = wantUntil
			c.activeWhy = wantWhy
		} else {
			c.activeUntil = time.Time{}
			c.activeWhy = ""
		}
	}

	var untilStr *string
	var untilMs int64
	if c.stopCurrent != nil && !c.activeUntil.IsZero() {
		formatted := c.activeUntil.UTC().Format("2006-01-02T15:04:05.000Z")
		untilStr = &formatted
		untilMs = c.activeUntil.UnixMilli()
	}

	return KeepAwakeState{
		Available:  avail,
		Active:     c.stopCurrent != nil,
		Enabled:    manualActive,
		AutoOnAway: autoOnAway,
		MaxMinutes: f.MaxMinutes,
		Until:      untilStr,
		UntilMs:    untilMs,
		Reason:     c.activeWhy,
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
