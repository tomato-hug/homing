package library

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxRemoteBytes  = 40 << 20
	maxExpandBytes  = 100 << 20
	maxExtractFiles = 5000
	remotePlanTTL   = 30 * time.Minute
)

var (
	ErrNoRemoteUpdate = errors.New("library: remote source has no newer version")
	ErrUpdateMismatch = errors.New("library: update destination does not match the installation")
)

// RemoteProvider is a source adapter shown in the library's Sources tab.
type RemoteProvider struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Example string `json:"example"`
	Note    string `json:"note"`
	Ready   bool   `json:"ready"`
}

// RemoteProviders describes what L2 can import without executing third-party
// installers.
func RemoteProviders() []RemoteProvider {
	return []RemoteProvider{
		{
			ID: "github", Label: "GitHub", Example: "https://github.com/owner/repo#v1.2.0",
			Note: "下載 repository archive；可指定 branch、tag 或 commit。", Ready: true,
		},
		{
			ID: "skills.sh", Label: "skills.sh", Example: "https://skills.sh/owner/repo/skill-name",
			Note: "解析為對應 GitHub 來源，優先尋找 skills/<name> 子目錄。", Ready: true,
		},
		{
			ID: "direct-url", Label: "直接 URL", Example: "https://example.com/pack.tar.gz",
			Note: "支援 .tar.gz／.tgz／.zip 與直接檔案，下載上限 40 MB。", Ready: true,
		},
		{
			ID: "mcp", Label: "MCP Registry", Example: "mcp://io.example/weather",
			Note: "從官方 Registry 取得 server.json，只保存描述，不安裝或啟動伺服器。", Ready: true,
		},
	}
}

// RemotePlan pins the exact bytes downloaded and the dry-run import plan.
type RemotePlan struct {
	OK             bool           `json:"ok"`
	FetchID        string         `json:"fetch_id"`
	Provider       string         `json:"provider"`
	URL            string         `json:"url"`
	ResolvedURL    string         `json:"resolved_url"`
	DownloadSHA256 string         `json:"download_sha256"`
	Trust          string         `json:"trust"`
	Manifest       *Manifest      `json:"manifest,omitempty"`
	Inspection     Inspection     `json:"inspection"`
	Plan           ImportPlan     `json:"plan"`
	Warnings       []string       `json:"warnings,omitempty"`
	FetchedAt      string         `json:"fetched_at"`
	ExpiresAt      string         `json:"expires_at"`
	LocalFiles     []ResourceFile `json:"-"`
}

