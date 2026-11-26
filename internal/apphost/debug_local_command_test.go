package apphost

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/models"
	"ding-ssh/internal/store"
)

func TestLocalJobNativeShellCwdAndEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 folder")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	shell, command := "sh", `printf '%s\n' "$DING_TEST_ENV"; pwd; printf problem >&2; exit 7`
	if runtime.GOOS == "windows" {
		shell, command = "powershell", `Write-Output $env:DING_TEST_ENV; Write-Output (Get-Location).Path; [Console]::Error.Write('problem'); exit 7`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var mu sync.Mutex
	out := map[string]string{}
	r := runLocalJobRequest(ctx, debugsrv.JobRequest{Local: true, Command: command, Shell: shell, Cwd: dir, Env: map[string]string{"DING_TEST_ENV": "中文 '$() & value"}}, func(stream string, data []byte) {
		mu.Lock()
		out[stream] += string(data)
		mu.Unlock()
	})
	if !r.Known || r.ExitCode == nil || *r.ExitCode != 7 || !strings.Contains(out["stdout"], dir) || !strings.Contains(out["stdout"], "中文 '$() & value") || out["stderr"] != "problem" {
		t.Fatalf("native execution lost shell/cwd/env/output: %+v %#v", r, out)
	}
}

func TestLocalJobValidationDoesNotExecute(t *testing.T) {
	for _, req := range []debugsrv.JobRequest{
		{Command: "echo no", Shell: "not-a-shell"},
		{Command: "echo no", Cwd: "relative"},
		{Command: "echo no", Env: map[string]string{"BAD=KEY": "x"}},
		{Command: "echo no", Mode: "invalid"},
	} {
		if _, err := localJobCommand(context.Background(), req); err == nil {
			t.Fatalf("invalid local command accepted: %+v", req)
		}
	}
}

func TestPowerShellNativeCommandKeepsTheScriptAsOneUnicodeArgument(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell argument construction")
	}
	cmd, err := localJobCommand(context.Background(), debugsrv.JobRequest{
		Local:   true,
		Shell:   "powershell",
		Command: "Write-Output '中文'; exit 7",
	})
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	if len(args) < 2 || args[len(args)-2] != "-Command" || args[len(args)-1] == "" {
		t.Fatalf("PowerShell script was not passed as one -Command argument: %#v", args)
	}
	if strings.Contains(strings.Join(args, " "), "-EncodedCommand") {
		t.Fatalf("PowerShell command unexpectedly uses encoded command: %#v", args)
	}
	if !strings.Contains(args[len(args)-1], "中文") || !strings.Contains(args[len(args)-1], "exit 7") {
		t.Fatalf("PowerShell script lost Unicode or command suffix: %q", args[len(args)-1])
	}
}

func TestPrepareLocalJobNeedsLocalExecButNotTerminalInput(t *testing.T) {
	settings, err := store.NewJSONSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := settings.Get()
	if err != nil {
		t.Fatal(err)
	}
	current.Debug = models.DefaultDebugSettings()
	current.Debug.CapLocalExec = true
	if err := settings.Save(current); err != nil {
		t.Fatal(err)
	}
	controller := &debugController{app: &App{settings: settings}}
	if _, err := controller.PrepareJob(context.Background(), debugsrv.JobRequest{Local: true, Command: "echo prepared"}); err != nil {
		t.Fatalf("local job should not need terminal.input: %v", err)
	}
	if _, err := controller.PrepareJob(context.Background(), debugsrv.JobRequest{SessionID: "remote", Command: "echo prepared"}); err == nil || !strings.Contains(err.Error(), string(debugsrv.CapTerminalInput)) {
		t.Fatalf("remote job should require terminal.input, got %v", err)
	}
}
