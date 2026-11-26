package debugsrv

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func policyTestServer(t *testing.T, approver ApprovalFunc) *Server {
	t.Helper()
	ensureAllRegistrations(t)
	s := New(Options{Gate: &fakeGate{}, Handler: &fakeHandler{}, Confirms: NewConfirmManager(DefaultConfirmTTL), Clients: NewClientRegistry(), Approver: approver, Audits: NewAuditLog()})
	t.Cleanup(s.ClosePolicy)
	return s
}

func policyTestCaller(t *testing.T, s *Server) (context.Context, ClientPolicy, string) {
	t.Helper()
	p, token, err := s.opts.Clients.Issue(ClientPolicy{Name: "scoped test client", Capabilities: []string{string(CapRead), string(CapTerminalRead), string(CapRemoteFSRead), string(CapRemoteFSWrite), string(CapTerminalInput), string(CapConnect), string(CapDisconnect), string(CapCredentialUse), string(CapConfigWrite)}, SessionIDs: []string{"s1"}, ServerIDs: []string{"srv1"}, ReadPaths: []string{"/srv/read", "/srv/app"}, WritePaths: []string{"/srv/app"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	authenticated, ok := s.opts.Clients.Authenticate(token)
	if !ok {
		t.Fatal("issued client did not authenticate")
	}
	return context.WithValue(context.Background(), callerKey{}, authenticated), p, token
}

func TestPolicyNativeApprovalFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   ApprovalFunc
	}{
		{"missing", nil},
		{"denied", func(context.Context, ApprovalRequest) (ApprovalDecision, error) { return ApprovalDecision{}, nil }},
		{"failed", func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{}, errors.New("native unavailable")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := policyTestServer(t, tc.fn)
			_, err := s.confirmPrepareContext(context.Background(), map[string]any{"action": OpSftpMkdir, "tool": OpSftpMkdir, "args": map[string]any{"sessionId": "s1", "path": "/srv/app/private"}})
			if err == nil || s.opts.Confirms.PendingCount() != 0 || s.opts.Handler.(*fakeHandler).count() != 0 {
				t.Fatalf("unapproved operation escaped: err=%v pending=%d", err, s.opts.Confirms.PendingCount())
			}
			audit := s.opts.Audits.Recent(1, "", "authorization")
			if len(audit) != 1 || audit[0].OK || audit[0].ClientID != "local-owner" || audit[0].OperationID == "" || audit[0].Args != "" || audit[0].Error != "" {
				t.Fatalf("unsafe/incomplete authorization audit: %#v", audit)
			}
		})
	}
}

