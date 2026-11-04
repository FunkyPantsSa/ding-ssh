package apphost

import (
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"golang.org/x/crypto/ssh"
)

// Password phrases are transient request inputs, excluded from ServerNode JSON,
// SQLite, configuration bundles and source configuration snapshots.
func (a *App) ConnectAuthenticated(id string, node models.ServerNode, cols, rows int, phrases map[string]string) (models.ConnectResult, error) {
	node.RuntimePassphrases = phrases
	return a.Connect(id, node, cols, rows)
}
func (a *App) TestServerLoginAuthenticated(node models.ServerNode, phrases map[string]string) models.ServerTestResult {
	node.RuntimePassphrases = phrases
	return a.TestServerLogin(node)
}
func (a *App) StartTunnelAuthenticated(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, phrases map[string]string) (models.TunnelInfo, error) {
	node.RuntimePassphrases = phrases
	return a.StartTunnel(node, name, mode, localPort, remoteHost, remotePort)
}

type AgentIdentity struct {
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
}

func (a *App) GetAgentIdentities(node models.ServerNode) ([]AgentIdentity, error) {
	signers, err := sshx.AgentSigners(node)
	if err != nil {
		return nil, err
	}
	result := []AgentIdentity{}
	for _, signer := range signers {
		result = append(result, AgentIdentity{signer.PublicKey().Type(), ssh.FingerprintSHA256(signer.PublicKey())})
	}
	return result, nil
}
