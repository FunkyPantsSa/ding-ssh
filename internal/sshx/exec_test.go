package sshx

import (
	"context"
	"encoding/binary"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"golang.org/x/crypto/ssh"
)

func execTestClient(t *testing.T, handle func(ssh.NewChannel)) *ssh.Client {
	t.Helper()
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSHChannels(t, "job-secret", false, handle)
	trustLocal(t, server)
	client, err := dialNodeContext(context.Background(), localNode(t, server, "job-secret"), noopProgress)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func TestIndependentExecSeparatesStreamsAndExit(t *testing.T) {
	command := make(chan string, 1)
	client := execTestClient(t, func(n ssh.NewChannel) {
		ch, reqs, e := n.Accept()
		if e != nil {
			return
		}
		defer ch.Close()
		for req := range reqs {
			if req.Type != "exec" {
				_ = req.Reply(false, nil)
				continue
			}
			var p struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &p)
			command <- p.Command
			_ = req.Reply(true, nil)
			_, _ = io.WriteString(ch, "out-中文")
			_, _ = io.WriteString(ch.Stderr(), "diagnostic")
			code := make([]byte, 4)
			binary.BigEndian.PutUint32(code, 7)
			_, _ = ch.SendRequest("exit-status", false, code)
			return
		}
	})
	m := NewManager(nil)
	m.sessions["s"] = &Session{ID: "s", client: client}
	p, err := m.PrepareExec(ExecRequest{SessionID: "s", Command: "run something"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	out := map[string]string{}
	r := p.Run(context.Background(), func(stream string, b []byte) { mu.Lock(); out[stream] += string(b); mu.Unlock() })
	if r.ExitCode == nil || *r.ExitCode != 7 || !r.Known || !r.TerminationProven {
		t.Fatalf("bad exit: %+v", r)
	}
	if out["stdout"] != "out-中文" || out["stderr"] != "diagnostic" {
		t.Fatalf("bad streams: %#v", out)
	}
	if <-command != "run something" {
		t.Fatal("command altered")
	}
}
func TestIndependentExecCancelDoesNotClaimRemoteTermination(t *testing.T) {
	started := make(chan struct{})
	client := execTestClient(t, func(n ssh.NewChannel) {
		ch, reqs, e := n.Accept()
		if e != nil {
			return
		}
		defer ch.Close()
		for req := range reqs {
			switch req.Type {
			case "exec":
				_ = req.Reply(true, nil)
				close(started)
			case "signal":
				_ = req.Reply(true, nil)
			}
		}
	})
	p := &PreparedExec{client: client, command: "long command"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ExecResult, 1)
	go func() { done <- p.Run(ctx, nil) }()
	<-started
	cancel()
	select {
	case r := <-done:
		if r.Known || r.TerminationProven || !r.CancelRequested {
			t.Fatalf("cancel overclaimed: %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel stuck")
	}
}
func TestExecCommandValidatesModesAndQuotes(t *testing.T) {
	if _, e := ExecCommand(ExecRequest{Command: "pwd", Cwd: "/tmp"}); e == nil {
		t.Fatal("raw cwd silently applied")
	}
	if _, e := ExecCommand(ExecRequest{Command: "pwd", Mode: "posix", Env: map[string]string{"BAD;exit": "x"}}); e == nil {
		t.Fatal("bad env accepted")
	}
	c, e := ExecCommand(ExecRequest{Command: "printf '%s' \"$X\"", Mode: "posix", Cwd: "/tmp/it's", Env: map[string]string{"X": "$(danger)\nhello"}})
	if e != nil || !strings.HasPrefix(c, "sh -c ") {
		t.Fatalf("bad posix command: %q %v", c, e)
	}
}

func TestIndependentExecPinsTransportAcrossSessionReplacement(t *testing.T) {
	fixture := func(label string) *ssh.Client {
		return execTestClient(t, func(n ssh.NewChannel) {
			ch, reqs, err := n.Accept()
			if err != nil {
				return
			}
			defer ch.Close()
			for req := range reqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				_ = req.Reply(true, nil)
				_, _ = io.WriteString(ch, label)
				_, _ = ch.SendRequest("exit-status", false, make([]byte, 4))
				return
			}
		})
	}
	oldClient, newClient := fixture("old-target"), fixture("new-target")
	m := NewManager(nil)
	m.sessions["same-id"] = &Session{ID: "same-id", client: oldClient}
	prepared, err := m.PrepareExec(ExecRequest{SessionID: "same-id", Command: "read-only-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	m.sessions["same-id"] = &Session{ID: "same-id", client: newClient}
	var output strings.Builder
	r := prepared.Run(context.Background(), func(stream string, b []byte) {
		if stream == "stdout" {
			output.Write(b)
		}
	})
	if !r.Known || r.ExitCode == nil || *r.ExitCode != 0 || output.String() != "old-target" {
		t.Fatalf("prepared command retargeted: %q %+v", output.String(), r)
	}
}

func TestIndependentExecBoundsUnresponsiveChannelOpen(t *testing.T) {
	var opens atomic.Int32
	client := execTestClient(t, func(ssh.NewChannel) {
		opens.Add(1) // Deliberately never accept or reject the SSH channel.
	})
	prepared := &PreparedExec{client: client, command: "never-started"}
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		r := prepared.Run(ctx, nil)
		cancel()
		if !r.CancelRequested || r.ExitCode != nil {
			t.Fatalf("unresponsive open result %+v", r)
		}
	}
	if opens.Load() != 8 || len(transportExecSlots(client)) != 8 {
		t.Fatalf("abandoned opens exceeded per-transport budget: opens=%d slots=%d", opens.Load(), len(transportExecSlots(client)))
	}
	_ = client.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, exists := execTransportBudgets.Load(client); !exists {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("transport budget was retained after disconnection")
}
