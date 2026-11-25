package cloud

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRPCCompressionRequiresNegotiationAndPreservesBody(t *testing.T) {
	body := map[string]any{"text": strings.Repeat("demo 合成上下文\n", 2000)}
	full, _ := json.Marshal(body)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(full) })
	for _, encoding := range []string{"", "unknown", "gzip"} {
		raw := dispatchRPC(context.Background(), handler, mustJSON(map[string]any{
			"type": "rpc", "rid": "demo-compressed", "method": "GET", "path": "/api/demo", "accept_encoding": encoding,
		}))
		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if encoding != "gzip" {
			if result["body_encoding"] != nil || result["body"].(map[string]any)["text"] != body["text"] {
				t.Fatal("legacy body changed")
			}
			continue
		}
		if result["body_encoding"] != "gzip+base64" {
			t.Fatal("negotiated response was not compressed")
		}
		if len(raw) >= len(full)/4 {
			t.Fatal("fixture did not shrink enough")
		}
		encoded, err := base64.StdEncoding.DecodeString(result["body_base64"].(string))
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		reader.Close()
		if !bytes.Equal(decoded, full) {
			t.Fatal("compression changed JSON bytes")
		}
	}
}
