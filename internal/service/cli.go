package service

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/cli"
	"github.com/tomato-hug/homing/internal/platform"
)

// The CLI side of this package: `homing ls`. It reads the same files `homing
// serve` writes, so it works whether the server is running or not.

func init() {
	cli.Register(cli.Command{
		Name:    "ls",
		Summary: "列出会话：来源、标题、状态、未读、最后活动",
		Run:     runLS,
	})
}

func runLS(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	var (
		dataDir  = fs.String("data-dir", "", "数据目录（默认按操作系统）")
		source   = fs.String("source", "", "只看某个来源（codex/claude/workbuddy/opencode/zcode/adapter）")
		limit    = fs.Int("n", 20, "最多显示多少行")
		archived = fs.Bool("archived", false, "连已归档的会话一起列出")
		wide     = fs.Bool("w", false, "不截断标题")
	)
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
	deps := &api.Deps{Log: quietLogger(), DataDir: *dataDir}
	rows := deps.Summaries(ctx, 200, *source, "")
	if !*archived {
		kept := rows[:0]
		for _, row := range rows {
			if !row.Archived {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if len(rows) > *limit {
		rows = rows[:*limit]
	}
	if len(rows) == 0 {
		fmt.Fprintf(os.Stdout, "没有会话。数据目录：%s\n", *dataDir)
		return nil
	}
	printSessions(os.Stdout, rows, *wide)
	return nil
}

// printSessions writes one line per session. Columns are the ones you actually
// triage by: which agent, what it is, whether it needs you, when it moved.
func printSessions(w io.Writer, rows []api.Summary, wide bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "来源\t状态\t未读\t最后活动\t标题")
	for _, row := range rows {
		title := row.Title
		if title == "" {
			title = "（无标题）"
		}
		if !wide {
			title = truncate(title, 48)
		}
		if row.Pinned {
			title = "* " + title
		}
		unread := ""
		if row.Unread > 0 {
			unread = fmt.Sprintf("%d", row.Unread)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			row.Source,
			statusLabel(row.Status),
			unread,
			ago(derefString(row.LastTS)),
			title,
		)
	}
	tw.Flush()
}

// statusLabel turns the machine status into something short enough for a column.
func statusLabel(status string) string {
	switch status {
	case "permission":
		return "待授权"
	case "needs_input":
		return "等回复"
	case "running":
		return "运行中"
	case "error":
		return "出错"
	case "done":
		return "完成"
	case "":
		return "-"
	}
	return status
}

// ago renders a timestamp as a rough age; exact times are noise in a list.
func ago(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

// quietLogger keeps the CLI from printing server-side log lines to the terminal.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
