package debugsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestToolProfilesShareHTTPCatalog(t *testing.T) {
	ensureAllRegistrations(t)
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	fullCount := 0
	for _, profile := range []string{"full", "operations", "debug"} {
		t.Run(profile, func(t *testing.T) {
			headers := map[string]string{toolProfileHeader: profile}
			status, body := httpDoHeaders(t, base, http.MethodGet, "/v1/tools", nil, headers)
			if status != http.StatusOK {
				t.Fatal(status, body)
			}
			var catalog struct {
				Profile string    `json:"profile"`
				Tools   []mcpTool `json:"tools"`
			}
			if err := json.Unmarshal([]byte(body), &catalog); err != nil {
				t.Fatal(err)
			}
			status, body = httpDoHeaders(t, base, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, headers)
			if status != http.StatusOK {
				t.Fatal(status, body)
			}
			var response struct {
				Result struct {
					Tools []mcpTool `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(body), &response); err != nil {
				t.Fatal(err)
			}
			if catalog.Profile != profile || len(catalog.Tools) != len(response.Result.Tools) {
				t.Fatalf("profile/catalog mismatch: %s %d %d", catalog.Profile, len(catalog.Tools), len(response.Result.Tools))
			}
			names := toolNamesOf(catalog.Tools)
			for _, tool := range response.Result.Tools {
				if !names[tool.Name] {
					t.Fatalf("MCP-only tool %s", tool.Name)
				}
			}
			if !names[OpToolsSearch] || !names[OpAppPreflight] {
				t.Fatal("discovery/preflight tools missing")
			}
			if profile == "full" {
				fullCount = len(catalog.Tools)
			} else if len(catalog.Tools) >= fullCount {
				t.Fatalf("profile did not reduce tools: %d >= %d", len(catalog.Tools), fullCount)
			}
			if profile == "operations" && (names["open_tab"] || names[OpTerminalRun] || names[OpUIClick] || !names[OpExecStart]) {
				t.Fatal("operations discovery should prefer independent jobs and semantic sessions")
			}
		})
	}
	status, _ := httpDo(t, base, http.MethodGet, "/v1/tools?profile=invalid", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid profile status %d", status)
	}
}

func TestToolSearchFindsHiddenToolsWithoutChangingAuthority(t *testing.T) {
	ensureAllRegistrations(t)
	s := New(Options{Handler: &fakeHandler{}, Gate: &stubGate{denied: map[Capability]string{CapUIWrite: "disabled"}}})
	ctx := context.WithValue(context.Background(), toolProfileContextKey{}, "operations")
	content, _, err := s.mcpCallToolAudited(ctx, OpToolsSearch, map[string]any{"query": "ui.click", "includeSchema": true})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &result); err != nil {
		t.Fatal(err)
	}
	if result["total"] != float64(1) {
		t.Fatalf("hidden tool not discovered: %+v", result)
	}
	tools := result["tools"].([]any)
	if tools[0].(map[string]any)["inputSchema"] == nil {
		t.Fatal("schema missing")
	}
	if result["platform"] == nil || result["workflows"] == nil {
		t.Fatal("execution guidance missing")
	}
	if _, _, err := s.mcpCallToolAudited(ctx, OpUIClick, map[string]any{"selector": "button"}); err == nil {
		t.Fatal("discovering a hidden tool must not grant its capability")
	}
	_, err = s.searchTools(map[string]any{"limit": 0})
	if err == nil {
		t.Fatal("invalid pagination accepted")
	}
}

func TestToolProfilePersistsWithinMCPSession(t *testing.T) {
	ensureAllRegistrations(t)
	base, _ := startHTTP(t, &stubGate{}, &stubExecutor{})
	request := func(body string, session, profile string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set(mcpSessionHeader, session)
		}
		if profile != "" {
			req.Header.Set(toolProfileHeader, profile)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, string(raw)
	}
	response, body := request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "", "operations")
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode, body)
	}
	session := response.Header.Get(mcpSessionHeader)
	if session == "" {
		t.Fatal("session missing")
	}
	request(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, session, "")
	response, body = request(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, session, "")
	if response.StatusCode != http.StatusOK || strings.Contains(body, `"name":"ui.click"`) || !strings.Contains(body, `"name":"exec.start"`) {
		t.Fatalf("profile was not retained: status=%d body=%s", response.StatusCode, body)
	}
	response, _ = request(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, session, "debug")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("session profile changed silently: %d", response.StatusCode)
	}
}

func TestToolDiscoveryAliasAndExecutionGuidance(t *testing.T) {
	ensureAllRegistrations(t)
	s := New(Options{Gate: &stubGate{}})
	for _, tool := range mcpTools(s) {
		if canonical := canonicalToolNames[tool.Name]; canonical != "" && tool.Meta["canonicalName"] != canonical {
			t.Fatalf("missing canonical hint for %s", tool.Name)
		}
		if tool.Name == OpTerminalRun && !strings.Contains(tool.Description, OpExecStart) {
			t.Fatal("interactive tool lacks execution guidance")
		}
	}
}

func TestExecStartDiscoveryExplainsConditionalCapabilities(t *testing.T) {
	ensureAllRegistrations(t)
	s := New(Options{Gate: &stubGate{}})
	var listed *mcpTool
	for _, tool := range mcpTools(s) {
		if tool.Name == OpExecStart {
			copy := tool
			listed = &copy
			break
		}
	}
	if listed == nil {
		t.Fatal("exec.start missing from MCP tool list")
	}
	if !strings.Contains(listed.Description, "local=true") || !strings.Contains(listed.Description, string(CapLocalExec)) || !strings.Contains(listed.Description, string(CapTerminalInput)) {
		t.Fatalf("tool description omits conditional authorization: %q", listed.Description)
	}
	if listed.Meta["capabilityMode"] != "conditional" {
		t.Fatalf("MCP metadata does not mark conditional capabilities: %#v", listed.Meta)
	}
	assertExecStartCapabilityConditions(t, listed.Meta["conditionalCapabilities"])

	// Keep the old union field for existing catalog clients, while making its
	// conditional meaning explicit in additive fields.
	entry := toolCatalogEntry(*listed)
	legacyCaps, ok := entry["capability"].([]string)
	if !ok || !hasString(legacyCaps, string(CapTerminalInput)) {
		t.Fatalf("legacy capability union changed unexpectedly: %#v", entry["capability"])
	}
	if entry["capabilityMode"] != "conditional" {
		t.Fatalf("catalog entry lacks capabilityMode: %#v", entry)
	}
	assertExecStartCapabilityConditions(t, entry["conditionalCapabilities"])

	search, err := s.searchTools(map[string]any{"query": OpExecStart, "includeSchema": true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	b, _ := json.Marshal(search)
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	items := decoded["tools"].([]any)
	var searchEntry map[string]any
	for _, item := range items {
		candidate := item.(map[string]any)
		if candidate["name"] == OpExecStart {
			searchEntry = candidate
			break
		}
	}
	if searchEntry == nil {
		t.Fatalf("exec.start missing from %d search results", len(items))
	}
	searchMeta := searchEntry["_meta"].(map[string]any)
	if searchEntry["capabilityMode"] != "conditional" || searchMeta["capabilityMode"] != "conditional" {
		t.Fatalf("tools.search omitted conditional capability metadata: %#v", searchEntry)
	}
	assertExecStartCapabilityConditions(t, searchEntry["conditionalCapabilities"])
}

func assertExecStartCapabilityConditions(t *testing.T, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var conditions []map[string]any
	if err := json.Unmarshal(b, &conditions); err != nil {
		t.Fatal(err)
	}
	if len(conditions) != 2 || conditions[0]["when"] != "local=true" || !equalStringListAny(conditions[0]["requires"], string(CapLocalExec)) || conditions[1]["when"] != "local omitted or false (sessionId targets SSH)" || !equalStringListAny(conditions[1]["requires"], string(CapTerminalInput)) {
		t.Fatalf("incorrect exec.start capability conditions: %#v", conditions)
	}
}

func equalStringListAny(value any, want string) bool {
	items, ok := value.([]any)
	return ok && len(items) == 1 && items[0] == want
}
