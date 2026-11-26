package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/store"
)

func TestLauncherFlagsRejectAmbiguousOrUnsafeProfiles(t *testing.T) {
	for _, args := range [][]string{
		{"--launch=always"}, {"--launch=if-needed"}, {"--app-path=relative"},
		{"--profile=relative"}, {"--wait-ready-ms=1"}, {"--wait-ready-ms=120001"},
		{"--profile=" + t.TempDir(), "--url=http://127.0.0.1:3"},
		{"--profile=" + filepath.Join(normalConfigDir(), "nested-test")},
	} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("accepted unsafe/ambiguous flags: %v", args)
		}
	}
	opts, err := parseFlags([]string{"--launch=if-needed", "--app-path=/test/app", "--profile=" + filepath.Join(t.TempDir(), "new"), "--wait-ready-ms=500"}, io.Discard)
	if err != nil || !strictAttachment(opts) || !opts.waitReadySet {
		t.Fatalf("valid flags failed: %+v %v", opts, err)
	}
}

func TestNewProfileIsProvisionedWithoutTouchingNormalConfig(t *testing.T) {
	normal := t.TempDir()
	t.Setenv(appdir.EnvDevConfigDir, normal)
	sentinel := []byte("normal settings must stay unchanged")
	if err := os.WriteFile(filepath.Join(normal, "settings.json"), sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "isolated")
	created, err := provisionProfile(profile)
	if err != nil || !created {
		t.Fatalf("provision: created=%v err=%v", created, err)
	}
	db, err := store.OpenSQLite(filepath.Join(profile, "ding-ssh.db"))
	if err != nil {
		t.Fatal(err)
	}
	settingsStore := store.NewSQLiteSettingsStore(db)
	st, err := settingsStore.Get()
	if err != nil {
		t.Fatal(err)
	}
	d := st.Debug
	if !d.Enabled || d.Port != 0 || d.BindLAN || !d.CapUIInspect || !d.CapUIWrite || !d.CapLocalExec || !d.TrustedAutomation || d.AllowSecrets || d.AllowEval || d.CapRemoteFSWrite || d.CapSudoCredential {
		t.Fatalf("incorrect developer profile: %+v", d)
	}
	st.Debug.Enabled = false
	if err := settingsStore.Save(st); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := provisionProfile(profile); err == nil || !strings.Contains(err.Error(), "DEBUG_DISABLED") {
		t.Fatalf("existing disabled profile was elevated: %v", err)
	}
	enabled, err := profileDebugEnabled(profile)
	if err != nil || enabled {
		t.Fatalf("existing policy changed: %v %v", enabled, err)
	}
	got, _ := os.ReadFile(filepath.Join(normal, "settings.json"))
	if string(got) != string(sentinel) {
		t.Fatal("normal config modified")
	}
	if _, err := os.Stat(filepath.Join(normal, "ding-ssh.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("normal database was created")
	}
}

func writeDiscovery(t *testing.T, path string, info debugInfo) {
	t.Helper()
	raw, _ := json.Marshal(info)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProfileResolutionIgnoresNormalInstanceEnvironment(t *testing.T) {
	profile := t.TempDir()
	writeDiscovery(t, filepath.Join(profile, "debug.json"), debugInfo{URL: "http://127.0.0.1:2345", Token: "isolated", InstanceID: "isolated", PID: 123})
	opts := options{profile: profile}
	cfg, err := resolveConfig(opts, func(string) string { return "normal-instance" }, false)
	if err != nil || cfg.url != "http://127.0.0.1:2345" || cfg.token != "isolated" {
		t.Fatalf("profile inherited normal environment: %+v %v", cfg, err)
	}
	if reloaded, err := resolveConfig(options{url: cfg.url, token: "explicit", debugRaw: filepath.Join(profile, "debug.json")}, func(string) string { return "environment" }, true); err != nil || reloaded.token != "explicit" || reloaded.url != cfg.url {
		t.Fatalf("explicit target lost on reconnect: %+v %v", reloaded, err)
	}
}

func TestStrictAttachmentChecksIdentityBuildAndFrontend(t *testing.T) {
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer isolated" {
			t.Error("missing auth")
		}
		if r.URL.Path == "/v1/health" {
			id, build := "instance-test", "build-test"
			if mode.Load() == 1 {
				id = "other"
			}
			if mode.Load() == 2 {
				build = "other"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pid": 123, "instanceId": id, "buildId": build})
			return
		}
		if r.URL.Path != "/v1/app/observe" || r.URL.Query().Get("includeImage") != "false" {
			t.Errorf("unexpected readiness request: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"instanceId": "instance-test", "frontend": map[string]any{"ready": mode.Load() != 3}})
	}))
	defer server.Close()
	cfg := config{url: server.URL, token: "isolated", discovery: &debugInfo{PID: 123, InstanceID: "instance-test", BuildID: "build-test"}}
	deps := launcherDeps{client: server.Client(), alive: func(int) bool { return true }}
	for _, tc := range []struct {
		mode int32
		want string
	}{{0, ""}, {1, "INSTANCE_MISMATCH"}, {2, "BUILD_MISMATCH"}, {3, "FRONTEND_NOT_READY"}} {
		mode.Store(tc.mode)
		err := validateAttachment(context.Background(), cfg, options{expectedBuildID: "build-test"}, deps)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("mode %d: %v", tc.mode, err)
		}
	}
	deps.alive = func(int) bool { return false }
	if err := validateAttachment(context.Background(), cfg, options{}, deps); err == nil || !strings.Contains(err.Error(), "STALE_DISCOVERY") {
		t.Fatalf("stale process accepted: %v", err)
	}
}

