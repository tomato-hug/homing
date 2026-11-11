// Package updater implements `homing update`. It downloads release binaries
// from GitHub, verifies the SHA-256 manifest published beside them, and only
// then atomically replaces the current executable.
package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/cli"
)

const (
	repository = "tomato-hug/homing"
	maxAsset   = 150 << 20
)

var version = "dev"

// SetVersion connects the linker-injected cmd/homing version to the updater.
func SetVersion(value string) {
	if strings.TrimSpace(value) != "" {
		version = value
	}
}

var releasesURL = "https://api.github.com/repos/" + repository + "/releases/latest"

func init() {
	cli.Register(cli.Command{
		Name:    "update",
		Summary: "检查并安装 GitHub 上的新版本（校验 SHA-256）",
		Run:     run,
	})
}

type release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	HTMLURL string  `json:"html_url"`
	Body    string  `json:"body"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Result describes a completed update check without changing files.
type Result struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	UpToDate    bool   `json:"up_to_date"`
	Asset       string `json:"asset"`
	ReleaseURL  string `json:"release_url"`
	ReleaseNote string `json:"release_note,omitempty"`
}

// Check queries the latest published release for this OS and architecture.
func Check(ctx context.Context, currentVersion string) (Result, error) {
	rel, err := latestRelease(ctx)
	if err != nil {
		return Result{}, err
	}
	latest := cleanVersion(rel.TagName)
	assetName := binaryAsset(runtime.GOOS, runtime.GOARCH)
	if findAsset(rel, assetName) == nil {
		return Result{}, fmt.Errorf("版本 %s 没有提供 %s 安装包", latest, assetName)
	}
	if findAsset(rel, "checksums.txt") == nil {
		return Result{}, fmt.Errorf("版本 %s 缺少 checksums.txt，拒绝更新", latest)
	}
	return Result{
		Current:     currentVersion,
		Latest:      latest,
		UpToDate:    !isNewer(latest, currentVersion),
		Asset:       assetName,
		ReleaseURL:  rel.HTMLURL,
		ReleaseNote: rel.Body,
	}, nil
}

// Apply downloads and verifies the latest release, then replaces executable.
// When force is false, a development build or an already-current version is
// left untouched.
func Apply(ctx context.Context, currentVersion, executable string, force bool) (Result, error) {
	result, err := Check(ctx, currentVersion)
	if err != nil {
		return result, err
	}
	if result.UpToDate && !force {
		return result, nil
	}
	if currentVersion == "dev" && !force {
		return result, errors.New("这是开发构建；确认要覆盖当前可执行文件时加 --force")
	}
	rel, err := latestRelease(ctx)
	if err != nil {
		return result, err
	}
	binary := findAsset(rel, result.Asset)
	checksum := findAsset(rel, "checksums.txt")
	if binary == nil || checksum == nil {
		return result, errors.New("release assets changed while updating; retry")
	}
	if binary.Size > maxAsset {
		return result, fmt.Errorf("安装包过大：%d bytes", binary.Size)
	}
	return result, install(ctx, binary.BrowserDownloadURL, checksum.BrowserDownloadURL, executable)
}

func latestRelease(ctx context.Context) (release, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		releasesURL, nil,
	)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "homing-updater")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return release{}, fmt.Errorf("GitHub release API: %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&rel); err != nil {
		return release{}, err
	}
	if cleanVersion(rel.TagName) == "" || len(rel.Assets) == 0 {
		return release{}, errors.New("latest release has no usable version or assets")
	}
	return rel, nil
}

func install(ctx context.Context, binaryURL, checksumURL, executable string) error {
	if strings.TrimSpace(executable) == "" {
		return errors.New("cannot determine current executable")
	}
	if info, err := os.Stat(executable); err != nil {
		return err
	} else if info.IsDir() {
		return errors.New("current executable path is a directory")
	}
	checksums, err := download(ctx, checksumURL, 2<<20)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	expected, err := checksumFor(checksums, filepath.Base(binaryURLPath(binaryURL)))
	if err != nil {
		// Browser URLs sometimes carry the asset name in a query string. Try
		// the local target name before giving up.
		expected, err = checksumFor(checksums, filepath.Base(executable))
		if err != nil {
			return err
		}
	}
	data, err := download(ctx, binaryURL, maxAsset)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	actual := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return errors.New("SHA-256 mismatch; downloaded binary was discarded")
	}
	return replaceExecutable(executable, data)
}

func replaceExecutable(executable string, data []byte) error {
	dir := filepath.Dir(executable)
	temp, err := os.CreateTemp(dir, ".homing-update-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}

	if runtime.GOOS != "windows" {
		return os.Rename(tempName, executable)
	}

	// Windows cannot replace a running image in one rename. Move the old
	// binary aside, install the new one, and restore on failure.
	old := executable + ".old"
	_ = os.Remove(old)
	if err := os.Rename(executable, old); err != nil {
		return err
	}
	if err := os.Rename(tempName, executable); err != nil {
		_ = os.Rename(old, executable)
		return err
	}
	_ = os.Remove(old)
	return nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "homing-updater")
	response, err := (&http.Client{Timeout: 90 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download exceeded size limit")
	}
	return data, nil
}

func checksumFor(data []byte, name string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		candidate := strings.TrimPrefix(fields[len(fields)-1], "*")
		if candidate == name {
			sum := strings.ToLower(fields[0])
			if len(sum) != 64 {
				return "", fmt.Errorf("invalid checksum for %s", name)
			}
			if _, err := hex.DecodeString(sum); err != nil {
				return "", fmt.Errorf("invalid checksum for %s: %w", name, err)
			}
			return sum, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

func binaryAsset(goos, goarch string) string {
	name := "homing_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func findAsset(rel release, name string) *asset {
	for index := range rel.Assets {
		if rel.Assets[index].Name == name {
			return &rel.Assets[index]
		}
	}
	return nil
}

func binaryURLPath(url string) string {
	if index := strings.IndexAny(url, "?#"); index >= 0 {
		return url[:index]
	}
	return url
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "refs/tags/"), "v")
	return value
}

func isNewer(latest, current string) bool {
	latest = cleanVersion(latest)
	current = cleanVersion(current)
	if current == "" || current == "dev" {
		return true
	}
	return compareVersions(latest, current) > 0
}

func compareVersions(left, right string) int {
	leftCore, leftPre := splitVersion(left)
	rightCore, rightPre := splitVersion(right)
	leftParts := strings.Split(leftCore, ".")
	rightParts := strings.Split(rightCore, ".")
	for i := 0; i < max(len(leftParts), len(rightParts)); i++ {
		a, b := 0, 0
		if i < len(leftParts) {
			a, _ = strconv.Atoi(leftParts[i])
		}
		if i < len(rightParts) {
			b, _ = strconv.Atoi(rightParts[i])
		}
		if a != b {
			if a > b {
				return 1
			}
			return -1
		}
	}
	switch {
	case leftPre == "" && rightPre != "":
		return 1
	case leftPre != "" && rightPre == "":
		return -1
	case leftPre == rightPre:
		return 0
	case leftPre > rightPre:
		return 1
	default:
		return -1
	}
}

func splitVersion(value string) (string, string) {
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		core := value[:index]
		pre := ""
		if value[index] == '-' {
			pre = value[index+1:]
			if plus := strings.IndexByte(pre, '+'); plus >= 0 {
				pre = pre[:plus]
			}
		}
		return core, pre
	}
	return value, ""
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "只检查，不下载")
	force := fs.Bool("force", false, "覆盖当前版本（开发构建必须显式指定）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *checkOnly {
		result, err := Check(ctx, version)
		if err != nil {
			return err
		}
		printResult(result)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	result, err := Apply(ctx, version, executable, *force)
	if err != nil {
		return err
	}
	printResult(result)
	if result.UpToDate {
		fmt.Println("已经是最新版本。")
	} else {
		fmt.Println("更新完成；重新启动 homing 后生效。")
	}
	return nil
}

func printResult(result Result) {
	fmt.Printf("当前版本：%s\n最新版本：%s\n目标安装包：%s\n", result.Current, result.Latest, result.Asset)
	if result.ReleaseURL != "" {
		fmt.Printf("发布页：%s\n", result.ReleaseURL)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
