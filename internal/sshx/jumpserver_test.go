package sshx

import (
	"ding-ssh/internal/models"
	"testing"
)

func TestJumpServerDirectorySyncIsolation(t *testing.T) {
	notified := false
	m := NewManager(func(string, interface{}) { notified = true })
	m.sessions["jump"] = &Session{server: models.ServerNode{SSHOptions: models.SSHOptions{JumpServerMode: true}}}
	if err := m.SyncSftpToTerminal("jump", "/Default/资产"); err == nil {
		t.Fatal("virtual path must never be sent to the terminal")
	}
	if err := m.SetSftpPathFromTerminal("jump", "/root"); err != nil {
		t.Fatal(err)
	}
	if notified {
		t.Fatal("terminal path must not overwrite the asset tree")
	}
	m.sessions["jump"].prefetchHomeAndSync() // Must not open SFTP to infer a shell home.
}
