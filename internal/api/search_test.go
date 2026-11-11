package api

import "testing"

func TestSnippetAroundKeepsUTF8Boundaries(t *testing.T) {
	snippet, ok := snippetAround("修正登入按鈕並重跑完整測試", "登入")
	if !ok {
		t.Fatal("expected a match")
	}
	if snippet.Match != "登入" {
		t.Fatalf("match = %q", snippet.Match)
	}
	if snippet.Before != "修正" || snippet.After != "按鈕並重跑完整測試" {
		t.Fatalf("snippet = %+v", snippet)
	}
}

func TestSnippetAroundIsCaseInsensitive(t *testing.T) {
	snippet, ok := snippetAround("Fix the Login button", "login")
	if !ok || snippet.Match != "Login" {
		t.Fatalf("snippet = %+v ok=%v", snippet, ok)
	}
}

func TestSnippetAroundRejectsMissingText(t *testing.T) {
	if _, ok := snippetAround("没有匹配", "不存在"); ok {
		t.Fatal("unexpected match")
	}
	if _, ok := snippetAround("", "x"); ok {
		t.Fatal("empty text should not match")
	}
}
