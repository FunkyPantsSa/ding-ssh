// Package i18n localizes application-owned messages. Protocol identifiers and user data are untouched.
package i18n

import (
	_ "embed"
	"encoding/json"
	"sync/atomic"
)

//go:embed en.json
var englishJSON []byte
var english = func() map[string]string {
	var result map[string]string
	if err := json.Unmarshal(englishJSON, &result); err != nil {
		panic(err)
	}
	return result
}()
var useEnglish atomic.Bool

func SetLanguage(language string) { useEnglish.Store(language == "en") }
func T(message string) string {
	if useEnglish.Load() {
		if translated, ok := english[message]; ok {
			return translated
		}
	}
	return message
}
