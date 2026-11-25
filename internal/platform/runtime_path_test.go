package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacRuntimePathLaunchesAgentWithUserInstalledInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	userHome := t.TempDir()
	agentDir := filepath.Join(userHome, ".local", "bin")
	nodeDir := filepath.Join(userHome, ".npm-global", "bin")
	for _, dir := range []string{agentDir, nodeDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	agentPath := filepath.Join(agentDir, "demo-desktop-agent")
	if err := os.WriteFile(agentPath, []byte("#!/usr/bin/env demo-interpreter\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "demo-interpreter"), []byte("#!/bin/sh\nprintf 'demo-agent-ready'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(agentPath)
	cmd.Env = []string{"PATH=" + macRuntimePath("/usr/bin:/bin", userHome)}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "demo-agent-ready" {
		t.Fatalf("Finder-like agent launch: output=%q error=%v", out, err)
	}
}

func TestMacRuntimePathPreservesExplicitPrecedenceAndIsIdempotent(t *testing.T) {
	userHome := t.TempDir()
	dir := filepath.Join(userHome, ".local", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := "/demo/custom/bin:/usr/bin:" + dir
	got := macRuntimePath(original, userHome)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("explicit command precedence changed: %q", got)
	}
	if count := strings.Count(got, dir); count != 1 {
		t.Fatalf("user bin appears %d times", count)
	}
	if next := macRuntimePath(got, userHome); next != got {
		t.Fatalf("not idempotent: %q -> %q", got, next)
	}
}

func TestMacRuntimePathUsesNewestNVMVersionNumerically(t *testing.T) {
	userHome := t.TempDir()
	for _, version := range []string{"v9.9.0", "v22.1.0", "v22.12.0"} {
		dir := filepath.Join(userHome, ".nvm", "versions", "node", version, "bin")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	got := macRuntimePath("/usr/bin:/bin", userHome)
	want := filepath.Join(userHome, ".nvm", "versions", "node", "v22.12.0", "bin")
	if !strings.Contains(got, want) || strings.Contains(got, "v9.9.0") || strings.Contains(got, "v22.1.0/") {
		t.Fatalf("incorrect Node fallback: %q", got)
	}
}

func TestMacRuntimePathDoesNotAddMissingOrRelativeDirectories(t *testing.T) {
	got := macRuntimePath("/usr/bin:/bin", "")
	for _, part := range filepath.SplitList(got) {
		if !filepath.IsAbs(part) {
			t.Fatalf("added unsafe relative directory %q", part)
		}
	}
	userHome := t.TempDir()
	got = macRuntimePath("/usr/bin:/bin", userHome)
	if strings.Contains(got, userHome) {
		t.Fatalf("added non-existent installation directory: %q", got)
	}
}
