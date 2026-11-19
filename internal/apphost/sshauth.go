package apphost

import (
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"golang.org/x/crypto/ssh"
)

// Password phrases are transient request inputs, excluded from ServerNode JSON,
// SQLite, configuration bundles and source configuration snapshots.
func (a *App) ConnectAuthenticated(id string, node models.ServerNode, cols, rows int, phrases map[string]string) (models.ConnectResult, error) {
	// 只记录口令条数，绝不含口令值本身。
	logx.Debugf("[apphost] 带运行期口令建立 SSH 连接: session=%s server=%s phraseCount=%d",
		id, node.Name, len(phrases))
	node.RuntimePassphrases = phrases
	return a.Connect(id, node, cols, rows)
}
func (a *App) TestServerLoginAuthenticated(node models.ServerNode, phrases map[string]string) models.ServerTestResult {
	logx.Debugf("[apphost] 带运行期口令测试服务器登录: server=%s host=%s user=%s phraseCount=%d",
		node.Name, node.Host, node.User, len(phrases))
	node.RuntimePassphrases = phrases
	return a.TestServerLogin(node)
}
func (a *App) StartTunnelAuthenticated(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, phrases map[string]string) (models.TunnelInfo, error) {
	logx.Debugf("[apphost] 带运行期口令启动隧道: name=%s mode=%s server=%s phraseCount=%d",
		name, mode, node.Name, len(phrases))
	node.RuntimePassphrases = phrases
	return a.StartTunnel(node, name, mode, localPort, remoteHost, remotePort)
}

type AgentIdentity struct {
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
}

func (a *App) GetAgentIdentities(node models.ServerNode) ([]AgentIdentity, error) {
	logx.Debugf("[apphost] 读取 SSH Agent 身份列表: server=%s host=%s user=%s", node.Name, node.Host, node.User)
	signers, err := sshx.AgentSigners(node)
	if err != nil {
		logx.Warnf("[apphost] 读取 SSH Agent 身份列表失败: server=%s err=%v", node.Name, err)
		return nil, err
	}
	result := []AgentIdentity{}
	for _, signer := range signers {
		result = append(result, AgentIdentity{signer.PublicKey().Type(), ssh.FingerprintSHA256(signer.PublicKey())})
	}
	logx.Infof("[apphost] 读取 SSH Agent 身份列表完成: server=%s count=%d", node.Name, len(result))
	return result, nil
}
