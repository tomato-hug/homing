package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// Away reports whether the phone's "away from the Mac" mode is enabled.
func (s *Store) Away() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Away.Enabled
}

// SetAway persists the away switch in config.json without touching the token or
// proxy settings. New code should use StartAway / EndAway so the period log is
// updated too; this wrapper keeps older tests and callers working.
func (s *Store) SetAway(enabled bool) error {
	if enabled {
		_, err := s.StartAway("manual", "")
		return err
	}
	_, _, err := s.EndAway()
	return err
}

// AutoAwaySettings returns the lock/idle trigger. It is disabled by default;
// five minutes is the documented default once enabled.
func (s *Store) AutoAwaySettings() (enabled bool, minutes int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	minutes = s.config.Away.AutoLockMinutes
	if minutes <= 0 {
		minutes = 5
	}
	return s.config.Away.AutoLock, minutes
}

// SetAutoAway persists the automatic away trigger without changing the current
// away switch. Callers turn away mode on/off through StartAway/EndAway.
func (s *Store) SetAutoAway(enabled bool, minutes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if minutes <= 0 {
		minutes = 5
	}
	if minutes > 240 {
		minutes = 240
	}
	s.config.Away.AutoLock = enabled
	s.config.Away.AutoLockMinutes = minutes
	return platform.WriteJSON(s.Paths.Config, s.config)
}

// HomeNewActivity is the attention scope: "away" (only activity while away)
// or "unread" (every unseen event).
func (s *Store) HomeNewActivity() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.config.Home.NewActivity == "unread" {
		return "unread"
	}
	return "away"
}

// SetHomeNewActivity persists the attention scope.
func (s *Store) SetHomeNewActivity(scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope != "unread" {
		scope = "away"
	}
	s.config.Home.NewActivity = scope
	return platform.WriteJSON(s.Paths.Config, s.config)
}

// CollabReturn is the configured default for handing a child result back to
// its parent. Automatic injection is opt-in; the safe default is a card.
func (s *Store) CollabReturn() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.config.Home.CollabReturn == "inject" {
		return "inject"
	}
	return "card"
}

// SetCollabReturn enables or disables automatic result injection. It never
// changes the return mode of dispatches that have already been created.
func (s *Store) SetCollabReturn(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "card" && mode != "inject" {
		return errors.New("collab_return 只能是 card 或 inject")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Home.CollabReturn = mode
	return platform.WriteJSON(s.Paths.Config, s.config)
}

// SearchDisabled reports the user's opt-out for the local transcript index.
func (s *Store) SearchDisabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Search.Disabled
}

// SetSearchDisabled persists the transcript-index opt-out. Disabling never
// deletes data by itself; callers decide whether to clear the index too.
func (s *Store) SetSearchDisabled(disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Search.Disabled = disabled
	return platform.WriteJSON(s.Paths.Config, s.config)
}

// AwayPeriods returns every period in append order.
func (s *Store) AwayPeriods() []AwayPeriod {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AwayPeriod, len(s.away))
	copy(out, s.away)
	return out
}

// CurrentAwayPeriod returns the open period, if any.
func (s *Store) CurrentAwayPeriod() (AwayPeriod, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentAwayLocked()
}

// LastAwayPeriod returns the most recently started period, open or closed.
func (s *Store) LastAwayPeriod() (AwayPeriod, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.away) == 0 {
		return AwayPeriod{}, false
	}
	out := s.away[0]
	for _, period := range s.away[1:] {
		if period.StartedAt >= out.StartedAt {
			out = period
		}
	}
	return out, true
}

// StartAway opens a period and keeps config.away.enabled as the fast cache.
// Starting twice returns the existing period instead of creating a duplicate.
func (s *Store) StartAway(source, deviceID string) (AwayPeriod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.currentAwayLocked(); ok {
		if !s.config.Away.Enabled {
			s.config.Away.Enabled = true
			_ = platform.WriteJSON(s.Paths.Config, s.config)
		}
		return current, nil
	}
	if strings.TrimSpace(source) == "" {
		source = "manual"
	}
	period := AwayPeriod{
		ID:        "awp_" + randomID(10),
		StartedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Source:    source,
		DeviceID:  truncateRunes(strings.TrimSpace(deviceID), 80),
	}
	s.away = append(s.away, period)
	s.config.Away.Enabled = true
	if err := appendJSONL(s.Paths.AwayPeriods, period); err != nil {
		return period, err
	}
	if s.history != nil {
		if err := s.history.putAwayPeriod(period); err != nil {
			return period, err
		}
	}
	return period, platform.WriteJSON(s.Paths.Config, s.config)
}

