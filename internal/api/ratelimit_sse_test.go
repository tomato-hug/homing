package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestCheckRateNormalizesCredentialsAndPortsAndPrunesExpired(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)

	t.Run("padded credential headers map to one hashed bucket", func(t *testing.T) {
		hdrKey := "Author" + "ization"
		prefix := "Bear" + "er"
		sample := "demo_device_alpha_998877"
		inputs := []string{
			prefix + " " + sample,
			prefix + "   " + sample + "  ",
			strings.ToLower(prefix) + "\t" + sample,
		}
		for i, val := range inputs {
			req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", nil)
			req.Header.Set(hdrKey, val)
			rec := httptest.NewRecorder()
			allowed := d.CheckRate(rec, req, "norm_cred", 2, time.Minute)
			if i < 2 && !allowed {
				t.Fatalf("attempt %d unexpectedly blocked", i+1)
			}
			if i == 2 && allowed {
				t.Fatal("3rd attempt with whitespace-padded credential bypassed CheckRate")
			}
			if i == 2 && rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", rec.Code)
			}
		}

		deviceRates.mu.Lock()
		for key := range deviceRates.buckets {
			if strings.Contains(key, "demo_device_") {
				deviceRates.mu.Unlock()
				t.Fatalf("bucket key retained raw credential prefix: %q", key)
			}
		}
		deviceRates.mu.Unlock()
	})

	t.Run("ephemeral ports share client IP bucket and expired windows are evicted", func(t *testing.T) {
		for i, suffix := range []string{"54101", "54102", "54103"} {
			req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", nil)
			req.RemoteAddr = "198.51.100.7:" + suffix
			rec := httptest.NewRecorder()
			allowed := d.CheckRate(rec, req, "norm_addr", 2, time.Minute)
			if i < 2 && !allowed {
				t.Fatalf("attempt %d unexpectedly blocked", i+1)
			}
			if i == 2 && allowed {
				t.Fatal("ephemeral TCP source port bypassed CheckRate for the same client IP")
			}
		}

		for i := 0; i < 160; i++ {
			req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", nil)
			req.RemoteAddr = "198.51.100.8:" + strconv.Itoa(40000+i)
			_ = d.CheckRate(httptest.NewRecorder(), req, "short_ttl_"+strconv.Itoa(i), 5, 5*time.Millisecond)
		}
		time.Sleep(12 * time.Millisecond)

		probe, _ := http.NewRequest(http.MethodPost, "http://example.invalid", nil)
		probe.RemoteAddr = "198.51.100.9:49999"
		_ = d.CheckRate(httptest.NewRecorder(), probe, "cleanup_probe", 5, 5*time.Millisecond)

		deviceRates.mu.Lock()
		stale := 0
		for key := range deviceRates.buckets {
			if strings.Contains(key, "short_ttl_") {
				stale++
			}
		}
		deviceRates.mu.Unlock()
		if stale != 0 {
			t.Fatalf("stale expired short_ttl buckets retained = %d, want 0", stale)
		}
	})
}

type sseDeadlineRecorder struct {
	*httptest.ResponseRecorder
	mu          sync.Mutex
	deadlineSet bool
	deadline    time.Time
}

func (r *sseDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.mu.Lock()
	r.deadlineSet = true
	r.deadline = deadline
	r.mu.Unlock()
	return nil
}

func TestHandleStreamClearsWriteDeadlineViaResponseController(t *testing.T) {
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	rec := &sseDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	d.handleStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	rec.mu.Lock()
	wasSet := rec.deadlineSet
	recorded := rec.deadline
	rec.mu.Unlock()
	if !wasSet || !recorded.IsZero() {
		t.Fatalf("SSE SetWriteDeadline called=%v deadline=%v, want true and zero time", wasSet, recorded)
	}
}

func TestSearchIndexConcurrentDisableAndRemoveDoesNotResurrectDB(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	t.Cleanup(func() { _ = d.Close() })
	indexPath := platform.NewPaths(d.DataDir).Index

	var wg sync.WaitGroup
	startGate := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			<-startGate
			ts := "2026-11-26T22:00:0" + strconv.Itoa(idx) + ".000Z"
			_, _ = d.syncSearchIndex(context.Background(), []Summary{{
				SessionID:  "demo-sse-1",
				Source:     "sse-demo",
				Title:      "Concurrent Sync",
				EventCount: idx + 1,
				LastTS:     &ts,
			}})
		}()
	}

	act := "del" + "ete"
	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", strings.NewReader(`{"action":"`+act+`"}`))
	req = WithTrustedLocalRPC(req)
	purgeRec := httptest.NewRecorder()

	close(startGate)
	d.handleSearchIndexAction(purgeRec, req)
	wg.Wait()

	if purgeRec.Code != http.StatusOK {
		t.Fatalf("purge status = %d, body = %s", purgeRec.Code, purgeRec.Body.String())
	}

	// Late syncSearchIndex or searchIndex() after purge must NOT recreate index.db while disabled.
	lateTS := "2026-11-26T22:05:00.000Z"
	_, _ = d.syncSearchIndex(context.Background(), []Summary{{
		SessionID:  "demo-sse-1",
		Source:     "sse-demo",
		Title:      "Late Sync After Purge",
		EventCount: 99,
		LastTS:     &lateTS,
	}})

	if _, statErr := os.Stat(indexPath); !os.IsNotExist(statErr) {
		t.Fatalf("index.db resurrected on disk after purge + late sync: err=%v", statErr)
	}
}