func TestLauncherStartsOnceAndWaitsForFrontend(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "isolated")
	binary := filepath.Join(t.TempDir(), "test-app")
	if err := os.WriteFile(binary, []byte("test executable"), 0700); err != nil {
		t.Fatal(err)
	}
	var checks, starts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"pid": 123, "instanceId": "new", "buildId": "expected"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"instanceId": "new", "frontend": map[string]any{"ready": checks.Add(1) >= 2}})
	}))
	defer server.Close()
	deps := launcherDeps{getenv: func(string) string { return "" }, client: server.Client(), alive: func(pid int) bool { return pid == 123 }, start: func(path, dir string) (int, <-chan error, error) {
		starts.Add(1)
		if path != binary || dir != profile {
			t.Errorf("wrong launch: %s %s", path, dir)
		}
		writeDiscovery(t, filepath.Join(profile, "debug.json"), debugInfo{PID: 123, InstanceID: "new", Profile: profile, URL: server.URL, Token: "isolated", BuildID: "expected"})
		return 123, make(chan error), nil
	}}
	cfg, err := prepareTarget(context.Background(), options{launch: "if-needed", appPath: binary, profile: profile, waitReadyMs: 2000, expectedBuildID: "expected"}, &logf2{quiet: true}, deps)
	if err != nil || cfg.discovery.InstanceID != "new" || starts.Load() != 1 || checks.Load() < 2 {
		t.Fatalf("cold boot failed: cfg=%+v err=%v starts=%d checks=%d", cfg, err, starts.Load(), checks.Load())
	}
}

func TestLauncherDoesNotRestartLiveUnreadyOrMismatchedInstance(t *testing.T) {
	var mode atomic.Int32
	profile := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			id := "live"
			if mode.Load() == 1 {
				id = "wrong"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pid": 123, "instanceId": id, "buildId": "expected"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"frontend": map[string]any{"ready": false}})
	}))
	defer server.Close()
	writeDiscovery(t, filepath.Join(profile, "debug.json"), debugInfo{PID: 123, InstanceID: "live", URL: server.URL, Token: "isolated", BuildID: "expected"})
	var starts int
	binary := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(binary, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	deps := launcherDeps{getenv: func(string) string { return "" }, client: server.Client(), alive: func(int) bool { return true }, start: func(string, string) (int, <-chan error, error) { starts++; return 0, nil, errors.New("must not start") }}
	for _, tc := range []struct {
		mode int32
		want string
	}{{0, "READY_TIMEOUT"}, {1, "INSTANCE_MISMATCH"}} {
		mode.Store(tc.mode)
		_, err := prepareTarget(context.Background(), options{launch: "if-needed", appPath: binary, debugRaw: filepath.Join(profile, "debug.json"), waitReadyMs: 200, expectedBuildID: "expected"}, &logf2{quiet: true}, deps)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("mode=%d err=%v", tc.mode, err)
		}
	}
	if starts != 0 {
		t.Fatal("live instance was restarted")
	}
}

func TestLauncherCancellationAndEarlyExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := prepareTarget(ctx, options{waitReadySet: true, waitReadyMs: 100, debugRaw: filepath.Join(t.TempDir(), "missing.json")}, &logf2{quiet: true}, defaultLauncherDeps())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if !processAlive(os.Getpid()) || processAlive(-1) {
		t.Fatal("process liveness check failed")
	}
	start := time.Now()
	profile := filepath.Join(t.TempDir(), "new")
	binary := filepath.Join(t.TempDir(), "app")
	_ = os.WriteFile(binary, []byte("test"), 0700)
	deps := defaultLauncherDeps()
	deps.getenv = func(string) string { return "" }
	deps.start = func(string, string) (int, <-chan error, error) {
		done := make(chan error, 1)
		done <- errors.New("exit 7")
		return 123, done, nil
	}
	_, err = prepareTarget(context.Background(), options{launch: "if-needed", appPath: binary, profile: profile, waitReadyMs: 2000, expectedBuildID: "expected"}, &logf2{quiet: true}, deps)
	if err == nil || !strings.Contains(err.Error(), "LAUNCH_EXITED") || time.Since(start) > time.Second {
		t.Fatalf("early exit not reported promptly: %v", err)
	}
}