// RemoteRequest is one remote dry-run request.
type RemoteRequest struct {
	URL  string `json:"url"`
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type fetchRecord struct {
	Plan      RemotePlan `json:"plan"`
	SourceDir string     `json:"source_dir"`
}

// RemoteUpdatePlan pins a newer version without touching the installed tree.
// It is the dry-run half of PlanRemoteUpdate -> InstallRemoteUpdate.
type RemoteUpdatePlan struct {
	OK                      bool       `json:"ok"`
	InstallationID          string     `json:"installation_id"`
	ResourceID              string     `json:"resource_id"`
	Name                    string     `json:"name"`
	CurrentVersion          string     `json:"current_version,omitempty"`
	AvailableVersion        string     `json:"available_version,omitempty"`
	CurrentChecksum         string     `json:"current_checksum,omitempty"`
	AvailableChecksum       string     `json:"available_checksum,omitempty"`
	CurrentDownloadSHA256   string     `json:"current_download_sha256,omitempty"`
	AvailableDownloadSHA256 string     `json:"available_download_sha256,omitempty"`
	UpdateAvailable         bool       `json:"update_available"`
	Plan                    RemotePlan `json:"plan"`
	Warnings                []string   `json:"warnings,omitempty"`
	CheckedAt               string     `json:"checked_at"`
}

// PlanRemote downloads a bounded source, validates it, and returns a dry-run.
// The fetched tree is retained in ~/.homing/cache/library for InstallRemote.
func PlanRemote(ctx context.Context, home string, request RemoteRequest) (RemotePlan, error) {
	return planRemote(ctx, home, request, false)
}

func planRemote(
	ctx context.Context,
	home string,
	request RemoteRequest,
	allowExisting bool,
) (RemotePlan, error) {
	rawURL := strings.TrimSpace(request.URL)
	if rawURL == "" {
		return RemotePlan{}, errors.New("url 不能为空")
	}
	fetchID, err := randomID("fetch")
	if err != nil {
		return RemotePlan{}, err
	}
	cacheDir := filepath.Join(home, ".homing", "cache", "library", fetchID)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return RemotePlan{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(cacheDir)
		}
	}()

	resolved, err := resolveRemote(ctx, rawURL, request.Ref)
	if err != nil {
		return RemotePlan{}, err
	}
	downloaded := filepath.Join(cacheDir, "download")
	sum, contentType, err := downloadTo(ctx, resolved.DownloadURL, downloaded)
	if err != nil {
		return RemotePlan{}, err
	}
	sourceDir := filepath.Join(cacheDir, "source")
	switch {
	case resolved.ForceJSON || strings.Contains(strings.ToLower(contentType), "application/json"):
		if err := os.MkdirAll(sourceDir, 0o700); err != nil {
			return RemotePlan{}, err
		}
		if err := moveFile(downloaded, filepath.Join(sourceDir, "server.json")); err != nil {
			return RemotePlan{}, err
		}
		if request.Kind == "" {
			request.Kind = KindMCP
		}
	case isArchiveURL(resolved.DownloadURL, contentType):
		if err := extractArchive(downloaded, sourceDir); err != nil {
			return RemotePlan{}, err
		}
	default:
		if strings.Contains(strings.ToLower(contentType), "text/html") {
			return RemotePlan{}, errors.New("這個網址回傳 HTML 頁面，不是可下載的資源包")
		}
		if err := os.MkdirAll(sourceDir, 0o700); err != nil {
			return RemotePlan{}, err
		}
		name := filepath.Base(resolved.DownloadURL)
		if name == "." || name == "/" || name == "" {
			name = "resource"
		}
		if err := moveFile(downloaded, filepath.Join(sourceDir, name)); err != nil {
			return RemotePlan{}, err
		}
	}

	sourceDir, err = selectSkillSubdir(sourceDir, resolved.Subdir)
	if err != nil {
		return RemotePlan{}, err
	}

	var manifest *Manifest
	var warnings []string
	parsed, manifestErr := ReadManifest(sourceDir)
	switch {
	case manifestErr == nil:
		manifest = &parsed
		warnings = append(warnings, ManifestWarnings(parsed)...)
		if request.Kind == "" {
			request.Kind = normalizeKind(parsed.Kind)
		}
		if request.Name == "" {
			request.Name = parsed.Metadata.Name
		}
	case errors.Is(manifestErr, ErrManifestMissing):
		warnings = append(warnings, "沒有 homing.resource.yaml；只能作為草稿匯入")
	default:
		return RemotePlan{}, manifestErr
	}

	kind := normalizeKind(request.Kind)
	if kind == "" {
		kind = detectKind(sourceDir)
	}
	inspection, err := Inspect(sourceDir, kind)
	if err != nil {
		return RemotePlan{}, err
	}
	inspection.Kind = kind
	if manifest != nil {
		inspection.Name = firstNonEmpty(manifest.Metadata.Name, inspection.Name)
		inspection.Description = firstNonEmpty(manifest.Metadata.Description, inspection.Description)
		inspection.Version = firstNonEmpty(manifest.Metadata.Version, inspection.Version)
	}
	if strings.TrimSpace(request.Name) != "" {
		inspection.Name = truncateRunes(strings.TrimSpace(request.Name), 100)
	}
	plan, err := buildImportPlan(home, sourceDir, kind, inspection.Name, allowExisting)
	if err != nil {
		return RemotePlan{}, err
	}
	inspection.Name = plan.Name
	inspection.Kind = plan.Kind
	plan.Inspection = inspection
	plan.Warnings = mergeStrings(plan.Warnings, warnings)

	now := time.Now().UTC()
	remote := RemotePlan{
		OK: true, FetchID: fetchID, Provider: resolved.Provider, URL: rawURL,
		ResolvedURL: resolved.DownloadURL, DownloadSHA256: "sha256:" + sum,
		Trust: "verified-source", Manifest: manifest, Inspection: inspection,
		Plan: plan, Warnings: mergeStrings(warnings, plan.Warnings),
		FetchedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(remotePlanTTL).Format(time.RFC3339),
	}
	record := fetchRecord{Plan: remote, SourceDir: sourceDir}
	if err := writeJSONAtomic(filepath.Join(cacheDir, "fetch.json"), record); err != nil {
		return RemotePlan{}, err
	}
	_ = os.Chmod(filepath.Join(cacheDir, "fetch.json"), 0o600)
	cleanup = false
	return remote, nil
}

