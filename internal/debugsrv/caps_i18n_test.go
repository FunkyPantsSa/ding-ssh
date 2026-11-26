package debugsrv

import (
	"strings"
	"testing"
	"unicode"

	"ding-ssh/internal/i18n"
)

func TestCapabilityDescriptionsHaveCompleteEnglishTranslations(t *testing.T) {
	defer i18n.SetLanguage("zh-CN")
	i18n.SetLanguage("en")
	for _, capability := range AllCapabilities() {
		for _, text := range []string{CapabilityLabel(capability), CapabilityDescription(capability)} {
			if text == "" || strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				t.Errorf("capability %q has an untranslated label or description: %q", capability, text)
			}
		}
	}
}
