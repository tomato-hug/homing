package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCreatesCapabilityAndStopsTraversal(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "示例图片.png")
	if err := os.WriteFile(image, []byte("demo-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("do-not-serve"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := ManagerFor(t.TempDir())
	attachments := manager.Build("图片：`"+image+"`，另外 ![x]("+outside+")", root)
	if len(attachments) != 1 {
		t.Fatalf("attachments=%v", attachments)
	}
	attachment := attachments[0]
	if attachment.Kind != "image" || attachment.Name != "示例图片.png" {
		t.Fatalf("attachment=%+v", attachment)
	}
	if !strings.HasPrefix(attachment.URL, "/f/") {
		t.Fatalf("url=%q", attachment.URL)
	}
	parts := strings.Split(strings.TrimPrefix(attachment.URL, "/f/"), "/")
	if len(parts) != 2 {
		t.Fatalf("url parts=%v", parts)
	}
	real, err := manager.Resolve(parts[0], parts[1])
	resolvedImage, _ := filepath.EvalSymlinks(image)
	if err != nil || real != resolvedImage {
		t.Fatalf("resolve=%q err=%v", real, err)
	}
	if _, err := manager.Resolve(parts[0], "../secret.txt"); err != ErrForbidden {
		t.Fatalf("traversal err=%v, want ErrForbidden", err)
	}
	if _, err := manager.Resolve(parts[0], "missing.png"); err != ErrNotFound {
		t.Fatalf("missing err=%v, want ErrNotFound", err)
	}
}

func TestBuildKeepsOnlyAllowedRegularFiles(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	allowed := filepath.Join(root, "demo.pdf")
	if err := os.WriteFile(allowed, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "my-secret.txt")
	if err := os.WriteFile(blocked, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := ManagerFor(t.TempDir())
	attachments := manager.Build(strings.Join([]string{allowed, blocked, filepath.Join(root, "missing.txt")}, "\n"), root)
	if len(attachments) != 1 || attachments[0].Name != "demo.pdf" {
		t.Fatalf("attachments=%+v", attachments)
	}
}

func TestNormalizeFilesAndDeliveryText(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "demo.txt")
	if err := os.WriteFile(file, []byte("demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := NormalizeFiles([]string{"", file, file, filepath.Join(root, "missing")})
	if len(got) != 1 || got[0] != file {
		t.Fatalf("files=%v", got)
	}
	text := DeliveryText("示例", got)
	if !strings.Contains(text, "用户从手机上传了文件") || !strings.Contains(text, file) {
		t.Fatalf("delivery text=%q", text)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"../../示例 文件.png": "示例_文件.png",
		"a:b?.txt":        "a_b_.txt",
		".hidden":         "hidden",
		"":                "",
	}
	for input, want := range cases {
		if got := SanitizeName(input); got != want {
			t.Errorf("SanitizeName(%q)=%q, want %q", input, got, want)
		}
	}
}
