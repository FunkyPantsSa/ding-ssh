package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestHostKeyVerifier(t *testing.T) {
	key := testHostKey(t)
	other := testHostKey(t)
	for _, host := range []string{"example.org:22", "example.org:2222", "[2001:db8::1]:2222"} {
		t.Run(host, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "known_hosts")
			line := knownhosts.Line([]string{host}, key)
			if err := os.WriteFile(file, []byte(line+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			verify, err := loadHostKeyVerifier([]string{file})
			if err != nil {
				t.Fatal(err)
			}
			remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
			if err := verify(host, remote, key); err != nil {
				t.Fatal(err)
			}
			var mismatch *knownhosts.KeyError
			if err := verify(host, remote, other); !errors.As(err, &mismatch) || len(mismatch.Want) == 0 {
				t.Fatalf("expected changed key error: %v", err)
			}
		})
	}
}

func TestHostKeyVerifierFailsClosed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "known_hosts")
	verify, err := loadHostKeyVerifier([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if err := verify("unknown:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, testHostKey(t)); err == nil {
		t.Fatal("unknown host accepted")
	}
	if err := os.WriteFile(file, []byte("broken trust data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHostKeyVerifier([]string{file}); err == nil {
		t.Fatal("malformed trust store accepted")
	}
}

func TestHostKeyVerifierHashedAndRevoked(t *testing.T) {
	key := testHostKey(t)
	host := "example.org:22"
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	for _, revoked := range []bool{false, true} {
		file := filepath.Join(t.TempDir(), "known_hosts")
		line := knownhosts.Line([]string{knownhosts.HashHostname(knownhosts.Normalize(host))}, key)
		if revoked {
			line = "@revoked " + line
		}
		if err := os.WriteFile(file, []byte(line+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		verify, err := loadHostKeyVerifier([]string{file})
		if err != nil {
			t.Fatal(err)
		}
		err = verify(host, remote, key)
		if revoked && err == nil {
			t.Fatal("revoked key accepted")
		}
		if !revoked && err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplicitTrustRefusesChangedKey(t *testing.T) {
	t.Setenv("DING_SSH_DEV_CONFIG_DIR", t.TempDir())
	first, second := testHostKey(t), testHostKey(t)
	host := "trust-unit.invalid:2222"
	if err := SaveHostTrust(host, string(ssh.MarshalAuthorizedKey(first))); err != nil {
		t.Fatal(err)
	}
	if err := SaveHostTrust(host, string(ssh.MarshalAuthorizedKey(second))); err == nil {
		t.Fatal("changed key overwritten")
	}
	records, err := ListHostTrust()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Hosts == "[trust-unit.invalid]:2222" && record.Fingerprint != ssh.FingerprintSHA256(first) {
			t.Fatal("trust was modified")
		}
	}
}
