//go:build !darwin && !windows

package platform

import (
	"errors"
	"time"
)

func defaultKeepAwakeAvailable() bool { return false }

func defaultKeepAwakeRunner(time.Duration) (func(), error) {
	return nil, errors.New("keep-awake not supported on this OS")
}
