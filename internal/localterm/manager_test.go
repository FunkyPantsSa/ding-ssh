package localterm

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/models"
)

func TestLocalTerminalRealPTYLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell commands; Windows requires native CI")
	}
	t.Setenv("HOME", t.TempDir())
	if runtime.GOOS == "darwin" {
		t.Setenv("LC_ALL", "en_US.UTF-8")
	} else {
		t.Setenv("LC_ALL", "C.UTF-8")
	}
	var mu sync.Mutex
	var output string
	m := NewManager(func(_ string, payload interface{}) {
		if e, ok := payload.(models.OutputEvent); ok {
			data, _ := base64.StdEncoding.DecodeString(e.Data)
			mu.Lock()
			output += string(data)
			mu.Unlock()
		}
	})
	t.Cleanup(m.CloseAll)
	if err := m.Connect("", "", 80, 24); err == nil {
		t.Fatal("empty ID accepted")
	}
	if err := m.Connect("audit", "bash", 80, 24); err != nil {
		t.Fatal(err)
	}
	if !m.Has("audit") || m.SessionName("audit") == "" {
		t.Fatal("missing live session")
	}
	if err := m.Write("audit", "invalid%"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	if err := m.Resize("audit", 93, 27); err != nil {
		t.Fatal(err)
	}
	command := "printf '\\101\\125\\104\\111\\124\\137'; printf 'é中文'; stty size\r"
	if err := m.Write("audit", base64.StdEncoding.EncodeToString([]byte(command))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := output
		mu.Unlock()
		if strings.Contains(got, "AUDIT_é中文") && strings.Contains(got, "27 93") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing real shell output or resize: %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Disconnect("audit"); err != nil {
		t.Fatal(err)
	}
	if m.Has("audit") {
		t.Fatal("disconnected session retained")
	}
	if err := m.Write("audit", "eA=="); err == nil {
		t.Fatal("closed session accepted input")
	}
	if err := m.Connect("audit", "bash", 0, 0); err != nil {
		t.Fatal(err)
	}
	m.CloseAll()
	if m.Has("audit") {
		t.Fatal("close all retained session")
	}
}

func TestImmediateShellExitDoesNotLeaveConnectedSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "zsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", path)
	m := NewManager(func(string, interface{}) {})
	t.Cleanup(m.CloseAll)
	for i := 0; i < 60; i++ {
		if err := m.Connect("quick-exit", "zsh", 80, 24); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for m.Has("quick-exit") && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if m.Has("quick-exit") {
			t.Fatal("already exited shell remains registered as connected")
		}
	}
}

func TestLocalTerminalEnvironmentDefaults(t *testing.T) {
	got := augmentEnv([]string{"PATH=/tmp"})
	if !strings.Contains(strings.Join(got, "\n"), "TERM=xterm-256color") {
		t.Fatal(got)
	}
	base := []string{"TERM=custom", "COLORTERM=custom"}
	if got := augmentEnv(base); strings.Join(got, ":") != strings.Join(base, ":") {
		t.Fatal(got)
	}
	if Normalize("invalid-shell") != DefaultShell() {
		t.Fatal("invalid preference not normalized")
	}
	if ShellTitle(" bash ") != "bash" {
		t.Fatal("shell title not trimmed")
	}
}

func TestLocalTerminalConcurrentReplacementCleansAllSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	connected, closed := 0, 0
	m := NewManager(func(_ string, payload interface{}) {
		if e, ok := payload.(models.StatusEvent); ok {
			mu.Lock()
			defer mu.Unlock()
			if e.Status == models.StatusConnected {
				connected++
			}
			if e.Status == models.StatusClosed {
				closed++
			}
		}
	})
	t.Cleanup(m.CloseAll)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.Connect("same-id", "bash", 80, 24); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	m.CloseAll()
	mu.Lock()
	defer mu.Unlock()
	if connected != 12 || closed != connected {
		t.Fatalf("leaked replaced sessions: connected=%d closed=%d", connected, closed)
	}
}
