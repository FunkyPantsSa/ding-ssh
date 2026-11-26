package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolProfileFlagsAndForwarding(t *testing.T) {
	t.Setenv(envToolProfile, "operations")
	opts, err := parseFlags(nil, &bytes.Buffer{})
	if err != nil || opts.toolProfile != "operations" {
		t.Fatalf("env profile: %q %v", opts.toolProfile, err)
	}
	opts, err = parseFlags([]string{"-tool-profile", "debug"}, &bytes.Buffer{})
	if err != nil || opts.toolProfile != "debug" {
		t.Fatalf("flag profile: %q %v", opts.toolProfile, err)
	}
	if _, err = parseFlags([]string{"-tool-profile", "invalid"}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid profile accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ding-SSH-Tool-Profile") != "debug" {
			t.Error("profile header not forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	c, _ := testClient(t, server.URL)
	c.opts.toolProfile = opts.toolProfile
	if _, err := c.doRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.post(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatal(err)
	}
}
