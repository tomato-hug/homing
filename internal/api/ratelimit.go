package api

import (
	"net/http"
	"sync"
	"time"
)

type rateBucket struct {
	window time.Time
	count  int
}

var deviceRates = struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}{buckets: map[string]*rateBucket{}}

// CheckRate enforces a per-device sliding window for expensive write actions.
// It returns true when the request may proceed and writes 429 otherwise.
func (d *Deps) CheckRate(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	limit int,
	window time.Duration,
) bool {
	if limit <= 0 {
		return true
	}
	key := d.rateKey(r) + "\x00" + action
	now := time.Now()
	deviceRates.mu.Lock()
	bucket := deviceRates.buckets[key]
	if bucket == nil || now.Sub(bucket.window) >= window {
		bucket = &rateBucket{window: now}
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
	header := r.Header.Get("Authorization")
	if len(header) > len("Bearer ") && header[:len("Bearer ")] == "Bearer " {
		token := header[len("Bearer "):]
		if len(token) > 12 {
			return token[:12]
		}
		return token
	}
	return r.RemoteAddr
}
