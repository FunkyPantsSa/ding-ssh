package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type preflightTargetHandler struct {
	fakeHandler
	local  bool
	known  bool
	probes int
}

func (h *preflightTargetHandler) SessionTargetLocal(context.Context, string) (bool, bool, error) {
	h.probes++
	return h.local, h.known, nil
}

func preflightResult(t *testing.T, s *Server, ctx context.Context, tool string, args map[string]any) map[string]any {
	t.Helper()
	content, err := s.handlePreflight(ctx, map[string]any{"tool": tool, "args": args})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPreflightReportsEveryMissingCapabilityWithoutExecuting(t *testing.T) {
	ensureAllRegistrations(t)
	h := &preflightTargetHandler{}
	s := New(Options{Handler: h, Gate: &fakeGate{denied: map[Capability]string{CapConnect: "connect disabled", CapLocalExec: "local disabled"}}, Confirms: NewConfirmManager(DefaultConfirmTTL)})
	out := preflightResult(t, s, context.Background(), OpSessionsConnect, map[string]any{"local": true})
	if out["allowed"] != false || !reflect.DeepEqual(out["missingCapabilities"], []any{string(CapConnect), string(CapLocalExec)}) {
		t.Fatalf("missing capabilities not aggregated: %#v", out)
	}
	if h.count() != 0 || s.opts.Confirms.PendingCount() != 0 {
		t.Fatal("preflight executed an operation or requested confirmation")
	}
	err := s.authorizeInvocation(context.Background(), OpSessionsConnect, map[string]any{"local": true})
	meta := toolErrorMeta(err)
	if meta["errorCode"] != "PERMISSION_DENIED" || !reflect.DeepEqual(meta["missingCapabilities"], []string{string(CapConnect), string(CapLocalExec)}) || !errors.Is(err, ErrForbidden) {
		t.Fatalf("execution differs from preflight: %v %#v", err, meta)
	}
}

func TestPreflightLocalReconnectUsesLocalExecutionOnly(t *testing.T) {
	ensureAllRegistrations(t)
	h := &preflightTargetHandler{local: true, known: true}
	s := New(Options{Handler: h, Gate: &fakeGate{denied: map[Capability]string{CapCredentialUse: "credentials disabled"}}})
	for _, tool := range []string{OpSessionsReconnect, "reconnect_session"} {
		args := map[string]any{"sessionId": "local"}
		if tool == "reconnect_session" {
			args = map[string]any{"id": "local"}
		}
		out := preflightResult(t, s, context.Background(), tool, args)
		if out["allowed"] != true || strings.Contains(strings.Join(uiStringList(out["requiredCapabilities"]), ","), string(CapCredentialUse)) {
			t.Fatalf("local reconnect depends on remote credentials: %#v", out)
		}
		if err := s.authorizeInvocation(context.Background(), tool, args); err != nil {
			t.Fatal(err)
		}
	}
	h.local = false
	out := preflightResult(t, s, context.Background(), OpSessionsReconnect, map[string]any{"sessionId": "remote"})
	if out["allowed"] != false || !reflect.DeepEqual(out["missingCapabilities"], []any{string(CapCredentialUse)}) {
		t.Fatalf("remote credential check weakened: %#v", out)
	}
	h.local = true
	s.opts.Gate = &fakeGate{denied: map[Capability]string{CapLocalExec: "local disabled"}}
	out = preflightResult(t, s, context.Background(), OpSessionsReconnect, map[string]any{"sessionId": "local"})
	if out["allowed"] != false || !reflect.DeepEqual(out["missingCapabilities"], []any{string(CapLocalExec)}) {
		t.Fatalf("local execution cap bypassed: %#v", out)
	}
}

func TestPreflightSeparatesApprovalAndPathConstraints(t *testing.T) {
	ensureAllRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	out := preflightResult(t, env.srv, context.Background(), OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/file", "content": "private contents must not be echoed"})
	if out["allowed"] != true || out["confirmationRequired"] != true || env.srv.opts.Confirms.PendingCount() != 0 {
		t.Fatalf("approval preview has side effects: %#v", out)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "private contents") {
		t.Fatal("preflight leaked planned payload")
	}
	out = preflightResult(t, env.srv, context.Background(), OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/etc/file", "content": "x"})
	if out["allowed"] != false || out["confirmationRequired"] != true || len(out["issues"].([]any)) == 0 {
		t.Fatalf("write path failure not reported: %#v", out)
	}
}

func TestPreflightValidatesArgumentsAndDoesNotProbeUnauthorizedSession(t *testing.T) {
	s := policyTestServer(t, nil)
	h := &preflightTargetHandler{known: true}
	s.opts.Handler = h
	ctx, _, _ := policyTestCaller(t, s)
	out := preflightResult(t, s, ctx, OpSessionsReconnect, map[string]any{"sessionId": "not-authorized"})
	if out["allowed"] != false || h.probes != 0 {
		t.Fatalf("scoped preflight probed another target: %#v probes=%d", out, h.probes)
	}
	for _, args := range []map[string]any{{"local": "true"}, {"local": true, "typo": true}} {
		out = preflightResult(t, s, context.Background(), OpSessionsConnect, args)
		if out["allowed"] != false || len(out["issues"].([]any)) == 0 {
			t.Fatalf("invalid argument accepted: %#v", out)
		}
	}
	out = preflightResult(t, s, context.Background(), OpExecStart, map[string]any{"local": true, "command": "echo x", "timeoutMs": 1})
	if out["allowed"] != false {
		t.Fatalf("invalid execution budget accepted: %#v", out)
	}
}

func TestPreflightScopedPathFailureDoesNotRevealHostAllowlist(t *testing.T) {
	ensureAllRegistrations(t)
	s := policyTestServer(t, nil)
	ctx, _, _ := policyTestCaller(t, s)
	// The client scope permits /srv/app, but the global host allowlist is
	// deliberately different. Execution policy must reject before disclosing it.
	s.opts.RemoteFSPaths = &sftpStubScope{list: []string{"/srv/host-secret"}}
	out := preflightResult(t, s, ctx, OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/file", "content": "safe"})
	b, _ := json.Marshal(out)
	if out["allowed"] != false || strings.Contains(string(b), "host-secret") {
		t.Fatalf("scoped preflight revealed host policy: %#v", out)
	}
}

func TestLocalExecStartRequiresOnlyLocalExecutionCapability(t *testing.T) {
	ensureAllRegistrations(t)
	h := &preflightTargetHandler{}
	s := New(Options{Handler: h, Gate: &fakeGate{denied: map[Capability]string{CapTerminalInput: "terminal input disabled"}}})
	args := map[string]any{"local": true, "command": "echo local"}
	out := preflightResult(t, s, context.Background(), OpExecStart, args)
	if out["allowed"] != true || !reflect.DeepEqual(out["requiredCapabilities"], []any{string(CapLocalExec)}) {
		t.Fatalf("local exec must require only exec.local: %#v", out)
	}
	if err := s.authorizeInvocation(context.Background(), OpExecStart, args); err != nil {
		t.Fatalf("execution rejected despite local-only capability: %v", err)
	}

	remoteArgs := map[string]any{"sessionId": "remote", "command": "echo remote"}
	remote := preflightResult(t, s, context.Background(), OpExecStart, remoteArgs)
	if remote["allowed"] != false || !reflect.DeepEqual(remote["missingCapabilities"], []any{string(CapTerminalInput)}) {
		t.Fatalf("remote exec must still require terminal.input: %#v", remote)
	}
}

func TestOperationErrorRecoveryMetadataSurvivesWrapping(t *testing.T) {
	err := &OperationError{Code: "FRONTEND_NOT_READY", Message: "not ready", Retryable: true, RetryAfterMs: 100, NextSteps: []string{"wait"}}
	meta := toolErrorMeta(err)
	if meta["retryable"] != true || meta["retryAfterMs"] != 100 || meta["outcomeKnown"] != true {
		t.Fatalf("lost recovery info: %#v", meta)
	}
}
