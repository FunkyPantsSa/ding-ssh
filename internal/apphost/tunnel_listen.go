package apphost

import (
	"net"
	"sort"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

func (a *App) StartTunnelWithListenHost(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	logx.Debugf("[apphost] 启动隧道（指定监听地址）: name=%s mode=%s listenHost=%s listenPort=%d phraseCount=%d",
		name, mode, listenHost, localPort, len(phrases))
	node.RuntimePassphrases = phrases
	return a.startTunnel(node, name, mode, localPort, remoteHost, remotePort, listenHost)
}

func (a *App) UpdateTunnelWithListenHost(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	logx.Debugf("[apphost] 修改隧道（指定监听地址）: id=%s name=%s mode=%s listenHost=%s listenPort=%d phraseCount=%d",
		id, name, mode, listenHost, localPort, len(phrases))
	node.RuntimePassphrases = phrases
	return a.updateTunnel(id, node, name, mode, localPort, remoteHost, remotePort, listenHost)
}

func (a *App) ListLocalIPv4() ([]string, error) {
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 读取本机 IPv4 地址被拒（保险库未解锁）: err=%v", err)
		return nil, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		logx.Errorf("[apphost] 读取本机网卡列表失败: err=%v", err)
		return nil, err
	}
	seen := make(map[string]bool)
	addresses := make([]string, 0)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			logx.Debugf("[apphost] 读取网卡地址失败，已跳过: iface=%s err=%v", iface.Name, err)
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && !ip.IsUnspecified() && !seen[ip.String()] {
				addresses = append(addresses, ip.String())
				seen[ip.String()] = true
			}
		}
	}
	sort.Strings(addresses)
	logx.Infof("[apphost] 读取本机 IPv4 地址完成: count=%d addresses=%v", len(addresses), addresses)
	return addresses, nil
}
