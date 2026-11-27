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

func TestSnippetAroundUnicodeCaseFoldLengthChange(t *testing.T) {
	var b [40]rune
	for i := range b {
		b[i] = 'İ'
	}
	text := string(b[:]) + "LoginButton異常排查"
	snippet, ok := snippetAround(text, "login")
	if !ok {
		t.Fatal("expected case-insensitive match after 'İ' prefix")
	}
	if snippet.Match != "Login" {
		t.Fatalf("match = %q, want %q", snippet.Match, "Login")
	}
	if snippet.After != "Button異常排查" {
		t.Fatalf("after = %q, want %q", snippet.After, "Button異常排查")
	}

	kelvinSnippet, ok := snippetAround("感測器回報 Kelvin 數值超標", "kelvin")
	if !ok {
		t.Fatal("expected case-insensitive match for Kelvin sign")
	}
	if kelvinSnippet.Match != "Kelvin" {
		t.Fatalf("kelvin match = %q, want %q", kelvinSnippet.Match, "Kelvin")
	}

	var expand [40]rune
	for i := range expand {
		expand[i] = 'Ⱥ'
	}
	expandSnippet, ok := snippetAround(string(expand[:])+"Login", "login")
	if !ok || expandSnippet.Match != "Login" {
		t.Fatalf("expandSnippet = %+v ok=%v, want Match=Login", expandSnippet, ok)
	}
}
