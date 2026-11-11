package cloud

import (
	"context"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
)

// cloudSSEEvents is the live surface mirrored over the encrypted connector.
// Notifications are deliberately excluded: service.deliver already sends them
// through DeliverNotification, including the LAN/cloud online checks.
var cloudSSEEvents = map[string]bool{
	"message":       true,
	"reply_status":  true,
	"item":          true,
	"turn":          true,
	"session":       true,
	"dispatch":      true,
	"collab_result": true,
}

// bridgeEvents mirrors the local SSE feed to every paired phone while this
// WebSocket session is alive. The phone can fetch full items after reconnecting;
// this bridge only carries live changes.
func (c *Connector) bridgeEvents(ctx context.Context) {
	durable, cancelDurable := api.SubscribeEvents()
	defer cancelDurable()
	named, cancelNamed := api.SubscribeNamedEvents()
	defer cancelNamed()

	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-durable:
			if !open {
				return
			}
			if err := c.SendEvent(ctx, "message", event); err != nil {
				c.log.Debug("cloud event bridge message failed", "err", err)
			}
		case message, open := <-named:
			if !open {
				return
			}
			if !cloudSSEEvents[message.Name] {
				continue
			}
			// Phones receive final answers by default. Reasoning is available
			// from the items endpoint only when the user explicitly opts in.
			if message.Name == "item" && cloudItemIsReasoning(message.Data) {
				continue
			}
			if err := c.SendEvent(ctx, message.Name, message.Data); err != nil {
				c.log.Debug("cloud event bridge event failed",
					"event", message.Name, "session", message.SessionID, "err", err)
			}
		}
	}
}

func cloudItemIsReasoning(data any) bool {
	payload, ok := data.(map[string]any)
	if !ok {
		return false
	}
	item, ok := payload["item"].(agent.Item)
	if !ok {
		return false
	}
	return item.Type == "reasoning" || item.Type == "thinking"
}
