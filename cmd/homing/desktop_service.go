package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/awayauto"
	"github.com/tomato-hug/homing/internal/service"
)

type desktopService struct {
	URL   string
	ctx   context.Context
	owned bool
	close func() error
}

func (s *desktopService) Close() error { return s.close() }

func desktopToken(dir string) string {
	// Do not use platform.NewPaths here: attaching must not create directories.
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Token)
}

// probeDesktopService verifies the public identity before sending local owner
// credentials. A refused connection means no service; other failures are not
// permission to take over an occupied port.
func probeDesktopService(ctx context.Context, baseURL, token string, client *http.Client) (bool, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) {
		return false, fmt.Errorf("桌面界面只能连接本机服务")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	get := func(path string, authorized bool, out any) error {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return err
		}
		if authorized {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := copyClient.Do(r)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("本机服务验证失败（HTTP %d）", response.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(out)
	}
	var health struct {
		OK   bool   `json:"ok"`
		Impl string `json:"impl"`
	}
	if err := get("/health", false, &health); err != nil {
		if isConnectionRefused(err) {
			return false, nil
		}
		return false, err
	}
	if !health.OK || health.Impl != "go" {
		return false, fmt.Errorf("端口已被其他程序使用，请选择其他端口")
	}
	if token == "" {
		return false, fmt.Errorf("已有 Homing 服务，请使用它的数据目录启动桌面界面")
	}
	var info struct {
		Product string `json:"product"`
		Runtime string `json:"runtime"`
	}
	if err := get("/api/connection", true, &info); err != nil {
		return false, err
	}
	if info.Product != "Homing" || info.Runtime != "go" {
		return false, fmt.Errorf("端口未提供可验证的 Homing 服务")
	}
	return true, nil
}

func startDesktopService(parent context.Context, addr, dataDir, version string, log *slog.Logger) (*desktopService, error) {
	resolvedDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	dataDir = resolvedDir
	if log == nil {
		log = slog.Default()
	}
	client := &http.Client{Timeout: time.Second}
	baseURL := "http://" + browserAddress(addr)
	attach := func() (*desktopService, error) {
		ok, err := probeDesktopService(parent, baseURL, desktopToken(dataDir), client)
		if err != nil || !ok {
			return nil, err
		}
		// The same credential may have been copied to a different profile. Verify
		// the service's data directory as well before sharing its lifecycle.
		r, err := http.NewRequestWithContext(parent, http.MethodGet, baseURL+"/api/connection", nil)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+desktopToken(dataDir))
		noRedirect := *client
		noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := noRedirect.Do(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var info struct {
			DataDir string `json:"data_dir"`
		}
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&info) != nil || !sameDesktopDataDir(info.DataDir, dataDir) {
			return nil, fmt.Errorf("已有服务使用不同的数据目录，请使用匹配的 --data-dir")
		}
		return &desktopService{URL: baseURL, ctx: parent, close: func() error { return nil }}, nil
	}
	if _, port, err := net.SplitHostPort(addr); err != nil {
		return nil, err
	} else if port != "0" {
		existing, err := attach()
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// Another launch may have won the bind after our initial probe.
		if existing, probeErr := attach(); probeErr == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("本机服务无法监听 %s：%w", addr, err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	deps := &api.Deps{Log: log, DataDir: dataDir, Version: version}
	srv := &http.Server{Handler: api.NewMux(deps), ReadHeaderTimeout: 10 * time.Second}
	if st, err := deps.Store(); err == nil {
		go awayauto.New(st, log).Run(ctx)
	}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("desktop service stopped", "err", err)
			cancel()
		}
	}()
	var once sync.Once
	var closeErr error
	return &desktopService{URL: "http://" + browserAddress(listener.Addr().String()), ctx: ctx, owned: true, close: func() error {
		once.Do(func() {
			cancel()
			shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			closeErr = srv.Shutdown(shutdown)
			if closeErr != nil {
				_ = srv.Close()
			}
			service.Flush(deps)
			closeErr = errors.Join(closeErr, deps.Close())
		})
		return closeErr
	}}, nil
}

func sameDesktopDataDir(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	canonical := func(p string) string {
		p, _ = filepath.Abs(p)
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return resolved
		}
		return filepath.Clean(p)
	}
	return canonical(a) == canonical(b)
}

const winsockECONNREFUSED = syscall.Errno(10061)

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, winsockECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused it")
}
