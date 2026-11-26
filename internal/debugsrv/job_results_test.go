package debugsrv

import (
	"context"
	"testing"
)

func TestExecStartWaitReturnsResultAndDoesNotChangeIdempotency(t *testing.T) {
	f := &jobsFixture{runner: func(_ context.Context, emit func(string, []byte)) JobExecutionResult {
		emit("stdout", []byte("中文 result"))
		emit("stderr", []byte("problem"))
		code := 7
		return JobExecutionResult{Known: true, ExitCode: &code, TerminationProven: true}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	args := map[string]any{"sessionId": "s", "command": "test", "idempotencyKey": "one", "waitMs": 2000}
	v := jobsJSON(t, s, context.Background(), OpExecStart, args)
	if v["complete"] != true || v["outcomeKnown"] != true || v["exitCode"] != float64(7) || v["stdout"] != "中文 result" || v["stderr"] != "problem" {
		t.Fatalf("incomplete execution result: %#v", v)
	}
	args["waitMs"], args["includeOutput"] = 3000, false
	again := jobsJSON(t, s, context.Background(), OpExecStart, args)
	if again["jobId"] != v["jobId"] || f.starts.Load() != 1 {
		t.Fatalf("delivery options started a duplicate job: %#v", again)
	}
	if _, ok := again["stdout"]; ok {
		t.Fatal("includeOutput=false ignored")
	}
}

func TestExecStartWaitTimeoutRemainsResumable(t *testing.T) {
	release := make(chan struct{})
	f := &jobsFixture{runner: func(_ context.Context, emit func(string, []byte)) JobExecutionResult {
		<-release
		emit("stdout", []byte("later"))
		return JobExecutionResult{Known: true}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	v := jobsJSON(t, s, context.Background(), OpExecStart, map[string]any{"sessionId": "s", "command": "wait", "waitMs": 1})
	if v["complete"] != false || v["outcomeKnown"] != false || v["nextAction"] == nil {
		t.Fatalf("wait timeout lost task state: %#v", v)
	}
	close(release)
	got := jobsJSON(t, s, context.Background(), OpJobsWait, map[string]any{"jobId": v["jobId"], "timeoutMs": 2000, "includeOutput": true})
	if got["complete"] != true || got["stdout"] != "later" {
		t.Fatalf("could not resume: %#v", got)
	}
	if _, err := s.handleJobsTool(context.Background(), OpExecStart, map[string]any{"sessionId": "s", "command": "never", "waitMs": 30001}); err == nil {
		t.Fatal("invalid wait accepted")
	}
	if f.starts.Load() != 1 {
		t.Fatal("invalid delivery option executed a command")
	}
}
