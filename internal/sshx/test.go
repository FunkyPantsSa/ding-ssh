package sshx

import (
	"errors"
	"net"
	"strconv"
	"time"

	"ding-ssh/internal/models"
)

// TestTimeout 在线状态测试的 TCP 拨号超时时间。
const TestTimeout = 5 * time.Second

// TestConnectivity 测试服务器在线状态：
// 通过 TCP 连接 host:port 测量网络延迟（毫秒），并返回 SSH 端口是否畅通。
// 连接失败时 LatencyMS 为 -1，Reachable 为 false，Error 为失败原因。
func TestConnectivity(host string, port int) models.ServerTestResult {
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, TestTimeout)
	if err != nil {
		return models.ServerTestResult{
			LatencyMS: -1,
			Reachable: false,
			Error:     testDialError(err),
		}
	}
	_ = conn.Close()
	return models.ServerTestResult{
		LatencyMS: time.Since(start).Milliseconds(),
		Reachable: true,
	}
}

// testDialError 将拨号错误转换为简洁的失败原因文案。
func testDialError(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "连接超时"
	}
	if msg := err.Error(); msg != "" {
		return msg
	}
	return "连接失败"
}
