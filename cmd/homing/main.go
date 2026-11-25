// Command homing is the desktop side of Homing: one binary that is the menu-bar /
// tray app, a window, a headless server (`homing serve`) and a CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/awayauto"
	"github.com/tomato-hug/homing/internal/cli"
	"github.com/tomato-hug/homing/internal/gui"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/service"
	"github.com/tomato-hug/homing/internal/updater"

	// Packages that plug themselves in from init().
	_ "github.com/tomato-hug/homing/internal/adapter"
	_ "github.com/tomato-hug/homing/internal/agent/builtin"
	_ "github.com/tomato-hug/homing/internal/cloud"
	_ "github.com/tomato-hug/homing/internal/library"
	_ "github.com/tomato-hug/homing/internal/mcpserver"
	_ "github.com/tomato-hug/homing/internal/relayapi"
	_ "github.com/tomato-hug/homing/internal/tui"
)

var version = "dev" // set with -ldflags "-X main.version=..."

const usage = `homing — every coding agent, one pocket inbox

用法:
  homing [flags]         打开托盘、托盘面板和主窗口
  homing serve [flags]   只跑本机服务，不带界面（服务器、远程开发机、Docker）
  homing version         显示版本
`

func main() {
	platform.PrepareRuntimeEnvironment()
	updater.SetVersion(version)
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "--help", "-h":
			cmd = args[0]
			args = nil
		default:
			if !strings.HasPrefix(args[0], "-") {
				cmd, args = args[0], args[1:]
			}
		}
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	case "":
		err = desktop(args)
	default:
		c, ok := cli.Lookup(cmd)
		if !ok {
			fmt.Fprintf(os.Stderr, "不认识的命令 %q\n\n", cmd)
			printUsage(os.Stderr)
			os.Exit(2)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = c.Run(ctx, args)
		stop()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "homing:", err)
		os.Exit(1)
	}
}

// desktop is `homing` without a subcommand: the same local service as
// `homing serve`, plus the native tray / panel / window shell.
func desktop(args []string) error {
	fs := flag.NewFlagSet("homing", flag.ContinueOnError)
	addr := fs.String("addr", "0.0.0.0:18787", "listen address (all interfaces, so paired phones on the LAN can reach it)")
	dataDir := fs.String("data-dir", "", "data directory (default: per-OS app data dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !gui.Available() {
		return gui.ErrHeadlessBuild
	}
	if *dataDir == "" {
		d, err := platform.DataDir()
		if err != nil {
			return err
		}
		*dataDir = d
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	local, err := startDesktopService(ctx, *addr, *dataDir, version, log)
	if err != nil {
		return err
	}
	defer local.Close()
	log.Info("homing desktop", "url", local.URL, "owns_service", local.owned)
	guiErr := gui.Run(local.ctx, gui.Options{
		URL: local.URL, DataDir: *dataDir, Version: version, Log: log,
	})
	return errors.Join(guiErr, local.Close())
}

func browserAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// printUsage prints the built-in commands plus everything registered through
// internal/cli.
func printUsage(w io.Writer) {
	fmt.Fprint(w, usage)
	for _, c := range cli.All() {
		fmt.Fprintf(w, "  homing %-15s %s\n", c.Name, c.Summary)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	// Same address as the Node relay it replaces: phones on the LAN connect to
	// 18787 directly. Every /api route still needs a token; /console and
	// /api/autostart only answer loopback requests.
	addr := fs.String("addr", "0.0.0.0:18787", "listen address")
	dataDir := fs.String("data-dir", "", "data directory (default: per-OS app data dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" {
		d, err := platform.DataDir()
		if err != nil {
			return err
		}
		*dataDir = d
	}
	absoluteDir, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	*dataDir = absoluteDir
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	deps := &api.Deps{Log: log, DataDir: *dataDir, Version: version}
	// Anything the notifier is still batching goes out before we stop.
	defer func() {
		service.Flush(deps)
		_ = deps.Close()
	}()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: api.NewMux(deps), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if st, storeErr := deps.Store(); storeErr == nil {
		go awayauto.New(st, log).Run(ctx)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(listener) }()
	consoleURL := "http://" + browserAddress(listener.Addr().String()) + "/console/"
	log.Info("homing serve", "addr", listener.Addr().String(), "console", consoleURL, "data", *dataDir, "version", version)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
