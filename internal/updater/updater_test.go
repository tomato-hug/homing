package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		newer   bool
	}{
		{"0.8.0", "0.7.0", true},
		{"v1.0.0", "1.0.0", false},
		{"0.7.0", "0.7.0", false},
		{"0.7.0", "0.7.1", false},
		{"1.0.0-rc.1", "1.0.0", false},
		{"2.0.0", "dev", true},
	}
	for _, test := range tests {
		if got := isNewer(test.latest, test.current); got != test.newer {
			t.Fatalf("isNewer(%q, %q) = %v, want %v", test.latest, test.current, got, test.newer)
		}
	}
}

func TestChecksumForRequiresExactAsset(t *testing.T) {
	data := []byte("not-a-real-release")
	sum := sha256.Sum256(data)
	manifest := []byte(hex.EncodeToString(sum[:]) + "  homing_demo\n")
	got, err := checksumFor(manifest, "homing_demo")
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum = %q", got)
	}
	if _, err := checksumFor(manifest, "other"); err == nil {
		t.Fatal("expected missing checksum error")
	}
}

func TestReplaceExecutablePreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homing")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new" {
		t.Fatalf("contents = %q", raw)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

func TestCheckFindsPlatformAsset(t *testing.T) {
	assetName := binaryAsset(runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"tag_name":"v0.8.0",
			"html_url":"https://example.test/release",
			"assets":[
				{"name":%q,"browser_download_url":"https://example.test/homing"},
				{"name":"checksums.txt","browser_download_url":"https://example.test/checksums"}
			]
		}`, assetName)
	}))
	defer server.Close()
	original := releasesURL
	releasesURL = server.URL
	t.Cleanup(func() { releasesURL = original })

	result, err := Check(context.Background(), "0.7.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Latest != "0.8.0" || result.UpToDate || result.Asset != assetName {
		t.Fatalf("result = %+v", result)
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  homing_demo\n"))
		case "/binary":
			_, _ = w.Write([]byte("payload"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "homing_demo")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := install(context.Background(), server.URL+"/binary", server.URL+"/checksums", path); err == nil {
		t.Fatal("expected checksum failure")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "old" {
		t.Fatalf("target changed after failure: %q", raw)
	}
}
