package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileDatabaseConcurrentQueueAndAcknowledgements(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "demo-relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for message := 0; message < 8; message++ {
				id := fmt.Sprintf("demo-envelope-%d-%d", worker, message)
				if err := db.Enqueue(ctx, []byte(`{"demo":"ciphertext"}`), id, "demo-host", "demo-phone", time.Now()); err != nil {
					errors <- err
					return
				}
				acked, err := db.Ack(ctx, "demo-phone", id)
				if err != nil {
					errors <- err
					return
				}
				if !acked {
					errors <- fmt.Errorf("missing acknowledgement for %s", id)
					return
				}
			}
		}(worker)
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	count, err := db.QueueCount(ctx, "demo-phone")
	if err != nil || count != 0 {
		t.Errorf("queue after acknowledgements = %d, error = %v", count, err)
	}
}
