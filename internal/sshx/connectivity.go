package sshx

import (
	"ding-ssh/internal/i18n"
	"errors"
	"net"
	"strconv"
	"time"

	"ding-ssh/internal/logx"
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
		reason := testDialError(err)
		logx.Warnf("[sshx] 连通性测试失败: addr=%s elapsed=%s reason=%s err=%v", addr, time.Since(start), reason, err)
		return models.ServerTestResult{
			LatencyMS: -1,
			Reachable: false,
			Error:     reason,
		}
	}
	_ = conn.Close()
	latency := time.Since(start).Milliseconds()
	logx.Infof("[sshx] 连通性测试通过: addr=%s latency=%dms elapsed=%s", addr, latency, time.Since(start))
	return models.ServerTestResult{
		LatencyMS: latency,
		Reachable: true,
	}
}

// TestServerLogin 使用节点配置实际完成一次 SSH 登录（TCP 拨号 + 版本协商 + 用户鉴权），
// 用于验证「当前表单里的用户名 / 密码 / 私钥」能否登录成功。
// 成功时 Reachable=true、LatencyMS 为登录耗时；失败时 Reachable=false、Error 为失败原因。
func TestServerLogin(node models.ServerNode) models.ServerTestResult {
	res := models.ServerTestResult{NodeID: node.ID}
	logx.Debugf("[sshx] 开始 SSH 登录测试: server=%s host=%s:%d user=%s auth=%s jumps=%d",
		node.Name, node.Host, node.Port, node.User, node.AuthType, len(node.JumpServerIDs))
	start := time.Now()
	client, err := dialNode(node, noopProgress)
	if err != nil {
		reason := connectError(err).Error()
		logx.Warnf("[sshx] SSH 登录测试失败: server=%s host=%s:%d user=%s elapsed=%s reason=%s",
			node.Name, node.Host, node.Port, node.User, time.Since(start), reason)
		res.LatencyMS = -1
		res.Error = reason
		return res
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	res.Reachable = true
	logx.Infof("[sshx] SSH 登录测试通过: server=%s host=%s:%d user=%s latency=%dms",
		node.Name, node.Host, node.Port, node.User, res.LatencyMS)
	_ = client.Close()
	return res
}

// testDialError 将拨号错误转换为简洁的失败原因文案。
func testDialError(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return i18n.T("连接超时")
	}
	if msg := err.Error(); msg != "" {
		return msg
	}
	return i18n.T("连接失败")
}
