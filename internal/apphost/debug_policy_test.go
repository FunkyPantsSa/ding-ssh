package apphost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/models"
	"ding-ssh/internal/store"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestDebugSettingsChangesNeedNativeApprovalForExpansion(t *testing.T) {
	base := models.DefaultDebugSettings()
	for _, tc := range []struct {
		name   string
		change func(*models.DebugSettings)
	}{
		{"enable", func(d *models.DebugSettings) { d.Enabled = true }},
		{"terminal", func(d *models.DebugSettings) { d.CapTerminalInput = true }},
		{"read", func(d *models.DebugSettings) { d.CapRemoteFSRead = true }},
		{"trusted automation", func(d *models.DebugSettings) { d.TrustedAutomation = true }},
		{"network", func(d *models.DebugSettings) { d.BindLAN = true }},
		{"listen port", func(d *models.DebugSettings) { d.Port++ }},
		{"path scope", func(d *models.DebugSettings) { d.SFTPWriteAllowlist = []string{"/tmp"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := base
			tc.change(&after)
			if err := (&App{}).approveDebugSettingsChange(base, after); err == nil {
				t.Fatal("headless permission expansion succeeded")
			}
		})
	}
	before := base
	before.Enabled, before.CapTerminalInput, before.AllowEval = true, true, true
	before.SFTPWriteAllowlist = []string{"/srv/app", "/tmp"}
	for _, paths := range [][]string{nil, {"/srv/app"}, {"/srv/app/narrow"}} {
		after := base
		after.SFTPWriteAllowlist = paths
		if err := (&App{}).approveDebugSettingsChange(before, after); err != nil {
			t.Fatalf("headless narrowing %v failed: %v", paths, err)
		}
	}
	if err := (&App{}).approveDebugSettingsChange(before, before); err != nil {
		t.Fatal(err)
	}
}

func TestDebugCapabilityBindingCannotExpandWithoutNativeApproval(t *testing.T) {
	a := newReloadTestApp(t)
	a.debugDialog = nil
	if err := a.SetCapability(string(debugsrv.CapLocalExec), true); err == nil {
		t.Fatal("scriptable capability binding expanded permission")
	}
	settings, err := a.settings.Get()
	if err != nil || settings.Debug.CapLocalExec {
		t.Fatalf("denied change was persisted: %v", err)
	}
	a.debugDialog = func(wailsruntime.MessageDialogOptions) (string, error) { return "批准", nil }
	if err := a.SetCapability(string(debugsrv.CapTerminalInput), true); err != nil {
		t.Fatal(err)
	}
	a.debugDialog = nil
	if err := a.SetCapability(string(debugsrv.CapTerminalInput), false); err != nil {
		t.Fatalf("headless capability revocation failed: %v", err)
	}
	settings, _ = a.settings.Get()
	if settings.Debug.CapTerminalInput {
		t.Fatal("capability revocation was not persisted")
	}
}

func TestDebugNativeApprovalDenialCancellationAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, choice string
		err          error
	}{
		{"deny", "拒绝", nil}, {"cancel", "", nil}, {"native error", "批准", errors.New("dialog failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{debugDialog: func(opts wailsruntime.MessageDialogOptions) (string, error) {
				if opts.DefaultButton != "拒绝" || opts.CancelButton != "拒绝" {
					t.Fatal("approval default is permissive")
				}
				return tc.choice, tc.err
			}}
			decision, err := (&debugController{app: a}).approveDebugOperation(context.Background(), debugsrv.ApprovalRequest{Tool: "sftp.write", Summary: "write file"})
			if decision.Approved || (tc.err != nil && err == nil) {
				t.Fatalf("native denial/error approved: %#v %v", decision, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{debugDialog: func(wailsruntime.MessageDialogOptions) (string, error) { cancel(); return "允许一次", nil }}
	if d, err := (&debugController{app: a}).approveDebugOperation(ctx, debugsrv.ApprovalRequest{}); d.Approved || err == nil {
		t.Fatal("cancelled native approval succeeded")
	}
	if _, err := (&debugController{app: &App{}}).approveDebugOperation(context.Background(), debugsrv.ApprovalRequest{}); err == nil {
		t.Fatal("missing native channel approved operation")
	}
	a = &App{debugDialog: func(wailsruntime.MessageDialogOptions) (string, error) {
		t.Fatal("oversized approval reached native dialog")
		return "允许一次", nil
	}}
	if d, err := (&debugController{app: a}).approveDebugOperation(context.Background(), debugsrv.ApprovalRequest{Summary: strings.Repeat("x", 13000)}); d.Approved || err == nil {
		t.Fatal("approval accepted a command too long to review")
	}
}

func TestDebugPresetsHaveExplicitTrustBoundaries(t *testing.T) {
	a := newReloadTestApp(t)
	if err := a.ApplyDebugPreset("observe"); err != nil {
		t.Fatal(err)
	}
	st, _ := a.settings.Get()
	d := st.Debug
	if !d.CapTerminalRead || !d.CapRemoteFSRead || !d.CapUIInspect || d.CapTerminalInput || d.CapRemoteFSWrite || d.AllowEval || d.TrustedAutomation || d.AllowSecrets || d.CapLocalExec {
		t.Fatalf("unsafe observe preset: %#v", d)
	}
	if err := a.ApplyDebugPreset("operate"); err != nil {
		t.Fatal(err)
	}
	st, _ = a.settings.Get()
	d = st.Debug
	if !d.CapTerminalInput || !d.CapRemoteFSWrite || !d.CapConnect || !d.CapCredentialUse || !d.CapDisconnect || !d.CapTransferCancel || d.TrustedAutomation || d.AllowEval || d.AllowSecrets || d.CapSudoCredential || d.CapLocalExec {
		t.Fatalf("unsafe operate preset: %#v", d)
	}
	a.debugDialog = nil
	if err := a.ApplyDebugPreset("developer"); err == nil {
		t.Fatal("developer trust enabled without native approval")
	}
	after, _ := a.settings.Get()
	if !reflect.DeepEqual(after.Debug, d) {
		t.Fatal("unapproved preset changed settings")
	}
	a.debugDialog = func(opts wailsruntime.MessageDialogOptions) (string, error) {
		if !strings.Contains(opts.Message, "trustedAutomation") {
			t.Fatal("approval omitted trust change")
		}
		return "批准", nil
	}
	if err := a.ApplyDebugPreset("developer"); err != nil {
		t.Fatal(err)
	}
	st, _ = a.settings.Get()
	if !st.Debug.TrustedAutomation || !st.Debug.AllowEval || !st.Debug.CapUIWrite {
		t.Fatal("approved developer preset missing capabilities")
	}
	if err := a.ApplyDebugPreset("unknown"); err == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestDebugCredentialUseWriteAndExportAreIndependent(t *testing.T) {
	c := newSudoCleanupHost(t, []string{"/tmp"})
	st, _ := c.app.settings.Get()
	st.Debug.CapCredentialUse, st.Debug.CapSudoCredential, st.Debug.CapSecretWrite = true, true, true
	st.Debug.AllowSecrets = false
	if err := c.app.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	gate := debugGate{c}
	for _, cap := range []debugsrv.Capability{debugsrv.CapCredentialUse, debugsrv.CapSudoCredential, debugsrv.CapSecretsWrite} {
		if err := gate.Allow(cap, ""); err != nil {
			t.Fatalf("internal use/write required plaintext export: cap=%s err=%v", cap, err)
		}
	}
	if err := gate.Allow(debugsrv.CapSecretsRead, ""); err == nil {
		t.Fatal("credential use/write implicitly granted plaintext export")
	}
	st.Debug.AllowSecrets, st.Debug.CapCredentialUse = true, false
	if err := c.app.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := gate.Allow(debugsrv.CapSecretsRead, ""); err != nil {
		t.Fatal(err)
	}
	if err := gate.Allow(debugsrv.CapSudoCredential, ""); err == nil {
		t.Fatal("plaintext export implicitly granted internal credential use")
	}
}

func TestDebugSettingsHostRejectsJSONCaseAliases(t *testing.T) {
	a := newReloadTestApp(t)
	a.debugDialog = func(wailsruntime.MessageDialogOptions) (string, error) {
		t.Fatal("MCP settings patch reached native permissions dialog")
		return "批准", nil
	}
	for _, key := range []string{"debug", "Debug", "dEbUg"} {
		data, _ := json.Marshal(map[string]any{key: map[string]any{"allowEval": true}})
		if _, err := a.debug.Handle(context.Background(), debugsrv.OpSettingsUpdate, data); err == nil {
			t.Fatalf("host accepted debug permission alias %q", key)
		}
	}
}

func TestDebugDiagnosticsZipContainsOnlySafeMetadata(t *testing.T) {
	a := newReloadTestApp(t)
	token := a.debug.boot.Token
	const password = "unlabelled-saved-password-DO-NOT-EXPORT"
	const privateKey = "unlabelled-private-key-DO-NOT-EXPORT"
	const terminal = "unlabelled-terminal-output-DO-NOT-EXPORT"
	const rawLog = "unlabelled-log-line-DO-NOT-EXPORT"
	serverStore, err := store.NewJSONStore(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.store = serverStore
	if err := serverStore.Save(models.ServerNode{ID: "secret-server", Password: password, KeyContent: privateKey}); err != nil {
		t.Fatal(err)
	}
	a.debug.hub.PublishRaw("ssh:output:s1", models.OutputEvent{SessionID: "s1", Data: base64.StdEncoding.EncodeToString([]byte(terminal))})
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logDir, "application.log"), []byte(rawLog+"\n"+password+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	if err := a.writeDiagnostics(zw, logDir); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range zr.File {
		seen[entry.Name] = true
		if entry.Name != "debug.json" && entry.Name != "state.json" && entry.Name != "env.json" {
			t.Fatalf("unexpected raw diagnostic entry: %s", entry.Name)
		}
		f, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{token, password, privateKey, terminal, base64.StdEncoding.EncodeToString([]byte(terminal)), rawLog} {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("sensitive content exported in %s", entry.Name)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("missing safe diagnostic metadata: %v", seen)
	}
}
