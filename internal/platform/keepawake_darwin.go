//go:build darwin

package platform

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

func defaultKeepAwakeAvailable() bool {
	if flag.Lookup("test.v") != nil {
		return false
	}
	_, err := os.Stat("/usr/bin/caffeinate")
	return err == nil
}

func defaultKeepAwakeRunner(duration time.Duration) (func(), error) {
	secs := int(duration.Seconds())
	if secs < 1 {
		secs = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/usr/bin/caffeinate", "-i", "-t", strconv.Itoa(secs))
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
		})
	}, nil
}
