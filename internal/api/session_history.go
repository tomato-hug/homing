package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
	"github.com/tomato-hug/homing/internal/store"
)

// ItemReader promises a tail, not a complete transcript. These bounds never
// imply the beginning of a session, even when a reader returns fewer items.
const historyReadLimit = 20000
const historyPageBytes = 4 << 20

type historyItem struct {
	agent.Item
	ItemKey     string `json:"item_key"`
	fingerprint string
	duplicates  int
}

type historyCursor struct {
	Version     int    `json:"v"`
	Source      string `json:"s"`
	Session     string `json:"c"`
	Anchor      int64  `json:"a"`
	Key         string `json:"k"`
	Fingerprint string `json:"f"`
	Duplicates  int    `json:"d"`
}

// handleSessionHistory is opt-in. Legacy callers retain the original response
// shape and limits. All reading here is local; only the selected page is sent.
func (d *Deps) handleSessionHistory(w http.ResponseWriter, r *http.Request, session string) {
	q := r.URL.Query()
	source := strings.TrimSpace(q.Get("source"))
	anchorID, err := strconv.ParseInt(q.Get("anchor_event_id"), 10, 64)
	before := q.Get("before")
	if source == "" || err != nil || anchorID <= 0 || q.Has("after_seq") || q.Has("limit") || (q.Has("turns") && (before == "" || q.Get("turns") != "10")) || (q.Has("before") && before == "") {
		writeHistoryError(w, 400, "invalid_history_request", "历史请求需要来源和通知锚点；前翻每次为 10 轮")
		return
	}
	s, ok := d.store()
	if !ok {
		writeHistoryError(w, 503, "history_store_unavailable", "通知记录暂时不可用")
		return
	}
	var cursor historyCursor
	if before != "" {
		cursor, err = decodeHistoryCursor(before, s.Token())
		if err != nil || cursor.Source != source || cursor.Session != session || cursor.Anchor != anchorID {
			writeHistoryError(w, 400, "invalid_history_cursor", "历史游标不属于这个会话或已损坏")
			return
		}
	}
	var anchor *store.Event
	for _, event := range s.Events() {
		if event.ID == anchorID {
			copy := event
			anchor = &copy
			break
		}
	}
	if anchor == nil {
		writeHistoryError(w, 410, "history_anchor_expired", "通知锚点已不在电脑的保留记录中")
		return
	}
	if anchor.Source != source || anchor.SessionID != session {
		writeHistoryError(w, 409, "history_source_mismatch", "通知锚点与来源或会话不一致")
		return
	}
	anchorTime, err := time.Parse(time.RFC3339Nano, anchor.Ts)
	if err != nil {
		writeHistoryError(w, 409, "history_anchor_unresolved", "通知锚点没有可验证的时间")
		return
	}
	raw, status, code := readHistoryWindow(r, source, session)
	if code != "" {
		writeHistoryError(w, status, code, "当前来源无法提供可验证的历史窗口")
		return
	}
	items := keyHistoryItems(raw, source, session)
	anchorIndex := -1
	if text := strings.TrimSpace(anchor.Text); text != "" {
		for i, item := range items {
			ts, err := time.Parse(time.RFC3339Nano, item.TS)
			if err == nil && !ts.After(anchorTime) && item.Role == "assistant" && (item.Type == "message" || item.Type == "assistant") && !item.Partial && strings.TrimSpace(item.Text) == text {
				if anchorIndex >= 0 {
					anchorIndex = -2
					break
				}
				anchorIndex = i
			}
		}
	}
	anchorStatus := "timestamp_only"
	if anchorIndex >= 0 {
		anchorStatus = "matched"
	}
	start, rounds := anchorIndex, 0
	page := make([]historyItem, 0)
	if before == "" {
		// Matching the notified output never grants access to adjacent older rows:
		// hook delivery can lag, and those rows may include private user prompts.
		for i, item := range items {
			ts, err := time.Parse(time.RFC3339Nano, item.TS)
			if i == anchorIndex || (err == nil && ts.After(anchorTime)) {
				if start < 0 {
					start = i
				}
				page = append(page, item)
			}
		}
	} else {
		boundary := -1
		for i, item := range items {
			if item.ItemKey == cursor.Key && item.fingerprint == cursor.Fingerprint && item.duplicates == cursor.Duplicates {
				boundary = i
				break
			}
		}
		if boundary < 0 {
			writeHistoryError(w, 409, "history_cursor_expired", "游标对应条目已变化或离开可读取窗口")
			return
		}
		var pageErr string
		start, rounds, page, pageErr = previousHistoryRounds(items, boundary)
		if pageErr != "" {
			writeHistoryError(w, 409, pageErr, "当前窗口无法证明更早的完整轮次；已加载内容保留")
			return
		}
	}
	// Apply reasoning policy after finding user boundaries. It must not change
	// cursor positions or the definition of a conversational round.
	visible := make([]historyItem, 0, len(page))
	for _, item := range page {
		if !itemIsReasoning(item.Item) || queryBool(r, "include_reasoning") {
			visible = append(visible, item)
		}
	}
	encoded, err := json.Marshal(visible)
	if err != nil || len(encoded) > historyPageBytes {
		writeHistoryError(w, 413, "history_page_too_large", "完整历史页超过传输预算，未截断任何一轮")
		return
	}
	var next *string
	historyStatus, reason := "unknown", "anchor_unresolved"
	var more *bool
	if start >= 0 && start < len(items) {
		boundary := items[start]
		value := encodeHistoryCursor(historyCursor{1, source, session, anchorID, boundary.ItemKey, boundary.fingerprint, boundary.duplicates}, s.Token())
		next = &value
		reason = "reader_window_boundary"
		// The next request validates completeness. This indicates a retained user
		// boundary exists, never that the source's full history is available.
		for i := 0; i < start; i++ {
			if historyUser(items[i].Item) {
				yes := true
				more = &yes
				historyStatus = "available"
				reason = ""
				break
			}
		}
	}
	WriteJSON(w, 200, map[string]any{
		"session_id": session, "source": source, "items": visible, "count": len(visible), "turns": []any{}, "reason": nil,
		"history_version": 1, "anchor_event_id": anchorID, "anchor_status": anchorStatus,
		"before_cursor": next, "history_status": historyStatus, "has_more_before": more, "rounds_returned": rounds, "history_reason": optional(reason),
	})
}

