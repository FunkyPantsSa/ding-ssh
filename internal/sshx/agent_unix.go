//go:build !windows

package sshx

import "net"

func defaultAgentSocket() string { return "" }
func dialAgent(socket string) (net.Conn, error) {
	return net.DialTimeout("unix", socket, connectTimeout)
}
