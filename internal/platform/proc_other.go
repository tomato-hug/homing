//go:build !windows

package platform

import "os/exec"

// HideChildWindow is a no-op on POSIX platforms.
func HideChildWindow(*exec.Cmd) {}

// AttachParentConsoleIfCLI is a no-op on POSIX platforms.
func AttachParentConsoleIfCLI([]string) {}
