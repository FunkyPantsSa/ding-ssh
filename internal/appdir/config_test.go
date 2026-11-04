package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirUsesAbsoluteDevOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDevConfigDir, dir)
	if got := ConfigDir(); got != filepath.Clean(dir) {
		t.Fatalf("ConfigDir() = %q, want %q", got, filepath.Clean(dir))
	}
	if !IsDevOverride() {
		t.Fatal("IsDevOverride() = false, want true")
	}
}

func TestConfigDirRejectsRelativeDevOverride(t *testing.T) {
	t.Setenv(EnvDevConfigDir, "relative/path")
	wantBase, err := os.UserConfigDir()
	if err != nil {
		wantBase = "."
	} else {
		wantBase = filepath.Join(wantBase, "ding-ssh")
	}
	if got := ConfigDir(); got != wantBase {
		t.Fatalf("ConfigDir() = %q, want fallback %q", got, wantBase)
	}
	if IsDevOverride() {
		t.Fatal("IsDevOverride() = true for relative override")
	}
}
