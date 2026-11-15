package apphost

import "ding-ssh/internal/i18n"

// SetUILanguage synchronizes native dialogs and runtime errors with the frontend preference.
func (a *App) SetUILanguage(language string) { i18n.SetLanguage(language) }
