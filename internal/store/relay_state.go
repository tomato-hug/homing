package store

import (
	"crypto/sha256"
	"encoding/hex"
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
// proxy settings.
func (s *Store) SetAway(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Away.Enabled = enabled
	return platform.WriteJSON(s.Paths.Config, s.config)
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
		s.replies[i].Status = status
		s.replies[i].Message = message
		s.replies[i].Injected = status == "injected" || status == "held"
		updated := s.replies[i]
		_ = appendJSONL(s.Paths.Replies, updated)
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