func TestPolicyClientScopesUseActualHandlerTargets(t *testing.T) {
	s := policyTestServer(t, nil)
	ctx, _, _ := policyTestCaller(t, s)
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		deny bool
	}{
		{"read", OpSftpRead, map[string]any{"sessionId": "s1", "path": "/srv/read/file"}, false},
		{"write", OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/file"}, false},
		{"terminal", OpTerminalRun, map[string]any{"id": "s1"}, false},
		{"server", OpServersGet, map[string]any{"id": "srv1"}, false},
		{"cross session", OpSftpRead, map[string]any{"sessionId": "s2", "path": "/srv/read/file"}, true},
		{"terminal ID masking", OpTerminalRun, map[string]any{"id": "s2", "sessionId": "s1"}, true},
		{"server ID masking", OpServersGet, map[string]any{"id": "srv2", "serverId": "srv1"}, true},
		{"write outside read scope", OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/read/file"}, true},
		{"path boundary", OpSftpRead, map[string]any{"sessionId": "s1", "path": "/srv/read-evil/file"}, true},
		{"path traversal", OpSftpRead, map[string]any{"sessionId": "s1", "path": "/srv/read/../secret"}, true},
		{"relative read", OpSftpRead, map[string]any{"sessionId": "s1", "path": "."}, true},
		{"rename destination", OpSftpRename, map[string]any{"sessionId": "s1", "from": "/srv/app/a", "to": "/etc/a"}, true},
		{"batch server masking", OpServersMoveGroup, map[string]any{"serverId": "srv1", "ids": []any{"srv1", "srv2"}}, true},
		{"global terminals masking", "list_terminals", map[string]any{"sessionId": "s1"}, true},
		{"global servers masking", "list_servers", map[string]any{"serverId": "srv1"}, true},
		{"global server list masking", OpServersList, map[string]any{"serverId": "srv1"}, true},
		{"global server search masking", OpServersSearch, map[string]any{"serverId": "srv1"}, true},
		{"global groups masking", OpGroupsList, map[string]any{"serverId": "srv1"}, true},
		{"global tunnels masking", OpTunnelsHealth, map[string]any{"sessionId": "s1"}, true},
		{"plaintext", OpServersGet, map[string]any{"id": "srv1", "includeSecrets": true}, true},
		{"custom node", "open_tab", map[string]any{"serverId": "srv1", "node": map[string]any{"host": "attacker"}}, true},
		{"custom node case alias", "open_tab", map[string]any{"serverId": "srv1", "Node": map[string]any{"host": "attacker"}}, true},
		{"local case alias", "open_tab", map[string]any{"serverId": "srv1", "Local": true}, true},
		{"settings privilege patch", "update_settings", map[string]any{"settings": map[string]any{"debug": map[string]any{"allowEval": true}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.authorizeInvocation(ctx, tc.tool, tc.args)
			if (err != nil) != tc.deny {
				t.Fatalf("deny=%v err=%v", tc.deny, err)
			}
		})
	}
}

func TestPolicyOwnerCannotPatchDebugUsingJSONCaseAliases(t *testing.T) {
	s := policyTestServer(t, nil)
	for _, key := range []string{"debug", "Debug", "dEbUg", "DEBUG.allowEval"} {
		if err := s.authorizeInvocation(context.Background(), "update_settings", map[string]any{"settings": map[string]any{key: map[string]any{"allowEval": true}}}); err == nil {
			t.Fatalf("debug permission case alias %q escaped policy", key)
		}
	}
}

func TestPolicyClientCapabilitiesAndImmutableCopies(t *testing.T) {
	s := policyTestServer(t, nil)
	input := ClientPolicy{Name: "copy", Capabilities: []string{string(CapRemoteFSRead)}, SessionIDs: []string{"s1"}, ServerIDs: []string{"srv1"}, ReadPaths: []string{"/srv/read"}, WritePaths: []string{"/srv/write"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	p, token, err := s.opts.Clients.Issue(input)
	if err != nil {
		t.Fatal(err)
	}
	want := cloneClientPolicy(p)
	mutate := func(p ClientPolicy) {
		p.Capabilities[0], p.SessionIDs[0], p.ServerIDs[0], p.ReadPaths[0], p.WritePaths[0] = string(CapTerminalInput), "s2", "srv2", "/etc", "/etc"
	}
	mutate(input)
	mutate(p)
	auth, ok := s.opts.Clients.Authenticate(token)
	if !ok || !reflect.DeepEqual(auth, want) {
		t.Fatalf("issue policy mutated: %#v", auth)
	}
	mutate(auth)
	listed := s.opts.Clients.List()
	mutate(listed[0])
	auth, ok = s.opts.Clients.Authenticate(token)
	if !ok || !reflect.DeepEqual(auth, want) {
		t.Fatalf("returned policy mutated registry: %#v", auth)
	}
	ctx := context.WithValue(context.Background(), callerKey{}, auth)
	if err := s.authorizeInvocation(ctx, OpTerminalRun, map[string]any{"id": "s1"}); err == nil {
		t.Fatal("client used a capability it was not granted")
	}
	if err := s.authorizeInvocation(ctx, "read_terminal", map[string]any{"id": "s1"}); err == nil {
		t.Fatal("client read a terminal without terminal.read")
	}
	for _, cap := range []Capability{CapEval, CapTrustedAutomation, CapSecretsRead, CapLocalExec} {
		if _, _, err := s.opts.Clients.Issue(ClientPolicy{Capabilities: []string{string(cap)}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}); err == nil {
			t.Fatalf("issued unrestricted capability %s", cap)
		}
	}
}

func TestPolicyConnectRequiresClientCredentialUse(t *testing.T) {
	s := policyTestServer(t, nil)
	ensureSudoRegistrations(t)
	p, _, err := s.opts.Clients.Issue(ClientPolicy{Capabilities: []string{string(CapConnect), string(CapSudoCredential), string(CapTerminalInput)}, ServerIDs: []string{"srv1"}, SessionIDs: []string{"s1"}, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), callerKey{}, p)
	for _, tool := range []string{"open_tab", OpSessionsConnect, OpTerminalSudo} {
		if err := s.authorizeInvocation(ctx, tool, map[string]any{"serverId": "srv1", "id": "s1"}); err == nil || !strings.Contains(err.Error(), string(CapCredentialUse)) {
			t.Fatalf("client used saved credential without credentials.use: tool=%s err=%v", tool, err)
		}
	}
	// Local owner execution is independent of saved remote credentials.
	s.opts.Gate = &fakeGate{denied: map[Capability]string{CapCredentialUse: "credential use denied"}}
	if err := s.authorizeInvocation(context.Background(), "open_tab", map[string]any{"local": true}); err != nil {
		t.Fatalf("local terminal required remote credentials: %v", err)
	}
}

func TestPolicyObservationToolsRemainOwnerOnlyWithPreflight(t *testing.T) {
	s := policyTestServer(t, nil)
	p, _, err := s.opts.Clients.Issue(ClientPolicy{
		Capabilities: []string{string(CapRead), string(CapUIInspect), string(CapConnect)},
		AllTargets:   true,
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), callerKey{}, p)
	for _, tool := range []string{OpAppObserve, OpRuntimeCapabilities, OpDiagnostics, OpUIObserve, OpUIInspect, OpUIDiagnose, OpUIWait, OpUIAct, OpUICompare, OpVerifyRun, OpVerifyGet, OpTargetsProbe} {
		if err := s.authorizeInvocationPolicy(ctx, tool, nil); err == nil || !strings.Contains(err.Error(), "本机可信所有者") {
			t.Fatalf("owner-only observation tool escaped the merged policy: tool=%s err=%v", tool, err)
		}
	}
	for _, tool := range []string{OpAppPreflight, OpToolsSearch} {
		if err := s.authorizeInvocation(ctx, tool, nil); err != nil {
			t.Fatalf("scoped client lost discovery/preflight access: tool=%s err=%v", tool, err)
		}
	}
}

func TestPolicyConfirmationOwnershipRevocationAndExpiry(t *testing.T) {
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	env.srv.opts.Clients = NewClientRegistry()
	t.Cleanup(env.srv.ClosePolicy)
	ctx1, p1, _ := policyTestCaller(t, env.srv)
	ctx2, _, _ := policyTestCaller(t, env.srv)
	args := map[string]any{"sessionId": "s1", "path": "/srv/app/new"}
	prepare := func() string {
		t.Helper()
		_, token, _, err := env.srv.approveOperation(ctx1, OpSftpMkdir, OpSftpMkdir, args)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token := prepare()
	if _, _, err := env.srv.mcpCallToolAudited(ctx2, OpSftpMkdir, map[string]any{"sessionId": "s1", "path": "/srv/app/new", "token": token}); err == nil || env.h.count() != 0 {
		t.Fatal("another client used confirmation token")
	}
	if _, ok := env.srv.opts.Confirms.Peek(token); !ok {
		t.Fatal("cross-client rejection consumed the owner's token")
	}
	if _, err := env.srv.confirmCommit(ctx1, map[string]any{"token": token}); err != nil || env.h.count() != 1 {
		t.Fatalf("owner commit failed: %v", err)
	}
	if _, err := env.srv.confirmCommit(ctx1, map[string]any{"token": token}); err == nil || env.h.count() != 1 {
		t.Fatal("replayed confirmation")
	}
	token = prepare()
	env.srv.RevokeClient(p1.ID)
	if _, err := env.srv.confirmCommit(ctx1, map[string]any{"token": token}); err == nil || env.h.count() != 1 {
		t.Fatal("revoked caller committed a pending operation")
	}
	ctx3, p3, credential := policyTestCaller(t, env.srv)
	env.srv.opts.Clients.mu.Lock()
	e := env.srv.opts.Clients.entries[p3.ID]
	e.policy.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	env.srv.opts.Clients.entries[p3.ID] = e
	env.srv.opts.Clients.mu.Unlock()
	if _, ok := env.srv.opts.Clients.Authenticate(credential); ok {
		t.Fatal("expired credential authenticated")
	}
	if err := env.srv.authorizeInvocation(ctx3, OpSftpRead, map[string]any{"sessionId": "s1", "path": "/srv/app/a"}); err == nil {
		t.Fatal("in-flight expired identity retained authorization")
	}
	_, token, _, err := env.srv.approveOperation(context.Background(), OpSftpMkdir, OpSftpMkdir, args)
	if err != nil {
		t.Fatal(err)
	}
	env.srv.opts.Confirms.mu.Lock()
	entry := env.srv.opts.Confirms.entries[token]
	entry.pending.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	env.srv.opts.Confirms.entries[token] = entry
	env.srv.opts.Confirms.mu.Unlock()
	if _, err := env.srv.confirmCommit(context.Background(), map[string]any{"token": token}); err == nil || env.h.count() != 1 {
		t.Fatal("expired operation token executed")
	}
}

func TestPolicyGrantsBindExactPayloadAndCommand(t *testing.T) {
	for _, tc := range []struct {
		name, tool    string
		args, changed map[string]any
	}{
		{"command", OpTerminalSudo, map[string]any{"id": "s1", "command": "id"}, map[string]any{"id": "s1", "command": "whoami"}},
		{"command whitespace", OpTerminalSudo, map[string]any{"id": "s1", "command": "id"}, map[string]any{"id": "s1", "command": "id\n"}},
		{"file whitespace", OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/f", "content": "approved"}, map[string]any{"sessionId": "s1", "path": "/srv/app/f", "content": " approved\n"}},
		{"nested token payload", OpSftpPatch, map[string]any{"sessionId": "s1", "path": "/srv/app/f", "env": map[string]any{"token": "approved"}}, map[string]any{"sessionId": "s1", "path": "/srv/app/f", "env": map[string]any{"token": "changed"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := policyTestServer(t, func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
				calls++
				return ApprovalDecision{Approved: calls == 1, AllowFor: 10 * time.Minute}, nil
			})
			if _, _, _, err := s.approveOperation(context.Background(), tc.tool, tc.tool, tc.args); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := s.approveOperation(context.Background(), tc.tool, tc.tool, tc.args); err != nil || calls != 1 {
				t.Fatalf("identical grant was not reusable: %v calls=%d", err, calls)
			}
			if _, _, _, err := s.approveOperation(context.Background(), tc.tool, tc.tool, tc.changed); err == nil || calls != 2 {
				t.Fatalf("grant covered changed payload: err=%v calls=%d", err, calls)
			}
			m := NewConfirmManager(DefaultConfirmTTL)
			token, _ := m.Prepare(tc.tool, tc.args, "")
			if err := m.Commit(token, tc.tool, tc.changed); err == nil {
				t.Fatal("operation token covered changed payload")
			}
		})
	}
}

func TestPolicyInvalidationRejectsApprovalStillOpen(t *testing.T) {
	opened, release := make(chan struct{}), make(chan struct{})
	s := policyTestServer(t, func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
		close(opened)
		<-release
		return ApprovalDecision{Approved: true, AllowFor: time.Minute}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, _, _, err := s.approveOperation(context.Background(), OpSftpMkdir, OpSftpMkdir, map[string]any{"sessionId": "s1", "path": "/srv/app/a"})
		done <- err
	}()
	<-opened
	s.InvalidateAuthorizations()
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "变更") || s.opts.Confirms.PendingCount() != 0 {
		t.Fatalf("stale dialog issued token: err=%v pending=%d", err, s.opts.Confirms.PendingCount())
	}
}

func TestPolicyApprovalSummaryDoesNotHideCommandSuffix(t *testing.T) {
	command := strings.Repeat("echo harmless;", 1000) + "rm important-file"
	projection := approvalProjection(map[string]any{"command": command, "content": " secret file bytes "})
	if projection["command"] != command {
		t.Fatal("approval hid part of the command")
	}
	payload, ok := projection["content"].(map[string]any)
	if !ok || payload["bytes"] != len(" secret file bytes ") || payload["sha256"] == "" || strings.Contains(NormalizeArgsJSON(projection), "secret file bytes") {
		t.Fatalf("file approval summary lacks safe identity: %#v", projection)
	}
}

func TestPolicyLogsCannotBypassTerminalReadCapability(t *testing.T) {
	s := policyTestServer(t, nil)
	s.opts.Gate = &fakeGate{denied: map[Capability]string{CapTerminalRead: "terminal.read is disabled"}}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"MCP tool", func() error {
			failed, text, protocolErr := callTool(t, s, "read_logs", map[string]any{})
			if protocolErr != "" {
				return errors.New(protocolErr)
			}
			if failed {
				return errors.New(text)
			}
			return nil
		}},
		{"MCP resource", func() error { _, err := s.mcpReadResource(context.Background(), "app://logs/tail"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil || !strings.Contains(err.Error(), "terminal.read") {
				t.Fatalf("logs bypassed terminal.read: %v", err)
			}
			if s.opts.Handler.(*fakeHandler).count() != 0 {
				t.Fatal("denied log read reached host")
			}
		})
	}
	s.opts.Addr, s.opts.Token = "127.0.0.1:0", "test-token"
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	status, body := httpDo(t, "http://"+addr, http.MethodGet, "/v1/logs/tail", nil)
	if status != http.StatusForbidden || !strings.Contains(body, "terminal.read") || s.opts.Handler.(*fakeHandler).count() != 0 {
		t.Fatalf("HTTP logs bypassed terminal.read: status=%d body=%s", status, body)
	}
	// Enabling terminal.read restores the same endpoint without changing its
	// authentication or business handler.
	s.opts.Gate.(*fakeGate).denied = nil
	status, body = httpDo(t, "http://"+addr, http.MethodGet, "/v1/logs/tail", nil)
	if status != http.StatusOK || s.opts.Handler.(*fakeHandler).count() != 1 {
		t.Fatalf("authorized log read failed: status=%d body=%s", status, body)
	}
}