// InstallRemote consumes one dry-run fetch token and records enough metadata
// for a recoverable rollback.
func InstallRemote(home, fetchID string) (RemotePlan, Installation, error) {
	record, cacheDir, err := loadFetchRecord(home, fetchID)
	if err != nil {
		return record.Plan, Installation{}, err
	}
	plan, err := Install(home, record.SourceDir, record.Plan.Plan.Kind, record.Plan.Plan.Name)
	if err != nil {
		return record.Plan, Installation{}, err
	}
	installation := Installation{
		ID:             mustRandomID("inst"),
		ResourceID:     resourceID(plan.Kind, plan.Name),
		Name:           plan.Name,
		Kind:           plan.Kind,
		Version:        record.Plan.Inspection.Version,
		Destination:    plan.Destination,
		SourceURL:      record.Plan.URL,
		ResolvedURL:    record.Plan.ResolvedURL,
		DownloadSHA256: record.Plan.DownloadSHA256,
		Trust:          record.Plan.Trust,
		Manifest:       record.Plan.Manifest,
		State:          "installed",
		InstalledAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := appendInstallation(home, installation); err != nil {
		_, _ = Remove(home, installation.ResourceID, plan.Destination)
		return record.Plan, Installation{}, err
	}
	_ = os.RemoveAll(cacheDir)
	record.Plan.Plan = plan
	return record.Plan, installation, nil
}

func loadFetchRecord(home, fetchID string) (fetchRecord, string, error) {
	fetchID = strings.TrimSpace(fetchID)
	if fetchID == "" || !safeID(fetchID) {
		return fetchRecord{}, "", errors.New("fetch_id 无效")
	}
	cacheDir := filepath.Join(home, ".homing", "cache", "library", fetchID)
	var record fetchRecord
	raw, err := os.ReadFile(filepath.Join(cacheDir, "fetch.json"))
	if err != nil {
		return record, "", errors.New("遠端預覽已過期或不存在，請重新 plan")
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, "", err
	}
	expires, err := time.Parse(time.RFC3339, record.Plan.ExpiresAt)
	if err != nil || time.Now().After(expires) {
		_ = os.RemoveAll(cacheDir)
		return record, "", errors.New("遠端預覽已過期，請重新 plan")
	}
	if !isWithin(record.SourceDir, cacheDir) {
		return record, "", errors.New("遠端快取路徑越界")
	}
	return record, cacheDir, nil
}

// PlanRemoteUpdate fetches the recorded source again and compares both the
// manifest version and the content checksum with the installed tree.
func PlanRemoteUpdate(ctx context.Context, home, installationID string) (RemoteUpdatePlan, error) {
	installation, ok, err := findInstallation(home, strings.TrimSpace(installationID))
	if err != nil {
		return RemoteUpdatePlan{}, err
	}
	if !ok {
		return RemoteUpdatePlan{}, errors.New("安裝紀錄不存在")
	}
	if installation.State != "installed" {
		return RemoteUpdatePlan{}, fmt.Errorf("安裝紀錄目前是 %s", installation.State)
	}
	if strings.TrimSpace(installation.SourceURL) == "" {
		return RemoteUpdatePlan{}, errors.New("本機匯入的資源沒有遠端來源，無法檢查更新")
	}
	if !managedDestination(home, installation.Destination) {
		return RemoteUpdatePlan{}, errors.New("安裝目的地不在 Homing 管理目錄內")
	}
	remote, err := planRemote(ctx, home, RemoteRequest{
		URL: installation.SourceURL, Kind: installation.Kind, Name: installation.Name,
	}, true)
	if err != nil {
		return RemoteUpdatePlan{}, err
	}
	if filepath.Clean(remote.Plan.Destination) != filepath.Clean(installation.Destination) {
		return RemoteUpdatePlan{}, ErrUpdateMismatch
	}
	current, inspectErr := Inspect(installation.Destination, installation.Kind)
	warnings := append([]string(nil), remote.Warnings...)
	if inspectErr != nil {
		warnings = append(warnings, "目前版本無法重新檢查："+inspectErr.Error())
	}
	availableVersion := firstNonEmpty(
		remote.Inspection.Version,
		func() string {
			if remote.Manifest != nil {
				return remote.Manifest.Metadata.Version
			}
			return ""
		}(),
	)
	updateAvailable := strings.TrimSpace(installation.Version) != strings.TrimSpace(availableVersion) ||
		(current.Checksum != "" && remote.Inspection.Checksum != "" && current.Checksum != remote.Inspection.Checksum) ||
		(inspectErr != nil)
	if !updateAvailable {
		_ = os.RemoveAll(filepath.Join(home, ".homing", "cache", "library", remote.FetchID))
		remote.FetchID = ""
		remote.ExpiresAt = ""
	}
	return RemoteUpdatePlan{
		OK: true, InstallationID: installation.ID, ResourceID: installation.ResourceID,
		Name:           installation.Name,
		CurrentVersion: installation.Version, AvailableVersion: availableVersion,
		CurrentChecksum: current.Checksum, AvailableChecksum: remote.Inspection.Checksum,
		CurrentDownloadSHA256:   installation.DownloadSHA256,
		AvailableDownloadSHA256: remote.DownloadSHA256,
		UpdateAvailable:         updateAvailable, Plan: remote,
		Warnings:  mergeStrings(warnings, remote.Plan.Warnings),
		CheckedAt: nowRFC3339(),
	}, nil
}

// InstallRemoteUpdate atomically swaps a pinned update into place. The prior
// tree is kept under ~/.homing/library/backups so a later rollback can restore
// the exact previous version.
func InstallRemoteUpdate(
	home, installationID, fetchID string,
) (RemotePlan, Installation, error) {
	installation, ok, err := findInstallation(home, strings.TrimSpace(installationID))
	if err != nil {
		return RemotePlan{}, Installation{}, err
	}
	if !ok {
		return RemotePlan{}, Installation{}, errors.New("安裝紀錄不存在")
	}
	if installation.State != "installed" {
		return RemotePlan{}, Installation{}, fmt.Errorf("安裝紀錄目前是 %s", installation.State)
	}
	if !managedDestination(home, installation.Destination) {
		return RemotePlan{}, Installation{}, errors.New("安裝目的地不在 Homing 管理目錄內")
	}
	record, cacheDir, err := loadFetchRecord(home, fetchID)
	if err != nil {
		return record.Plan, Installation{}, err
	}
	if filepath.Clean(record.Plan.Plan.Destination) != filepath.Clean(installation.Destination) {
		return record.Plan, Installation{}, ErrUpdateMismatch
	}
	current, err := Inspect(installation.Destination, installation.Kind)
	if err == nil &&
		current.Checksum == record.Plan.Inspection.Checksum &&
		strings.TrimSpace(installation.Version) == strings.TrimSpace(record.Plan.Inspection.Version) {
		return record.Plan, installation, ErrNoRemoteUpdate
	}

	tmp, err := stageCopy(record.SourceDir, installation.Destination)
	if err != nil {
		return record.Plan, Installation{}, err
	}
	defer os.RemoveAll(tmp)

	backupRoot := filepath.Join(home, ".homing", "library", "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return record.Plan, Installation{}, err
	}
	backup := filepath.Join(
		backupRoot,
		fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), filepath.Base(installation.Destination)),
	)
	movedOld := false
	if _, statErr := os.Stat(installation.Destination); statErr == nil {
		if err := os.Rename(installation.Destination, backup); err != nil {
			return record.Plan, Installation{}, err
		}
		movedOld = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return record.Plan, Installation{}, statErr
	}
	if err := os.Rename(tmp, installation.Destination); err != nil {
		if movedOld {
			_ = os.Rename(backup, installation.Destination)
		}
		return record.Plan, Installation{}, err
	}

	previous := installation
	installation.Version = firstNonEmpty(
		record.Plan.Inspection.Version,
		func() string {
			if record.Plan.Manifest != nil {
				return record.Plan.Manifest.Metadata.Version
			}
			return ""
		}(),
	)
	if movedOld {
		installation.PreviousVersion = previous.Version
		installation.PreviousDownloadSHA256 = previous.DownloadSHA256
		installation.PreviousResolvedURL = previous.ResolvedURL
		installation.PreviousTrust = previous.Trust
		installation.PreviousManifest = previous.Manifest
		installation.PreviousInstalledAt = previous.InstalledAt
	} else {
		installation.PreviousVersion = ""
		installation.PreviousDownloadSHA256 = ""
		installation.PreviousResolvedURL = ""
		installation.PreviousTrust = ""
		installation.PreviousManifest = nil
		installation.PreviousInstalledAt = ""
	}
	installation.Destination = record.Plan.Plan.Destination
	installation.SourceURL = record.Plan.URL
	installation.ResolvedURL = record.Plan.ResolvedURL
	installation.DownloadSHA256 = record.Plan.DownloadSHA256
	installation.Trust = record.Plan.Trust
	installation.Manifest = record.Plan.Manifest
	installation.State = "installed"
	installation.InstalledAt = nowRFC3339()
	installation.RolledBackAt = ""
	installation.TrashPath = ""
	if movedOld {
		installation.BackupPath = backup
	}
	if err := appendInstallation(home, installation); err != nil {
		_ = os.RemoveAll(installation.Destination)
		if movedOld {
			_ = os.Rename(backup, installation.Destination)
		}
		return record.Plan, Installation{}, err
	}
	_ = os.RemoveAll(cacheDir)
	return record.Plan, installation, nil
}

