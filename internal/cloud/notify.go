package cloud

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/notify"
)

// ErrNoNotificationTransport means no paired phone could receive the
// notification through LAN or cloud.
var ErrNoNotificationTransport = errors.New("cloud: no paired phone transport")

// AttachPush plugs the cloud transports into the notification pipeline. The
// lookup happens when a payload is delivered, because api.NewMux may run
// service.Start before the cloud route creates the hub.
func AttachPush(deps *api.Deps, setPush func(func(notify.Payload))) {
	if deps == nil || setPush == nil {
		return
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	api.SetTestPushSink(func(ctx context.Context, payload notify.Payload) (int, error) {
		return DeliverNotification(ctx, deps.DataDir, payload)
	})
	setPush(func(payload notify.Payload) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		delivered, err := DeliverNotification(ctx, deps.DataDir, payload)
		if err != nil {
			log.Warn("notify transport failed",
				"level", payload.Level, "source", payload.Source, "err", err)
			return
		}
		log.Debug("notify delivered",
			"level", payload.Level, "source", payload.Source, "phones", delivered)
	})
}

// DeliverNotification sends one finished notification payload to every paired
// phone. Phones online over the LAN are served directly; the rest go through
// the cloud connector where the relay queues them and sends a content-free FCM
// wake-up.
func DeliverNotification(ctx context.Context, dataDir string, payload notify.Payload) (int, error) {
	hub, _ := HubFor(dataDir)
	connector, _ := ConnectorFor(dataDir)
	return deliverNotification(ctx, hub, connector, payload)
}

func deliverNotification(
	ctx context.Context,
	hub *Hub,
	connector *Connector,
	payload notify.Payload,
) (int, error) {
	delivered := 0
	var firstErr error
	lanOnline := map[string]bool{}

	if hub != nil {
		lanOnline = hub.OnlinePeers()
		sent, err := hub.SendEvent(ctx, "notify", payload)
		delivered += sent
		if err != nil {
			firstErr = err
		}
	}
	if connector != nil {
		sent, err := connector.SendEventExcept(ctx, "notify", payload, lanOnline)
		delivered += sent
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if delivered == 0 && firstErr == nil {
		return 0, ErrNoNotificationTransport
	}
	return delivered, firstErr
}
