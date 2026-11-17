package apphost

import (
	"fmt"
	"os"
	"path/filepath"

	"ding-ssh/internal/models"
	"ding-ssh/internal/sshconfig"
	"ding-ssh/internal/sshx"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) PreviewSSHConfig(path string) ([]sshconfig.Preview, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".ssh", "config")
	}
	return sshconfig.Parse(path)
}
func (a *App) SelectSSHConfig() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择 OpenSSH Config"})
}

type SSHImportChoice struct {
	Alias    string `json:"alias"`
	Strategy string `json:"strategy"`
}

// ImportSSHConfig reparses the source and validates the full resulting graph
// before a single atomic store operation. Preview data is never trusted input.
func (a *App) ImportSSHConfig(path string, choices []SSHImportChoice) (int, error) {
	if err := a.ensureUnlocked(); err != nil {
		return 0, err
	}
	preview, err := a.PreviewSSHConfig(path)
	if err != nil {
		return 0, err
	}
	existing, err := a.store.List()
	if err != nil {
		return 0, err
	}
	byAlias := map[string]sshconfig.Preview{}
	for _, entry := range preview {
		byAlias[entry.Alias] = entry
	}
	selected := map[string]models.ServerNode{}
	strategies := map[string]string{}
	for _, choice := range choices {
		if choice.Strategy == "skip" {
			continue
		}
		if choice.Strategy != "copy" && choice.Strategy != "overwrite" {
			return 0, fmt.Errorf("无效冲突策略")
		}
		if _, duplicate := selected[choice.Alias]; duplicate {
			return 0, fmt.Errorf("重复导入项")
		}
		entry, ok := byAlias[choice.Alias]
		if !ok || len(entry.Warnings) > 0 {
			return 0, fmt.Errorf("主机 %s 需要人工检查", choice.Alias)
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
			return 0, fmt.Errorf("%s 存在多个冲突，无法安全覆盖", choice.Alias)
		}
		if len(matches) > 0 {
			if choice.Strategy == "overwrite" {
				node.ID = matches[0].ID
				node.Group = matches[0].Group
				node.BgImage = matches[0].BgImage
				node.BlurAmount = matches[0].BlurAmount
			} else {
				node.Name += "（导入副本）"
			}
		}
		selected[choice.Alias] = node
		strategies[choice.Alias] = choice.Strategy
	}
	for alias, node := range selected {
		for _, jump := range byAlias[alias].Jumps {
			target, ok := selected[jump]
			if !ok {
				return 0, fmt.Errorf("%s 依赖 %s，请同时选择导入", alias, jump)
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
			return 0, err
		}
	}
	batch, ok := a.store.(interface {
		SaveBatch([]models.ServerNode) error
	})
	if !ok {
		return 0, fmt.Errorf("存储不支持原子导入")
	}
	if err := batch.SaveBatch(incoming); err != nil {
		return 0, err
	}
	return len(incoming), nil
}
