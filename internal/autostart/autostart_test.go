package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRegistrationIsPerUserAndRemovable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("do not mutate the real registry during tests")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPIMAGE", "")
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "app.ding-ssh.login.plist")
	if runtime.GOOS == "linux" {
		path = filepath.Join(home, "config", "autostart", "ding-ssh.desktop")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		var document any
		if err = xml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
	}
	if err = Set(false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("entry was not removed")
	}
	if err = Set(false); err != nil {
		t.Fatal("disabling an absent entry must be idempotent:", err)
	}
}
