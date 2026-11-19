package apphost

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"fmt"
)

// resolveSSHNode 把节点上的跳板机 ID 链解析成可直接用于连接/建隧道的完整节点。
// 这里是高频路径（连接、测试、保存、建隧道都会调用），只记 DEBUG/WARN，不记成功 INFO。
func (a *App) resolveSSHNode(node models.ServerNode) (models.ServerNode, error) {
	logx.Debugf("[apphost] 解析 SSH 节点跳板链: name=%s host=%s user=%s jumps=%d",
		node.Name, node.Host, node.User, len(node.JumpServerIDs))
	if a.store == nil {
		if len(node.JumpServerIDs) > 0 {
			logx.Warnf("[apphost] 服务器存储未就绪，无法解析跳板链: name=%s jumps=%d", node.Name, len(node.JumpServerIDs))
			return node, fmt.Errorf("%s", i18n.T("服务器存储尚未就绪"))
		}
		logx.Debugf("[apphost] 服务器存储未就绪，按原节点返回: name=%s", node.Name)
		return node, nil
	}
	servers, err := a.store.List()
	if err != nil {
		// 调用方（绑定方法）会记录用户可见的失败，这里只留 DEBUG 避免重复。
		logx.Debugf("[apphost] 读取服务器列表失败，跳板链未解析: name=%s err=%v", node.Name, err)
		return node, err
	}
	resolved, err := sshx.ResolveJumpChain(node, servers)
	for i := range resolved.ResolvedJumps {
		resolved.ResolvedJumps[i].RuntimePassphrases = node.RuntimePassphrases
	}
	if err != nil {
		// 调用方会记录用户可见的失败，这里只留 DEBUG 避免重复。
		logx.Debugf("[apphost] 解析跳板链失败: name=%s jumps=%d err=%v", node.Name, len(node.JumpServerIDs), err)
	} else if len(resolved.ResolvedJumps) > 0 {
		logx.Debugf("[apphost] 跳板链解析完成: name=%s resolvedJumps=%d", node.Name, len(resolved.ResolvedJumps))
	}
	return resolved, err
}