// Never mix a driver's session with a same-named session from another source.
// Native rows remain authoritative for their time range. Only driver rows
// strictly newer than that range are appended, so mirrored ACP/native records
// cannot create duplicate prompts or fake additional rounds.
func readHistoryWindow(r *http.Request, source, session string) ([]agent.Item, int, string) {
	a, found := agent.Get(ctxOf(r), source)
	var native []agent.Item
	var readErr error
	supported := false
	if found {
		if reader, ok := a.(agent.ItemReader); ok {
			supported = true
			native, readErr = reader.ReadItems(ctxOf(r), session, historyReadLimit)
		}
	}
	var live []agent.Item
	if driver.Default().SessionAgent(session) == source {
		live = driver.Default().Items(session, historyReadLimit)
	}
	if readErr != nil {
		return nil, 502, "history_read_failed"
	}
	if !supported && len(live) == 0 {
		return nil, 501, "history_unsupported"
	}
	if len(native) > historyReadLimit || len(live) > historyReadLimit {
		return nil, 413, "history_page_too_large"
	}
	return mergeHistoryWindows(native, live)
}

func mergeHistoryWindows(native, live []agent.Item) ([]agent.Item, int, string) {
	if len(native) == 0 {
		return live, 0, ""
	}
	if len(live) == 0 {
		return native, 0, ""
	}
	// Some native parsers and ACP record the same output at different times.
	// Reconcile only a unique common suffix, verified row by row. An uncertain
	// overlap must not fabricate a second prompt or silently lose tool results.
	nativeKeys := make([]string, len(native))
	liveKeys := make([]string, len(live))
	counts := map[string]int{}
	for i, item := range native {
		nativeKeys[i] = historySemanticKey(item)
		counts[nativeKeys[i]]++
	}
	for i, item := range live {
		liveKeys[i] = historySemanticKey(item)
	}
	last := nativeKeys[len(nativeKeys)-1]
	match := -1
	if counts[last] == 1 {
		for i, key := range liveKeys {
			if key == last {
				if match >= 0 {
					match = -2
					break
				}
				match = i
			}
		}
	}
	if match >= 0 {
		overlap := match + 1
		if len(native) < overlap {
			overlap = len(native)
		}
		valid := true
		for i := 0; i < overlap; i++ {
			if nativeKeys[len(native)-1-i] != liveKeys[match-i] {
				valid = false
				break
			}
		}
		if valid {
			out := append(append([]agent.Item(nil), native...), live[match+1:]...)
			if len(out) > historyReadLimit {
				return nil, 413, "history_page_too_large"
			}
			return out, 0, ""
		}
	}
	// Disjoint snapshots can join only at a new, later user boundary. Otherwise
	// partial native/driver representations require source-specific reconciliation.
	var lastNative time.Time
	for _, item := range native {
		if ts, err := time.Parse(time.RFC3339Nano, item.TS); err == nil && ts.After(lastNative) {
			lastNative = ts
		}
	}
	firstLive, err := time.Parse(time.RFC3339Nano, live[0].TS)
	if err == nil && !lastNative.IsZero() && firstLive.After(lastNative) && historyUser(live[0]) {
		disjoint := true
		for _, key := range liveKeys {
			if counts[key] > 0 {
				disjoint = false
				break
			}
		}
		if disjoint {
			out := append(append([]agent.Item(nil), native...), live...)
			if len(out) > historyReadLimit {
				return nil, 413, "history_page_too_large"
			}
			return out, 0, ""
		}
	}
	return nil, 409, "history_mixed_transcript"
}

