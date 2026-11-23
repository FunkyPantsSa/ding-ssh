package apphost

import (
	"ding-ssh/internal/models"
)

// TunnelService 负责本地端口转发、远程端口转发及动态 SOCKS5 隧道的创建与生命周期管理。
type TunnelService struct {
	app *App
}

func NewTunnelService(app *App) *TunnelService {
	return &TunnelService{app: app}
}

func (s *TunnelService) StartTunnel(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	return s.app.StartTunnel(node, name, mode, localPort, remoteHost, remotePort)
}

func (s *TunnelService) StartTunnelAuthenticated(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, phrases map[string]string) (models.TunnelInfo, error) {
	return s.app.StartTunnelAuthenticated(node, name, mode, localPort, remoteHost, remotePort, phrases)
}

func (s *TunnelService) StartTunnelWithListenHost(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	return s.app.StartTunnelWithListenHost(node, name, mode, localPort, remoteHost, remotePort, listenHost, phrases)
}

func (s *TunnelService) UpdateTunnel(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	return s.app.UpdateTunnel(id, node, name, mode, localPort, remoteHost, remotePort)
}

func (s *TunnelService) UpdateTunnelWithListenHost(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string, phrases map[string]string) (models.TunnelInfo, error) {
	return s.app.UpdateTunnelWithListenHost(id, node, name, mode, localPort, remoteHost, remotePort, listenHost, phrases)
}

func (s *TunnelService) StopTunnel(id string) error {
	return s.app.StopTunnel(id)
}

func (s *TunnelService) RestartTunnel(id string) error {
	return s.app.RestartTunnel(id)
}

func (s *TunnelService) RemoveTunnel(id string) error {
	return s.app.RemoveTunnel(id)
}

func (s *TunnelService) ListTunnels() []models.TunnelInfo {
	return s.app.ListTunnels()
}

func (s *TunnelService) ListLocalIPv4() ([]string, error) {
	return s.app.ListLocalIPv4()
}
