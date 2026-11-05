package sshx

import (
	"encoding/base64"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"strings"
	"testing"
)

func challengeFor(t *testing.T, host string, key ssh.PublicKey, sources []string) HostTrustChallenge {
	t.Helper()
	err := verifyConnectionHostSources(host, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key, sources)
	if err == nil {
		t.Fatal("expected challenge")
	}
	parts := strings.Split(err.Error(), "SSH_HOST_TRUST:")
	if len(parts) != 2 {
		t.Fatal(err)
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var view HostTrustChallenge
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	return view
}
func TestHostTrustApprovalAndReplacement(t *testing.T) {
	t.Setenv("DING_SSH_DEV_CONFIG_DIR", t.TempDir())
	host := "challenge.invalid:22"
	first, second := testHostKey(t), testHostKey(t)
	view := challengeFor(t, host, first, nil)
	if err := ResolveHostTrust(view.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if err := ResolveHostTrust(view.ID, true, false); err == nil {
		t.Fatal("consumed challenge accepted")
	}
	view = challengeFor(t, host, first, nil)
	if err := ResolveHostTrust(view.ID, true, false); err != nil {
		t.Fatal(err)
	}
	view = challengeFor(t, host, second, nil)
	if !view.Changed {
		t.Fatal("change was not identified")
	}
	if err := ResolveHostTrust(view.ID, true, false); err == nil {
		t.Fatal("implicit replacement accepted")
	}
	view = challengeFor(t, host, second, nil)
	if err := ResolveHostTrust(view.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHostKey(host, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, second, nil); err != nil {
		t.Fatal(err)
	}
}
func TestRevokedKeyCannotCreateApprovalChallenge(t *testing.T) {
	t.Setenv("DING_SSH_DEV_CONFIG_DIR", t.TempDir())
	key := testHostKey(t)
	file := trustPaths(nil)[0]
	if err := os.WriteFile(file, []byte("@revoked "+knownhosts.Line([]string{"revoked.invalid"}, key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := verifyConnectionHost("revoked.invalid:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key)
	if err == nil || strings.Contains(err.Error(), "SSH_HOST_TRUST:") {
		t.Fatalf("revoked key offered approval: %v", err)
	}
}

func TestReadOnlyKeyRotationPinsAppWithoutEditingSource(t *testing.T) {
	t.Setenv("DING_SSH_DEV_CONFIG_DIR", t.TempDir())
	host := "readonly.invalid:22"
	old, newKey := testHostKey(t), testHostKey(t)
	source := t.TempDir() + "/known_hosts"
	original := knownhosts.Line([]string{host}, old) + "\n"
	if err := os.WriteFile(source, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	view := challengeFor(t, host, newKey, []string{source})
	if !view.CanReplace {
		t.Fatal("rotation was disabled")
	}
	if err := ResolveHostTrust(view.ID, true, true); err != nil {
		t.Fatal(err)
	}
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	if err := VerifyHostKey(host, addr, newKey, []string{source}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHostKey(host, addr, old, []string{source}); err == nil {
		t.Fatal("old key accepted after rotation")
	}
	data, _ := os.ReadFile(source)
	if string(data) != original {
		t.Fatal("read-only source modified")
	}
	if err := os.WriteFile(source, []byte(original+"@revoked "+knownhosts.Line([]string{host}, newKey)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHostKey(host, addr, newKey, []string{source}); err == nil {
		t.Fatal("app pin bypassed revocation")
	}
}
