package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func packInstallRecordsPath(home string) string {
	return filepath.Join(home, ".homing", "library", "pack-installations.jsonl")
}

func appendPackInstallation(home string, installation PackInstallation) error {
	path := packInstallRecordsPath(home)
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

// PackInstallations returns the last known state for each pack transaction.
func PackInstallations(home string) ([]PackInstallation, error) {
	file, err := os.Open(packInstallRecordsPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return []PackInstallation{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	latest := map[string]PackInstallation{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 2<<20)
	for scanner.Scan() {
		var installation PackInstallation
		if json.Unmarshal(scanner.Bytes(), &installation) != nil || installation.ID == "" {
			continue
		}
		latest[installation.ID] = installation
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	out := make([]PackInstallation, 0, len(latest))
	for _, installation := range latest {
		out = append(out, installation)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].InstalledAt > out[j].InstalledAt
	})
	return out, nil
}

func findPackInstallation(home, id string) (PackInstallation, bool, error) {
	all, err := PackInstallations(home)
	if err != nil {
		return PackInstallation{}, false, err
	}
	for _, installation := range all {
		if installation.ID == id {
			return installation, true, nil
		}
	}
	return PackInstallation{}, false, nil
}
