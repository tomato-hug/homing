package api

import (
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// processStartedAt is the Go process lifetime shown on the connection page.
// Node reports the relay process uptime; the Go server is one process too.
var processStartedAt = time.Now()

type connectionInfo struct {
	Version        string                      `json:"version"`
	Product        string                      `json:"product"`
	Mode           string                      `json:"mode"`
	Port           int                         `json:"port"`
	LocalURL       string                      `json:"local_url"`
	LANURLs        []string                    `json:"lan_urls"`
	HostName       string                      `json:"host_name"`
	Platform       string                      `json:"platform"`
	PlatformRaw    string                      `json:"platform_raw"`
	Arch           string                      `json:"arch"`
	Node           string                      `json:"node"`
	Runtime        string                      `json:"runtime"`
	RuntimeVersion string                      `json:"runtime_version"`
	DataDir        string                      `json:"data_dir"`
	ConsoleDir     string                      `json:"console_dir"`
	ConsoleEnabled bool                        `json:"console_enabled"`
	Packaged       bool                        `json:"packaged"`
	UptimeSeconds  int64                       `json:"uptime_s"`
	Autostart      platform.AutostartState     `json:"autostart"`
	Security       connectionSecurity          `json:"security"`
	Options        map[string]connectionOption `json:"options"`
	Cloud          CloudStatus                 `json:"cloud"`
}

// CloudStatus is the live state of the optional cloud/self-hosted relay
// connector. It is additive so clients written against the LAN-only server
// continue to work unchanged.
type CloudStatus struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Server     string `json:"server"`
	Peers      int    `json:"peers"`
}

type connectionSecurity struct {
	TLS        bool   `json:"tls"`
	E2E        string `json:"e2e"`
	LocalAuth  string `json:"local_auth"`
	RemoteAuth string `json:"remote_auth"`
}

type connectionOption struct {
	Available   bool   `json:"available"`
	Label       string `json:"label"`
	Note        string `json:"note,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

func init() {
	Register("connection", 20, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/connection", d.guard(d.handleConnection))
	})
}

// SetCloudStatusProvider lets internal/cloud expose its connector state
// without making internal/api import cloud (which would create a cycle).
func (d *Deps) SetCloudStatusProvider(fn func() CloudStatus) {
	d.cloudStatusMu.Lock()
	d.cloudStatusFn = fn
	d.cloudStatusMu.Unlock()
}

func (d *Deps) cloudStatus() CloudStatus {
	d.cloudStatusMu.RLock()
	fn := d.cloudStatusFn
	d.cloudStatusMu.RUnlock()
	if fn == nil {
		return CloudStatus{}
	}
	return fn()
}

func (d *Deps) handleConnection(w http.ResponseWriter, r *http.Request) {
	port := requestPort(r)
	version := d.Version
	if version == "" {
		version = platform.Version
	}
	cloud := d.cloudStatus()
	e2e := "available"
	if cloud.Configured {
		e2e = "configured"
		if cloud.Connected {
			e2e = "connected"
		}
	}
	cloudNote := "可配置；填写云中转或自建 homingd 的 WebSocket 地址后启用。"
	if cloud.Configured {
		cloudNote = "已配置 " + cloud.Server
		if cloud.Connected {
			cloudNote += "，当前已连接。"
		} else {
			cloudNote += "，正在后台重连。"
		}
	}
	WriteJSON(w, http.StatusOK, connectionInfo{
		Version:        version,
		Product:        platform.Product,
		Mode:           "lan",
		Port:           port,
		LocalURL:       platform.LocalURL(port),
		LANURLs:        platform.LANURLs(port),
		HostName:       platform.HostName(),
		Platform:       platform.PlatformLabel(),
		PlatformRaw:    runtime.GOOS,
		Arch:           runtime.GOARCH,
		Node:           "",
		Runtime:        "go",
		RuntimeVersion: runtime.Version(),
		DataDir:        d.DataDir,
		ConsoleDir:     "embedded:web/console",
		ConsoleEnabled: true,
		Packaged:       true,
		UptimeSeconds:  int64(time.Since(processStartedAt).Round(time.Second) / time.Second),
		Autostart:      platform.AutostartInfo(),
		Security: connectionSecurity{
			TLS:        false,
			E2E:        e2e,
			LocalAuth:  "cookie",
			RemoteAuth: "device_token",
		},
		Options: map[string]connectionOption{
			"lan":         {Available: true, Recommended: true, Label: "局域网直连"},
			"cloud":       {Available: true, Label: "Homing 云中转", Note: cloudNote},
			"self_hosted": {Available: true, Label: "自建服务器", Note: "可配置；使用自己的 homingd 中继地址。"},
		},
		Cloud: cloud,
	})
}

func requestPort(r *http.Request) int {
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		if n, err := strconv.Atoi(port); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 18787
}
