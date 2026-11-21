package sshx

import (
	"encoding/base64"
	"io"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/models"
	"golang.org/x/crypto/ssh"
)

func sessionFixture(exit bool) func(ssh.NewChannel) {
	return func(in ssh.NewChannel) {
		ch, requests, err := in.Accept()
		if err != nil {
			return
		}
		defer ch.Close()
		for req := range requests {
			switch req.Type {
			case "pty-req", "window-change", "env":
				_ = req.Reply(true, nil)
			case "shell":
				_ = req.Reply(true, nil)
				if exit {
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					return
				}
				go func() { _, _ = io.Copy(ch, ch) }()
			default:
				_ = req.Reply(false, nil)
			}
		}
	}
}

func TestSSHManagerRealInteractiveChannelLifecycle(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSHChannels(t, "audit", false, sessionFixture(false))
	trustLocal(t, server)
	node := localNode(t, server, "audit")
	node.JumpServerMode = true
	output := make(chan string, 30)
	m := NewManager(func(_ string, payload interface{}) {
		if e, ok := payload.(models.OutputEvent); ok {
			data, _ := base64.StdEncoding.DecodeString(e.Data)
			output <- string(data)
		}
	})
	t.Cleanup(m.CloseAll)
	if err := m.Connect("terminal", node, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := m.Connect("terminal", node, 80, 24); err == nil {
		t.Fatal("duplicate connection accepted")
	}
	if err := m.Resize("terminal", 92, 28); err != nil {
		t.Fatal(err)
	}
	const text = "audit-é中文\n"
	if err := m.Write("terminal", base64.StdEncoding.EncodeToString([]byte(text))); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-output:
		if got != text {
			t.Fatalf("SSH output %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing SSH output")
	}
	// Concurrent connects/closes exercise the manager map and cleanup paths.
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := m.Connect(id, node, 80, 24); err != nil {
				t.Error(err)
				return
			}
			if err := m.Disconnect(id); err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	if err := m.Disconnect("terminal"); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("closed SSH sessions retained")
	}
}

func TestSSHImmediateShellExitDoesNotLeaveSession(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSHChannels(t, "audit", false, sessionFixture(true))
	trustLocal(t, server)
	node := localNode(t, server, "audit")
	node.JumpServerMode = true
	m := NewManager(func(string, interface{}) {})
	t.Cleanup(m.CloseAll)
	for i := 0; i < 80; i++ {
		_ = m.Connect("quick-exit", node, 80, 24)
		deadline := time.Now().Add(3 * time.Second)
		for len(m.List()) > 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if len(m.List()) > 0 {
			t.Fatal("exited SSH shell retained as connected")
		}
	}
}
