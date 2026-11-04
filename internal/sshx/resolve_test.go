package sshx

import (
	"ding-ssh/internal/models"
	"testing"
)

func TestResolveJumpChain(t *testing.T) {
	a := models.ServerNode{ID: "a"}
	b := models.ServerNode{ID: "b", SSHOptions: models.SSHOptions{JumpServerIDs: []string{"a"}}}
	target := models.ServerNode{ID: "target", SSHOptions: models.SSHOptions{JumpServerIDs: []string{"b"}}}
	got, err := ResolveJumpChain(target, []models.ServerNode{a, b})
	if err != nil || len(got.ResolvedJumps) != 2 || got.ResolvedJumps[0].ID != "a" {
		t.Fatalf("chain: %+v %v", got, err)
	}
	a.JumpServerIDs = []string{"target"}
	if _, err := ResolveJumpChain(target, []models.ServerNode{a, b, target}); err == nil {
		t.Fatal("cycle accepted")
	}
	if _, err := ResolveJumpChain(target, nil); err == nil {
		t.Fatal("missing hop accepted")
	}
}