// RollbackInstallation moves the managed resource into the recoverable trash.
func RollbackInstallation(home, id string) (Installation, string, error) {
	installation, ok, err := findInstallation(home, id)
	if err != nil {
		return Installation{}, "", err
	}
	if !ok {
		return Installation{}, "", errors.New("安裝紀錄不存在")
	}
	if installation.State != "installed" {
		return installation, "", fmt.Errorf("安裝紀錄目前是 %s，無法重複回滾", installation.State)
	}
	if !managedDestination(home, installation.Destination) {
		return installation, "", errors.New("安裝紀錄的目的地不在 Homing 管理目錄內，拒絕回滾")
	}
	trashDir := filepath.Join(home, ".homing", "trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return installation, "", err
	}
	backupPath := installation.BackupPath
	if backupPath != "" {
		backupRoot := filepath.Join(home, ".homing", "library", "backups")
		if !isWithin(backupPath, backupRoot) {
			return installation, "", errors.New("備份路徑不在 Homing 管理目錄內，拒絕回滾")
		}
		if _, err := os.Stat(backupPath); err != nil {
			return installation, "", errors.New("上一版備份不存在，已保留目前版本")
		}
	}
	trash := filepath.Join(trashDir, time.Now().UTC().Format("20060102T150405")+"-"+filepath.Base(installation.Destination))
	if err := os.Rename(installation.Destination, trash); err != nil {
		return installation, "", err
	}
	restoredBackup := false
	if backupPath != "" {
		if err := os.Rename(backupPath, installation.Destination); err != nil {
			_ = os.Rename(trash, installation.Destination)
			return installation, "", err
		}
		restoredBackup = true
	}
	installation.RolledBackAt = nowRFC3339()
	installation.TrashPath = trash
	if restoredBackup {
		installation.Version = installation.PreviousVersion
		installation.DownloadSHA256 = installation.PreviousDownloadSHA256
		installation.ResolvedURL = installation.PreviousResolvedURL
		installation.Trust = installation.PreviousTrust
		installation.Manifest = installation.PreviousManifest
		if installation.PreviousInstalledAt != "" {
			installation.InstalledAt = installation.PreviousInstalledAt
		}
		installation.State = "installed"
		installation.BackupPath = ""
		installation.PreviousVersion = ""
		installation.PreviousDownloadSHA256 = ""
		installation.PreviousResolvedURL = ""
		installation.PreviousTrust = ""
		installation.PreviousManifest = nil
		installation.PreviousInstalledAt = ""
	} else {
		installation.State = "rolled_back"
	}
	if err := appendInstallation(home, installation); err != nil {
		if restoredBackup {
			_ = os.Rename(installation.Destination, backupPath)
		}
		_ = os.Rename(trash, installation.Destination)
		return installation, "", err
	}
	return installation, trash, nil
}

