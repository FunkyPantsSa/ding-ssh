//go:build windows

package sshx

import (
	"golang.org/x/sys/windows"
	"net"
	"os"
	"time"
)

func defaultAgentSocket() string { return `\\.\pipe\openssh-ssh-agent` }
func dialAgent(socket string) (net.Conn, error) {
	name, err := windows.UTF16PtrFromString(socket)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, err
	}
	return &agentPipe{File: os.NewFile(uintptr(h), socket)}, nil
}

type agentPipe struct{ *os.File }
type pipeAddress string

func (a pipeAddress) Network() string                   { return "pipe" }
func (a pipeAddress) String() string                    { return string(a) }
func (c *agentPipe) LocalAddr() net.Addr                { return pipeAddress("local") }
func (c *agentPipe) RemoteAddr() net.Addr               { return pipeAddress(c.Name()) }
func (c *agentPipe) SetDeadline(t time.Time) error      { return c.File.SetDeadline(t) }
func (c *agentPipe) SetReadDeadline(t time.Time) error  { return c.File.SetReadDeadline(t) }
func (c *agentPipe) SetWriteDeadline(t time.Time) error { return c.File.SetWriteDeadline(t) }
