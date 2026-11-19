package apphost

import (
	"ding-ssh/internal/i18n"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshconfig"
	"ding-ssh/internal/sshx"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) PreviewSSHConfig(path string) ([]sshconfig.Preview, error) {
	start := time.Now()
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			logx.Warnf("[apphost] 解析 SSH 配置默认路径失败（取不到用户主目录）: err=%v", err)
			return nil, err
		}
		path = filepath.Join(home, ".ssh", "config")
		logx.Debugf("[apphost] 使用默认 SSH 配置路径: path=%q", path)
	}
	logx.Debugf("[apphost] 预览 SSH 配置: path=%q", path)
	previews, err := sshconfig.Parse(path)
	if err != nil {
		logx.Errorf("[apphost] 解析 SSH 配置失败: path=%q err=%v", path, err)
		return nil, err
	}
	warns := 0
	for _, entry := range previews {
		if len(entry.Warnings) > 0 {
			warns++
			logx.Debugf("[apphost] SSH 配置主机需要人工检查: alias=%s warnings=%d", entry.Alias, len(entry.Warnings))
		}
	}
	logx.Infof("[apphost] 预览 SSH 配置完成: path=%q hosts=%d warned=%d elapsed=%s",
		path, len(previews), warns, time.Since(start))
	return previews, nil
}
func (a *App) SelectSSHConfig() (string, error) {
	logx.Debugf("[apphost] 打开 SSH 配置文件选择对话框")
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: i18n.T("选择 OpenSSH Config")})
	if err != nil {
		logx.Warnf("[apphost] 打开 SSH 配置文件选择对话框失败: err=%v", err)
		return "", err
	}
	if path == "" {
		logx.Debugf("[apphost] 用户取消了 SSH 配置文件选择")
		return "", nil
	}
	logx.Infof("[apphost] 已选择 SSH 配置文件: path=%q", path)
	return path, nil
}

type SSHImportChoice struct {
	Alias    string `json:"alias"`
	Strategy string `json:"strategy"`
}

