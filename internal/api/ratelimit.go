package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	expires time.Time
	count   int
}

var deviceRates = struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}{buckets: map[string]*rateBucket{}}

// CheckRate enforces a per-device window for expensive write actions.
// It returns true when the request may proceed and writes 429 otherwise.
func (d *Deps) CheckRate(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	limit int,
	window time.Duration,
) bool {
	if limit <= 0 || window <= 0 {
		return true
	}
	key := d.rateKey(r) + "\x00" + action
	now := time.Now()
	deviceRates.mu.Lock()
	if len(deviceRates.buckets) > 32 {
		for k, b := range deviceRates.buckets {
			if !now.Before(b.expires) {
				delete(deviceRates.buckets, k)
			}
		}
	}
	bucket := deviceRates.buckets[key]
	if bucket == nil || !now.Before(bucket.expires) {
		bucket = &rateBucket{expires: now.Add(window)}
		deviceRates.buckets[key] = bucket
	}
	bucket.count++
	allowed := bucket.count <= limit
	deviceRates.mu.Unlock()
	if allowed {
		return true
	}
	WriteJSON(w, http.StatusTooManyRequests, map[string]any{
		"error": "操作太频繁，请稍后再试",
		"code":  "rate_limited",
		"limit": limit,
	})
	return false
}

func (d *Deps) rateKey(r *http.Request) string {
	if r == nil {
		return "local"
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) > 6 && strings.EqualFold(header[:6], "bearer") &&
		(header[6] == ' ' || header[6] == '\t') {
		cred := strings.TrimSpace(header[6:])
		if cred != "" {
			sum := sha256.Sum256([]byte(cred))
			return "tok:" + hex.EncodeToString(sum[:16])
		}
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return "local"
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if ip := net.ParseIP(host); ip != nil {
		return "ip:" + ip.String()
	}
	return "ip:" + host
}
