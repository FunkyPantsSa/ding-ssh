// Package i18n localizes application-owned messages. Protocol identifiers and user data are untouched.
package i18n

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"ding-ssh/internal/logx"
)

// 英文翻译缺失在英文界面下可能每次渲染都触发：采样记录，避免刷屏。
var missLog = logx.NewSampler(256, time.Minute)

//go:embed en.json
var englishJSON []byte
var english = func() map[string]string {
	var result map[string]string
	if err := json.Unmarshal(englishJSON, &result); err != nil {
		// 内嵌词表损坏属于构建缺陷：先记 ERROR 再 panic，保留可诊断线索。
		logx.Errorf("[i18n] 加载内嵌英文词表失败: bytes=%d err=%v", len(englishJSON), err)
		panic(err)
	}
	logx.Infof("[i18n] 内嵌英文词表已加载: entries=%d bytes=%d", len(result), len(englishJSON))
	return result
}()
var useEnglish atomic.Bool

func SetLanguage(language string) {
	previous := useEnglish.Load()
	next := language == "en"
	useEnglish.Store(next)
	if previous != next {
		logx.Infof("[i18n] 界面语言已切换: language=%q english=%v", language, next)
	} else {
		logx.Debugf("[i18n] 界面语言未变化: language=%q english=%v", language, next)
	}
	// 只按主语言子标签判断（zh-CN / zh-Hans / zh_CN 都是中文，en-US / en-GB 都是英文），
	// 否则启动时设置里的 zh-CN 会被误报成「非预期语言代码」。
	if base := languageTagBase(language); base != "en" && base != "zh" && base != "" {
		logx.Warnf("[i18n] 非预期语言代码，按中文处理: language=%q", language)
	}
}

// languageTagBase 取 BCP-47 / POSIX 语言标签的主子标签（小写）：
// "zh-CN" / "zh-Hans" / "zh_CN" -> "zh"，"en-US" -> "en"，空串保持空串。
func languageTagBase(language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	if i := strings.IndexAny(language, "-_"); i >= 0 {
		language = language[:i]
	}
	return language
}

func T(message string) string {
	if useEnglish.Load() {
		if translated, ok := english[message]; ok {
			return translated
		}
		logx.DebugSampled(missLog, "[i18n] 英文翻译缺失，回落原文: key=%q", message)
	}
	return message
}

// NewError keeps sentinel identity stable while resolving its message in the
// current UI language, including after switching languages without a restart.
func NewError(message string) error {
	return &localizedError{message: message}
}

type localizedError struct{ message string }

func (e *localizedError) Error() string { return T(e.message) }
