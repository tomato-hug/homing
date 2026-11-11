package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/protocol"
)

// rpcRequest is the decrypted payload a client sends to run a local /api call.
type rpcRequest struct {
	Type   string          `json:"type"`
	RID    string          `json:"rid"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

type rpcResult struct {
	Type   string `json:"type"`
	RID    string `json:"rid"`
	Status int    `json:"status"`
	Body   any    `json:"body"`
}

type eventPayload struct {
	Type string `json:"type"`
	RID  string `json:"rid,omitempty"`
	Name string `json:"name,omitempty"`
	Data any    `json:"data,omitempty"`
}

var allowedRPCMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPatch:  true,
	http.MethodPut:    true,
	http.MethodDelete: true,
}

const maxCloudUploadBytes = 64 * 1024

type cloudUploadBody struct {
	DataBase64  string `json:"data_base64"`
	ContentType string `json:"content_type,omitempty"`
}

// dispatchRPC runs one decrypted rpc payload against the local Homing HTTP
// interface and returns the rpc_result payload. Only /api/ paths are reachable:
// a phone must never be able to hit /console or /v1/ws through this door.
func dispatchRPC(ctx context.Context, handler http.Handler, payload []byte) []byte {
	var request rpcRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return mustJSON(rpcResult{
			Type:   "rpc_result",
			Status: http.StatusBadRequest,
			Body:   map[string]any{"ok": false, "error": "invalid_rpc"},
		})
	}
	if request.Type != "rpc" || request.RID == "" {
		return mustJSON(rpcResult{
			Type:   "rpc_result",
			RID:    request.RID,
			Status: http.StatusBadRequest,
			Body:   map[string]any{"ok": false, "error": "invalid_rpc"},
		})
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !allowedRPCMethods[method] || !strings.HasPrefix(request.Path, "/api/") {
		return mustJSON(rpcResult{
			Type:   "rpc_result",
			RID:    request.RID,
			Status: http.StatusForbidden,
			Body:   map[string]any{"ok": false, "error": "forbidden"},
		})
	}
	if handler == nil {
		return mustJSON(rpcResult{
			Type:   "rpc_result",
			RID:    request.RID,
			Status: http.StatusServiceUnavailable,
			Body:   map[string]any{"ok": false, "error": "local_api_unavailable"},
		})
	}

	body, contentType, err := rpcRequestBody(request, method)
	if err != nil {
		return mustJSON(rpcResult{
			Type:   "rpc_result",
			RID:    request.RID,
			Status: http.StatusRequestEntityTooLarge,
			Body:   map[string]any{"ok": false, "error": err.Error()},
		})
	}
	httpRequest := httptest.NewRequestWithContext(ctx, method, request.Path, body)
	if contentType != "" {
		httpRequest.Header.Set("Content-Type", contentType)
	} else if body.Len() > 0 {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpRequest.Header.Set("X-Homing-RPC", "1")
	httpRequest = api.WithTrustedLocalRPC(httpRequest)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpRequest)

	var decoded any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		decoded = recorder.Body.String()
	}
	return mustJSON(rpcResult{
		Type:   "rpc_result",
		RID:    request.RID,
		Status: recorder.Code,
		Body:   decoded,
	})
}

func rpcRequestBody(
	request rpcRequest,
	method string,
) (*bytes.Reader, string, error) {
	if len(request.Body) == 0 || string(request.Body) == "null" {
		return bytes.NewReader(nil), "", nil
	}
	if method != http.MethodPost || request.Path != "/api/upload" &&
		!strings.HasPrefix(request.Path, "/api/upload?") {
		return bytes.NewReader(request.Body), "application/json", nil
	}
	var upload cloudUploadBody
	if json.Unmarshal(request.Body, &upload) == nil &&
		upload.DataBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(upload.DataBase64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid_base64")
		}
		if len(data) > maxCloudUploadBytes {
			return nil, "", fmt.Errorf("cloud_upload_too_large")
		}
		contentType := strings.TrimSpace(upload.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return bytes.NewReader(data), contentType, nil
	}
	return bytes.NewReader(request.Body), "application/json", nil
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"type":"rpc_result","status":500,"body":{"ok":false,"error":"encode_failed"}}`)
	}
	return encoded
}

// sealPayload encrypts payload for peer. The envelope is addressed from this
// computer to the peer device.
func sealPayload(identity *Identity, peer Peer, payload any) (protocol.Envelope, error) {
	peerPublicKey, err := decodePublicKey(peer.PublicKey)
	if err != nil {
		return protocol.Envelope{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return protocol.Envelope{}, fmt.Errorf("cloud: encode payload: %w", err)
	}
	return protocol.CreateEnvelope(
		identity.DeviceID(),
		peer.DeviceID,
		encoded,
		identity.keys,
		peerPublicKey,
	)
}

func openPayload(identity *Identity, peerPublicKey ed25519.PublicKey, envelope protocol.Envelope) ([]byte, error) {
	if envelope.From == identity.DeviceID() {
		return nil, fmt.Errorf("cloud: refusing an envelope from ourselves")
	}
	return protocol.OpenEnvelope(envelope, identity.keys, peerPublicKey)
}

func decodePublicKey(value string) (ed25519.PublicKey, error) {
	raw, err := protocol.FromBase64(value)
	if err != nil {
		return nil, fmt.Errorf("cloud: peer public key is not base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("cloud: peer public key is not 32 bytes")
	}
	return ed25519.PublicKey(raw), nil
}

// envelopeMap converts a typed envelope into the loose JSON shape the relay
// forwards. It keeps the go struct as the single source of truth for field
// names.
func envelopeMap(envelope protocol.Envelope) map[string]any {
	return map[string]any{
		"v":     envelope.V,
		"t":     envelope.T,
		"id":    envelope.ID,
		"from":  envelope.From,
		"to":    envelope.To,
		"ts":    envelope.TS,
		"nonce": envelope.Nonce,
		"ct":    envelope.CT,
	}
}

func envelopeFromMap(message map[string]any) (protocol.Envelope, bool) {
	raw, err := json.Marshal(message)
	if err != nil {
		return protocol.Envelope{}, false
	}
	var envelope protocol.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return protocol.Envelope{}, false
	}
	if envelope.V != protocol.EnvelopeVersion || envelope.T != protocol.EnvelopeType ||
		envelope.From == "" || envelope.To == "" || envelope.ID == "" {
		return protocol.Envelope{}, false
	}
	return envelope, true
}
