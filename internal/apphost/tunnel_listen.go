package apphost

import (
	"net"
	"sort"

	"ding-ssh/internal/models"
)

func (a *App) StartTunnelWithListenHost(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	node.RuntimePassphrases = phrases
	return a.startTunnel(node, name, mode, localPort, remoteHost, remotePort, listenHost)
}

func (a *App) UpdateTunnelWithListenHost(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	node.RuntimePassphrases = phrases
	return a.updateTunnel(id, node, name, mode, localPort, remoteHost, remotePort, listenHost)
}

func (a *App) ListLocalIPv4() ([]string, error) {
	if err := a.ensureUnlocked(); err != nil {
		return nil, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
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
	return addresses, nil
}
