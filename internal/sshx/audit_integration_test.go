package sshx

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

func TestDynamicTunnelRealSOCKS5RoundTrip(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSH(t, "audit", true)
	trustLocal(t, server)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "dynamic-é中文") }))
	defer target.Close()
	tunnel := newTunnel("audit", "audit", TunnelDynamic, localNode(t, server, "audit"), 0, "", 0, nil)
	if err := tunnel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.Stop)
	dialer, err := proxy.SOCKS5("tcp", tunnel.listener.Addr().String(), nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dialer.Dial("tcp", target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(conn)
	if err != nil || !bytes.HasSuffix(got, []byte("dynamic-é中文")) {
		t.Fatalf("SOCKS payload %q %v", got, err)
	}
	// An unsupported SOCKS command must be rejected instead of forwarded.
	bad, err := net.DialTimeout("tcp", tunnel.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	_ = bad.SetDeadline(time.Now().Add(time.Second))
	_, _ = bad.Write([]byte{5, 1, 0})
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(bad, greeting); err != nil {
		t.Fatal(err)
	}
	_, _ = bad.Write([]byte{5, 2, 0, 1, 127, 0, 0, 1, 0, 80})
	reply := make([]byte, 10)
	if _, err := io.ReadFull(bad, reply); err != nil || reply[1] != 7 {
		t.Fatalf("unsupported command response %v %v", reply, err)
	}
}

func TestRemoteTunnelRealForwardedChannelRoundTrip(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	serverConn := make(chan *ssh.ServerConn, 1)
	server := startLocalSSH(t, "audit", false, func(conn *ssh.ServerConn, requests <-chan *ssh.Request) {
		serverConn <- conn
		for req := range requests {
			_ = req.Reply(true, nil)
		}
	})
	trustLocal(t, server)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "remote-roundtrip") }))
	defer target.Close()
	port := target.Listener.Addr().(*net.TCPAddr).Port
	tunnel := newTunnel("remote-audit", "audit", TunnelRemote, localNode(t, server, "audit"), port, "127.0.0.1", 22445, nil)
	if err := tunnel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.Stop)
	conn := <-serverConn
	timeout := time.AfterFunc(3*time.Second, func() { _ = conn.Close() })
	defer timeout.Stop()
	payload := struct {
		Host       string
		Port       uint32
		Origin     string
		OriginPort uint32
	}{"127.0.0.1", 22445, "127.0.0.1", 44556}
	ch, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	go ssh.DiscardRequests(reqs)
	if _, err := io.WriteString(ch, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(ch)
	if err != nil || !bytes.HasSuffix(got, []byte("remote-roundtrip")) {
		t.Fatalf("remote payload %q %v", got, err)
	}
}

func TestSFTPManagerCRUDAndBinaryTransfers(t *testing.T) {
	root, local := t.TempDir(), t.TempDir()
	client := startInProcessSFTP(t, root)
	state := &sessionSFTP{client: client}
	state.once.Do(func() {})
	m := NewManager(func(string, interface{}) {})
	m.sessions["audit"] = &Session{ID: "audit", sftpState: state}
	remoteDir := filepath.Join(root, "目录-é")
	if err := m.SftpMkdir("audit", remoteDir); err != nil {
		t.Fatal(err)
	}
	payload := append(bytes.Repeat([]byte{0, 1, 127, 128, 255}, 27000), []byte("中文é")...)
	source, target, download := filepath.Join(local, "源.bin"), filepath.Join(remoteDir, "文件.bin"), filepath.Join(local, "download.bin")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.SftpUpload("audit", source, target); err != nil {
		t.Fatal(err)
	}
	if err := m.SftpDownload("audit", target, download); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(download)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("binary roundtrip", err)
	}
	entries, err := m.SftpList("audit", remoteDir)
	if err != nil || len(entries) != 1 || entries[0].Size != int64(len(payload)) {
		t.Fatal(entries, err)
	}
	renamed := filepath.Join(remoteDir, "renamed.bin")
	if err := m.SftpRename("audit", target, renamed); err != nil {
		t.Fatal(err)
	}
	if err := m.SftpRemove("audit", remoteDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(remoteDir); !os.IsNotExist(err) {
		t.Fatal("recursive remove", err)
	}
	if err := m.SftpDownload("missing", renamed, download); err == nil {
		t.Fatal("unknown session accepted")
	}
	if err := m.SftpUpload("audit", filepath.Join(local, "missing-"+strconv.Itoa(len(payload))), renamed); err == nil {
		t.Fatal("missing source accepted")
	}
}
