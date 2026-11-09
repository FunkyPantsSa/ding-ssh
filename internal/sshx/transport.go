package sshx

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"ding-ssh/internal/models"
	"golang.org/x/crypto/ssh"
)

// chainConn owns every upstream client. Closing the target SSH connection
// releases the entire chain, including on terminal setup failures.
type chainConn struct {
	net.Conn
	upstream []*ssh.Client
	once     sync.Once
}

func (c *chainConn) Close() error {
	var err error
	c.once.Do(func() {
		// Close the underlying TCP transport before closing tunneled channels.
		// A channel close can itself block on an unresponsive upstream.
		for _, upstream := range c.upstream {
			_ = upstream.Close()
		}
		err = c.Conn.Close()
	})
	return err
}

func dialNode(node models.ServerNode, report progressFn) (*ssh.Client, error) {
	return dialNodeContext(context.Background(), node, report)
}
func dialNodeContext(ctx context.Context, node models.ServerNode, report progressFn) (*ssh.Client, error) {
	if len(node.JumpServerIDs) > 0 && len(node.ResolvedJumps) == 0 {
		return nil, fmt.Errorf("跳板机配置尚未解析，拒绝直连")
	}
	if len(node.ResolvedJumps) > 5 {
		return nil, fmt.Errorf("最多支持 5 级跳板机")
	}
	chain := append(append([]models.ServerNode{}, node.ResolvedJumps...), node)
	clients := []*ssh.Client{}
	success := false
	defer func() {
		if !success {
			for i := len(clients) - 1; i >= 0; i-- {
				clients[i].Close()
			}
		}
	}()
	for i, hop := range chain {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if hop.Port == 0 {
			hop.Port = 22
		}
		label := hop.Name
		if label == "" {
			label = hop.Host
		}
		progress := func(step, status, log, message string) { report(step, status, label+"："+log, message) }
		cfg, err := buildClientConfig(hop, progress)
		if err != nil {
			return nil, fmt.Errorf("%s：认证配置失败: %w", label, err)
		}
		var client *ssh.Client
		if i == 0 {
			client, err = dialSSHContext(ctx, hop.Host, hop.Port, cfg, progress)
		} else {
			address := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
			progress(models.ConnectStepTCP, "running", "建立跳板 TCP 通道 "+address, "")
			// Closing the upstream client interrupts both channel open and handshake.
			upstream := clients[i-1]
			stopCancel := context.AfterFunc(ctx, func() { upstream.Close() })
			timer := time.AfterFunc(connectTimeout, func() { upstream.Close() })
			var conn net.Conn
			conn, err = clients[i-1].Dial("tcp", address)
			if err == nil {
				wrapped := &chainConn{Conn: conn, upstream: append([]*ssh.Client{}, clients...)}
				var cc ssh.Conn
				var chans <-chan ssh.NewChannel
				var reqs <-chan *ssh.Request
				cc, chans, reqs, err = ssh.NewClientConn(wrapped, address, cfg)
				if err == nil {
					client = ssh.NewClient(cc, chans, reqs)
				} else {
					wrapped.Close()
				}
			}
			timer.Stop()
			stopCancel()
		}
		if err != nil {
			return nil, fmt.Errorf("%s：连接/身份认证失败: %w", label, err)
		}
		clients = append(clients, client)
	}
	success = true
	return clients[len(clients)-1], nil
}
