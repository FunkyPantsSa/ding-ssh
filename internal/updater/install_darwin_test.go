package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyMacAppBundle(t *testing.T) {
	root := t.TempDir()
	p := Plan{Source: filepath.Join(root, "download", "ding-ssh.app"), Target: filepath.Join(root, "Applications", "ding-ssh.app")}
	for bundle, version := range map[string]string{p.Source: "new app", p.Target: "old app"} {
		bin := filepath.Join(bundle, "Contents", "MacOS", "ding-ssh")
		if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte(version), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("MacOS/ding-ssh", filepath.Join(p.Source, "Contents", "binary-link")); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(p); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(p.Target, "Contents", "MacOS", "ding-ssh")
	b, err := os.ReadFile(bin)
	if err != nil || string(b) != "new app" {
		t.Fatalf("updated bundle = %q, %v", b, err)
	}
	info, err := os.Stat(p.Target)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("app bundle permissions changed: %v, %v", info, err)
	}
	link, err := os.Readlink(filepath.Join(p.Target, "Contents", "binary-link"))
	if err != nil || link != "MacOS/ding-ssh" {
		t.Fatalf("app bundle symlink not preserved: %q, %v", link, err)
	}
}
