package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/searchindex"
)

func (d *Deps) searchIndex() (*searchindex.Index, error) {
	d.searchMu.Lock()
	defer d.searchMu.Unlock()
	if d.searchValue != nil {
		return d.searchValue, nil
	}
	if d.searchErr != nil {
		return nil, d.searchErr
	}
	paths := platform.NewPaths(d.DataDir)
	index, err := searchindex.Open(paths.Index)
	if err != nil {
		d.searchErr = err
		return nil, err
	}
	d.searchValue = index
	return index, nil
}

func (d *Deps) resetSearchIndex() {
	d.searchMu.Lock()
	defer d.searchMu.Unlock()
	if d.searchValue != nil {
		_ = d.searchValue.Close()
	}
	d.searchValue = nil
	d.searchErr = nil
}

func (d *Deps) syncSearchIndex(ctx context.Context, summaries []Summary) (int, error) {
	index, err := d.searchIndex()
	if err != nil {
		return 0, err
	}
	versions, err := index.Versions(ctx)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, summary := range summaries {
		if summary.Archived || summary.IsEmpty || summary.SessionID == "" {
			continue
		}
		version := searchIndexVersion(summary)
		key := summary.Source + "\x00" + summary.SessionID
		if versions[key] == version {
			continue
		}
		items := readSearchableItems(ctx, summary)
		if err := index.ReplaceSession(
			ctx, summary.Source, summary.SessionID, version,
			searchDocuments(items),
		); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func searchIndexVersion(summary Summary) string {
	return fmt.Sprintf("%s|%d|%s|%s",
		deref(summary.LastTS), summary.EventCount,
		deref(summary.Model), deref(summary.Cwd),
	)
}

func readSearchableItems(ctx context.Context, summary Summary) []agent.Item {
	if found, ok := agent.Get(ctx, summary.Source); ok {
		if reader, ok := found.(agent.ItemReader); ok {
			if items, err := reader.ReadItems(ctx, summary.SessionID, 1000); err == nil {
				return mergeDriverItems(items, driver.Default().Items(summary.SessionID, 1000))
			}
		}
	}
	return driver.Default().Items(summary.SessionID, 1000)
}

func searchDocuments(items []agent.Item) []searchindex.Document {
	documents := make([]searchindex.Document, 0, len(items))
	remaining := searchindex.MaxSessionBytes
	for index := len(items) - 1; index >= 0 && remaining > 0; index-- {
		item := items[index]
		if item.Role != "user" && item.Role != "assistant" {
			continue
		}
		body := strings.TrimSpace(item.Text)
		if body == "" || (item.Type != "" && item.Type != "message" && item.Type != "reasoning") {
			continue
		}
		if len(body) > remaining {
			body = tailBytes(body, remaining)
			if body == "" {
				break
			}
		}
		documents = append(documents, searchindex.Document{
			ItemID: item.ID,
			Seq:    item.Seq,
			Role:   item.Role,
			Body:   body,
		})
		remaining -= len(body)
	}
	for left, right := 0, len(documents)-1; left < right; left, right = left+1, right-1 {
		documents[left], documents[right] = documents[right], documents[left]
	}
	return documents
}

func tailBytes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[len(text)-limit]) {
		limit--
	}
	return strings.TrimSpace(text[len(text)-limit:])
}

func (d *Deps) handleSearchIndexStatus(w http.ResponseWriter, r *http.Request) {
	disabled := false
	if s, ok := d.store(); ok {
		disabled = s.SearchDisabled()
	}
	paths := platform.NewPaths(d.DataDir)
	stats := searchindex.Stats{Path: paths.Index, Trigram: true}
	if _, err := os.Stat(paths.Index); err == nil {
		index, openErr := d.searchIndex()
		if openErr != nil {
			WriteJSON(w, http.StatusOK, map[string]any{
				"enabled": !disabled,
				"stats":   searchindex.Stats{Path: paths.Index, Trigram: false, OpenError: openErr.Error()},
				"error":   openErr.Error(),
				"fts5":    false,
			})
			return
		}
		loaded, statsErr := index.Stats(ctxOf(r))
		if statsErr != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": statsErr.Error()})
			return
		}
		stats = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": !disabled,
			"stats":   searchindex.Stats{Path: paths.Index, Trigram: true, OpenError: err.Error()},
			"error":   err.Error(),
			"fts5":    false,
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"enabled": !disabled,
		"stats":   stats,
		"fts5":    stats.Trigram,
	})
}

func (d *Deps) handleSearchIndexAction(w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "drive") {
		return
	}
	if !d.CheckRate(w, r, "search_index", 10, time.Minute) {
		return
	}
	body := readBody(r)
	action := strings.ToLower(strings.TrimSpace(stringOr(body["action"], "")))
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	switch action {
	case "enable", "disable":
		if err := s.SetSearchDisabled(action == "disable"); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
			return
		}
		d.Audit(r, "search_index_"+action, nil)
		WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "enabled": action == "enable",
		})
	case "rebuild":
		if err := s.SetSearchDisabled(false); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
			return
		}
		if index, err := d.searchIndex(); err == nil {
			_ = index.Clear(ctxOf(r))
		}
		summaries := d.summaries(ctxOf(r), 1000, "", "")
		updated, err := d.syncSearchIndex(ctxOf(r), summaries)
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.Audit(r, "search_index_rebuild", map[string]any{"sessions": updated})
		WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "enabled": true, "indexed": updated,
		})
	case "delete":
		if err := s.SetSearchDisabled(true); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
			return
		}
		d.resetSearchIndex()
		if err := searchindex.Remove(platform.NewPaths(d.DataDir).Index); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.Audit(r, "search_index_delete", nil)
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": false})
	default:
		WriteJSON(w, http.StatusBadRequest, map[string]any{
			"error": "action 只能是 enable / disable / rebuild / delete",
		})
	}
}
