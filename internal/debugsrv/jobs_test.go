package debugsrv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type jobsFixture struct {
	dir    string
	starts atomic.Int32
	runner JobRunner
}

func (f *jobsFixture) Handle(context.Context, string, json.RawMessage) (any, error) { return nil, nil }
func (f *jobsFixture) PrepareJob(_ context.Context, _ JobRequest) (JobRunner, error) {
	f.starts.Add(1)
	return f.runner, nil
}
func (f *jobsFixture) JobStateDirectory() string { return f.dir }
func jobsJSON(t *testing.T, s *Server, ctx context.Context, name string, args map[string]any) map[string]any {
	t.Helper()
	c, e := s.handleJobsTool(ctx, name, args)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal([]byte(c[0].(map[string]any)["text"].(string)), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestJobsOutputOwnershipAndIdempotency(t *testing.T) {
	f := &jobsFixture{dir: t.TempDir(), runner: func(_ context.Context, emit func(string, []byte)) JobExecutionResult {
		emit("stdout", []byte(strings.Repeat("x", 1800)))
		emit("stderr", []byte("diagnostic"))
		n := 3
		return JobExecutionResult{Known: true, ExitCode: &n, TerminationProven: true}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	ctx := WithCaller(context.Background(), "alice")
	args := map[string]any{"sessionId": "s", "command": "sensitive-command", "env": map[string]string{"PASSWORD": "very-secret"}, "maxBytes": 1024, "idempotencyKey": "once"}
	first := jobsJSON(t, s, ctx, OpExecStart, args)
	id := first["jobId"].(string)
	result := jobsJSON(t, s, ctx, OpJobsWait, map[string]any{"jobId": id, "timeoutMs": 2000})
	if result["state"] != "failed" || result["exitCode"] != float64(3) || result["truncated"] != true || result["capturedBytes"] != float64(1024) {
		t.Fatalf("bad job result %#v", result)
	}
	second := jobsJSON(t, s, ctx, OpExecStart, args)
	if second["jobId"] != id || f.starts.Load() != 1 {
		t.Fatalf("executed duplicate: %#v starts=%d", second, f.starts.Load())
	}
	args["command"] = "changed"
	if _, e := s.handleJobsTool(ctx, OpExecStart, args); e == nil {
		t.Fatal("reused idempotency for changed params")
	}
	if _, e := s.handleJobsTool(WithCaller(context.Background(), "bob"), OpJobsOutput, map[string]any{"jobId": id}); e == nil {
		t.Fatal("cross-client output allowed")
	}
	out := jobsJSON(t, s, ctx, OpJobsOutput, map[string]any{"jobId": id})
	if out["nextCursor"] != float64(1024) || out["truncated"] != true {
		t.Fatalf("bad output %#v", out)
	}
	// Wait returns when the job completed, then allow its atomic persistence to finish.
	deadline := time.Now().Add(time.Second)
	var b []byte
	var e error
	for time.Now().Before(deadline) {
		b, e = os.ReadFile(filepath.Join(f.dir, id+".json"))
		if e == nil && strings.Contains(string(b), `"finishedAt"`) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "sensitive-command") || strings.Contains(string(b), "very-secret") {
		t.Fatal("persisted command/env")
	}
	info, _ := os.Stat(filepath.Join(f.dir, id+".json"))
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe mode %v", info.Mode())
	}
}
func TestJobsCancellationAndWaitAreSeparate(t *testing.T) {
	started := make(chan struct{})
	f := &jobsFixture{runner: func(ctx context.Context, _ func(string, []byte)) JobExecutionResult {
		close(started)
		<-ctx.Done()
		return JobExecutionResult{Known: false, CancelRequested: true, Error: "remote exit unconfirmed"}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	ctx := WithCaller(context.Background(), "owner")
	id := jobsJSON(t, s, ctx, OpExecStart, map[string]any{"sessionId": "s", "command": "long"})["jobId"].(string)
	<-started
	v := jobsJSON(t, s, ctx, OpJobsWait, map[string]any{"jobId": id, "timeoutMs": 0})
	if v["state"] != "running" {
		t.Fatalf("wait changed state %#v", v)
	}
	jobsJSON(t, s, ctx, OpJobsCancel, map[string]any{"jobId": id})
	v = jobsJSON(t, s, ctx, OpJobsWait, map[string]any{"jobId": id, "timeoutMs": 2000})
	if v["state"] != "unknown" || v["terminationProven"] != false || v["cancelRequested"] != true {
		t.Fatalf("cancel overclaimed %#v", v)
	}
}
func TestJobsRestartMarksRunningUnknown(t *testing.T) {
	dir := t.TempDir()
	v := diskJob{View: jobView{ID: "job_123", State: "running", CreatedAt: time.Now().UnixMilli()}, Owner: "alice", Fingerprint: "hash", Idempotency: "unique"}
	b, _ := json.Marshal(v)
	if e := os.WriteFile(filepath.Join(dir, "job_123.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	f := &jobsFixture{dir: dir}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	out := jobsJSON(t, s, WithCaller(context.Background(), "alice"), OpJobsGet, map[string]any{"jobId": "job_123"})
	if out["state"] != "unknown" || out["errorCode"] != "RESULT_UNKNOWN" {
		t.Fatalf("restart overclaimed %#v", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "job_123.json"))
	if err != nil || !strings.Contains(string(b), `"finishedAt"`) || !strings.Contains(string(b), `"unknown"`) {
		t.Fatalf("recovered result was not persisted: %s %v", b, err)
	}
}
func TestJobsRejectInvalidTargetAndFractionalBudgets(t *testing.T) {
	for _, args := range []map[string]any{{"command": "x"}, {"command": "x", "local": true, "sessionId": "s"}, {"command": "x", "sessionId": "s", "timeoutMs": 200.5}, {"command": "x", "sessionId": "s", "maxBytes": "2048"}} {
		if _, e := parseJobRequest(args); e == nil {
			t.Fatalf("invalid request accepted %#v", args)
		}
	}
}
