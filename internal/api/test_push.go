package api

import (
	"context"
	"sync"

	"github.com/tomato-hug/homing/internal/notify"
)

// TestPushSink is installed by internal/cloud, which owns the LAN and relay
// transports. Keeping the callback here avoids an api -> cloud import cycle
// while letting the settings endpoint exercise the real delivery path.
type TestPushSink func(context.Context, notify.Payload) (int, error)

var testPushSink = struct {
	sync.RWMutex
	fn TestPushSink
}{}

func SetTestPushSink(fn TestPushSink) {
	testPushSink.Lock()
	testPushSink.fn = fn
	testPushSink.Unlock()
}

func deliverTestPush(ctx context.Context, payload notify.Payload) (int, error) {
	testPushSink.RLock()
	fn := testPushSink.fn
	testPushSink.RUnlock()
	if fn == nil {
		return 0, nil
	}
	return fn(ctx, payload)
}