func historySemanticKey(item agent.Item) string {
	item.ID = ""
	item.Seq = 0
	item.TS = ""
	if item.Type == "user" || item.Type == "assistant" {
		item.Type = "message"
	}
	data, _ := json.Marshal(item)
	return historyDigest(data)
}

func historyUser(item agent.Item) bool {
	return item.Role == "user" && (item.Type == "user" || item.Type == "message") && item.Tool == nil
}

// A round starts at a real user prompt, followed by all emitted agent/tool
// entries up to the next prompt. The first expansion may overlap the visible
// anchor response to recover its owning prompt; clients upsert item_key.
func previousHistoryRounds(items []historyItem, boundary int) (int, int, []historyItem, string) {
	end := boundary
	if !historyUser(items[boundary].Item) {
		end = len(items)
		for i := boundary + 1; i < len(items); i++ {
			if historyUser(items[i].Item) {
				end = i
				break
			}
		}
	}
	var starts []int
	for i := 0; i < end; i++ {
		if historyUser(items[i].Item) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return 0, 0, nil, "history_unavailable"
	}
	if len(starts) > 10 {
		starts = starts[len(starts)-10:]
	}
	start := starts[0]
	// A half-finished tool response is not a complete conversational round.
	for _, item := range items[start:end] {
		if item.Partial || (item.Tool != nil && (item.Tool.Status == "running" || item.Tool.Status == "pending")) {
			return 0, 0, nil, "history_round_incomplete"
		}
	}
	for i, pos := range starts {
		next := end
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		if next <= pos+1 {
			return 0, 0, nil, "history_round_incomplete"
		}
	}
	return start, len(starts), items[start:end], ""
}

// Sequential parser IDs and Seq are local to a sliding file window, so they
// cannot identify a historical item. Immutable rows use a content fingerprint;
// ACP and tool-call IDs additionally permit in-place updates of partial rows.
func keyHistoryItems(raw []agent.Item, source, session string) []historyItem {
	out := make([]historyItem, 0, len(raw))
	counts := map[string]int{}
	for _, item := range raw {
		canonical := item
		canonical.ID = ""
		canonical.Seq = 0
		if ts, err := time.Parse(time.RFC3339Nano, canonical.TS); err == nil {
			canonical.TS = ts.UTC().Format(time.RFC3339Nano)
		}
		data, _ := json.Marshal(canonical)
		fingerprint := historyDigest(data)
		identity := fingerprint
		if strings.HasPrefix(item.ID, "acp-") || strings.Contains(item.ID, "-tool-") {
			identity = "provider:" + item.ID
		}
		key := historyDigest([]byte(source + "\x00" + session + "\x00" + identity))
		counts[key]++
		out = append(out, historyItem{Item: item, ItemKey: key, fingerprint: fingerprint})
	}
	occurrences := map[string]int{}
	for i := range out {
		base := out[i].ItemKey
		occurrences[base]++
		out[i].duplicates = counts[base]
		out[i].ItemKey = fmt.Sprintf("h1-%s-%d", base, occurrences[base])
	}
	return out
}

func historyDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encodeHistoryCursor(cursor historyCursor, secret string) string {
	data, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("homing-history-v1\x00"))
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func decodeHistoryCursor(raw, secret string) (historyCursor, error) {
	var cursor historyCursor
	invalid := errors.New("invalid history cursor")
	if len(raw) > 4096 {
		return cursor, invalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return cursor, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, invalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("homing-history-v1\x00"))
	mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Key == "" || cursor.Fingerprint == "" || cursor.Duplicates < 1 {
		return cursor, invalid
	}
	return cursor, nil
}

func writeHistoryError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": message, "code": code})
}
