package debugsrv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobsStateWriteFailurePreventsExecution(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dir, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	f := &jobsFixture{dir: dir, runner: func(context.Context, func(string, []byte)) JobExecutionResult {
		runs.Add(1)
		return JobExecutionResult{Known: true}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	if _, err := s.handleJobsTool(context.Background(), OpExecStart, map[string]any{"sessionId": "s", "command": "never-run"}); err == nil {
		t.Fatal("unsafe unpersisted job was accepted")
	}
	if runs.Load() != 0 {
		t.Fatal("executed despite persistence failure")
	}
}

func TestJobsLimitsOwnershipAndRevocation(t *testing.T) {
	f := &jobsFixture{runner: func(ctx context.Context, _ func(string, []byte)) JobExecutionResult {
		<-ctx.Done()
		return JobExecutionResult{Known: false, CancelRequested: true}
	}}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	owner := WithCaller(context.Background(), "alice")
	stranger := WithCaller(context.Background(), "bob")
	ids := []string{}
	for i := 0; i < 8; i++ {
		args := map[string]any{"sessionId": "s", "command": "wait"}
		if i == 0 {
			args["idempotencyKey"] = "one"
		}
		ids = append(ids, jobsJSON(t, s, owner, OpExecStart, args)["jobId"].(string))
	}
	if _, err := s.handleJobsTool(owner, OpExecStart, map[string]any{"sessionId": "s", "command": "ninth"}); err == nil {
		t.Fatal("unbounded jobs accepted")
	}
	duplicate := jobsJSON(t, s, owner, OpExecStart, map[string]any{"sessionId": "s", "command": "wait", "idempotencyKey": "one"})
	if duplicate["jobId"] != ids[0] || f.starts.Load() != 8 {
		t.Fatal("idempotency failed at capacity")
	}
	for _, op := range []string{OpJobsGet, OpJobsWait, OpJobsOutput, OpJobsCancel} {
		if _, err := s.handleJobsTool(stranger, op, map[string]any{"jobId": ids[0]}); err == nil {
			t.Fatalf("other owner could invoke %s", op)
		}
	}
	if got := jobsJSON(t, s, stranger, OpJobsList, nil); got["count"] != float64(0) {
		t.Fatalf("other owner listed tasks: %#v", got)
	}
	s.CancelClientJobs("alice")
	for _, id := range ids {
		v := jobsJSON(t, s, owner, OpJobsWait, map[string]any{"jobId": id, "timeoutMs": 2000})
		if v["cancelRequested"] != true || (v["state"] != "unknown" && v["state"] != "cancelled") {
			t.Fatalf("revocation did not cancel task %#v", v)
		}
	}
}

func TestJobsMalformedStateIsNotLoaded(t *testing.T) {
	dir := t.TempDir()
	d := diskJob{View: jobView{ID: "job_bad", State: "succeeded", FinishedAt: time.Now().UnixMilli(), CapturedBytes: 1}, Owner: "alice", Chunks: []jobChunk{{Cursor: 100, Stream: "stdout", Data: []byte("x")}}}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(filepath.Join(dir, "job_bad.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	f := &jobsFixture{dir: dir}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	if _, err := s.handleJobsTool(WithCaller(context.Background(), "alice"), OpJobsGet, map[string]any{"jobId": "job_bad"}); err == nil {
		t.Fatal("forged cursors were loaded")
	}
}

func TestJobsChangedEventsStayWithTaskOwner(t *testing.T) {
	clients := NewClientRegistry()
	contexts := []context.Context{}
	for _, name := range []string{"alice", "bob"} {
		p, _, err := clients.Issue(ClientPolicy{Name: name, Capabilities: []string{string(CapRead), string(CapTerminalRead), string(CapTerminalInput)}, SessionIDs: []string{"shared"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
		contexts = append(contexts, context.WithValue(context.Background(), callerKey{}, p))
	}
	f := &jobsFixture{runner: func(context.Context, func(string, []byte)) JobExecutionResult { return JobExecutionResult{Known: true} }}
	s := New(Options{Handler: f, Clients: clients, Gate: &fakeGate{}})
	defer s.CloseJobs()
	id := jobsJSON(t, s, contexts[0], OpExecStart, map[string]any{"sessionId": "shared", "command": "fixture"})["jobId"].(string)
	b, _ := json.Marshal(map[string]any{"jobId": id, "state": "succeeded"})
	event := Event{Topic: "jobs.changed", SessionID: "shared", Data: b}
	if !s.authorizeEvent(contexts[0], event) {
		t.Fatal("owner could not observe its task")
	}
	if s.authorizeEvent(contexts[1], event) || s.authorizeEvent(context.Background(), event) {
		t.Fatal("task state disclosed to another identity on the same SSH session")
	}
	s.RevokeClient(CallerID(contexts[0]))
	if s.authorizeEvent(contexts[0], event) {
		t.Fatal("revoked client still received task state")
	}
	if s.authorizeEvent(contexts[1], Event{Topic: "jobs.changed", SessionID: "shared", Data: []byte(`{"jobId":"missing"}`)}) {
		t.Fatal("unknown task state disclosed")
	}
}

type serviceFixture struct {
	jobsFixture
	requests []JobRequest
	stdout   string
}

func (f *serviceFixture) PrepareJob(_ context.Context, req JobRequest) (JobRunner, error) {
	f.starts.Add(1)
	f.requests = append(f.requests, req)
	return func(_ context.Context, emit func(string, []byte)) JobExecutionResult {
		emit("stdout", []byte(f.stdout))
		n := 0
		return JobExecutionResult{Known: true, ExitCode: &n, TerminationProven: true}
	}, nil
}

func TestServicesFixedCommandsAndStructuredResults(t *testing.T) {
	f := &serviceFixture{stdout: "Id=nginx.service\nActiveState=active\nMainPID=123\n"}
	s := New(Options{Handler: f})
	defer s.CloseJobs()
	args := map[string]any{"sessionId": "s", "unit": "nginx.service"}
	v := jobsJSON(t, s, context.Background(), OpServicesStatus, args)
	props := v["properties"].(map[string]any)
	if v["available"] != true || props["ActiveState"] != "active" || props["MainPID"] != "123" {
		t.Fatalf("bad service status %#v", v)
	}
	if req := f.requests[0]; req.Mode != "posix" || req.TimeoutMs != 10000 || req.MaxBytes != 262144 || !strings.Contains(req.Command, "show --property=") || !strings.HasSuffix(req.Command, "-- 'nginx.service'") {
		t.Fatalf("unsafe service command %+v", req)
	}
	f.stdout = "{\"MESSAGE\":\"started\"}\ninvalid-json\n{\"MESSAGE\":\"ready\"}\n"
	v = jobsJSON(t, s, context.Background(), OpServicesLogs, args)
	if v["count"] != float64(2) || v["parseErrors"] != float64(1) || v["partial"] != true || !strings.Contains(f.requests[1].Command, "--lines=100 --unit='nginx.service'") {
		t.Fatalf("bad journal output %#v", v)
	}
	for _, unit := range []string{"", "--all", "x;id", "x'$(id)", "x\nreboot", "a/b", strings.Repeat("x", 201)} {
		if _, err := s.handleJobsTool(context.Background(), OpServicesStatus, map[string]any{"sessionId": "s", "unit": unit}); err == nil {
			t.Fatalf("injected unit accepted: %q", unit)
		}
	}
	if f.starts.Load() != 2 {
		t.Fatal("invalid unit reached executor")
	}
	if _, err := serviceRequest(OpServicesLogs, map[string]any{"sessionId": "s", "unit": "nginx", "lines": 1001}); err == nil {
		t.Fatal("unbounded logs accepted")
	}
}

func TestServicesRestartRequiresNativeApproval(t *testing.T) {
	ensureAllRegistrations(t)
	f := &serviceFixture{}
	s := New(Options{Handler: f, Gate: &fakeGate{}, Confirms: NewConfirmManager(DefaultConfirmTTL), Approver: func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
		return ApprovalDecision{}, nil
	}})
	defer s.CloseJobs()
	defer s.ClosePolicy()
	args := map[string]any{"sessionId": "s", "unit": "nginx.service"}
	if _, _, err := s.mcpCallToolAudited(context.Background(), OpServicesRestart, args); err == nil {
		t.Fatal("restart without token accepted")
	}
	prepare := map[string]any{"action": OpServicesRestart, "tool": OpServicesRestart, "args": args}
	if _, err := s.confirmPrepareContext(context.Background(), prepare); err == nil {
		t.Fatal("native rejection bypassed")
	}
	if f.starts.Load() != 0 {
		t.Fatal("unapproved restart reached executor")
	}
	s.opts.Approver = approveTestOperation
	content, err := s.confirmPrepareContext(context.Background(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	var approved map[string]any
	_ = json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &approved)
	args["token"] = approved["token"]
	content, _, err = s.mcpCallToolAudited(context.Background(), OpServicesRestart, args)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &result)
	jobsJSON(t, s, context.Background(), OpJobsWait, map[string]any{"jobId": result["jobId"], "timeoutMs": 2000})
	if f.starts.Load() != 1 || f.requests[0].Command != "systemctl --no-ask-password restart -- 'nginx.service'" {
		t.Fatalf("bad approved restart %#v", f.requests)
	}
	if _, _, err = s.mcpCallToolAudited(context.Background(), OpServicesRestart, args); err == nil {
		t.Fatal("approval token reused")
	}
}

func TestSessionsAliasesPreserveOriginalGuards(t *testing.T) {
	ensureAllRegistrations(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
		cap  Capability
	}{
		{OpSessionsConnect, map[string]any{"serverId": "srv"}, CapCredentialUse},
		{OpSessionsConnect, map[string]any{"local": true}, CapLocalExec},
		{OpSessionsDisconnect, map[string]any{"sessionId": "s"}, CapDisconnect},
		{OpSessionsReconnect, map[string]any{"sessionId": "s"}, CapConnect},
		{OpSessionsReconnect, map[string]any{"sessionId": "s"}, CapCredentialUse},
	} {
		h := &fakeHandler{}
		s := New(Options{Handler: h, Gate: &fakeGate{denied: map[Capability]string{tc.cap: "denied"}}})
		if _, _, err := s.mcpCallToolAudited(context.Background(), tc.tool, tc.args); err == nil {
			t.Fatalf("%s bypassed %s", tc.tool, tc.cap)
		}
		if h.count() != 0 {
			t.Fatalf("%s reached host before authorization", tc.tool)
		}
	}
	h := &appctlStubHandler{replies: map[string]any{OpSessionSnapshot: map[string]any{"sessionId": "s", "tabId": "s", "status": "closed", "backendConnected": false, "tabExists": true}}}
	s := New(Options{Handler: h, Gate: &fakeGate{}})
	if _, _, err := s.mcpCallToolAudited(context.Background(), OpSessionsDisconnect, map[string]any{"sessionId": "s"}); err != nil {
		t.Fatal(err)
	}
	if !h.called(OpSessionDisconnect) || h.args[0]["id"] != "s" {
		t.Fatalf("alias target altered: %#v", h.args)
	}
}

func TestServersSearchPaginationAndSecretIsolation(t *testing.T) {
	h := &appctlStubHandler{replies: map[string]any{OpServersList: map[string]any{"servers": []any{
		map[string]any{"id": "3", "name": "Zebra", "host": "prod.example", "group": "prod", "password": "secret-search-key"},
		map[string]any{"id": "2", "name": "Alpha", "host": "prod.example", "group": "prod"},
		map[string]any{"id": "1", "name": "Alpha", "host": "dev.example", "group": "dev"},
	}}}}
	s := New(Options{Handler: h})
	page := jobsJSON(t, s, context.Background(), OpServersSearch, map[string]any{"limit": 1})
	items := page["servers"].([]any)
	if page["nextOffset"] != float64(1) || page["hasMore"] != true || items[0].(map[string]any)["id"] != "1" {
		t.Fatalf("unstable first page %#v", page)
	}
	page = jobsJSON(t, s, context.Background(), OpServersSearch, map[string]any{"query": "PROD.EXAMPLE", "group": "prod", "offset": 1, "limit": 1})
	if page["total"] != float64(2) || page["hasMore"] != false || page["servers"].([]any)[0].(map[string]any)["id"] != "3" {
		t.Fatalf("bad filtered page %#v", page)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "secret-search-key") {
		t.Fatal("password leaked in search")
	}
	if hidden := jobsJSON(t, s, context.Background(), OpServersSearch, map[string]any{"query": "secret-search-key"}); hidden["total"] != float64(0) {
		t.Fatal("password was searchable")
	}
	if defaults := jobsJSON(t, s, context.Background(), OpServersSearch, nil); defaults["limit"] != float64(100) {
		t.Fatalf("documented search default mismatched %#v", defaults)
	}
	for _, args := range []map[string]any{{"offset": -1}, {"offset": 0.5}, {"limit": 501}, {"limit": "100"}} {
		if _, err := s.handleJobsTool(context.Background(), OpServersSearch, args); err == nil {
			t.Fatalf("invalid pagination accepted %#v", args)
		}
	}
}
