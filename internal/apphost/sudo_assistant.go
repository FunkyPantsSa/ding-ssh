package apphost

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/models"
	"errors"
)

// WriteInteractive is used only by human terminal input. Automation continues
// to use Write and cannot accidentally arm the interactive password assistant.
func (a *App) WriteInteractive(sessionID, data, command string) error {
	if a.local != nil && a.local.Has(sessionID) {
		return a.Write(sessionID, data)
	}
	if a.manager == nil {
		return errors.New(i18n.T("会话不存在"))
	}
	return a.manager.WriteInteractive(sessionID, data, command)
}

// savedSudoPassword binds the latest stored credential to the actual connected
// host/user, never to a server ID supplied by the frontend. Private-key phrases
// occupy the legacy Password field too and must never be used here.
func (a *App) savedSudoPassword(connected models.ServerNode) (string, error) {
	if err := a.ensureUnlocked(); err != nil {
		return "", err
	}
	if a.store != nil && connected.ID != "" {
		nodes, err := a.store.List()
		if err != nil {
			return "", errors.New(i18n.T("无法读取保存的登录密码，请手动输入"))
		}
		for _, node := range nodes {
			if node.ID != connected.ID {
				continue
			}
			if node.Host != connected.Host || node.Port != connected.Port || node.User != connected.User || !node.SudoAutoFill {
				return "", errors.New(i18n.T("服务器配置已变化，请重新连接后再使用 sudo 自动填写"))
			}
			if (node.AuthType == "password" || node.AuthType == "") && node.Password != "" {
				return node.Password, nil
			}
			break
		}
	}
	return "", errors.New(i18n.T("没有可用的登录密码，请手动输入 sudo 密码"))
}

// SubmitSudoPassword never returns/logs a password or puts it in terminal
// commands. Stored passwords stay in Go; a manual override is transient.
func (a *App) SubmitSudoPassword(sessionID, requestID, password string, useSaved bool) error {
	if a.manager == nil {
		return errors.New(i18n.T("会话不存在"))
	}
	node, err := a.manager.SudoServer(sessionID, requestID)
	if err != nil {
		return err
	}
	if useSaved {
		password, err = a.savedSudoPassword(node)
		if err != nil {
			return err
		}
	}
	return a.manager.SubmitSudoPassword(sessionID, requestID, password, useSaved)
}

func (a *App) CancelSudoPrompt(sessionID, requestID string) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.CancelSudoPrompt(sessionID, requestID)
}
