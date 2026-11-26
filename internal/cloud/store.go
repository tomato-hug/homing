package cloud

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Peer is a phone or another computer this machine has paired with.
type Peer struct {
	DeviceID  string    `json:"device_id"`
	PublicKey string    `json:"public_key"`
	Name      string    `json:"name,omitempty"`
	Platform  string    `json:"platform,omitempty"`
	PairedAt  time.Time `json:"paired_at"`
}

// PeerStore is a small JSON file with the pairing list and the relay server
// address. It is deliberately separate from internal/store (still owned by
// WorkBuddy): when that lands, this becomes a thin adapter over it.
type PeerStore struct {
	path           string
	mu             sync.RWMutex
	server         string
	relayKey       string
	useSystemProxy bool
	peers          map[string]Peer
}

type peerFile struct {
	Version        int             `json:"version"`
	Server         string          `json:"server,omitempty"`
	RelayKey       string          `json:"relay_key,omitempty"`
	UseSystemProxy bool            `json:"use_system_proxy,omitempty"`
	Peers          map[string]Peer `json:"peers"`
}

func LoadPeerStore(dir string) (*PeerStore, error) {
	path := filepath.Join(dir, "peers.json")
	store := &PeerStore{path: path, peers: make(map[string]Peer)}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cloud: read peers: %w", err)
	}
	var file peerFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("cloud: parse peers: %w", err)
	}
	store.server = file.Server
	store.relayKey = file.RelayKey
	store.useSystemProxy = file.UseSystemProxy
	for id, peer := range file.Peers {
		if peer.DeviceID == "" {
			peer.DeviceID = id
		}
		if peer.DeviceID == "" || peer.PublicKey == "" {
			continue
		}
		store.peers[id] = peer
	}
	return store, nil
}

func (s *PeerStore) saveLocked() error {
	file := peerFile{
		Version:        1,
		Server:         s.server,
		RelayKey:       s.relayKey,
		UseSystemProxy: s.useSystemProxy,
		Peers:          s.peers,
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(encoded, '\n'), 0o600)
}

func (s *PeerStore) Add(peer Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if peer.DeviceID == "" || peer.PublicKey == "" {
		return fmt.Errorf("cloud: peer needs a device id and public key")
	}
	if peer.PairedAt.IsZero() {
		peer.PairedAt = time.Now().UTC()
	}
	s.peers[peer.DeviceID] = peer
	return s.saveLocked()
}

func (s *PeerStore) Remove(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, deviceID)
	return s.saveLocked()
}

func (s *PeerStore) Get(deviceID string) (Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[deviceID]
	return peer, ok
}

func (s *PeerStore) List() []Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.peers))
	for _, peer := range s.peers {
		out = append(out, peer)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PairedAt.Before(out[j].PairedAt) })
	return out
}

func (s *PeerStore) Server() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server
}

func (s *PeerStore) SetServer(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.server = server
	return s.saveLocked()
}

func (s *PeerStore) UseSystemProxy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.useSystemProxy
}

func (s *PeerStore) SetUseSystemProxy(useProxy bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.useSystemProxy = useProxy
	return s.saveLocked()
}

func (s *PeerStore) RelayKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.relayKey
}

func (s *PeerStore) RelayKeyHint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.relayKey == "" {
		return ""
	}
	if len(s.relayKey) <= 10 {
		return s.relayKey[:min(4, len(s.relayKey))] + "••••"
	}
	return s.relayKey[:10] + "••••"
}

func (s *PeerStore) SetRelayKey(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relayKey = key
	return s.saveLocked()
}
