package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFixture(t *testing.T) (Plan, string, []byte) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "download", "unpacked", "ding-ssh.exe")
	target := filepath.Join(root, "Program Files", "测试 应用", "ding-ssh.exe")
	for path, data := range map[string]string{source: "verified new version", target: "old version"} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := fileSHA256(source)
	if err != nil {
		t.Fatal(err)
	}
	p := Plan{Source: source, Target: target, PID: os.Getpid(), Restart: true, SourceSHA256: hash}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "installer.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p, path, b
}

func assertInstalledVersion(t *testing.T, p Plan, want string) {
	t.Helper()
	b, err := os.ReadFile(p.Target)
	if err != nil || string(b) != want {
		t.Fatalf("installed version = %q, err = %v; want %q", b, err, want)
	}
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(p.Target), ".ding-update-stage-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("temporary staged files left behind: %v, %v", staged, err)
	}
}

func TestInstallElevationPolicy(t *testing.T) {
	tests := []struct {
		name       string
		probeError error
		elevated   bool
		wantUAC    bool
		wantError  bool
	}{
		{name: "writable directory installs without UAC"},
		{name: "protected directory requests UAC", probeError: os.ErrPermission, wantUAC: true},
		{name: "missing directory does not request UAC", probeError: os.ErrNotExist, wantError: true},
		{name: "already elevated does not loop", probeError: os.ErrPermission, elevated: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, path, b := installFixture(t)
			prompted := false
			err := installWithElevation(p, path, b, func(string) error { return tt.probeError }, tt.elevated, func(path, hash string) error {
				prompted = true
				verified, _, err := readInstallPlan(path, hash)
				if err != nil {
					return err
				}
				return applyPlan(verified)
			})
			if prompted != tt.wantUAC || (err != nil) != tt.wantError {
				t.Fatalf("UAC = %v, error = %v; want UAC %v, error %v", prompted, err, tt.wantUAC, tt.wantError)
			}
			want := "verified new version"
			if tt.wantError {
				want = "old version"
			}
			assertInstalledVersion(t, p, want)
		})
	}
}

func TestCancellingElevationPreservesOldVersionAndRestartsFromBroker(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "install on exit", true: "install now"}[restart], func(t *testing.T) {
			p, path, b := installFixture(t)
			p.Restart = restart
			cancelled := errors.New("UAC cancelled")
			restarted := false
			err := completeUpdate(p, func() error {
				return installWithElevation(p, path, b, func(string) error { return os.ErrPermission }, false, func(string, string) error {
					return cancelled
				})
			}, func() error {
				restarted = true
				assertInstalledVersion(t, p, "old version")
				return nil
			})
			if !errors.Is(err, cancelled) || restarted != restart {
				t.Fatalf("error = %v, restarted = %v; want cancelled and restart = %v", err, restarted, restart)
			}
			assertInstalledVersion(t, p, "old version")
			if _, err := os.Stat(p.Target + ".update-backup"); !os.IsNotExist(err) {
				t.Fatalf("cancelled update touched the old version: %v", err)
			}
		})
	}
}

func TestElevationRejectsMissingSourceHash(t *testing.T) {
	p, path, b := installFixture(t)
	p.SourceSHA256 = ""
	err := installWithElevation(p, path, b, func(string) error { return os.ErrPermission }, false, func(string, string) error {
		t.Fatal("unverified update requested UAC")
		return nil
	})
	if err == nil {
		t.Fatal("accepted update without a checksum")
	}
	assertInstalledVersion(t, p, "old version")
}

func TestInstallRejectsChangedPlan(t *testing.T) {
	p, path, b := installFixture(t)
	hash := sha256.Sum256(b)
	p.Target = filepath.Join(filepath.Dir(p.Target), "another.exe")
	changed, _ := json.Marshal(p)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readInstallPlan(path, hex.EncodeToString(hash[:])); err == nil {
		t.Fatal("accepted plan changed after launch")
	}
}

func TestInstallRejectsChangedExecutableBeforeReplacingOldVersion(t *testing.T) {
	p, _, _ := installFixture(t)
	if err := os.WriteFile(p.Source, []byte("modified after verification"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(p); err == nil {
		t.Fatal("accepted modified executable")
	}
	assertInstalledVersion(t, p, "old version")
	if _, err := os.Stat(p.Target + ".update-backup"); !os.IsNotExist(err) {
		t.Fatalf("checksum failure touched the old version: %v", err)
	}
}

func TestInstallRollsBackFailedReplacement(t *testing.T) {
	p, _, _ := installFixture(t)
	blocked := errors.New("replacement blocked")
	err := applyPlanWithRename(p, func(from, to string) error {
		if strings.HasPrefix(filepath.Base(from), ".ding-update-stage-") {
			return blocked
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, blocked) {
		t.Fatalf("error = %v, want replacement failure", err)
	}
	assertInstalledVersion(t, p, "old version")
}

func TestInstallPreservesExistingBackup(t *testing.T) {
	p, _, _ := installFixture(t)
	backup := p.Target + ".update-backup"
	if err := os.WriteFile(backup, []byte("previous backup"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(p); err == nil {
		t.Fatal("overwrote pending backup")
	}
	b, err := os.ReadFile(backup)
	if err != nil || string(b) != "previous backup" {
		t.Fatalf("existing backup was modified: %q, %v", b, err)
	}
	assertInstalledVersion(t, p, "old version")
}

func TestSuccessfulUpdateRestartsOnlyWhenRequested(t *testing.T) {
	for _, restart := range []bool{false, true} {
		p, _, _ := installFixture(t)
		p.Restart = restart
		restarted := false
		err := completeUpdate(p, func() error { return applyPlan(p) }, func() error {
			restarted = true
			assertInstalledVersion(t, p, "verified new version")
			return nil
		})
		if err != nil || restarted != restart {
			t.Fatalf("error = %v, restarted = %v; want restart = %v", err, restarted, restart)
		}
		assertInstalledVersion(t, p, "verified new version")
	}
}
