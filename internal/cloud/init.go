package cloud

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/platform"
)

// The route is registered from init() so cmd/homing never has to import cloud
// explicitly (it already blank-imports it) and so tests can call api.NewMux.
func init() {
	api.Register("cloud", 900, func(mux *http.ServeMux, deps *api.Deps) {
		log := deps.Log
		if log == nil {
			log = slog.Default()
		}
		dataDir := deps.DataDir
		if dataDir == "" {
			resolved, err := platform.DataDir()
			if err != nil {
				log.Error("cloud: cannot resolve data dir", "err", err)
				return
			}
			dataDir = resolved
		}
		hub, err := EnsureHub(dataDir, mux, log)
		if err != nil {
			log.Error("cloud: cannot start LAN hub", "err", err)
			return
		}
		mux.Handle("GET /v1/ws", hub)
		mountPairControl(mux, dataDir, hub)
		mountConfig(mux, dataDir, hub, deps)
		deps.SetCloudStatusProvider(func() api.CloudStatus {
			return StatusFor(dataDir, hub)
		})

		// A configured relay address means the user already paired this computer
		// with a cloud/self-hosted server; keep it connected in the background.
		if server := hub.Peers().Server(); server != "" {
			if _, err := EnsureConnector(dataDir, server, hub.Identity(), hub.Peers(), mux, log); err != nil {
				log.Error("cloud: cannot start relay connector", "server", server, "err", err)
			}
		}
	})
}

var registry struct {
	sync.Mutex
	hubs       map[string]*Hub
	connectors map[string]*Connector
	activeHub  *Hub
}

func init() {
	registry.hubs = make(map[string]*Hub)
	registry.connectors = make(map[string]*Connector)
}

// EnsureHub returns the process-wide LAN hub for dataDir, creating it once.
func EnsureHub(dataDir string, handler http.Handler, log *slog.Logger) (*Hub, error) {
	registry.Lock()
	defer registry.Unlock()
	if hub := registry.hubs[dataDir]; hub != nil {
		registry.activeHub = hub
		return hub, nil
	}
	hub, err := NewHub(dataDir, handler, log)
	if err != nil {
		return nil, err
	}
	registry.hubs[dataDir] = hub
	registry.activeHub = hub
	return hub, nil
}

// HubFor returns the LAN hub built for a data directory, if any.
func HubFor(dataDir string) (*Hub, bool) {
	registry.Lock()
	defer registry.Unlock()
	hub, ok := registry.hubs[dataDir]
	return hub, ok
}

// ActiveHub is what the future `homing pair` command will use.
func ActiveHub() *Hub {
	registry.Lock()
	defer registry.Unlock()
	return registry.activeHub
}

// EnsureConnector starts (once) the background connector for dataDir.
func EnsureConnector(
	dataDir, server string,
	identity *Identity,
	peers *PeerStore,
	handler http.Handler,
	log *slog.Logger,
) (*Connector, error) {
	registry.Lock()
	defer registry.Unlock()
	if connector := registry.connectors[dataDir]; connector != nil {
		return connector, nil
	}
	connector, err := NewConnector(ConnectorConfig{
		ServerURL:      server,
		RelayKey:       peers.RelayKey(),
		Identity:       identity,
		Peers:          peers,
		Handler:        handler,
		Logger:         log,
		UseSystemProxy: peers.UseSystemProxy(),
	})
	if err != nil {
		return nil, err
	}
	registry.connectors[dataDir] = connector
	go connector.Run(context.Background())
	return connector, nil
}

// ConnectorFor returns the background connector for a data directory, if any.
func ConnectorFor(dataDir string) (*Connector, bool) {
	registry.Lock()
	defer registry.Unlock()
	connector, ok := registry.connectors[dataDir]
	return connector, ok
}

// StatusFor reports the live relay connector state for one data directory.
func StatusFor(dataDir string, hub *Hub) api.CloudStatus {
	status := api.CloudStatus{
		DirectDial:       true,
		ProxyEnvDetected: hasLocalProxyEnv(),
	}
	if hub != nil {
		status.Server = hub.Peers().Server()
		status.Configured = status.Server != ""
		status.Peers = len(hub.Peers().List())
		status.DirectDial = !hub.Peers().UseSystemProxy()
		status.RelayKeyHint = hub.Peers().RelayKeyHint()
		status.RelayKeyConfigured = status.RelayKeyHint != ""
	}
	if connector, ok := ConnectorFor(dataDir); ok && connector != nil {
		status.AuthRequired = connector.AuthRequired()
		status.Connected = connector.Connected() && !status.AuthRequired
		status.DirectDial = connector.DirectDial()
		status.LatencyMs = connector.LatencyMs()
		status.LastError = connector.LastError()
		status.RelayNotice = connector.RelayNotice()
		fakeDetected, fakeBypassed, resolvedIP := connector.DialDiagnostics()
		status.TunFakeIPDetected = fakeDetected
		status.TunFakeIPBypassed = fakeBypassed
		status.ResolvedIP = resolvedIP
	}
	return status
}

func installConnector(dataDir string, connector *Connector) {
	registry.Lock()
	old := registry.connectors[dataDir]
	registry.connectors[dataDir] = connector
	registry.Unlock()
	if old != nil && old != connector {
		old.Close()
	}
	go connector.Run(context.Background())
}

func removeConnector(dataDir string) {
	registry.Lock()
	connector := registry.connectors[dataDir]
	delete(registry.connectors, dataDir)
	registry.Unlock()
	if connector != nil {
		connector.Close()
	}
}
