package sshx

import (
	"testing"
	"time"
)

type blockedTerminalInput struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockedTerminalInput) Write(data []byte) (int, error) {
	close(w.started)
	<-w.release
	return len(data), nil
}

func (*blockedTerminalInput) Close() error { return nil }

func TestBlockedTerminalWriteAllowsKeepAliveUpdate(t *testing.T) {
	input := &blockedTerminalInput{started: make(chan struct{}), release: make(chan struct{})}
	session := &Session{stdin: input}
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_ = session.Write([]byte("x"))
	}()
	defer func() {
		close(input.release)
		<-writeDone
	}()
	<-input.started

	updated := make(chan struct{})
	go func() {
		session.SetKeepAliveEnabled(true)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("blocked terminal write held the session state lock")
	}
}
