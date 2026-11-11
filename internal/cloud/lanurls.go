package cloud

import (
	"os"
	"strconv"
	"strings"

	"github.com/tomato-hug/homing/internal/platform"
)

// LANWebSocketURLs advertises the same protocol-v1 endpoint over local IPv4 so
// a cloud-paired phone can fall back to direct LAN when the relay is down.
// HOMING_LAN_PORT exists for non-default test or self-hosted layouts.
func LANWebSocketURLs() []string {
	port := 18787
	if raw := strings.TrimSpace(os.Getenv("HOMING_LAN_PORT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed < 65536 {
			port = parsed
		}
	}
	httpURLs := platform.LANURLs(port)
	out := make([]string, 0, len(httpURLs))
	for _, raw := range httpURLs {
		if strings.HasPrefix(raw, "http://") {
			out = append(out, "ws://"+strings.TrimPrefix(raw, "http://")+"/v1/ws")
		}
	}
	return out
}
