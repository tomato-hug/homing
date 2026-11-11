package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/cli"
	"github.com/tomato-hug/homing/internal/platform"
)

const (
	serverName       = "homing"
	toolName         = "homing.dispatch"
	latestProtocol   = "2025-06-18"
	maxMCPLineBytes  = 4 << 20
	maxMCPLineInRune = 12000
)

var (
	dispatchTargetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	allowedPresets        = map[string]bool{
		"ask": true, "do": true, "review": true, "handoff": true,
	}
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type dispatchArguments struct {
	TargetAgent string `json:"target_agent"`
	Task        string `json:"task"`
	Preset      string `json:"preset,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

// Server is a stdio MCP bridge for one fixed parent session. Keeping the
// parent out of the tool input means an agent cannot make a sibling task appear
// to belong to a different user conversation.
type Server struct {
	API             *API
	ParentSessionID string
	ParentSource    string
	Version         string
	Out             io.Writer

	mu          sync.Mutex
	source      string
	initialized bool
	encoder     *json.Encoder
}

func (s *Server) Serve(ctx context.Context, in io.Reader) error {
	if s.API == nil {
		return errors.New("MCP API client 未配置")
	}
	if strings.TrimSpace(s.ParentSessionID) == "" {
		return errors.New("缺少固定父会话 id")
	}
	if s.Out == nil {
		return errors.New("MCP 输出流未配置")
	}
	s.encoder = json.NewEncoder(s.Out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), maxMCPLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			if writeErr := s.write(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "Parse error"},
			}); writeErr != nil {
				return writeErr
			}
			continue
		}
		// Notifications have no id; they never receive a response.
		if len(request.ID) == 0 || string(request.ID) == "null" {
			continue
		}
		response := s.handle(ctx, request)
		if err := s.write(response); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 MCP 输入失败：%w", err)
	}
	return nil
}

func (s *Server) handle(ctx context.Context, request rpcRequest) rpcResponse {
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		s.initialized = true
		response.Result = map[string]any{
			"protocolVersion": negotiatedProtocol(request.Params),
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    serverName,
				"version": s.version(),
			},
			"instructions": "只暴露 homing.dispatch。它会把一个新任务交给同一台 Mac 上的另一个 coding agent；结果以协作卡回到固定父会话，不会自动注入父模型。",
		}
		return response
	case "ping":
		response.Result = map[string]any{}
		return response
	case "tools/list":
		if !s.initialized {
			return rpcFailure(request.ID, -32002, "Server not initialized")
		}
		response.Result = map[string]any{"tools": []any{dispatchTool()}}
		return response
	case "tools/call":
		if !s.initialized {
			return rpcFailure(request.ID, -32002, "Server not initialized")
		}
		var params toolCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return rpcFailure(request.ID, -32602, "Invalid tools/call params")
		}
		if params.Name != toolName {
			return rpcFailure(request.ID, -32602, "Unknown tool: "+params.Name)
		}
		result, err := s.callDispatch(ctx, params.Arguments)
		if err != nil {
			response.Result = toolError(err)
			return response
		}
		response.Result = result
		return response
	default:
		return rpcFailure(request.ID, -32601, "Method not found: "+request.Method)
	}
}

func negotiatedProtocol(params json.RawMessage) string {
	var init initializeParams
	if json.Unmarshal(params, &init) == nil {
		switch init.ProtocolVersion {
		case "2024-11-05", "2025-03-26", latestProtocol:
			return init.ProtocolVersion
		}
	}
	return latestProtocol
}

func dispatchTool() map[string]any {
	return map[string]any{
		"name":        toolName,
		"title":       "Dispatch to another coding agent",
		"description": "Start one new child task for another installed coding agent on this Mac. The child runs in the parent session's working directory; ask/do/review return a collaboration card, while handoff returns no result. This tool cannot read transcripts, reply to the parent, forward results, choose another parent, or auto-inject output.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"target_agent": map[string]any{
					"type":        "string",
					"description": "Agent id from Homing's dispatchable list, for example claude or codex.",
					"pattern":     "^[a-z0-9][a-z0-9._-]{0,63}$",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "One self-contained task for the child agent.",
					"minLength":   1,
					"maxLength":   maxMCPLineInRune,
				},
				"preset": map[string]any{
					"type":        "string",
					"enum":        []string{"ask", "do", "review", "handoff"},
					"default":     "ask",
					"description": "ask and review prefer a read-only child; do can modify files; handoff does not return a result card.",
				},
				"dry_run": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "Validate and show the dispatch plan without starting a process.",
				},
			},
			"required": []string{"target_agent", "task"},
		},
		"annotations": map[string]any{
			"readOnlyHint":    false,
			"destructiveHint": true,
			"idempotentHint":  false,
			"openWorldHint":   true,
		},
	}
}

func (s *Server) callDispatch(ctx context.Context, raw json.RawMessage) (any, error) {
	var args dispatchArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("参数必须是 JSON 对象：%w", err)
	}
	target := strings.ToLower(strings.TrimSpace(args.TargetAgent))
	if !dispatchTargetPattern.MatchString(target) {
		return nil, errors.New("target_agent 格式不合法")
	}
	task := strings.TrimSpace(args.Task)
	if task == "" {
		return nil, errors.New("task 不能为空")
	}
	if utf8.RuneCountInString(task) > maxMCPLineInRune {
		return nil, fmt.Errorf("task 超过 %d 字元", maxMCPLineInRune)
	}
	preset := strings.ToLower(strings.TrimSpace(args.Preset))
	if preset == "" {
		preset = "ask"
	}
	if !allowedPresets[preset] {
		return nil, errors.New("preset 只能是 ask / do / review / handoff")
	}
	source, err := s.parentSource(ctx)
	if err != nil {
		return nil, err
	}
	workspace := "readonly"
	switch preset {
	case "do":
		workspace = "auto"
	case "handoff":
		workspace = "shared"
	}
	body := map[string]any{
		"parent_session_id": s.ParentSessionID,
		"parent_source":     source,
		"target_agent":      target,
		"text":              task,
		"preset":            preset,
		"workspace":         workspace,
		"return":            "card",
	}
	if args.DryRun {
		body["dry_run"] = true
	}
	response, err := s.API.Dispatch(ctx, body)
	if err != nil {
		return nil, err
	}
	return dispatchResult(response, args.DryRun, target, preset), nil
}

func (s *Server) parentSource(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.source != "" {
		source := s.source
		s.mu.Unlock()
		return source, nil
	}
	s.mu.Unlock()
	info, err := s.API.Parent(ctx, s.ParentSessionID)
	if err != nil {
		return "", err
	}
	source := strings.ToLower(strings.TrimSpace(info.Source))
	if source == "" {
		return "", errors.New("父会话没有可识别的 Agent 来源")
	}
	expected := strings.ToLower(strings.TrimSpace(s.ParentSource))
	if expected != "" && source != expected {
		return "", fmt.Errorf("父会话来源是 %s，与 MCP 配置的 %s 不一致", source, expected)
	}
	s.mu.Lock()
	s.source = source
	s.mu.Unlock()
	return source, nil
}

func dispatchResult(response map[string]any, dryRun bool, target, preset string) map[string]any {
	dispatchID := stringValue(response["dispatch_id"])
	targetSessionID := stringValue(response["target_session_id"])
	status := stringValue(response["status"])
	if dryRun {
		status = "planned"
	}
	text := fmt.Sprintf("已建立 @%s 的 %s 子任務", target, preset)
	if dryRun {
		text = fmt.Sprintf("@%s 的 %s 派發預覽已通過", target, preset)
	}
	if dispatchID != "" {
		text += "\n派發 id：" + dispatchID
	}
	if targetSessionID != "" {
		text += "\n子會話：" + targetSessionID
	}
	text += "\n結果會以協作卡回到父會話，不會自動注入父 Agent。"
	structured := map[string]any{
		"ok":                true,
		"dry_run":           dryRun,
		"dispatch_id":       dispatchID,
		"target_agent":      target,
		"target_session_id": targetSessionID,
		"preset":            preset,
		"status":            status,
	}
	if dryRun {
		if plan, ok := response["plan"]; ok {
			structured["plan"] = plan
		}
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": structured,
		"isError":           false,
	}
}

func toolError(err error) map[string]any {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "派發失敗"
	}
	structured := map[string]any{"ok": false, "error": message}
	var typed *apiError
	if errors.As(err, &typed) {
		structured["http_status"] = typed.Status
		if typed.Code != "" {
			structured["code"] = typed.Code
		}
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": "派發失敗：" + message}},
		"structuredContent": structured,
		"isError":           true,
	}
}

func rpcFailure(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
}

func (s *Server) write(response rpcResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.encoder == nil {
		s.encoder = json.NewEncoder(s.Out)
	}
	return s.encoder.Encode(response)
}

func (s *Server) version() string {
	if strings.TrimSpace(s.Version) == "" {
		return platform.Version
	}
	return s.Version
}

func stringValue(v any) string {
	value, _ := v.(string)
	return strings.TrimSpace(value)
}

// RunCommand implements `homing mcp`. It is registered through internal/cli so
// the command remains available in the headless and desktop builds alike.
func RunCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("url", envOr("HOMING_MCP_URL", "http://127.0.0.1:18787"),
		"Homing 本机服务地址（只允许回环地址）")
	dataDir := fs.String("data-dir", "", "数据目录（默认与 homing serve 相同）")
	parentSession := fs.String("parent-session", os.Getenv("HOMING_MCP_PARENT_SESSION"),
		"固定的父会话 id；建议通过启动参数或环境变量提供")
	parentSource := fs.String("parent-source", os.Getenv("HOMING_MCP_PARENT_SOURCE"),
		"可选的父会话 Agent 来源；不填则从会话读取并校验")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parentSessionValue := strings.TrimSpace(*parentSession)
	if parentSessionValue == "" {
		return errors.New("用法: homing mcp --parent-session <会话 id> [--url http://127.0.0.1:18787]")
	}
	dir := strings.TrimSpace(*dataDir)
	if dir == "" {
		resolved, err := platform.DataDir()
		if err != nil {
			return err
		}
		dir = resolved
	}
	token := strings.TrimSpace(os.Getenv("HOMING_MCP_TOKEN"))
	if token == "" {
		var err error
		token, err = readOwnerToken(dir)
		if err != nil {
			return err
		}
	}
	client, err := newAPI(*baseURL, token)
	if err != nil {
		return err
	}
	server := &Server{
		API:             client,
		ParentSessionID: parentSessionValue,
		ParentSource:    strings.TrimSpace(*parentSource),
		Version:         platform.Version,
		Out:             stdout,
	}
	return server.Serve(ctx, stdin)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func init() {
	cli.Register(cli.Command{
		Name:    "mcp",
		Summary: "以 stdio MCP 暴露 homing.dispatch（固定父会话，仅回环）",
		Run: func(ctx context.Context, args []string) error {
			return RunCommand(ctx, args, os.Stdin, os.Stdout, os.Stderr)
		},
	})
}
