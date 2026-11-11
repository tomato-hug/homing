package cloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/tomato-hug/homing/internal/cli"
)

func init() {
	cli.Register(cli.Command{
		Name:    "pair",
		Summary: "打印配对二维码和 6 位码",
		Run:     runPair,
	})
}

func runPair(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	serve := fs.String("serve", envOr("HOMING_SERVE", "http://127.0.0.1:18787"),
		"running `homing serve` base URL")
	server := fs.String("server", "", "WebSocket URL the phone should use (default: LAN address)")
	lanIP := fs.String("lan-ip", "", "LAN IPv4 address to put in the QR code")
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait for a phone")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	serveURL, err := normalizeServeURL(*serve)
	if err != nil {
		return err
	}
	wsServer := *server
	var lanErr error
	if wsServer == "" {
		wsServer, lanErr = lanWebSocketURL(serveURL, *lanIP)
	}
	fmt.Printf("homing serve: %s\n", serveURL)

	var started pairStartResponse
	if err := callJSON(ctx, http.MethodPost, serveURL+"/api/cloud/pair/start",
		map[string]string{"server": wsServer}, &started); err != nil {
		return fmt.Errorf("连接 homing serve 失败（先运行 `homing serve`）: %w", err)
	}
	mode := "局域网直连"
	if started.Mode == "cloud" {
		mode = "云中转"
	}
	fmt.Printf("模式: %s\n", mode)
	if started.Mode == "lan" && wsServer == "" {
		if lanErr != nil {
			return fmt.Errorf("局域网配对需要手机可访问的地址: %w", lanErr)
		}
		return errors.New("局域网配对需要 --lan-ip 或 --server")
	}
	phoneServer := started.Server
	if phoneServer == "" {
		phoneServer = wsServer
	}
	if phoneServer != "" {
		fmt.Printf("手机将连接: %s\n", phoneServer)
	}
	if started.URI == "" {
		return errors.New("homing serve 返回了空的配对 URI")
	}
	fmt.Println()
	qrterminal.GenerateHalfBlock(started.URI, qrterminal.L, os.Stdout)
	fmt.Printf("\n配对码: %s\n", started.Code6)
	fmt.Printf("电脑指纹: %s\n", started.Fingerprint)
	fmt.Printf("配对 URI: %s\n", started.URI)
	if started.Mode == "lan" {
		fmt.Println("提示: 手机需要能访问上面的 ws:// 地址；若 serve 只监听 127.0.0.1，请改用 --addr 0.0.0.0:18787。")
	}
	fmt.Println("\n在手机 Homing 中扫码或输入配对码；下面会显示四个校验词。")

	return waitForPair(ctx, serveURL, *timeout)
}

func waitForPair(ctx context.Context, serveURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	seen := make(map[string]bool)
	reader := bufio.NewReader(os.Stdin)
	for {
		if ctx.Err() != nil {
			fmt.Println("\n已取消。")
			return nil
		}
		var list pairRequestList
		if err := callJSON(ctx, http.MethodGet, serveURL+"/api/cloud/pair/requests", nil, &list); err != nil {
			return err
		}
		for _, request := range list.Requests {
			if seen[request.ClientID] {
				continue
			}
			seen[request.ClientID] = true
			name := request.Name
			if name == "" {
				name = request.ClientID
			}
			platform := request.Platform
			if platform == "" {
				platform = "unknown"
			}
			fmt.Printf("\n手机 %s（%s）请求配对\n", name, platform)
			fmt.Printf("四词校验: %s\n", strings.Join(request.SAS, " "))
			fmt.Print("和电脑上显示的四词一致就输入 y 接受，直接回车拒绝: ")
			answer, _ := reader.ReadString('\n')
			accept := isYes(answer)
			var result map[string]any
			if err := callJSON(ctx, http.MethodPost, serveURL+"/api/cloud/pair/decision",
				pairDecision{ClientID: request.ClientID, Accept: accept}, &result); err != nil {
				return err
			}
			if accept {
				fmt.Printf("已配对: %s\n", name)
				return nil
			}
			fmt.Println("已拒绝。继续等待其它手机…")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待配对超时（%s）", timeout)
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func callJSON(ctx context.Context, method, endpoint string, requestBody, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		if failure.Error != "" {
			return fmt.Errorf("%s: %s", response.Status, failure.Error)
		}
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	if responseBody == nil {
		return nil
	}
	return json.Unmarshal(raw, responseBody)
}

func normalizeServeURL(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", errors.New("--serve 不能为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("无效的 --serve: %w", err)
	}
	if parsed.Host == "" {
		return "", errors.New("无效的 --serve: 缺少主机")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}

func lanWebSocketURL(serveURL, overrideIP string) (string, error) {
	parsed, err := url.Parse(serveURL)
	if err != nil {
		return "", err
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
	}
	ip := strings.TrimSpace(overrideIP)
	if ip == "" {
		ip = detectLANIP()
	}
	if ip == "" {
		return "", errors.New("找不到局域网 IPv4 地址，请用 --lan-ip 或 --server 指定")
	}
	return "ws://" + net.JoinHostPort(ip, port) + "/v1/ws", nil
}

func detectLANIP() string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	fallback := ""
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if !ok {
			continue
		}
		ip := network.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.IsPrivate() {
			return ip.String()
		}
		if fallback == "" && ip.IsGlobalUnicast() {
			fallback = ip.String()
		}
	}
	return fallback
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "是", "确认", "接受":
		return true
	}
	return false
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
