//go:build windows

package platform

import (
	"os"
	"os/exec"
	"syscall"
)

const attachParentProcess = ^uint32(0) // (DWORD)-1

// HideChildWindow configures cmd on Windows so spawning background CLI helpers
// (reg.exe, tasklist.exe, node, ACP agents) from a GUI subsystem binary never
// flashes a black conhost.exe window on the user's desktop.
func HideChildWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return
	}
	cmd.SysProcAttr.HideWindow = true
}

// AttachParentConsoleIfCLI attaches stdout/stderr to the parent PowerShell/CMD
// console when a -H=windowsgui binary (Homing.exe) is invoked with CLI arguments.
func AttachParentConsoleIfCLI(args []string) {
	if len(args) == 0 {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	attachConsole := kernel32.NewProc("AttachConsole")
	r, _, _ := attachConsole.Call(uintptr(attachParentProcess))
	if r == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = out
	}
	if errOut, err := os.OpenFile("CONERR$", os.O_WRONLY, 0); err == nil {
		os.Stderr = errOut
	}
}
