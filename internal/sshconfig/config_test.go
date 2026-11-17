package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func configFile(t *testing.T, text string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}
func TestSnapshotRules(t *testing.T) {
	file := configFile(t, `Host bastion b
 HostName 192.0.2.1
 User admin
Host target
 HostName 10.0.0.2
 Port 2222
 ProxyJump bastion
 IdentityFile "~/.ssh/first key"
 IdentityFile ~/.ssh/second
 IdentitiesOnly yes
Host * !bastion
 User default
 Port 23
Host *
 Port 24
`)
	list, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("aliases: %v", list)
	}
	if list[0].Node.User != "admin" || list[0].Node.Port != 24 {
		t.Fatalf("first values/default negation: %+v", list[0])
	}
	target := list[2]
	if target.Node.Port != 2222 || target.Node.User != "default" || len(target.Node.IdentityFiles) != 2 || !target.Node.IdentitiesOnly || target.Jumps[0] != "bastion" {
		t.Fatalf("target: %+v", target)
	}
}
func TestUnsafeConfigNeverExecutes(t *testing.T) {
	file := configFile(t, "Host bad\n ProxyCommand touch /tmp/should-never-exist\n")
	list, err := Parse(file)
	if err != nil || len(list[0].Warnings) == 0 {
		t.Fatalf("missing warning: %v %v", list, err)
	}
	file = configFile(t, "Host bad\nMatch exec arbitrary\n User root\n")
	if _, err := Parse(file); err == nil {
		t.Fatal("Match accepted")
	}
}
func TestIncludeCycle(t *testing.T) {
	file := configFile(t, "")
	if err := os.WriteFile(file, []byte("Include "+file+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(file); err == nil {
		t.Fatal("cycle accepted")
	}
}
