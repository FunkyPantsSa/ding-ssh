package debugsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func protocolRequest(t *testing.T, base, method, body string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	r, err := http.NewRequest(method, base+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer test-token")
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

type protocolTargetHandler struct {
	mu  sync.Mutex
	ids []string
}

func (h *protocolTargetHandler) Handle(_ context.Context, op string, raw json.RawMessage) (any, error) {
	if op == OpSessions {
		return map[string]any{"tabs": []any{map[string]any{"sessionId": "allowed", "sftpPath": "/srv/work"}}}, nil
	}
	if op == OpServers || op == OpServersList {
		return map[string]any{"servers": []any{map[string]any{"id": "server-1", "name": "one", "host": "example", "password": "super-secret-password", "keyContent": "super-secret-private-key"}}}, nil
	}
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	h.mu.Lock()
	h.ids = append(h.ids, appctlString(args, "id"))
	h.mu.Unlock()
	return map[string]any{"ok": true}, nil
}

func protocolPolicyServer(t *testing.T) (*Server, string, string, *protocolTargetHandler) {
	t.Helper()
	clients := NewClientRegistry()
	_, token, err := clients.Issue(ClientPolicy{Name: "restricted", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Capabilities: []string{string(CapRead), string(CapTerminalRead), string(CapTerminalInput), string(CapRemoteFSRead)}, SessionIDs: []string{"allowed"}, ReadPaths: []string{"/srv"}})
	if err != nil {
		t.Fatal(err)
	}
	h := &protocolTargetHandler{}
	s := New(Options{Addr: "127.0.0.1:0", Token: "test-token", Clients: clients, Handler: h, Hub: NewHub(), Gate: &stubGate{}, Confirms: NewConfirmManager(DefaultConfirmTTL), Approver: approveTestOperation})
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s, "http://" + addr, token, h
}

func TestProtocolURLTargetCannotBeSubstituted(t *testing.T) {
	_, base, token, h := protocolPolicyServer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}
	status, body := httpDoHeaders(t, base, "POST", "/v1/terminals/allowed/input", map[string]any{"data": "pwd\r"}, headers)
	if status != http.StatusOK {
		t.Fatalf("authorized target failed: %d %s", status, body)
	}
	for _, tc := range []struct {
		path string
		args map[string]any
	}{
		{"/v1/terminals/denied/input", map[string]any{"id": "allowed", "data": "pwd\r"}},
		{"/v1/terminals/denied/input", map[string]any{"sessionId": "allowed", "data": "pwd\r"}},
		{"/v1/terminals/allowed/input?id=denied", map[string]any{"data": "pwd\r"}},
		{"/v1/terminals/allowed/input", map[string]any{"id": "denied", "data": "pwd\r"}},
		{"/v1/terminals/denied/input", map[string]any{"data": "pwd\r"}},
	} {
		status, body = httpDoHeaders(t, base, "POST", tc.path, tc.args, headers)
		if status != http.StatusBadRequest && status != http.StatusForbidden {
			t.Fatalf("target substitution accepted: %d %s", status, body)
		}
	}
	status, _ = httpDoHeaders(t, base, "GET", "/v1/terminals/denied/buffer?sessionId=allowed", nil, headers)
	if status != http.StatusBadRequest && status != http.StatusForbidden {
		t.Fatalf("read target substitution accepted: %d", status)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ids) != 1 || h.ids[0] != "allowed" {
		t.Fatalf("unexpected executions: %v", h.ids)
	}
}

func TestHTTPConnectionRoutesEnforceAllToolCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		args               map[string]any
		denied             Capability
	}{
		{"connect", "POST", "/v1/tabs", map[string]any{"serverId": "server-1"}, CapConnect},
		{"close", "DELETE", "/v1/tabs/tab-1", nil, CapDisconnect},
		{"disconnect", "POST", "/v1/sessions/tab-1/disconnect", nil, CapDisconnect},
		{"reconnect-needs-connect", "POST", "/v1/sessions/tab-1/reconnect", nil, CapConnect},
		{"reconnect-needs-disconnect", "POST", "/v1/sessions/tab-1/reconnect", nil, CapDisconnect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, _ := startHTTP(t, &stubGate{denied: map[Capability]string{tc.denied: "test capability disabled"}}, &stubExecutor{})
			status, body := httpDo(t, base, tc.method, tc.path, tc.args)
			if status != http.StatusForbidden {
				t.Fatalf("disabled tool capability bypassed: %d %s", status, body)
			}
		})
	}
}

