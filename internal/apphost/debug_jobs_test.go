package apphost

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/sshx"
)

func TestLocalIndependentJobOutputAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	var mu sync.Mutex
	out := map[string]string{}
	r := runLocalJob(context.Background(), "printf 'hello'; printf 'problem' >&2; exit 9", func(stream string, b []byte) { mu.Lock(); out[stream] += string(b); mu.Unlock() })
	if !r.Known || r.ExitCode == nil || *r.ExitCode != 9 || out["stdout"] != "hello" || out["stderr"] != "problem" {
		t.Fatalf("bad local result %+v %#v", r, out)
	}
}

func TestJobsHostSessionWaitObservesWithoutConnecting(t *testing.T) {
	c := &debugController{app: &App{manager: sshx.NewManager(nil)}}
	raw := json.RawMessage(`{"sessionId":"missing","status":"closed","timeoutMs":0}`)
	v, err := c.handleJobsHostOp(context.Background(), debugsrv.OpSessionsWait, raw)
	if err != nil {
		t.Fatal(err)
	}
	view := v.(map[string]any)
	if view["exists"] != false || view["status"] != "closed" || view["matched"] != true || len(c.app.manager.List()) != 0 {
		t.Fatalf("observation changed sessions %#v", view)
	}
	for _, bad := range []string{`{"sessionId":"s","timeoutMs":0.5}`, `{"sessionId":"s","timeoutMs":"100"}`, `{"sessionId":"s","timeoutMs":30001}`, `{"sessionId":"s","status":"unknown"}`} {
		if _, err = c.handleJobsHostOp(context.Background(), debugsrv.OpSessionsWait, json.RawMessage(bad)); err == nil {
			t.Fatalf("bad wait args accepted %s", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.handleJobsHostOp(ctx, debugsrv.OpSessionsWait, json.RawMessage(`{"sessionId":"s","status":"connected"}`)); err != context.Canceled {
		t.Fatalf("wait ignored caller cancellation %v", err)
	}
}
func TestLocalIndependentJobCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	r := runLocalJob(ctx, "sleep 30", nil)
	if !r.CancelRequested || !r.Cancelled || time.Since(started) > 3*time.Second {
		t.Fatalf("bad cancel %+v", r)
	}
}
func TestLocalExecPOSIXEnvQuotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	var out strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := runLocalJob(ctx, "env 'X=$(should-not-run)' sh -c 'printf %s \"$X\"'", func(stream string, b []byte) {
		if stream == "stdout" {
			out.Write(b)
		}
	})
	if r.ExitCode == nil || *r.ExitCode != 0 || out.String() != "$(should-not-run)" {
		t.Fatalf("bad quoted env %q %+v", out.String(), r)
	}
}
