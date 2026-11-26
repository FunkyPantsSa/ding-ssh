package sshx

import (
	"testing"

	"ding-ssh/internal/models"
)

func TestSessionInfoReflectsClosedState(t *testing.T) {
	s := &Session{
		ID:        "sess-1",
		server:    models.ServerNode{Name: "Prod", Host: "10.0.0.1", User: "root"},
		createdAt: 12345,
	}
	if info := s.Info(); info.Status != models.StatusConnected {
		t.Fatalf("open session status = %q, want %q", info.Status, models.StatusConnected)
	}
	s.closed = true
	if info := s.Info(); info.Status != models.StatusDisconnected {
		t.Fatalf("closed session status = %q, want %q", info.Status, models.StatusDisconnected)
	}
}
