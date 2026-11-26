//go:build windows

package platform

import (
	"flag"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	esSystemRequired = 0x00000001
	esContinuous     = 0x80000000
)

func defaultKeepAwakeAvailable() bool {
	if flag.Lookup("test.v") != nil {
		return false
	}
	return true
}

// defaultKeepAwakeRunner holds a Win32 SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)
// assertion on a dedicated OS thread (allowing the monitor to turn off while keeping CPU & network
// awake for background coding agents) until duration elapses or stop() is called.
func defaultKeepAwakeRunner(duration time.Duration) (func(), error) {
	if duration < time.Second {
		duration = time.Second
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setExecState := kernel32.NewProc("SetThreadExecutionState")
	if err := setExecState.Find(); err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	startedCh := make(chan error, 1)
	doneCh := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(doneCh)

		ret, _, callErr := setExecState.Call(uintptr(esContinuous | esSystemRequired))
		if ret == 0 {
			startedCh <- fmt.Errorf("SetThreadExecutionState failed: %v", callErr)
			return
		}
		startedCh <- nil

		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-stopCh:
		case <-timer.C:
		}
		_, _, _ = setExecState.Call(uintptr(esContinuous))
	}()

	if err := <-startedCh; err != nil {
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stopCh)
			select {
			case <-doneCh:
			case <-time.After(500 * time.Millisecond):
			}
		})
	}, nil
}
