// Command homing-server is the Homing relay server (cloud or self-hosted).
// It replaces the Node implementation in server/ once parity is verified.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tomato-hug/homing/internal/server"
)

var version = "dev" // set with -ldflags "-X main.version=..."

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "homing-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("homing-server", flag.ContinueOnError)
	defaultAddr := net.JoinHostPort(envOr("HOST", "0.0.0.0"), envOr("PORT", "8080"))
	addr := fs.String("addr", defaultAddr, "listen address")
	dbPath := fs.String("db", envOr("DB_PATH", "./data/homing.sqlite"), "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := server.New(server.Config{
		Addr:   *addr,
		DBPath: *dbPath,
		Logger: log,
	})
	if err != nil {
		return err
	}
	fcmCredentials := os.Getenv("HOMING_FCM_CREDENTIALS")
	srv.SetWaker(server.NewPushDispatcher(srv.DB(), log, fcmCredentials))

	listener, err := srv.Start()
	if err != nil {
		_ = srv.Close()
		return err
	}
	fmt.Printf("homing-server %s listening on %s\n", version, listener.Addr())
	fmt.Printf("websocket: ws://%s/v1/ws\n", listener.Addr())
	fmt.Printf("database: %s\n", *dbPath)
	if fcmCredentials == "" {
		fmt.Println("push: HOMING_FCM_CREDENTIALS 未设置，离线唤醒只会记录跳过")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Close() }()
	select {
	case err := <-done:
		return err
	case <-shutdown.Done():
		return shutdown.Err()
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
