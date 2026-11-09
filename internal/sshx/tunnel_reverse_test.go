package sshx

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"ding-ssh/internal/appdir"
)

func TestReverseDynamicTunnel(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())

	type forwardAddress struct {
		Host string
		Port uint32
	}
	const proxyPort = 1080
	requested := make(chan forwardAddress, 2)
	serverConn := make(chan *ssh.ServerConn, 1)
	server := startLocalSSH(t, "secret", false, func(conn *ssh.ServerConn, requests <-chan *ssh.Request) {
		serverConn <- conn
		for req := range requests {
			var addr forwardAddress
			if req.Type == "tcpip-forward" {
				if err := ssh.Unmarshal(req.Payload, &addr); err != nil {
					req.Reply(false, nil)
					continue
				}
				requested <- addr
			}
			req.Reply(true, nil)
		}
	})
	trustLocal(t, server)
	manager := NewManager(func(string, any) {})
	info, err := manager.StartTunnel(localNode(t, server, "secret"), "", string(TunnelReverseDynamic), 0, "0.0.0.0", proxyPort)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.StopTunnel(info.ID) })
	if addr := <-requested; addr.Host != "127.0.0.1" || addr.Port != proxyPort {
		t.Fatalf("unexpected remote bind: %+v", addr)
	}
	conn := <-serverConn
	timeout := time.AfterFunc(5*time.Second, func() { conn.Close() })
	defer timeout.Stop()
	open := func(origin string) ssh.Channel {
		t.Helper()
		payload := struct {
			Host       string
			Port       uint32
			Origin     string
			OriginPort uint32
		}{"127.0.0.1", proxyPort, origin, 55555}
		ch, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
		if err != nil {
			t.Fatal(err)
		}
		go ssh.DiscardRequests(reqs)
		t.Cleanup(func() { ch.Close() })
		return ch
	}

	external := open("192.0.2.1")
	if _, err := external.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("external peer should be closed, got %v", err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "local exit")
	}))
	defer target.Close()
	port := target.Listener.Addr().(*net.TCPAddr).Port
	client := open("127.0.0.1")
	exchange := func(request, expected []byte) {
		t.Helper()
		if _, err := client.Write(request); err != nil {
			t.Fatal(err)
		}
		response := make([]byte, len(expected))
		if _, err := io.ReadFull(client, response); err != nil || !bytes.Equal(response, expected) {
			t.Fatalf("SOCKS5 response = %v, err = %v, want %v", response, err, expected)
		}
	}
	exchange([]byte{5, 1, 0}, []byte{5, 0})
	exchange([]byte{5, 1, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil || !bytes.HasSuffix(response, []byte("local exit")) {
		t.Fatalf("response = %q, err = %v", response, err)
	}
}
