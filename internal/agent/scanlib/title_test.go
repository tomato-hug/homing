package scanlib

import "testing"

func TestTitleCleansAgentGeneratedPrefixes(t *testing.T) {
	cases := []string{
		"标题：创建鹈鹕骑自行车SVG动画",
		"会话标题: 创建鹈鹕骑自行车SVG动画",
		"# 標題：创建鹈鹕骑自行车SVG动画",
		"Title: 创建鹈鹕骑自行车SVG动画",
	}
	for _, raw := range cases {
		got, source := Title("codex", "demo-session-title", raw, "agent", "", "/tmp/demo-project")
		if got != "创建鹈鹕骑自行车SVG动画" {
			t.Errorf("Title(%q) = %q", raw, got)
		}
		if source != "agent" {
			t.Errorf("Title(%q) source = %q, want agent", raw, source)
		}
	}
}

func TestTitleKeepsOrdinaryNativeTitle(t *testing.T) {
	got, _ := Title("claude", "demo-session-title", "标题目录重构", "agent", "", "/tmp/demo-project")
	if got != "标题目录重构" {
		t.Fatalf("ordinary title changed: %q", got)
	}
}

func TestTitleKeepsUserNamedPrefix(t *testing.T) {
	raw := "标题：这是我自己取的名字"
	got, source := Title("claude", "demo-session-title", raw, "custom", "", "/tmp/demo-project")
	if got != raw {
		t.Fatalf("custom title changed: %q", got)
	}
	if source != "custom" {
		t.Fatalf("custom title source = %q", source)
	}
}
