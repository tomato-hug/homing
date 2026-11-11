package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Installation is the durable record that makes a remote import auditable and
// reversible. The JSONL file is append-only; the last line for an ID wins.
type Installation struct {
	ID                     string    `json:"id"`
	ResourceID             string    `json:"resource_id"`
	Name                   string    `json:"name"`
	Kind                   string    `json:"kind"`
	Version                string    `json:"version,omitempty"`
	Destination            string    `json:"destination"`
	SourceURL              string    `json:"source_url,omitempty"`
	ResolvedURL            string    `json:"resolved_url,omitempty"`
	DownloadSHA256         string    `json:"download_sha256,omitempty"`
	Trust                  string    `json:"trust"`
	Manifest               *Manifest `json:"manifest,omitempty"`
	State                  string    `json:"state"`
	InstalledAt            string    `json:"installed_at"`
	RolledBackAt           string    `json:"rolled_back_at,omitempty"`
	TrashPath              string    `json:"trash_path,omitempty"`
	BackupPath             string    `json:"backup_path,omitempty"`
	PreviousVersion        string    `json:"previous_version,omitempty"`
	PreviousDownloadSHA256 string    `json:"previous_download_sha256,omitempty"`
	PreviousResolvedURL    string    `json:"previous_resolved_url,omitempty"`
	PreviousTrust          string    `json:"previous_trust,omitempty"`
	PreviousManifest       *Manifest `json:"previous_manifest,omitempty"`
	PreviousInstalledAt    string    `json:"previous_installed_at,omitempty"`
}

func installRecordsPath(home string) string {
	return filepath.Join(home, ".homing", "library", "installations.jsonl")
}

func appendInstallation(home string, installation Installation) error {
	path := installRecordsPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encodeErr := encoder.Encode(installation)
	syncErr := file.Sync()
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// Installations returns the last known state of every remote installation.
func Installations(home string) ([]Installation, error) {
	file, err := os.Open(installRecordsPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return []Installation{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	latest := map[string]Installation{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 2<<20)
	for scanner.Scan() {
		var installation Installation
		if json.Unmarshal(scanner.Bytes(), &installation) != nil || installation.ID == "" {
			continue
		}
		latest[installation.ID] = installation
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(latest))
	for _, installation := range latest {
		out = append(out, installation)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].InstalledAt > out[j].InstalledAt
	})
	return out, nil
}

func findInstallation(home, id string) (Installation, bool, error) {
	all, err := Installations(home)
	if err != nil {
		return Installation{}, false, err
	}
	for _, installation := range all {
		if installation.ID == id {
			return installation, true, nil
		}
	}
	return Installation{}, false, nil
}

func writeJSONAtomic(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".json-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
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
	return os.Rename(tempName, path)
}

func managedDestination(home, destination string) bool {
	path, err := filepath.Abs(destination)
	if err != nil {
		return false
	}
	home, _ = filepath.Abs(home)
	for _, root := range []string{
		filepath.Join(home, ".homing", "skills"),
		filepath.Join(home, ".homing", "instructions"),
		filepath.Join(home, ".homing", "resources"),
		filepath.Join(home, ".homing", "integrations"),
	} {
		if isWithin(path, root) && path != root &&
			!strings.Contains(strings.TrimPrefix(path, root), string(filepath.Separator)+".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
