//go:build !windows

package sshx

import (
	"net"

	"ding-ssh/internal/logx"
)

func defaultAgentSocket() string { return "" }
func dialAgent(socket string) (net.Conn, error) {
	logx.Debugf("[sshx] SSH Agent 拨号: network=unix socket=%q", socket)
	return net.DialTimeout("unix", socket, connectTimeout)
}
