// Command homing is the desktop side of Homing: one binary that is the menu-bar /
// tray app, a window, a headless server (`homing serve`) and a CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/platform"

	// Packages that plug themselves in from init().
	_ "github.com/tomato-hug/homing/internal/adapter"
	_ "github.com/tomato-hug/homing/internal/agent/builtin"
	_ "github.com/tomato-hug/homing/internal/cloud"
)

var version = "dev" // set with -ldflags "-X main.version=..."

const usage = `homing — every coding agent, one pocket inbox

用法:
  homing                 打开托盘和窗口（界面版还在做，暂时请用 serve）
  homing serve [flags]   只跑本机服务，不带界面（服务器、远程开发机、Docker）
  homing version         显示版本
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	case "":
		fmt.Fprint(os.Stderr, "界面版还没完成，先用 `homing serve`。\n\n"+usage)
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "不认识的命令 %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "homing:", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	// 18788 while the Node relay still owns 18787; switch after the parity check.
	addr := fs.String("addr", "127.0.0.1:18788", "listen address")
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
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	deps := &api.Deps{Log: log, DataDir: *dataDir, Version: version}
	srv := &http.Server{Addr: *addr, Handler: api.NewMux(deps), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("homing serve", "addr", *addr, "data", *dataDir, "version", version)

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
