package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/models"
	"golang.org/x/crypto/ssh"
)

type localSSH struct {
	address  string
	key      ssh.PublicKey
	auth     atomic.Int32
	closed   atomic.Int32
	listener net.Listener
}

func startLocalSSH(t *testing.T, password string, forward bool, handleRequests ...func(*ssh.ServerConn, <-chan *ssh.Request)) *localSSH {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := &localSSH{key: signer.PublicKey()}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, secret []byte) (*ssh.Permissions, error) {
		server.auth.Add(1)
		if string(secret) != password {
			return nil, io.EOF
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.listener = listener
	server.address = listener.Addr().String()
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				defer sshConn.Close()
				defer server.closed.Add(1)
				if len(handleRequests) == 0 {
					go ssh.DiscardRequests(reqs)
				} else {
					go handleRequests[0](sshConn, reqs)
				}
				for request := range chans {
					if request.ChannelType() != "direct-tcpip" || !forward {
						request.Reject(ssh.Prohibited, "forward denied")
						continue
					}
					var target struct {
						Host       string
						Port       uint32
						Origin     string
						OriginPort uint32
					}
					if err := ssh.Unmarshal(request.ExtraData(), &target); err != nil {
						request.Reject(ssh.ConnectionFailed, "bad payload")
						continue
					}
					remote, err := net.DialTimeout("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))), time.Second)
					if err != nil {
						request.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					channel, requests, err := request.Accept()
					if err != nil {
						remote.Close()
						continue
					}
					go ssh.DiscardRequests(requests)
					go func() {
						defer channel.Close()
						defer remote.Close()
						go func() { io.Copy(channel, remote); channel.Close() }()
						io.Copy(remote, channel)
					}()
				}
			}()
		}
	}()
	return server
}
func localNode(t *testing.T, server *localSSH, password string) models.ServerNode {
	t.Helper()
	host, port, _ := net.SplitHostPort(server.address)
	number, _ := strconv.Atoi(port)
	return models.ServerNode{Host: host, Port: number, User: "test", AuthType: "password", Password: password}
}
func trustLocal(t *testing.T, server *localSSH) {
	t.Helper()
	if err := SaveHostTrust(server.address, string(ssh.MarshalAuthorizedKey(server.key))); err != nil {
		t.Fatal(err)
	}
}
func TestRealJumpChainAndCleanup(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	first := startLocalSSH(t, "first-secret", true)
	second := startLocalSSH(t, "second-secret", true)
	target := startLocalSSH(t, "target-secret", false)
	for _, server := range []*localSSH{first, second, target} {
		trustLocal(t, server)
	}
	node := localNode(t, target, "target-secret")
	node.JumpServerIDs = []string{"first", "second"}
	node.ResolvedJumps = []models.ServerNode{localNode(t, first, "first-secret"), localNode(t, second, "second-secret")}
	client, err := dialNode(node, noopProgress)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if first.closed.Load() > 0 && second.closed.Load() > 0 && target.closed.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("upstream connections were not closed")
}
func TestUnknownHostPreventsPasswordAuthentication(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, t.TempDir())
	server := startLocalSSH(t, "secret", false)
	if client, err := dialNode(localNode(t, server, "secret"), noopProgress); err == nil {
		client.Close()
		t.Fatal("unknown host connected")
	}
	if server.auth.Load() != 0 {
		t.Fatal("password sent before host verification")
	}
}
