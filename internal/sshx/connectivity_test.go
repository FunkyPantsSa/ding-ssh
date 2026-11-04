package sshx

import (
	"net"
	"testing"
)

func TestTestConnectivityUnreachable(t *testing.T) {
	// 占用再释放一个端口，绝大多数情况下连接会被拒绝。
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	res := TestConnectivity("127.0.0.1", port)
	if res.Reachable {
		t.Skip("端口被系统复用，跳过")
	}
	if res.LatencyMS != -1 {
		t.Errorf("LatencyMS = %d, want -1", res.LatencyMS)
	}
	if res.Error == "" {
		t.Error("Error 不应为空")
	}
}

func TestTestConnectivityReachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	res := TestConnectivity("127.0.0.1", port)
	if !res.Reachable {
		t.Fatalf("期望端口畅通，实际: %+v", res)
	}
	if res.LatencyMS < 0 {
		t.Errorf("LatencyMS = %d, 期望 >= 0", res.LatencyMS)
	}
}
