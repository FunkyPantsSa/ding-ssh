package apphost

import (
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetUILanguage synchronizes native dialogs and runtime errors with the frontend preference.
func (a *App) SetUILanguage(language string) {
	i18n.SetLanguage(language)
	a.refreshTrayMenu()
	if a.desktop != nil {
		application.InvokeSync(func() { setupApplicationMenu(a, a.desktop) })
	}
	logx.Infof("[apphost] 界面语言已切换: language=%s", language)
}