type resolvedSource struct {
	DownloadURL string
	Provider    string
	Subdir      string
	ForceJSON   bool
}

func resolveRemote(ctx context.Context, rawURL, ref string) (resolvedSource, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return resolvedSource{}, err
	}
	if parsed.Scheme == "mcp" {
		name := strings.Trim(strings.TrimPrefix(parsed.Path, "/"), " ")
		if name == "" {
			name = parsed.Host
		}
		if name == "" {
			return resolvedSource{}, errors.New("mcp:// 後面需要 server 名稱")
		}
		return resolvedSource{
			DownloadURL: "https://registry.modelcontextprotocol.io/v0/servers/" + url.PathEscape(name),
			Provider:    "mcp", ForceJSON: true,
		}, nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return resolvedSource{}, errors.New("只支援 https、http、mcp:// 或 GitHub/skills.sh 網址")
	}
	host := strings.ToLower(parsed.Hostname())
	if ref == "" && parsed.Fragment != "" {
		ref = parsed.Fragment
		parsed.Fragment = ""
	}
	if ref == "" {
		ref = parsed.Query().Get("ref")
	}
	switch host {
	case "github.com", "www.github.com":
		owner, repo, inferredRef, subdir, err := parseGitHubPath(parsed.Path)
		if err != nil {
			return resolvedSource{}, err
		}
		if ref == "" {
			ref = inferredRef
		}
		download, err := githubArchiveURL(ctx, owner, repo, ref)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{DownloadURL: download, Provider: "github", Subdir: subdir}, nil
	case "skills.sh", "www.skills.sh":
		parts := pathSegments(parsed.Path)
		if len(parts) < 2 {
			return resolvedSource{}, errors.New("skills.sh 網址需要 /<owner>/<repo>/<skill>")
		}
		subdir := ""
		if len(parts) >= 3 {
			subdir = parts[2]
		}
		download, err := githubArchiveURL(ctx, parts[0], parts[1], ref)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{DownloadURL: download, Provider: "skills.sh", Subdir: subdir}, nil
	case "raw.githubusercontent.com":
		return resolvedSource{DownloadURL: parsed.String(), Provider: "github-raw"}, nil
	case "registry.modelcontextprotocol.io":
		return resolvedSource{DownloadURL: parsed.String(), Provider: "mcp", ForceJSON: true}, nil
	}
	return resolvedSource{DownloadURL: parsed.String(), Provider: "direct-url"}, nil
}

