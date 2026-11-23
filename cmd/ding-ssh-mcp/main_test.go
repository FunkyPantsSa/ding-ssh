package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient(t *testing.T, url string) (*clientMCP, *bytes.Buffer) {
	t.Helper()
	c := newClientMCP(config{url: url, token: "test"}, options{quiet: true}, &logf2{quiet: true})
	out := &bytes.Buffer{}
	c.out = bufio.NewWriter(out)
	t.Cleanup(c.streamStop)
	return c, out
}

func TestStdioLongRequestDoesNotBlockCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		if msg.Method == "notifications/cancelled" {
			once.Do(func() { close(cancelled) })
			w.WriteHeader(202)
			return
		}
		if msg.ID == 1 {
			close(started)
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Error("cancellation blocked by long request")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{}})
	}))
	defer server.Close()
	c, out := testClient(t, server.URL)
	reader, writer := io.Pipe()
	done := make(chan int, 1)
	go func() { done <- c.serveStdin(reader) }()
	_, _ = io.WriteString(writer, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\"}\n")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("long request not started")
	}
	_, _ = io.WriteString(writer, "{\"jsonrpc\":\"2.0\",\"method\":\"notifications/cancelled\",\"params\":{\"requestId\":1}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"ping\"}\n")
	_ = writer.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdio did not finish")
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected output lines=%d", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("corrupt stdout: %s", line)
		}
	}
}

func TestTransportFailureAfterWriteIsNeverRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	c, _ := testClient(t, server.URL)
	_, err := c.doRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
	if err == nil || errors.Is(err, errNotForwarded) {
		t.Fatalf("ambiguous execution error marked retryable: %v", err)
	}
}

func TestConcurrentResponsesKeepStdoutValid(t *testing.T) {
	c, out := testClient(t, "http://127.0.0.1")
	var pending sync.WaitGroup
	for i := 0; i < 100; i++ {
		pending.Add(1)
		go func(id int) {
			defer pending.Done()
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": strings.Repeat("x", 1024)})
			c.writeLine(body)
		}(i)
	}
	pending.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 100 {
		t.Fatal(len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("interleaved JSON")
		}
	}
}

func TestRequestContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	c, _ := testClient(t, server.URL)
	done := make(chan error, 1)
	go func() { _, err := c.doRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); done <- err }()
	<-started
	c.streamStop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected cancellation %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
}
