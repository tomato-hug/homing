package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/api"
)

const (
	maxCloudFileBytes      = 20 * 1024 * 1024
	maxCloudFileChunkBytes = 64 * 1024
	fileAssemblyTTL        = 5 * time.Minute
)

type cloudFileChunk struct {
	Type        string `json:"type"`
	FID         string `json:"fid"`
	Seq         int    `json:"seq"`
	Last        bool   `json:"last"`
	Data        string `json:"data,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type cloudFileResult struct {
	Type   string `json:"type"`
	FID    string `json:"fid"`
	Status int    `json:"status"`
	Body   any    `json:"body"`
}

type fileAssembly struct {
	path        string
	contentType string
	nextSeq     int
	data        bytes.Buffer
	updatedAt   time.Time
}

type fileAssembler struct {
	sync.Mutex
	items map[string]*fileAssembly
}

var fileAssemblies = fileAssembler{items: make(map[string]*fileAssembly)}

func (a *fileAssembler) ensureLocked() {
	if a.items == nil {
		a.items = make(map[string]*fileAssembly)
	}
}

func (a *fileAssembler) sweepLocked(now time.Time) {
	for key, assembly := range a.items {
		if now.Sub(assembly.updatedAt) > fileAssemblyTTL {
			delete(a.items, key)
		}
	}
}

func (a *fileAssembler) dropPeer(peerID string) {
	if peerID == "" {
		return
	}
	prefix := peerID + "\x00"
	a.Lock()
	for key := range a.items {
		if strings.HasPrefix(key, prefix) {
			delete(a.items, key)
		}
	}
	a.Unlock()
}

func (a *fileAssembler) clear() {
	a.Lock()
	a.items = make(map[string]*fileAssembly)
	a.Unlock()
}

func (a *fileAssembler) dispatch(
	ctx context.Context,
	handler http.Handler,
	peerID string,
	plaintext []byte,
) ([]any, bool) {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(plaintext, &header) != nil {
		return nil, false
	}
	switch header.Type {
	case "file.chunk":
		var chunk cloudFileChunk
		if err := json.Unmarshal(plaintext, &chunk); err != nil {
			return []any{fileError("", http.StatusBadRequest, "invalid_file_chunk")}, true
		}
		result, respond := a.acceptChunk(ctx, handler, peerID, chunk)
		if !respond {
			return nil, true
		}
		return []any{result}, true
	case "file.request":
		var request struct {
			FID  string `json:"fid"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(plaintext, &request); err != nil {
			return []any{fileError("", http.StatusBadRequest, "invalid_file_request")}, true
		}
		return readFileChunks(ctx, handler, request.FID, request.Path), true
	default:
		return nil, false
	}
}

func (a *fileAssembler) acceptChunk(
	ctx context.Context,
	handler http.Handler,
	peerID string,
	chunk cloudFileChunk,
) (any, bool) {
	if !validTransferID(chunk.FID) || chunk.Seq < 0 {
		return fileError(chunk.FID, http.StatusBadRequest, "invalid_file_chunk"), true
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return fileError(chunk.FID, http.StatusBadRequest, "invalid_base64"), true
	}
	if len(data) > maxCloudFileChunkBytes {
		return fileError(chunk.FID, http.StatusRequestEntityTooLarge, "file_chunk_too_large"), true
	}

	key := peerID + "\x00" + chunk.FID
	now := time.Now()
	a.Lock()
	a.ensureLocked()
	a.sweepLocked(now)
	assembly := a.items[key]
	if chunk.Seq == 0 {
		if !validUploadPath(chunk.Path) {
			a.Unlock()
			return fileError(chunk.FID, http.StatusForbidden, "forbidden_upload_path"), true
		}
		assembly = &fileAssembly{
			path:        chunk.Path,
			contentType: strings.TrimSpace(chunk.ContentType),
			updatedAt:   now,
		}
		a.items[key] = assembly
	} else if assembly == nil {
		a.Unlock()
		return fileError(chunk.FID, http.StatusBadRequest, "unknown_file_transfer"), true
	}
	if chunk.Seq != assembly.nextSeq {
		if chunk.Seq == 0 {
			delete(a.items, key)
		}
		a.Unlock()
		return fileError(chunk.FID, http.StatusConflict, "file_chunk_out_of_order"), true
	}
	if assembly.data.Len()+len(data) > maxCloudFileBytes {
		delete(a.items, key)
		a.Unlock()
		return fileError(chunk.FID, http.StatusRequestEntityTooLarge, "file_too_large"), true
	}
	_, _ = assembly.data.Write(data)
	assembly.nextSeq++
	assembly.updatedAt = now
	if !chunk.Last {
		a.Unlock()
		return nil, false
	}
	delete(a.items, key)
	path := assembly.path
	contentType := assembly.contentType
	payload := append([]byte(nil), assembly.data.Bytes()...)
	a.Unlock()

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	status, body, _ := serveLocalRequest(ctx, handler, http.MethodPost, path, contentType, payload)
	return cloudFileResult{
		Type:   "file.result",
		FID:    chunk.FID,
		Status: status,
		Body:   body,
	}, true
}

