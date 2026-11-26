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
	"strconv"
	"strings"
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
	authMode := fs.String("auth-mode", envOr("HOMING_AUTH_MODE", server.AuthModeOpen), "auth mode: open | managed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	var trustedProxies []string
	if raw := strings.TrimSpace(os.Getenv("HOMING_TRUSTED_PROXIES")); raw != "" {
		trustedProxies = strings.Split(raw, ",")
	}
	smtpPort, _ := strconv.Atoi(envOr("HOMING_SMTP_PORT", "465"))
	freeWeekly, err := weeklyMinutesEnv("HOMING_FREE_WEEKLY_MINUTES", server.DefaultFreeWeeklyMinutes)
	if err != nil {
		return err
	}
	sponsorWeekly, err := weeklyMinutesEnv("HOMING_SPONSOR_WEEKLY_MINUTES", 0)
	if err != nil {
		return err
	}
	if strings.EqualFold(*authMode, server.AuthModeManaged) && strings.TrimSpace(os.Getenv("HOMING_ADMIN_TOKEN")) == "" {
		log.Warn("HOMING_AUTH_MODE=managed but HOMING_ADMIN_TOKEN is empty: /admin and the desktop admin panel stay disabled")
	}
	srv, err := server.New(server.Config{
		Addr:                 *addr,
		DBPath:               *dbPath,
		Logger:               log,
		TrustedProxies:       trustedProxies,
		AuthMode:             *authMode,
		RegistrationMode:     envOr("HOMING_REGISTRATION_MODE", server.RegModeOpen),
		AdminToken:           os.Getenv("HOMING_ADMIN_TOKEN"),
		FreeWeeklyMinutes:    freeWeekly,
		SponsorWeeklyMinutes: sponsorWeekly,
		SMTP: server.SMTPConfig{
			Host: os.Getenv("HOMING_SMTP_HOST"),
			Port: smtpPort,
			User: os.Getenv("HOMING_SMTP_USER"),
			Pass: os.Getenv("HOMING_SMTP_PASS"),
			From: os.Getenv("HOMING_SMTP_FROM"),
		},
		GitHubOAuth: server.GitHubOAuthConfig{
			ClientID:     os.Getenv("HOMING_GITHUB_CLIENT_ID"),
			ClientSecret: os.Getenv("HOMING_GITHUB_CLIENT_SECRET"),
			PublicBase:   os.Getenv("HOMING_PUBLIC_BASE_URL"),
		},
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
	fmt.Printf("homing-server %s listening on %s (auth=%s)\n", version, listener.Addr(), *authMode)
	fmt.Printf("websocket: ws://%s/v1/ws\n", listener.Addr())
	if strings.EqualFold(*authMode, server.AuthModeManaged) {
		fmt.Printf("account portal: http://%s/account\n", listener.Addr())
	}
	if strings.TrimSpace(os.Getenv("HOMING_ADMIN_TOKEN")) != "" {
		fmt.Printf("admin console:  http://%s/admin\n", listener.Addr())
	}
	fmt.Printf("database: %s\n", *dbPath)
	if strings.EqualFold(*authMode, server.AuthModeManaged) {
		if freeWeekly > 0 {
			fmt.Printf("weekly limit: free accounts %d min of phone activity per week (HOMING_FREE_WEEKLY_MINUTES)\n", freeWeekly)
		} else {
			fmt.Println("weekly limit: disabled for free accounts")
		}
	}
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

// weeklyMinutesEnv reads a weekly allowance in minutes. Unset uses fallback;
// "0" disables the limit.
func weeklyMinutesEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative number of minutes", key)
	}
	return value, nil
}
