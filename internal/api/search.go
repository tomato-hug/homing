package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
)

type SearchSnippet struct {
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
}

type SearchHit struct {
	Session Summary       `json:"session"`
	Snippet SearchSnippet `json:"snippet"`
	ItemID  string        `json:"item_id,omitempty"`
}

func init() {
	Register("search", 45, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/search", d.guard(d.handleSearch))
		mux.HandleFunc("GET /api/search/status", d.guard(d.handleSearchIndexStatus))
		mux.HandleFunc("POST /api/search/index", d.guard(d.handleSearchIndexAction))
	})
}

func (d *Deps) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "q 不能为空"})
		return
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope == "" {
		scope = "title"
	}
	if scope != "title" && scope != "content" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "scope 只能是 title 或 content"})
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	limit := clampInt(r.URL.Query().Get("limit"), 50, 1, 200)

	candidates := d.summaries(ctxOf(r), 1000, source, "")
	candidateLimit := len(candidates)
	if scope == "content" {
		d.handleContentSearch(w, r, query, source, limit, candidates)
		return
	}
	hits := make([]SearchHit, 0, limit)
	searched := 0
	for index := 0; index < candidateLimit && len(hits) < limit; index++ {
		summary := candidates[index]
		searched++
		if snippet, ok := searchSummary(summary, query); ok {
			hits = append(hits, SearchHit{Session: summary, Snippet: snippet})
		}
	}
	truncated := len(hits) >= limit && searched < len(candidates)
	WriteJSON(w, http.StatusOK, map[string]any{
		"query":     query,
		"scope":     scope,
		"hits":      hits,
		"searched":  searched,
		"truncated": truncated,
	})
}

func (d *Deps) handleContentSearch(
	w http.ResponseWriter,
	r *http.Request,
	query, source string,
	limit int,
	candidates []Summary,
) {
	if s, ok := d.store(); ok && s.SearchDisabled() {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "正文索引已关闭，可在设置中重新开启",
			"code":  "search_index_disabled",
		})
		return
	}
	updated, syncErr := d.syncSearchIndex(ctxOf(r), candidates)
	if errors.Is(syncErr, errSearchIndexDisabled) {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "正文索引已关闭，可在设置中重新开启",
			"code":  "search_index_disabled",
		})
		return
	}
	if syncErr != nil {
		d.searchFallback(w, r, query, source, limit, candidates, syncErr)
		return
	}
	rows, fallback, searchErr := d.querySearchIndex(ctxOf(r), query, source, limit)
	if errors.Is(searchErr, errSearchIndexDisabled) {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "正文索引已关闭，可在设置中重新开启",
			"code":  "search_index_disabled",
		})
		return
	}
	if searchErr != nil {
		d.searchFallback(w, r, query, source, limit, candidates, searchErr)
		return
	}
	byKey := make(map[string]Summary, len(candidates))
	for _, summary := range candidates {
		byKey[summary.Source+"\x00"+summary.SessionID] = summary
	}
	hits := make([]SearchHit, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		summary, ok := byKey[row.Source+"\x00"+row.SessionID]
		if !ok {
			continue
		}
		snippet, ok := snippetAround(row.Body, query)
		if !ok {
			continue
		}
		key := row.Source + "\x00" + row.SessionID + "\x00" + row.ItemID
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, SearchHit{
			Session: summary,
			Snippet: snippet,
			ItemID:  firstNonEmpty(row.ItemID, "seq-"+strconv.Itoa(row.Seq)),
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"query":          query,
		"scope":          "content",
		"hits":           hits,
		"searched":       len(candidates),
		"truncated":      len(rows) >= limit,
		"indexed":        updated,
		"fallback_like":  fallback,
		"indexed_search": true,
		"generated_at":   nowISO(),
	})
}

func (d *Deps) searchFallback(
	w http.ResponseWriter,
	r *http.Request,
	query, source string,
	limit int,
	candidates []Summary,
	cause error,
) {
	hits := make([]SearchHit, 0, limit)
	searched := 0
	for _, summary := range candidates {
		if source != "" && summary.Source != source {
			continue
		}
		if len(hits) >= limit {
			break
		}
		searched++
		if itemID, snippet, ok := searchSessionItems(ctxOf(r), summary, query); ok {
			hits = append(hits, SearchHit{
				Session: summary, Snippet: snippet, ItemID: itemID,
			})
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"query":           query,
		"scope":           "content",
		"hits":            hits,
		"searched":        searched,
		"truncated":       len(hits) >= limit,
		"indexed_search":  false,
		"degraded":        true,
		"degraded_reason": cause.Error(),
		"generated_at":    nowISO(),
	})
}

func searchSummary(summary Summary, query string) (SearchSnippet, bool) {
	fields := []string{summary.Title}
	if summary.Project != nil {
		fields = append(fields, *summary.Project)
	}
	if summary.Cwd != nil {
		fields = append(fields, *summary.Cwd)
	}
	for _, field := range fields {
		if snippet, ok := snippetAround(field, query); ok {
			return snippet, true
		}
	}
	return SearchSnippet{}, false
}

func searchSessionItems(
	ctx context.Context,
	summary Summary,
	query string,
) (string, SearchSnippet, bool) {
	found, ok := agent.Get(ctx, summary.Source)
	if !ok {
		return "", SearchSnippet{}, false
	}
	reader, ok := found.(agent.ItemReader)
	if !ok {
		return "", SearchSnippet{}, false
	}
	items, err := reader.ReadItems(ctx, summary.SessionID, 300)
	if err != nil {
		return "", SearchSnippet{}, false
	}
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if snippet, ok := snippetAround(text, query); ok {
			return item.ID, snippet, true
		}
	}
	return "", SearchSnippet{}, false
}

func snippetAround(text, query string) (SearchSnippet, bool) {
	text = strings.TrimSpace(text)
	if text == "" || query == "" {
		return SearchSnippet{}, false
	}
	lowerQuery := strings.ToLower(query)
	if lowerQuery == "" {
		return SearchSnippet{}, false
	}
	runes := []rune(text)
	var lowerBuf strings.Builder
	lowerBuf.Grow(len(text))
	byteToRune := make([]int, 0, len(text))
	for idx, r := range runes {
		folded := strings.ToLower(string(r))
		for i := 0; i < len(folded); i++ {
			byteToRune = append(byteToRune, idx)
		}
		lowerBuf.WriteString(folded)
	}
	byteIndex := strings.Index(lowerBuf.String(), lowerQuery)
	if byteIndex < 0 {
		return SearchSnippet{}, false
	}
	runeIndex := byteToRune[byteIndex]
	matchEnd := byteToRune[byteIndex+len(lowerQuery)-1] + 1
	if matchEnd > len(runes) {
		matchEnd = len(runes)
	}
	start := runeIndex - 32
	if start < 0 {
		start = 0
	}
	end := matchEnd + 80
	if end > len(runes) {
		end = len(runes)
	}
	return SearchSnippet{
		Before: strings.TrimSpace(string(runes[start:runeIndex])),
		Match:  string(runes[runeIndex:matchEnd]),
		After:  strings.TrimSpace(string(runes[matchEnd:end])),
	}, true
}
