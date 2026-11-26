package sshconfig

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWindowsImportPreservesNativePaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "配置 folder")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	included := filepath.Join(dir, "hosts")
	if err := os.WriteFile(included, []byte("Host native-host\n HostName 127.0.0.9\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "identity key")
	known := filepath.Join(dir, "known_hosts")
	file := configFile(t, "Include "+strconv.Quote(included)+"\nHost native-host\n IdentityFile "+strconv.Quote(key)+"\n UserKnownHostsFile "+strconv.Quote(known)+"\n")
	list, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Node.Host != "127.0.0.9" {
		t.Fatalf("native Include path lost: %+v", list)
	}
	node := list[0].Node
	if len(node.IdentityFiles) != 1 || node.IdentityFiles[0] != key || len(node.KnownHostsSources) != 1 || node.KnownHostsSources[0] != known {
		t.Fatalf("native key/trust paths changed: %+v", node.SSHOptions)
	}
}
