package cloud

import (
	"log/slog"
	"net/http"
	"testing"
)

func TestConfigureServerNormalizesPersistsAndClears(t *testing.T) {
	dir := t.TempDir()
	mux := http.NewServeMux()
	hub, err := EnsureHub(dir, mux, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	t.Cleanup(func() { removeConnector(dir) })

	status, err := ConfigureServer(dir, "ws://127.0.0.1:1", hub, mux, slog.Default())
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !status.Configured || status.Server != "ws://127.0.0.1:1/v1/ws" || status.Connected {
		t.Fatalf("status = %+v", status)
	}
	if got := hub.Peers().Server(); got != "ws://127.0.0.1:1/v1/ws" {
		t.Fatalf("persisted server = %q", got)
	}
	if connector, ok := ConnectorFor(dir); !ok || connector == nil {
		t.Fatal("connector was not installed")
	}

	cleared, err := ConfigureServer(dir, "  ", hub, mux, slog.Default())
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.Configured || cleared.Connected || cleared.Server != "" {
		t.Fatalf("cleared status = %+v", cleared)
	}
	if got := hub.Peers().Server(); got != "" {
		t.Fatalf("persisted server after clear = %q", got)
	}
	if _, ok := ConnectorFor(dir); ok {
		t.Fatal("connector still installed after clear")
	}
}

func TestConfigureServerRejectsInvalidAddressWithoutChangingState(t *testing.T) {
	dir := t.TempDir()
	mux := http.NewServeMux()
	hub, err := EnsureHub(dir, mux, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)

	if _, err := ConfigureServer(dir, "ftp://relay.example.test", hub, mux, slog.Default()); err == nil {
		t.Fatal("invalid address was accepted")
	}
	if got := hub.Peers().Server(); got != "" {
		t.Fatalf("invalid address changed persisted server to %q", got)
	}
	if _, ok := ConnectorFor(dir); ok {
		t.Fatal("invalid address installed a connector")
	}
}
