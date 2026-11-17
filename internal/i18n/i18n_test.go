package i18n

import (
	"regexp"
	"slices"
	"testing"
)

func TestLanguageAndFallback(t *testing.T) {
	defer SetLanguage("zh-CN")
	SetLanguage("zh-CN")
	if T("主密码错误") != "主密码错误" {
		t.Fatal("Chinese should be the default")
	}
	SetLanguage("en")
	if T("主密码错误") != "Incorrect master password" {
		t.Fatal("missing English translation")
	}
	if T("user supplied text") != "user supplied text" {
		t.Fatal("unknown text must be preserved")
	}
	SetLanguage("invalid")
	if T("主密码错误") != "主密码错误" {
		t.Fatal("unsupported language must fall back to Chinese")
	}
}

func TestFormatPlaceholders(t *testing.T) {
	pattern := regexp.MustCompile(`%[+#\- 0]*[0-9]*\.?[0-9]*[a-zA-Z]`)
	for source, translated := range english {
		a, b := pattern.FindAllString(source, -1), pattern.FindAllString(translated, -1)
		if !slices.Equal(a, b) {
			t.Errorf("format changed for %q: %v -> %v", source, a, b)
		}
	}
}
