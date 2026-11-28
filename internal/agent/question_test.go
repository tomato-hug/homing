package agent

import "testing"

func TestParseInteractiveQuestions_WorkBuddyAskUserQuestionJSON(t *testing.T) {
	raw := `{"questions":[{"header":"部署目标","multiSelect":false,"options":[{"description":"直接部署到测试环境","label":"Staging"},{"description":"发布生产版本","label":"Production"}],"question":"请选择这次要发布的环境？"},{"header":"附加检查","multiSelect":true,"options":[{"label":"跑全量单测"},{"label":"跳过构建"}],"question":"需要执行哪些检查？"}]}`

	if !IsInteractiveQuestionTool("AskUserQuestion") {
		t.Fatal("expected AskUserQuestion to be interactive")
	}
	if !IsInteractiveQuestionTool("request_user_input") {
		t.Fatal("expected request_user_input to be interactive")
	}
	if !IsInteractiveQuestionTool("ExitPlanMode") {
		t.Fatal("expected ExitPlanMode to be interactive")
	}
	if IsInteractiveQuestionTool("Bash") {
		t.Fatal("did not expect Bash to be interactive")
	}

	qs, summary := ParseInteractiveQuestions("AskUserQuestion", raw)
	if len(qs) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(qs))
	}
	if qs[0].Header != "部署目标" || qs[0].MultiSelect {
		t.Fatalf("unexpected q0: %+v", qs[0])
	}
	if len(qs[0].Options) != 2 || qs[0].Options[0].Label != "Staging" || qs[0].Options[0].Description != "直接部署到测试环境" {
		t.Fatalf("unexpected q0 options: %+v", qs[0].Options)
	}
	if !qs[1].MultiSelect || len(qs[1].Options) != 2 {
		t.Fatalf("unexpected q1: %+v", qs[1])
	}
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}

	title, body := PrimaryQuestionPrompt(qs, "")
	if title != "待确认：部署目标" {
		t.Fatalf("unexpected title %q", title)
	}
	if body == "" {
		t.Fatal("expected non-empty prompt body")
	}
}

func TestParseInteractiveQuestions_ExitPlanModeDefaults(t *testing.T) {
	qs, _ := ParseInteractiveQuestions("ExitPlanMode", map[string]any{})
	if len(qs) != 1 || len(qs[0].Options) != 2 {
		t.Fatalf("expected default ExitPlanMode prompt with 2 options, got %+v", qs)
	}
}