func parseGitHubPath(rawPath string) (owner, repo, ref, subdir string, err error) {
	parts := pathSegments(rawPath)
	if len(parts) < 2 {
		return "", "", "", "", errors.New("GitHub 網址需要 /<owner>/<repo>")
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		ref = parts[3]
		if len(parts) > 4 {
			subdir = strings.Join(parts[4:], "/")
		}
	}
	return owner, repo, ref, subdir, nil
}

func githubArchiveURL(ctx context.Context, owner, repo, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		var info struct {
			DefaultBranch string `json:"default_branch"`
		}
		request, err := http.NewRequestWithContext(
			ctx, http.MethodGet, "https://api.github.com/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), nil,
		)
		if err != nil {
			return "", err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("User-Agent", "homing-library")
		response, err := remoteHTTPClient().Do(request)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return "", fmt.Errorf("GitHub repository: %s", response.Status)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&info); err != nil {
			return "", err
		}
		ref = info.DefaultBranch
	}
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("無法判斷 GitHub 預設分支")
	}
	return "https://codeload.github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) +
		"/tar.gz/" + url.PathEscape(ref), nil
}

func downloadTo(ctx context.Context, rawURL, destination string) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Accept", "application/octet-stream, application/gzip, application/zip, application/json")
	request.Header.Set("User-Agent", "homing-library")
	response, err := remoteHTTPClient().Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下載失敗：%s", response.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxRemoteBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", "", copyErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if written > maxRemoteBytes {
		return "", "", errors.New("下載超過 40 MB 上限")
	}
	return hex.EncodeToString(hash.Sum(nil)), response.Header.Get("Content-Type"), nil
}

