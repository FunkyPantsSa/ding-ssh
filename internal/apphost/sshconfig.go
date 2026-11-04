package apphost

import (
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"fmt"
)

func (a *App) resolveSSHNode(node models.ServerNode) (models.ServerNode, error) {
	if a.store == nil {
		if len(node.JumpServerIDs) > 0 {
			return node, fmt.Errorf("服务器存储尚未就绪")
		}
		return node, nil
	}
	servers, err := a.store.List()
	if err != nil {
		return node, err
	}
	resolved, err := sshx.ResolveJumpChain(node, servers)
	for i := range resolved.ResolvedJumps {
		resolved.ResolvedJumps[i].RuntimePassphrases = node.RuntimePassphrases
	}
	return resolved, err
}
