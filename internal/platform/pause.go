// Pause is homing's "stop sending notifications" switch. The tray menu and the
// console's notification page share one state in <data>/push-pause.json; while it
// is on, events are still stored and still show up in the inbox, they just do not
// ring the phone.
package platform

import (
	"time"
)

// maxPauseMinutes caps a pause at 24 hours, like the Node relay.
const maxPauseMinutes = 24 * 60

type pauseFile struct {
	UntilMs int64   `json:"until_ms"`
	Reason  *string `json:"reason"`
	SetAt   string  `json:"set_at"`
}

// PauseState is the payload GET /api/push/pause answers with.
type PauseState struct {
	Paused  bool    `json:"paused"`
	Until   *string `json:"until"`
	UntilMs int64   `json:"until_ms,omitempty"`
	Minutes int     `json:"minutes"`
	Reason  *string `json:"reason"`
	SetAt   *string `json:"set_at"`
}

// GetPause reads the pause state; a missing or malformed file means "not paused".
func GetPause(p Paths) PauseState {
	var file pauseFile
	if !ReadJSON(p.Pause, &file) || file.UntilMs == 0 {
		return PauseState{Paused: false, Until: nil, Minutes: 0, Reason: nil, SetAt: nil}
	}
	now := time.Now()
	paused := file.UntilMs > now.UnixMilli()
	until := time.UnixMilli(file.UntilMs).UTC().Format("2006-01-02T15:04:05.000Z")
	minutes := int((file.UntilMs - now.UnixMilli()) / 60000)
	if minutes < 0 {
		minutes = 0
	}
	state := PauseState{Paused: paused, Until: &until, UntilMs: file.UntilMs, Minutes: minutes, Reason: file.Reason}
	if file.SetAt != "" {
		setAt := file.SetAt
		state.SetAt = &setAt
	}
	return state
}

// IsPaused answers the question the notifier asks before every push.
func IsPaused(p Paths) bool { return GetPause(p).Paused }

// SetPause pauses for the given minutes (0 clears it) and returns the new state.
func SetPause(p Paths, minutes int, reason *string) PauseState {
	now := time.Now()
	if minutes <= 0 {
		_ = WriteJSON(p.Pause, pauseFile{UntilMs: 0, Reason: nil, SetAt: now.UTC().Format("2006-01-02T15:04:05.000Z")})
		return GetPause(p)
	}
	if minutes > maxPauseMinutes {
		minutes = maxPauseMinutes
	}
	_ = WriteJSON(p.Pause, pauseFile{
		UntilMs: now.Add(time.Duration(minutes) * time.Minute).UnixMilli(),
		Reason:  reason,
		SetAt:   now.UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	return GetPause(p)
}

// ClearPause resumes pushes immediately.
func ClearPause(p Paths) PauseState { return SetPause(p, 0, nil) }