// dispatchFilePayload handles the encrypted file.chunk and file.request
// payloads on the default package assembler.
func dispatchFilePayload(
	ctx context.Context,
	handler http.Handler,
	peerID string,
	plaintext []byte,
) ([]any, bool) {
	return fileAssemblies.dispatch(ctx, handler, peerID, plaintext)
}

func readFileChunks(
	ctx context.Context,
	handler http.Handler,
	fid string,
	path string,
) []any {
	if !validTransferID(fid) {
		return []any{fileError(fid, http.StatusBadRequest, "invalid_transfer_id")}
	}
	if !validDownloadPath(path) {
		return []any{fileError(fid, http.StatusForbidden, "forbidden_download_path")}
	}
	status, body, raw := serveLocalRequest(ctx, handler, http.MethodGet, path, "", nil)
	if status < 200 || status >= 300 {
		return []any{cloudFileResult{
			Type:   "file.result",
			FID:    fid,
			Status: status,
			Body:   body,
		}}
	}
	if len(raw) > maxCloudFileBytes {
		return []any{fileError(fid, http.StatusRequestEntityTooLarge, "file_too_large")}
	}
	if len(raw) == 0 {
		return []any{cloudFileChunk{Type: "file.chunk", FID: fid, Seq: 0, Last: true}}
	}
	out := make([]any, 0, (len(raw)+maxCloudFileChunkBytes-1)/maxCloudFileChunkBytes)
	for offset, seq := 0, 0; offset < len(raw); seq++ {
		end := offset + maxCloudFileChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		out = append(out, cloudFileChunk{
			Type: "file.chunk",
			FID:  fid,
			Seq:  seq,
			Last: end == len(raw),
			Data: base64.StdEncoding.EncodeToString(raw[offset:end]),
		})
		offset = end
	}
	return out
}

func serveLocalRequest(
	ctx context.Context,
	handler http.Handler,
	method string,
	path string,
	contentType string,
	body []byte,
) (int, any, []byte) {
	if handler == nil {
		return http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "local_api_unavailable",
		}, nil
	}
	request := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("X-Homing-RPC", "1")
	request = api.WithTrustedLocalRPC(request)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	raw := append([]byte(nil), recorder.Body.Bytes()...)
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		decoded = string(raw)
	}
	return recorder.Code, decoded, raw
}

func fileError(fid string, status int, code string) cloudFileResult {
	return cloudFileResult{
		Type:   "file.result",
		FID:    fid,
		Status: status,
		Body:   map[string]any{"ok": false, "error": code},
	}
}

func validTransferID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' ||
			char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func validUploadPath(path string) bool {
	return path == "/api/upload" || strings.HasPrefix(path, "/api/upload?")
}

func validDownloadPath(path string) bool {
	return strings.HasPrefix(path, "/f/") && !strings.Contains(path, "..")
}

func sweepFileAssembliesLocked(now time.Time) {
	fileAssemblies.sweepLocked(now)
}

// clearFileAssemblies is used by tests to isolate transfers between cases.
func clearFileAssemblies() {
	fileAssemblies.clear()
}