// ImportSSHConfig reparses the source and validates the full resulting graph
// before a single atomic store operation. Preview data is never trusted input.
func (a *App) ImportSSHConfig(path string, choices []SSHImportChoice) (int, error) {
	start := time.Now()
	logx.Debugf("[apphost] 导入 SSH 配置开始: path=%q choices=%d", path, len(choices))
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 导入 SSH 配置被拒（保险库未解锁）: err=%v", err)
		return 0, err
	}
	preview, err := a.PreviewSSHConfig(path)
	if err != nil {
		// PreviewSSHConfig 已记录 ERROR，这里只补导入上下文，避免重复 ERROR。
		logx.Debugf("[apphost] 导入 SSH 配置中止（解析源文件失败）: path=%q err=%v", path, err)
		return 0, err
	}
	existing, err := a.store.List()
	if err != nil {
		logx.Errorf("[apphost] 导入 SSH 配置失败（读取现有服务器）: path=%q err=%v", path, err)
		return 0, err
	}
	byAlias := map[string]sshconfig.Preview{}
	for _, entry := range preview {
		byAlias[entry.Alias] = entry
	}
	selected := map[string]models.ServerNode{}
	strategies := map[string]string{}
	skipped := 0
	for _, choice := range choices {
		if choice.Strategy == "skip" {
			skipped++
			logx.Debugf("[apphost] 跳过 SSH 配置主机: alias=%s", choice.Alias)
			continue
		}
		if choice.Strategy != "copy" && choice.Strategy != "overwrite" {
			logx.Warnf("[apphost] 导入 SSH 配置被拒（无效冲突策略）: alias=%s strategy=%s", choice.Alias, choice.Strategy)
			return 0, fmt.Errorf("%s", i18n.T("无效冲突策略"))
		}
		if _, duplicate := selected[choice.Alias]; duplicate {
			logx.Warnf("[apphost] 导入 SSH 配置被拒（重复导入项）: alias=%s", choice.Alias)
			return 0, fmt.Errorf("%s", i18n.T("重复导入项"))
		}
		entry, ok := byAlias[choice.Alias]
		if !ok || len(entry.Warnings) > 0 {
			logx.Warnf("[apphost] 导入 SSH 配置被拒（主机需要人工检查）: alias=%s found=%v warnings=%d",
				choice.Alias, ok, len(entry.Warnings))
			return 0, fmt.Errorf(i18n.T("主机 %s 需要人工检查"), choice.Alias)
		}
		node := entry.Node
		node.ID = uuid.NewString()
		matches := []models.ServerNode{}
		for _, old := range existing {
			if old.Name == node.Name || old.ImportAlias == choice.Alias && old.ImportSource == node.ImportSource {
				matches = append(matches, old)
			}
		}
		if choice.Strategy == "overwrite" && len(matches) > 1 {
			logx.Warnf("[apphost] 导入 SSH 配置被拒（多个冲突无法安全覆盖）: alias=%s matches=%d", choice.Alias, len(matches))
			return 0, fmt.Errorf(i18n.T("%s 存在多个冲突，无法安全覆盖"), choice.Alias)
		}
		if len(matches) > 0 {
			if choice.Strategy == "overwrite" {
				node.ID = matches[0].ID
				node.Group = matches[0].Group
				node.BgImage = matches[0].BgImage
				node.BlurAmount = matches[0].BlurAmount
				logx.Debugf("[apphost] 覆盖已有服务器: alias=%s id=%s", choice.Alias, node.ID)
			} else {
				node.Name += "（导入副本）"
				logx.Debugf("[apphost] 以副本方式导入冲突主机: alias=%s newName=%s", choice.Alias, node.Name)
			}
		}
		selected[choice.Alias] = node
		strategies[choice.Alias] = choice.Strategy
	}
	for alias, node := range selected {
		for _, jump := range byAlias[alias].Jumps {
			target, ok := selected[jump]
			if !ok {
				logx.Warnf("[apphost] 导入 SSH 配置被拒（跳板机未同时选择导入）: alias=%s jump=%s", alias, jump)
				return 0, fmt.Errorf(i18n.T("%s 依赖 %s，请同时选择导入"), alias, jump)
			}
			node.JumpServerIDs = append(node.JumpServerIDs, target.ID)
		}
		selected[alias] = node
	}
	combined := append([]models.ServerNode{}, existing...)
	incoming := []models.ServerNode{}
	for _, entry := range preview {
		node, ok := selected[entry.Alias]
		if !ok {
			continue
		}
		incoming = append(incoming, node)
		replaced := false
		for i := range combined {
			if combined[i].ID == node.ID {
				combined[i] = node
				replaced = true
				break
			}
		}
		if !replaced {
			combined = append(combined, node)
		}
	}
	for _, node := range combined {
		if _, err := sshx.ResolveJumpChain(node, combined); err != nil {
			logx.Warnf("[apphost] 导入 SSH 配置被拒（导入后跳板链校验失败）: name=%s err=%v", node.Name, err)
			return 0, err
		}
	}
	batch, ok := a.store.(interface {
		SaveBatch([]models.ServerNode) error
	})
	if !ok {
		logx.Errorf("[apphost] 导入 SSH 配置失败（存储不支持原子导入）: path=%q", path)
		return 0, fmt.Errorf("%s", i18n.T("存储不支持原子导入"))
	}
	if err := batch.SaveBatch(incoming); err != nil {
		logx.Errorf("[apphost] 导入 SSH 配置失败（写入存储）: path=%q count=%d err=%v", path, len(incoming), err)
		return 0, err
	}
	overwrites := 0
	for _, strategy := range strategies {
		if strategy == "overwrite" {
			overwrites++
		}
	}
	logx.Infof("[apphost] 导入 SSH 配置完成: path=%q ok=%d skipped=%d overwrite=%d elapsed=%s",
		path, len(incoming), skipped, overwrites, time.Since(start))
	return len(incoming), nil
}
