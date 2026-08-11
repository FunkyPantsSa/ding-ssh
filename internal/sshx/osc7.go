package sshx

import (
	"regexp"
	"strings"
)

// OSC7DirSync 解析终端输出中的目录同步信息，支持两种模式：
// 1. OSC 7 转义序列：\033]7;file://hostname/path\007
// 2. Shell Prompt 备选匹配：常见提示符格式如 [user@host /path]$
//
// 返回解析到的绝对路径，以及是否匹配成功。
// 未匹配时返回 ("", false)。

// osc7Pattern 匹配 OSC 7 转义序列：\033]7;file://[hostname]/[path]\007
var osc7Pattern = regexp.MustCompile(`\x1b\]7;file://[^/]+(/[^\x07]*)\x07`)

// promptPatterns 备选 Shell Prompt 匹配模式（按优先级降序排列）
// 匹配格式： [user@host /path]$ 或 user@host:/path$ 或 /path#
var promptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[[^\]]+@[^\]]+\]\s+(\S+)\s*[$#]\s*$`),          // [user@host /path]$
	regexp.MustCompile(`[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:(\S+)[$#]\s*$`), // user@host:/path$
	regexp.MustCompile(`^(/[^\s]*)\s*[$#]\s*$`),                          // /path$
}

// commonHomePaths 常见 home 目录缩写映射
var commonHomePaths = map[string]string{
	"~":  "",
	"~/": "",
}

// ParseOSC7 从输出字节流中提取 OSC 7 转义序列中的路径。
// 返回路径和是否匹配。
func ParseOSC7(data []byte) (string, bool) {
	m := osc7Pattern.FindSubmatch(data)
	if m != nil {
		path := string(m[1])
		if path != "" {
			return path, true
		}
	}
	return "", false
}

// ParsePrompt 从输出字节流末尾（最后 512 字节）匹配 Shell Prompt 提取路径。
// 仅匹配行尾的提示符，避免误匹配中间输出。
func ParsePrompt(data []byte) (string, bool) {
	// 只检查末尾 512 字节，提高匹配效率
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	tail := string(data[len(data)-checkLen:])

	// 只匹配末尾行（最后一个换行符之后的内容）
	lastNewline := strings.LastIndex(tail, "\n")
	if lastNewline >= 0 {
		tail = tail[lastNewline+1:]
	}

	for _, pat := range promptPatterns {
		m := pat.FindStringSubmatch(tail)
		if m != nil {
			path := m[1]
			// 处理 ~ 缩写
			if strings.HasPrefix(path, "~") {
				path = "/home" + path[1:] // 通用 fallback
			}
			if path != "" {
				return path, true
			}
		}
	}
	return "", false
}

// ParseDirFromOutput 综合解析终端输出中的目录变更信息。
// 优先尝试 OSC 7，失败则回退 Prompt 匹配。
func ParseDirFromOutput(data []byte) (string, bool) {
	if path, ok := ParseOSC7(data); ok {
		return path, true
	}
	return ParsePrompt(data)
}