// EndAway closes the open period. It is idempotent: with no open period the
// last closed one is returned and config stays disabled.
func (s *Store) EndAway() (AwayPeriod, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Away.Enabled = false
	configErr := platform.WriteJSON(s.Paths.Config, s.config)
	current, ok := s.currentAwayLocked()
	if !ok {
		return AwayPeriod{}, false, configErr
	}
	index := s.awayIndexLocked(current.ID)
	if index < 0 {
		return AwayPeriod{}, false, configErr
	}
	s.away[index].EndedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if err := appendJSONL(s.Paths.AwayPeriods, s.away[index]); err != nil {
		return s.away[index], true, err
	}
	if s.history != nil {
		if err := s.history.putAwayPeriod(s.away[index]); err != nil {
			return s.away[index], true, err
		}
	}
	return s.away[index], true, configErr
}

// ClearAwayPeriod marks the period's new activity as seen.
func (s *Store) ClearAwayPeriod(id string) (AwayPeriod, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.awayIndexLocked(id)
	if index < 0 {
		return AwayPeriod{}, false, nil
	}
	s.away[index].ClearedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	err := appendJSONL(s.Paths.AwayPeriods, s.away[index])
	if err == nil && s.history != nil {
		err = s.history.putAwayPeriod(s.away[index])
	}
	return s.away[index], true, err
}

func (s *Store) currentAwayLocked() (AwayPeriod, bool) {
	for i := len(s.away) - 1; i >= 0; i-- {
		if s.away[i].EndedAt == "" {
			return s.away[i], true
		}
	}
	return AwayPeriod{}, false
}

func (s *Store) awayIndexLocked(id string) int {
	for i := range s.away {
		if s.away[i].ID == id {
			return i
		}
	}
	return -1
}

func randomID(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(buf)
}

// Proxy returns a copy of the /p/:port allow-list.
func (s *Store) Proxy() (bool, []int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ports := append([]int(nil), s.config.Proxy.Ports...)
	return s.config.Proxy.Enabled, ports
}

// UpdateReplyStatus changes a reply in memory and appends the new revision to
// replies.jsonl. Replies are an append-only log, so on restart the last line for
// an id wins.
func (s *Store) UpdateReplyStatus(id int64, status, message string) (Reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.replies {
		if s.replies[i].ID != id {
			continue
		}
		if status == "queued" && replyStatusIsDelivered(s.replies[i].Status) {
			return s.replies[i], true
		}
		s.replies[i].Status = status
		s.replies[i].Message = message
		s.replies[i].Injected = status == "injected" || status == "held"
		updated := s.replies[i]
		if err := appendJSONL(s.Paths.Replies, updated); err != nil {
			return updated, false
		}
		if s.history != nil {
			if err := s.history.putReply(updated); err != nil {
				return updated, false
			}
		}
		return updated, true
	}
	return Reply{}, false
}

// TouchDeviceByToken updates a paired phone from its device token.
func (s *Store) TouchDeviceByToken(token, appVersion, ip string) (Device, bool) {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	digest := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices.Devices {
		device := &s.devices.Devices[i]
		if device.TokenSHA256 == "" || device.TokenSHA256 != digest {
			continue
		}
		device.LastSeenMs = time.Now().UnixMilli()
		if appVersion != "" {
			device.AppVersion = truncateRunes(appVersion, 32)
		}
		if ip != "" {
			device.LastIP = ip
		}
		_ = platform.WriteJSON(s.Paths.Devices, s.devices)
		return *device, true
	}
	return Device{}, false
}

// RenameDevice changes the display name of one paired phone.
func (s *Store) RenameDevice(id, name string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices.Devices {
		device := &s.devices.Devices[i]
		if device.ID != id {
			continue
		}
		if clean := truncateRunes(strings.TrimSpace(name), 60); clean != "" {
			device.Name = clean
		}
		_ = platform.WriteJSON(s.Paths.Devices, s.devices)
		return *device, true
	}
	return Device{}, false
}

// SetDeviceScopes replaces one phone's read/reply/drive permissions. An empty
// list restores the legacy full-access behaviour.
func (s *Store) SetDeviceScopes(id string, scopes []string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleaned := make([]string, 0, len(scopes))
	seen := map[string]bool{}
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope != "read" && scope != "reply" && scope != "drive" {
			continue
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		cleaned = append(cleaned, scope)
	}
	for i := range s.devices.Devices {
		device := &s.devices.Devices[i]
		if device.ID != id {
			continue
		}
		device.Scopes = cleaned
		_ = platform.WriteJSON(s.Paths.Devices, s.devices)
		return *device, true
	}
	return Device{}, false
}

// RemoveDevice revokes one paired phone. Its token stops authenticating on the
// very next request because auth reads the same in-memory device list.
func (s *Store) RemoveDevice(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices.Devices {
		if s.devices.Devices[i].ID != id {
			continue
		}
		s.devices.Devices = append(s.devices.Devices[:i], s.devices.Devices[i+1:]...)
		_ = platform.WriteJSON(s.Paths.Devices, s.devices)
		return true
	}
	return false
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
