package adapter

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IngestKinds are the event kinds a custom agent may push. They use the same
// words as the Node relay, so P0/P1/P2 grading stays identical.
var IngestKinds = map[string]bool{
	"needs_reply": true,
	"approval":    true,
	"done":        true,
	"error":       true,
	"message":     true,
}

// IngestToken is one agent's credential. The file layout matches the Node relay's
// data/ingest-tokens.json so a data directory can move between the two
// implementations unchanged.
type IngestToken struct {
	Agent      string     `json:"agent"`
	Token      string     `json:"token"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	Reply      *struct {
		URL       string `json:"url,omitempty"`
		TimeoutMS int    `json:"timeout_ms,omitempty"`
	} `json:"reply,omitempty"`
}

type tokenFile struct {
	Version int           `json:"version"`
	Agents  []IngestToken `json:"agents"`
}

// IngestStore keeps the tokens on disk under the relay/agent data directory.
type IngestStore struct {
	path string
	mu   sync.Mutex
}

// NewIngestStore uses <dataDir>/ingest-tokens.json.
func NewIngestStore(dataDir string) *IngestStore {
	return &IngestStore{path: filepath.Join(dataDir, "ingest-tokens.json")}
}

func (s *IngestStore) load() tokenFile {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return tokenFile{Version: 1}
	}
	var f tokenFile
	if json.Unmarshal(raw, &f) != nil {
		return tokenFile{Version: 1}
	}
	if f.Version == 0 {
		f.Version = 1
	}
	return f
}

func (s *IngestStore) save(f tokenFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", s.path, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Issue creates (or replaces) a token for agent.
func (s *IngestStore) Issue(agentID, replyURL string, replyTimeoutMS int) (string, error) {
	if agentID == "" {
		return "", errors.New("agent 不能为空")
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := "hmg_" + base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	out := f.Agents[:0]
	for _, a := range f.Agents {
		if a.Agent != agentID {
			out = append(out, a)
		}
	}
	t := IngestToken{Agent: agentID, Token: token, CreatedAt: time.Now().UTC()}
	if replyURL != "" {
		t.Reply = &struct {
			URL       string `json:"url,omitempty"`
			TimeoutMS int    `json:"timeout_ms,omitempty"`
		}{URL: replyURL, TimeoutMS: replyTimeoutMS}
	}
	f.Agents = append(out, t)
	if err := s.save(f); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke accepts either a token or an agent id.
func (s *IngestStore) Revoke(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	now := time.Now().UTC()
	hit := false
	for i := range f.Agents {
		if f.Agents[i].Token == key || f.Agents[i].Agent == key {
			if f.Agents[i].RevokedAt == nil {
				f.Agents[i].RevokedAt = &now
			}
			hit = true
		}
	}
	if hit {
		_ = s.save(f)
	}
	return hit
}

// List returns the tokens without exposing the secrets (only their last 6 chars),
// which is what GET /api/adapters shows.
func (s *IngestStore) List() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	out := make([]map[string]any, 0, len(f.Agents))
	for _, a := range f.Agents {
		kind := "none"
		if a.Reply != nil {
			kind = "url"
		}
		tail := a.Token
		if len(tail) > 6 {
			tail = tail[len(tail)-6:]
		}
		out = append(out, map[string]any{
			"agent":        a.Agent,
			"created_at":   a.CreatedAt,
			"revoked_at":   a.RevokedAt,
			"last_seen_at": a.LastSeenAt,
			"reply":        kind,
			"token_tail":   tail,
		})
	}
	return out
}

// Lookup finds a live token.
func (s *IngestStore) Lookup(token string) *IngestToken {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	for i := range f.Agents {
		if f.Agents[i].Token == token && f.Agents[i].RevokedAt == nil {
			t := f.Agents[i]
			return &t
		}
	}
	return nil
}

// ReplyChannel returns the live token registered for agentID when it has a
// callback URL. It deliberately ignores revoked tokens and read-only agents.
func (s *IngestStore) ReplyChannel(agentID string) *IngestToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	for i := range f.Agents {
		token := f.Agents[i]
		if token.Agent == agentID && token.RevokedAt == nil &&
			token.Reply != nil && token.Reply.URL != "" {
			return &token
		}
	}
	return nil
}

// Touch records that the agent pushed something.
func (s *IngestStore) Touch(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load()
	now := time.Now().UTC()
	for i := range f.Agents {
		if f.Agents[i].Agent == agentID {
			f.Agents[i].LastSeenAt = &now
		}
	}
	_ = s.save(f)
}

// IngestBody is the JSON a custom agent posts to /api/ingest.
type IngestBody struct {
	Agent     string `json:"agent"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
}
