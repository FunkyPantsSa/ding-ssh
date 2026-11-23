package sshx

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"fmt"
	"strings"
)

// ResolveJumpChain expands stable references, rejecting duplicates, cycles,
// missing nodes and paths exceeding the supported hop limit.
func ResolveJumpChain(node models.ServerNode, servers []models.ServerNode) (models.ServerNode, error) {
	byID := map[string]models.ServerNode{}
	for _, server := range servers {
		byID[server.ID] = server
	}
	seen := map[string]bool{}
	if node.ID != "" {
		seen[node.ID] = true
	}
	resolved := []models.ServerNode{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			logx.Warnf("[sshx] 跳板路径包含循环或重复服务器: id=%s", id)
			return fmt.Errorf(i18n.T("跳板路径包含循环或重复服务器：%s"), id)
		}
		hop, ok := byID[id]
		if !ok {
			logx.Warnf("[sshx] 找不到跳板服务器: id=%s", id)
			return fmt.Errorf(i18n.T("找不到跳板服务器：%s"), id)
		}
		seen[id] = true
		for _, parent := range hop.JumpServerIDs {
			if err := visit(parent); err != nil {
				return err
			}
		}
		resolved = append(resolved, hop)
		logx.Debugf("[sshx] 跳板链路节点: hop=%d server=%s host=%s:%d", len(resolved), hop.Name, hop.Host, hop.Port)
		if len(resolved) > 5 {
			logx.Warnf("[sshx] 跳板层级超过上限: hops=%d limit=5", len(resolved))
			return fmt.Errorf("%s", i18n.T("最多支持 5 级跳板机"))
		}
		return nil
	}
	for _, id := range node.JumpServerIDs {
		if err := visit(id); err != nil {
			return node, err
		}
	}
	node.ResolvedJumps = resolved
	if len(resolved) > 0 {
		chain := make([]string, 0, len(resolved)+1)
		for _, hop := range resolved {
			chain = append(chain, hop.Name)
		}
		chain = append(chain, node.Name)
		logx.Infof("[sshx] 跳板链路解析完成: hops=%d chain=%s", len(resolved), strings.Join(chain, " -> "))
	}
	return node, nil
}
