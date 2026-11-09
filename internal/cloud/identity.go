package cloud

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomato-hug/homing/internal/protocol"
)

// Identity is the computer's long-lived Ed25519 identity. The seed lives in
// <data-dir>/cloud/identity.json with mode 0600; on macOS this file should
// eventually move into the Keychain, which is why the seed is the only secret
// stored.
type Identity struct {
	keys protocol.KeyPair
}

type identityFile struct {
	Version int    `json:"version"`
	Seed    string `json:"seed_base64"`
	Public  string `json:"public_key_base64,omitempty"`
}

// LoadIdentity reads the identity from dir, generating and saving one on first
// use.
func LoadIdentity(dir string) (*Identity, error) {
	path := filepath.Join(dir, "identity.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		var file identityFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("cloud: parse %s: %w", path, err)
		}
		seed, err := base64.StdEncoding.DecodeString(file.Seed)
		if err != nil {
			return nil, fmt.Errorf("cloud: identity seed: %w", err)
		}
		keys, err := protocol.KeyPairFromSeed(seed)
		if err != nil {
			return nil, fmt.Errorf("cloud: identity seed: %w", err)
		}
		return &Identity{keys: keys}, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("cloud: read identity: %w", err)
	}

	keys, err := protocol.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("cloud: generate identity: %w", err)
	}
	identity := &Identity{keys: keys}
	file := identityFile{
		Version: 1,
		Seed:    base64.StdEncoding.EncodeToString(keys.Seed()),
		Public:  base64.StdEncoding.EncodeToString(keys.PublicKey),
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cloud: create data directory: %w", err)
	}
	if err := writeFileAtomic(path, append(encoded, '\n'), 0o600); err != nil {
		return nil, err
	}
	return identity, nil
}

func (i *Identity) DeviceID() string { return protocol.DeviceID(i.keys.PublicKey) }

func (i *Identity) Fingerprint() string { return protocol.Fingerprint(i.keys.PublicKey) }

func (i *Identity) PublicKeyBase64() string { return protocol.ToBase64(i.keys.PublicKey) }

func (i *Identity) Keys() protocol.KeyPair { return i.keys }

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cloud: create %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("cloud: write %s: %w", path, err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
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
	if err := os.Rename(tempName, path); err != nil {
		// Windows cannot rename over an existing file.
		if removeErr := os.Remove(path); removeErr == nil {
			if err := os.Rename(tempName, path); err == nil {
				return nil
			}
		}
		return fmt.Errorf("cloud: replace %s: %w", path, err)
	}
	return nil
}