func TestProtocolResourceSubscriptionsEnforceScopeAndOwner(t *testing.T) {
	s, base, token, _ := protocolPolicyServer(t)
	auth := map[string]string{"Authorization": "Bearer " + token}
	_, initHeaders, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, auth)
	sid := initHeaders.Get(mcpSessionHeader)
	if sid == "" {
		t.Fatal("missing MCP session")
	}
	headers := map[string]string{"Authorization": "Bearer " + token, mcpSessionHeader: sid}
	_, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, headers)
	for _, uri := range []string{"terminal://allowed/buffer", "sftp://allowed/cwd"} {
		_, _, body := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"`+uri+`"}}`, headers)
		if strings.Contains(body, `"error"`) {
			t.Fatalf("authorized subscription failed: %s", body)
		}
	}
	for _, uri := range []string{"terminal://denied/buffer", "ui://view", "terminal:///buffer", "sftp://allowed/nested/cwd"} {
		_, _, body := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{"uri":"`+uri+`"}}`, headers)
		if !strings.Contains(body, `"error"`) {
			t.Fatalf("unauthorized subscription accepted: %s", body)
		}
	}
	status, _, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":4,"method":"ping"}`, map[string]string{mcpSessionHeader: sid})
	if status != http.StatusNotFound {
		t.Fatalf("another identity used transport session: %d", status)
	}
	p, ok := s.opts.Clients.Authenticate(token)
	if !ok {
		t.Fatal("client missing")
	}
	s.opts.Clients.Revoke(p.ID)
	status, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":5,"method":"ping"}`, headers)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked client retained access: %d", status)
	}
}

func TestPermissionsViewShowsEffectiveCapabilityIntersection(t *testing.T) {
	registry := NewClientRegistry()
	p, _, err := registry.Issue(ClientPolicy{Name: "read-only", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), AllTargets: true, Capabilities: []string{string(CapTerminalRead)}})
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{Clients: registry, Gate: &stubGate{open: map[Capability]bool{CapTerminalRead: true, CapTerminalInput: true, CapRemoteFSWrite: true}}})
	ctx := context.WithValue(context.Background(), callerKey{}, p)
	view := s.permissionsView(ctx)
	effective := view["effectiveCapabilities"].(map[string]bool)
	global := view["globalCapabilities"].(map[string]bool)
	if !effective[string(CapTerminalRead)] || effective[string(CapTerminalInput)] || effective[string(CapRemoteFSWrite)] || !global[string(CapTerminalInput)] {
		t.Fatalf("incorrect capability intersection: %v", view)
	}
	for _, action := range ConfirmableActions() {
		if action == "capability.enable" {
			t.Fatal("MCP self-elevation advertised")
		}
	}
}

