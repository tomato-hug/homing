package driver

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RunDriveTest implements `homing agents drive-test <id>`. It only creates a
// new temporary project and a new ACP session; it never opens or replies to a
// user's existing conversation.
func RunDriveTest(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("用法: homing agents drive-test <agent-id> [--cwd /tmp/...]")
	}
	id := strings.ToLower(strings.TrimSpace(args[0]))
	spec, ok := SpecFor(id)
	if !ok {
		return fmt.Errorf("未知 ACP Agent %q（可用: %s）", id, strings.Join(specIDs(), ", "))
	}
	fs := flag.NewFlagSet("drive-test", flag.ContinueOnError)
	cwdFlag := fs.String("cwd", "", "临时测试目录（必须位于系统临时目录下）")
	timeout := fs.Duration("timeout", 90*time.Second, "握手与单回合超时")
	prompt := fs.String("prompt", "Reply with exactly HOMING_ACP_OK. Do not use tools.", "无副作用测试提示")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cwd := strings.TrimSpace(*cwdFlag)
	var cleanup func()
	if cwd == "" {
		dir, err := os.MkdirTemp("", "homing-acp-*")
		if err != nil {
			return err
		}
		cwd = dir
		cleanup = func() { _ = os.RemoveAll(dir) }
	} else {
		if err := ensureTempCwd(cwd); err != nil {
			return err
		}
	}
	if cleanup != nil {
		defer cleanup()
	}
	if !spec.Installed() {
		return fmt.Errorf("找不到 %s；请先安装 %s", spec.Command[0], spec.Label)
	}

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	fmt.Printf("Agent: %s (%s)\n", spec.Label, spec.ID)
	fmt.Printf("命令: %s\n", spec.CommandString())
	fmt.Printf("目录: %s\n", cwd)
	if spec.Experimental {
		fmt.Println("状态: experimental")
	}
	if spec.SetupHint != "" {
		fmt.Printf("前置: %s\n", spec.SetupHint)
	}

	var updateMu sync.Mutex
	counts := map[string]int{}
	chars := 0
	permissionCount := 0
	client, err := Start(runCtx, spec, cwd, func(update Update) {
		updateMu.Lock()
		counts[update.Kind]++
		chars += len([]rune(update.Text))
		updateMu.Unlock()
	}, func(_ PermissionRequest) PermissionDecision {
		updateMu.Lock()
		permissionCount++
		updateMu.Unlock()
		return PermissionDecision{Allow: false}
	})
	if err != nil {
		return err
	}
	defer client.Close()

	initResult, err := client.Initialize(runCtx)
	if err != nil {
		if tail := sanitizeStderr(client.StderrTail()); tail != "" {
			return fmt.Errorf("initialize 失败: %w（stderr: %s）", err, tail)
		}
		return fmt.Errorf("initialize 失败: %w", err)
	}
	fmt.Printf("握手: ok（protocol=%v）\n", initResult["protocolVersion"])
	if methods, ok := initResult["authMethods"].([]any); ok && len(methods) > 0 {
		fmt.Printf("認證: Agent 要求先登入（%d 個方法），本次不修改任何憑證\n", len(methods))
	}

	sessionID, _, err := client.NewSession(runCtx, cwd)
	if err != nil {
		if tail := sanitizeStderr(client.StderrTail()); tail != "" {
			return fmt.Errorf("session/new 失败: %w（stderr: %s）", err, tail)
		}
		return fmt.Errorf("session/new 失败: %w", err)
	}
	fmt.Printf("会话: %s\n", maskID(sessionID))

	result, err := client.Prompt(runCtx, sessionID, *prompt)
	if err != nil {
		tail := sanitizeStderr(client.StderrTail())
		if tail != "" {
			return fmt.Errorf("session/prompt 失败: %w（stderr: %s）", err, tail)
		}
		return fmt.Errorf("session/prompt 失败: %w", err)
	}
	updateMu.Lock()
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	fmt.Printf("回合: stopReason=%v\n", result["stopReason"])
	fmt.Printf("更新: %d 类", len(kinds))
	for _, kind := range kinds {
		fmt.Printf(" · %s=%d", kind, counts[kind])
	}
	fmt.Printf("（共 %d 字，内容未打印）\n", chars)
	fmt.Printf("授权请求: %d（本次自动拒绝）\n", permissionCount)
	updateMu.Unlock()
	fmt.Println("結論: ACP 握手與單回合通過。")
	return nil
}

func specIDs() []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	return out
}

func ensureTempCwd(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	allowed := false
	for _, root := range []string{"/tmp", os.TempDir()} {
		root, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		relative, relErr := filepath.Rel(root, absolute)
		if relErr == nil && relative != "." && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return errors.New("drive-test 只允许在系统临时目录下运行，避免碰到真实项目")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return errors.New("--cwd 不是可用目录")
	}
	return nil
}

func maskID(id string) string {
	runes := []rune(id)
	if len(runes) <= 12 {
		return id
	}
	return string(runes[:8]) + "…" + string(runes[len(runes)-4:])
}

func sanitizeStderr(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	lines := strings.Split(value, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	joined := strings.TrimSpace(strings.Join(lines, " | "))
	runes := []rune(joined)
	if len(runes) > 500 {
		joined = string(runes[:500])
	}
	return joined
}
