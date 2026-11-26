package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func probeResult(t *testing.T, endpoint string, extra map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{"kind": "http", "url": endpoint, "timeoutMs": 200}
	for k, v := range extra {
		args[k] = v
	}
	content, err := (&Server{}).handleProbe(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal([]byte(uiArgString(uiMap(content[0]), "text")), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProbeIncompleteBodyCannotPassEvenWithMatchingNonce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = fmt.Fprint(w, "expected-nonce")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	result := probeResult(t, server.URL+"/?token=do-not-return-query-secret", map[string]any{"expectedNonce": "expected-nonce"})
	if result["available"] != true || result["state"] != "degraded" || result["expectationMatched"] != false || result["responseComplete"] != false {
		t.Fatalf("incomplete response=%+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "do-not-return-query-secret") || strings.Contains(string(encoded), "token=") {
		t.Fatalf("query leaked: %s", encoded)
	}
}

func TestProbeTruncatedBodyIsReachableButInconclusive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "expected-nonce"+strings.Repeat("x", 70000))
	}))
	defer server.Close()
	result := probeResult(t, server.URL, map[string]any{"expectedNonce": "expected-nonce"})
	if result["available"] != true || result["state"] != "degraded" || result["expectationMatched"] != false || result["truncated"] != true || result["errorCode"] != "PROBE_BODY_LIMIT" {
		t.Fatalf("truncated response=%+v", result)
	}
}

func TestProbeHealthyRequiresCompleteResponseAndAcceptableStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
		}
		_, _ = fmt.Fprint(w, "expected-nonce")
	}))
	defer server.Close()
	if result := probeResult(t, server.URL, map[string]any{"expectedNonce": "expected-nonce"}); result["state"] != "healthy" || result["responseComplete"] != true {
		t.Fatalf("complete response=%+v", result)
	}
	if result := probeResult(t, server.URL+"/fail", nil); result["state"] != "degraded" || result["expectationMatched"] != false {
		t.Fatalf("500 response=%+v", result)
	}
	if result := probeResult(t, server.URL+"/fail", map[string]any{"expectedStatus": 500}); result["state"] != "healthy" {
		t.Fatalf("explicit status response=%+v", result)
	}
}

func TestProbeTransportErrorDoesNotReturnURLQuery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	result := probeResult(t, server.URL+"/?password=transport-query-secret", nil)
	encoded, _ := json.Marshal(result)
	if result["errorCode"] != "PROBE_FAILED" || strings.Contains(string(encoded), "transport-query-secret") || strings.Contains(string(encoded), "password=") {
		t.Fatalf("transport response=%s", encoded)
	}
}