func extractArchive(archivePath, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	magic := make([]byte, 4)
	n, readErr := io.ReadFull(file, magic)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return readErr
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	switch {
	case n >= 2 && magic[0] == 'P' && magic[1] == 'K':
		_ = file.Close()
		if err := extractZip(archivePath, destination); err != nil {
			return err
		}
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("不是有效的 tar.gz：%w", err)
		}
		defer gzipReader.Close()
		if err := extractTar(gzipReader, destination); err != nil {
			return err
		}
	default:
		return errors.New("无法识别的压缩格式（只支持 zip 与 tar.gz）")
	}
	return flattenArchiveRoot(destination)
}

func extractTar(reader io.Reader, destination string) error {
	var total int64
	files := 0
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		relative, err := safeArchivePath(header.Name)
		if err != nil {
			return err
		}
		if relative == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			files++
			if files > maxExtractFiles {
				return errors.New("壓縮包檔案數超過上限")
			}
			total += header.Size
			if header.Size < 0 || total > maxExpandBytes {
				return errors.New("解壓後大小超過 100 MB 上限")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(0o600)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o700
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, tarReader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("壓縮包包含不支援的項目：%s", header.Name)
		}
	}
	return nil
}

func extractZip(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	var total int64
	files := 0
	for _, entry := range reader.File {
		relative, err := safeArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if relative == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			return fmt.Errorf("壓縮包包含不支援的檔案類型：%s", entry.Name)
		}
		files++
		if files > maxExtractFiles {
			return errors.New("壓縮包檔案數超過上限")
		}
		total += int64(entry.UncompressedSize64)
		if total > maxExpandBytes || entry.UncompressedSize64 > 20<<20 {
			return errors.New("解壓後大小超過上限")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if entry.Mode()&0o111 != 0 {
			mode = 0o700
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = source.Close()
			return err
		}
		_, copyErr := io.CopyN(file, source, int64(entry.UncompressedSize64))
		closeErr := file.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == "" {
		return "", nil
	}
	if path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("壓縮包路徑越界：%s", name)
	}
	if strings.Contains(clean, "\x00") {
		return "", errors.New("壓縮包路徑含 NUL")
	}
	return clean, nil
}

func flattenArchiveRoot(destination string) error {
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}
	root := filepath.Join(destination, entries[0].Name())
	temp := destination + ".root"
	if err := os.Rename(root, temp); err != nil {
		return err
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	return os.Rename(temp, destination)
}

func selectSkillSubdir(sourceDir, subdir string) (string, error) {
	subdir = strings.Trim(strings.TrimSpace(subdir), "/")
	if subdir == "" {
		return sourceDir, nil
	}
	candidates := []string{
		subdir,
		filepath.ToSlash(filepath.Join("skills", subdir)),
		filepath.ToSlash(filepath.Join(".agents", "skills", subdir)),
		filepath.ToSlash(filepath.Join("plugins", subdir)),
	}
	for _, candidate := range candidates {
		clean, err := safeArchivePath(candidate)
		if err != nil {
			continue
		}
		target := filepath.Join(sourceDir, filepath.FromSlash(clean))
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			return target, nil
		}
	}
	return sourceDir, nil
}

func isArchiveURL(rawURL, contentType string) bool {
	lowerPath := strings.ToLower(binaryURLPath(rawURL))
	lowerType := strings.ToLower(contentType)
	return strings.HasSuffix(lowerPath, ".tar.gz") ||
		strings.HasSuffix(lowerPath, ".tgz") ||
		strings.HasSuffix(lowerPath, ".zip") ||
		strings.Contains(lowerType, "application/zip") ||
		strings.Contains(lowerType, "application/gzip") ||
		strings.Contains(lowerType, "application/x-gzip")
}

func remoteHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return errors.New("redirect to unsupported scheme")
			}
			return nil
		},
	}
}

func pathSegments(rawPath string) []string {
	var out []string
	for _, segment := range strings.Split(strings.Trim(rawPath, "/"), "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

func moveFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, io.LimitReader(input, maxRemoteBytes))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func randomID(prefix string) (string, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(data[:]), nil
}

func mustRandomID(prefix string) string {
	id, err := randomID(prefix)
	if err != nil {
		return prefix + "_" + time.Now().UTC().Format("20060102150405")
	}
	return id
}

func safeID(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

// binaryURLPath removes query and fragment without pulling net/url into every
// caller's API.
func binaryURLPath(raw string) string {
	if index := strings.IndexAny(raw, "?#"); index >= 0 {
		return raw[:index]
	}
	return raw
}
