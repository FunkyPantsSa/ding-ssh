package sshx

import (
	"ding-ssh/internal/models"
	"fmt"
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
			return fmt.Errorf("跳板路径包含循环或重复服务器：%s", id)
		}
		hop, ok := byID[id]
		if !ok {
			return fmt.Errorf("找不到跳板服务器：%s", id)
		}
		seen[id] = true
		for _, parent := range hop.JumpServerIDs {
			if err := visit(parent); err != nil {
				return err
			}
		}
		resolved = append(resolved, hop)
		if len(resolved) > 5 {
			return fmt.Errorf("最多支持 5 级跳板机")
		}
		return nil
	}
	for _, id := range node.JumpServerIDs {
		if err := visit(id); err != nil {
			return node, err
		}
	}
	node.ResolvedJumps = resolved
	return node, nil
}
