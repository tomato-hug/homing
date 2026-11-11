package cloud

import (
	"strings"
	"testing"
)

func TestLANWebSocketURLsUseConfiguredPort(t *testing.T) {
	t.Setenv("HOMING_LAN_PORT", "19999")
	urls := LANWebSocketURLs()
	if len(urls) == 0 {
		t.Skip("host has no LAN IPv4 address")
	}
	for _, raw := range urls {
		if !strings.HasPrefix(raw, "ws://") || !strings.HasSuffix(raw, ":19999/v1/ws") {
			t.Fatalf("unexpected LAN URL %q", raw)
		}
	}
}
