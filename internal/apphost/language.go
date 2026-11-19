package apphost

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
)

// SetUILanguage synchronizes native dialogs and runtime errors with the frontend preference.
func (a *App) SetUILanguage(language string) {
	i18n.SetLanguage(language)
	logx.Infof("[apphost] 界面语言已切换: language=%s", language)
}