func TestLegacyServerListingsAlwaysRedactSecrets(t *testing.T) {
	s, base, _, _ := protocolPolicyServer(t)
	_, token, err := s.opts.Clients.Issue(ClientPolicy{Name: "metadata-reader", AllTargets: true, ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Capabilities: []string{string(CapRead)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, auth := range []string{"Bearer test-token", "Bearer " + token} {
		headers := map[string]string{"Authorization": auth}
		status, body := httpDoHeaders(t, base, "GET", "/v1/servers", nil, headers)
		if status != http.StatusOK || strings.Contains(body, "super-secret") || !strings.Contains(body, `"redacted":true`) {
			t.Fatalf("unsafe HTTP listing: %d %s", status, body)
		}
		_, _, body = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"app://servers"}}`, headers)
		if strings.Contains(body, `"error"`) || strings.Contains(body, "super-secret") {
			t.Fatalf("unsafe resource listing: %s", body)
		}
	}
}

func TestProtocolRejectsMalformedEnvelopeAndSchema(t *testing.T) {
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	for _, body := range []string{`{"id":1,"method":"ping"}`, `{"jsonrpc":"2.0","id":true,"method":"ping"}`, `{"jsonrpc":"2.0","id":null,"method":"ping"}`, `{"jsonrpc":"2.0","id":1,"method":""}`} {
		_, _, got := protocolRequest(t, base, "POST", body, nil)
		if !strings.Contains(got, `-32600`) {
			t.Fatalf("invalid envelope accepted: %s", got)
		}
	}
	for _, args := range []string{`{"id":"t","lines":1.5}`, `{"id":"t","lines":"2"}`, `{"id":"t","unexpected":1}`} {
		_, _, got := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_terminal","arguments":`+args+`}}`, nil)
		if !strings.Contains(got, `-32602`) {
			t.Fatalf("invalid arguments accepted: %s", got)
		}
	}
	for _, request := range []string{
		`{"name":"exec.start","arguments":{"sessionId":"t","command":"true","mode":"unknown"}}`,
		`{"name":"exec.start","arguments":{"sessionId":"t","command":"true","env":{"BAD":false}}}`,
		`{"name":"exec.plan","arguments":{"sessionIds":[],"command":"true"}}`,
	} {
		_, _, got := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+request+`}`, nil)
		if !strings.Contains(got, `-32602`) {
			t.Fatalf("invalid structured arguments accepted: %s", got)
		}
	}
	status, _, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Content-Type": "text/plain"})
	if status != 415 {
		t.Fatalf("content type status=%d", status)
	}
	status, _, _ = protocolRequest(t, base, "POST", strings.Repeat(" ", mcpBodyLimit+1), nil)
	if status != 413 {
		t.Fatalf("body limit status=%d", status)
	}
}

func TestProtocolSessionInitializationTemplatesAndExpiry(t *testing.T) {
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	_, headers, body := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	sid := headers.Get(mcpSessionHeader)
	if sid == "" {
		t.Fatalf("no session: %s", body)
	}
	h := map[string]string{mcpSessionHeader: sid, "MCP-Protocol-Version": "2025-06-18"}
	status, _, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, h)
	if status != 400 {
		t.Fatalf("uninitialized call status=%d", status)
	}
	status, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, h)
	if status != 202 {
		t.Fatal(status)
	}
	_, _, body = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":3,"method":"resources/templates/list"}`, h)
	if !strings.Contains(body, "uriTemplate") {
		t.Fatalf("templates missing: %s", body)
	}
	_, _, body = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":4,"method":"resources/subscribe","params":{"uri":"ui://view"}}`, h)
	if strings.Contains(body, "error") {
		t.Fatalf("subscribe failed: %s", body)
	}
	wrong := map[string]string{mcpSessionHeader: sid, "MCP-Protocol-Version": "2025-03-26"}
	status, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":5,"method":"ping"}`, wrong)
	if status != 400 {
		t.Fatal(status)
	}
	status, _, _ = protocolRequest(t, base, "DELETE", "", h)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":6,"method":"ping"}`, h)
	if status != 404 {
		t.Fatal(status)
	}
	s := New(Options{Hub: NewHub()})
	sess, err := s.mcpNewSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.mcpMu.Lock()
	sess.lastUsed = time.Now().Add(-mcpSessionTTL - time.Second)
	s.mcpMu.Unlock()
	if s.mcpGetSession(sess.id) != nil {
		t.Fatal("expired session retained")
	}
}

func TestProtocolOriginAndHostValidation(t *testing.T) {
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	status, _, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "https://attacker.invalid"})
	if status != 403 {
		t.Fatal(status)
	}
	status, _, _ = protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": base})
	if status != 200 {
		t.Fatal(status)
	}
	r, _ := http.NewRequest("GET", base+"/v1/health", nil)
	r.Host = "attacker.invalid"
	r.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal(resp.StatusCode)
	}
}

