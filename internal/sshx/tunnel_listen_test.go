package sshx

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
)

func TestLocalTunnelListenScope(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSH(t, "secret", true)
	trustLocal(t, server)
	node := localNode(t, server, "secret")
	manager := NewManager(func(string, any) {})

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "forwarded")
	}))
	defer target.Close()

	reserved, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	_ = reserved.Close()
	targetPort := target.Listener.Addr().(*net.TCPAddr).Port

	info, err := manager.StartTunnel(node, "test", string(TunnelLocal), port, "127.0.0.1", targetPort)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.RemoveTunnel(info.ID) })
	checkListener := func(want string) {
		t.Helper()
		tunnel, err := manager.getTunnel(info.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := tunnel.listener.Addr().(*net.TCPAddr).IP.String(); got != want {
			t.Fatalf("listener IP = %s, want %s", got, want)
		}
		if got := tunnel.Info().ListenHost; got != want {
			t.Fatalf("reported bind = %s, want %s", got, want)
		}
	}
	checkListener("127.0.0.1")

	if _, err := manager.UpdateTunnel(info.ID, node, "test", string(TunnelLocal), port, "127.0.0.1", targetPort, "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	checkListener("0.0.0.0")
	if err := manager.RestartTunnel(info.ID); err != nil {
		t.Fatal(err)
	}
	checkListener("0.0.0.0")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if body, err := io.ReadAll(response.Body); err != nil || string(body) != "forwarded" {
		t.Fatalf("unexpected forwarded response %q: %v", body, err)
	}
}
