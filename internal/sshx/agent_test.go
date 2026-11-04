//go:build !windows

package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"ding-ssh/internal/models"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func TestAgentIdentityFilteringAndSigning(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ding-agent-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ring := agent.NewKeyring()
	var wanted ssh.PublicKey
	for i := 0; i < 2; i++ {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := ring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			signer, _ := ssh.NewSignerFromKey(key)
			wanted = signer.PublicKey()
		}
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); agent.ServeAgent(ring, conn) }()
		}
	}()
	pubFile := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(pubFile+".pub", ssh.MarshalAuthorizedKey(wanted), 0600); err != nil {
		t.Fatal(err)
	}
	node := models.ServerNode{SSHOptions: models.SSHOptions{IdentityAgent: socket, IdentityFiles: []string{pubFile}, IdentitiesOnly: true}}
	signers, err := AgentSigners(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(signers) != 1 {
		t.Fatalf("expected one identity, got %d", len(signers))
	}
	message := []byte("agent-signature-test")
	signature, err := signers[0].Sign(rand.Reader, message)
	if err != nil {
		t.Fatal(err)
	}
	if err := wanted.Verify(message, signature); err != nil {
		t.Fatal(err)
	}
	node.IdentityFiles = nil
	if _, err := AgentSigners(node); err == nil {
		t.Fatal("IdentitiesOnly without identities accepted")
	}
}
