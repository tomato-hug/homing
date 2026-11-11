package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/api"
)

var ingestReplyOnce sync.Once

func registerIngestReplyTransport() {
	ingestReplyOnce.Do(func() {
		api.RegisterReplyTransport(func(ctx context.Context, source, sessionID, text string) (string, string, bool) {
			if ingest == nil {
				return "", "", false
			}
			token := ingest.ReplyChannel(source)
			if token == nil {
				return "", "", false
			}
			return deliverIngestReply(ctx, token, source, sessionID, text)
		})
	})
}

func deliverIngestReply(ctx context.Context, token *IngestToken, source, sessionID, text string) (string, string, bool) {
	if token == nil || token.Reply == nil || strings.TrimSpace(token.Reply.URL) == "" {
		return "", "", false
	}
	timeout := 10 * time.Second
	if token.Reply.TimeoutMS > 0 {
		timeout = time.Duration(token.Reply.TimeoutMS) * time.Millisecond
		if timeout > time.Minute {
			timeout = time.Minute
		}
	}
	body, err := json.Marshal(map[string]string{
		"agent":      source,
		"session_id": sessionID,
		"text":       text,
	})
	if err != nil {
		return "failed", "生成回调请求失败：" + err.Error(), true
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, token.Reply.URL, bytes.NewReader(body))
	if err != nil {
		return "failed", "回调地址无效：" + err.Error(), true
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "failed", fmt.Sprintf("回调失败：%v", err), true
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(raw))
		if detail != "" {
			detail = "：" + detail
		}
		return "failed", fmt.Sprintf("回调返回 %d%s", resp.StatusCode, detail), true
	}
	return "injected", "已回调 " + source, true
}
