package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tomato-hug/homing/internal/cli"
	"github.com/tomato-hug/homing/internal/driver"
)

func init() {
	cli.Register(cli.Command{
		Name:    "agents",
		Summary: "检查清单或验证 ACP 驱动（homing agents test / drive-test）",
		Run:     runAgents,
	})
}

func runAgents(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("用法: homing agents <test|drive-test> ...")
	}
	switch args[0] {
	case "test":
		if len(args) != 2 {
			return errors.New("用法: homing agents test <清单路径>")
		}
		return testManifest(ctx, args[1])
	case "drive-test":
		return driver.RunDriveTest(ctx, args[1:])
	default:
		return fmt.Errorf("不认识的 agents 子命令 %q（可用: test, drive-test）", args[0])
	}
}

// testManifest loads one manifest, prints every validation problem, then reads
// the session list and shows the count plus the first five titles. It never
// prints message content.
func testManifest(ctx context.Context, path string) error {
	manifest, problems, err := LoadManifest(path)
	if err != nil {
		return err
	}
	fmt.Printf("清单: %s\n", path)
	if manifest != nil {
		name := manifest.Name
		if name == "" {
			name = manifest.ID
		}
		fmt.Printf("agent: %s (%s)\n", manifest.ID, name)
	}
	if len(problems) == 0 {
		fmt.Println("校验: 通过")
	} else {
		fmt.Printf("校验问题 (%d):\n", len(problems))
		for _, problem := range problems {
			fmt.Printf("  - %s\n", problem)
		}
	}
	if manifest == nil {
		return errors.New("清单没有加载出内容")
	}

	agent := NewManifestAgent(manifest)
	sessions, listErr := agent.ListSessions(ctx, 0)
	if listErr != nil {
		fmt.Printf("读取会话失败: %v\n", listErr)
	} else {
		fmt.Printf("会话数: %d\n", len(sessions))
		limit := len(sessions)
		if limit > 5 {
			limit = 5
		}
		for index := 0; index < limit; index++ {
			title := strings.TrimSpace(sessions[index].Title)
			if title == "" {
				title = "(无标题)"
			}
			fmt.Printf("%d. %s\n", index+1, truncateTitle(title, 40))
		}
		if len(sessions) > limit {
			fmt.Printf("… 仅显示前 %d 条\n", limit)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("清单有 %d 个校验问题", len(problems))
	}
	return nil
}

func truncateTitle(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
