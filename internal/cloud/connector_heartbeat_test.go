package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A peer can send application data before its WebSocket pong. The connector
// must be able to reply to that data while its heartbeat is awaiting the pong.
func TestConnectorHeartbeatAllowsApplicationReply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), connectorHeartbeat+15*time.Second)
	pingSeen := make(chan struct{})
	result := make(chan error, 1)
	var once sync.Once
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var peer *websocket.Conn
		var err error
		peer, err = websocket.Accept(w, r, &websocket.AcceptOptions{
			OnPingReceived: func(ctx context.Context, _ []byte) bool {
				// This frame is ordered before the automatic WebSocket pong.
				err := peer.Write(ctx, websocket.MessageText, []byte(`{"t":"ping"}`))
				once.Do(func() { close(pingSeen) })
				if err != nil {
					select {
					case result <- err:
					default:
					}
				}
				return true
			},
		})
		if err != nil {
			result <- err
			return
		}
		defer peer.CloseNow()
		_, raw, err := peer.Read(ctx)
		if err == nil {
			var message map[string]any
			if json.Unmarshal(raw, &message) != nil || message["t"] != "pong" {
				err = fmt.Errorf("expected application pong, got %q", raw)
			}
		}
		select {
		case result <- err:
		default:
		}
	}))
	t.Cleanup(ts.Close)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	connector := &Connector{log: slog.Default()}
	go connector.readLoop(ctx, ws)
	go connector.heartbeat(ctx, ws)

	select {
	case <-pingSeen:
	case <-ctx.Done():
		t.Fatal("connector did not send its heartbeat")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("application reply failed during heartbeat: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat blocked the incoming application ping response")
	}
}
