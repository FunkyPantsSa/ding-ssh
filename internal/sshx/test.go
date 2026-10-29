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

// TestServerLogin 使用节点配置实际完成一次 SSH 登录（TCP 拨号 + 版本协商 + 用户鉴权），
// 用于验证「当前表单里的用户名 / 密码 / 私钥」能否登录成功。
// 成功时 Reachable=true、LatencyMS 为登录耗时；失败时 Reachable=false、Error 为失败原因。
func TestServerLogin(node models.ServerNode) models.ServerTestResult {
	res := models.ServerTestResult{NodeID: node.ID}
	config, err := buildClientConfig(node, noopProgress)
	if err != nil {
		// 构造鉴权配置失败（例如私钥无法解析、未选择私钥文件）
		res.Error = err.Error()
		return res
	}
	start := time.Now()
	client, err := dialSSH(node.Host, node.Port, config, noopProgress)
	if err != nil {
		res.LatencyMS = -1
		res.Error = connectError(err).Error()
		return res
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	res.Reachable = true
	_ = client.Close()
	return res
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
