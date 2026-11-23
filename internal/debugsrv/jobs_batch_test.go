package debugsrv

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecBatchPreflightFreezesRunnersAndLimitsConcurrency(t *testing.T) {
	var active, maximum, executed atomic.Int32
	release := make(chan struct{})
	f := &jobsFixture{runner: func(ctx context.Context, _ func(string, []byte)) JobExecutionResult {
		n := active.Add(1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		defer active.Add(-1)
		executed.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		code := 0
		return JobExecutionResult{Known: true, ExitCode: &code, TerminationProven: true}
	}}
	s := New(Options{Handler: f, Gate: &fakeGate{}})
	defer s.CloseJobs()
	ctx := context.Background()
	plan := jobsJSON(t, s, ctx, OpExecPlan, map[string]any{"sessionIds": []string{"one", "two", "three"}, "command": "echo deploy"})
	if f.starts.Load() != 3 || executed.Load() != 0 || plan["dryRun"] != true {
		t.Fatalf("preflight ran commands %#v", plan)
	}
	args := map[string]any{"planId": plan["planId"], "concurrency": 2}
	batch := jobsJSON(t, s, ctx, OpExecBatch, args)
	again := jobsJSON(t, s, ctx, OpExecBatch, args)
	if again["batchId"] != batch["batchId"] || f.starts.Load() != 3 {
		t.Fatal("reprepared targets or duplicated batch")
	}
	deadline := time.Now().Add(time.Second)
	for executed.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if executed.Load() != 2 || maximum.Load() != 2 {
		t.Fatalf("bad concurrency executed=%d max=%d", executed.Load(), maximum.Load())
	}
	close(release)
	for _, target := range batch["targets"].([]any) {
		id := target.(map[string]any)["job"].(map[string]any)["jobId"]
		view := jobsJSON(t, s, ctx, OpJobsWait, map[string]any{"jobId": id, "timeoutMs": 2000})
		if view["state"] != "succeeded" {
			t.Fatalf("batch failed %#v", view)
		}
	}
	if executed.Load() != 3 || maximum.Load() > 2 {
		t.Fatalf("duplicated or excessive parallelism executed=%d max=%d", executed.Load(), maximum.Load())
	}
}
func TestExecBatchRejectsExpiredOrOtherOwnerPlan(t *testing.T) {
	f := &jobsFixture{runner: func(context.Context, func(string, []byte)) JobExecutionResult { return JobExecutionResult{Known: true} }}
	s := New(Options{Handler: f, Gate: &fakeGate{}})
	defer s.CloseJobs()
	ctx := context.Background()
	plan := jobsJSON(t, s, ctx, OpExecPlan, map[string]any{"sessionIds": []any{"one"}, "command": "x"})
	id := plan["planId"].(string)
	if _, e := s.handleJobsTool(WithCaller(ctx, "other"), OpExecBatch, map[string]any{"planId": id}); e == nil {
		t.Fatal("another caller executed plan")
	}
	m := s.jobStore()
	m.mu.Lock()
	m.plans[id].expires = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if _, e := s.handleJobsTool(ctx, OpExecBatch, map[string]any{"planId": id}); e == nil {
		t.Fatal("expired plan executed")
	}
}