func TestProtocolQueryCredentialRejected(t *testing.T) {
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	r, _ := http.NewRequest("GET", base+"/v1/health?token=test-token", nil)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("URL credential accepted: %d", resp.StatusCode)
	}
	token := mcpPrepareToken(t, base, "settings.update", map[string]any{"settings": map[string]any{"uiScale": 120}})
	status, _ := httpDo(t, base, "PUT", "/v1/settings?confirmToken="+token, map[string]any{"uiScale": 120})
	if status != http.StatusForbidden {
		t.Fatalf("URL confirmation accepted: %d", status)
	}
	status, _ = httpDoHeaders(t, base, "PUT", "/v1/settings", map[string]any{"uiScale": 120}, map[string]string{"X-Ding-Confirm-Token": token})
	if status != http.StatusOK {
		t.Fatalf("header confirmation failed: %d", status)
	}
}

func TestProtocolOnlyOneStreamPerSession(t *testing.T) {
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	_, h, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	sid := h.Get(mcpSessionHeader)
	r, _ := http.NewRequest("GET", base+"/mcp", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	r.Header.Set(mcpSessionHeader, sid)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	status, _, _ := protocolRequest(t, base, "GET", "", map[string]string{mcpSessionHeader: sid})
	if status != http.StatusConflict {
		t.Fatalf("duplicate stream status=%d", status)
	}
}

func TestHubCursorGapAndSubscriberDrop(t *testing.T) {
	h := NewHub()
	id, ch := h.Subscribe("ssh.output")
	defer h.Unsubscribe(id)
	for i := 0; i < 600; i++ {
		h.PublishRaw("ssh:output:t", map[string]any{"data": "x"})
	}
	page := h.Since(1, 10)
	if page["gap"] != true || page["count"] != 10 {
		t.Fatalf("bad replay: %v", page)
	}
	first := <-ch
	if first.Seq != 1 {
		t.Fatal(first)
	}
	h.PublishRaw("ssh:output:t", map[string]any{"data": "y"})
	for len(ch) > 1 {
		<-ch
	}
	last := <-ch
	if last.Dropped == 0 {
		t.Fatal("subscriber drop not observable")
	}
}

func TestRecentEventsSupportsMCPCursors(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	for i := 0; i < 5; i++ {
		ts.srv.opts.Hub.PublishRaw("ssh:output:t", map[string]any{"data": "x"})
	}
	isErr, text, _ := callTool(t, ts.srv, "recent_events", map[string]any{"after": float64(2), "limit": float64(2)})
	if isErr {
		t.Fatal(text)
	}
	var page struct {
		Events     []Event `json:"events"`
		NextCursor uint64  `json:"nextCursor"`
		HasMore    bool    `json:"hasMore"`
	}
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Seq != 3 || page.NextCursor != 4 || !page.HasMore {
		t.Fatalf("incorrect MCP cursor page: %s", text)
	}
}

type cancellingHandler struct{ started chan struct{} }

func (h *cancellingHandler) Handle(ctx context.Context, op string, _ json.RawMessage) (any, error) {
	if op == OpSettings {
		close(h.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return map[string]any{}, nil
}

func TestProtocolCancellationInterruptsOutstandingRequest(t *testing.T) {
	h := &cancellingHandler{make(chan struct{})}
	s := New(Options{Addr: "127.0.0.1:0", Token: "test-token", Handler: h, Hub: NewHub(), Gate: &stubGate{}})
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	base := "http://" + addr
	done := make(chan string, 1)
	go func() {
		r, _ := http.NewRequest("POST", base+"/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":8.0,"method":"tools/call","params":{"name":"get_settings","arguments":{}}}`))
		r.Header.Set("Authorization", "Bearer test-token")
		r.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			done <- e.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		done <- string(b)
	}()
	select {
	case <-h.started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	status, _, _ := protocolRequest(t, base, "POST", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":8}}`, nil)
	if status != 202 {
		t.Fatal(status)
	}
	select {
	case got := <-done:
		if !strings.Contains(got, "canceled") {
			t.Fatalf("unexpected cancel result: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
}
