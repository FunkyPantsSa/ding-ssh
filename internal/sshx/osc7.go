package sshx

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseDirFromOutput 从终端输出中提取当前工作目录。
// 优先级：末行 Prompt 路径 > 最近一次 cd 参数 > 缓冲区中最后一次 OSC 7。
// 未匹配时返回 ("", false)。

// OSC 7：\033]7;file://[hostname]/path\007 或 ST(\033\\) 终止；hostname 可为空。
var osc7Pattern = regexp.MustCompile(`\x1b\]7;file://[^/\x07\x1b]*(/[^\x07\x1b]*)(?:\x07|\x1b\\)`)

// promptPatterns 末行提示符。按优先级：
// 1. [user@host /path]$   （CentOS/RHEL）
// 2. user@host:/path$     （Debian/Ubuntu）
// 3. 行首绝对路径 /$
var promptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[[^@\]]+@[^\]]+\s+(\S+)\]\s*[#%$]\s*$`),          // [user@host /path]$
	regexp.MustCompile(`[^\s@]+@[^\s:]+:(\S+)\s*[#%$]\s*$`),                // user@host:/path$
	regexp.MustCompile(`(?:^|[\s])(/[^\s]*)\s*[#%$]\s*$`),                 // ... /path$
}

// cdPattern 捕获输出中回显的 cd 命令（ECHO 开启时会出现在 stdout）。
var cdPattern = regexp.MustCompile(`(?:^|[\n])cd[ \t]+([^\n#;|&]+)\n`)

// ansiPattern 仅剥离 CSI / OSC / 单字符 ESC，保留换行以便取末行。
var ansiPattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[A-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][0-9A-B]|[NO])`)

func stripANSI(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansiPattern.ReplaceAllString(s, "")
	return applyBackspaces(s)
}

// applyBackspaces 处理退格/DEL，并丢掉响铃，得到终端上实际可见文本。
func applyBackspaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\b', 0x7f:
			t := b.String()
			if t == "" {
				continue
			}
			_, size := utf8.DecodeLastRuneInString(t)
			b.Reset()
			b.WriteString(t[:len(t)-size])
		case '\a':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func tailWindow(data []byte, n int) string {
	if len(data) > n {
		return string(data[len(data)-n:])
	}
	return string(data)
}

func lastLine(s string) string {
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func normalizeRemotePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	cleaned := path.Clean(p)
	if cleaned != "/" && strings.HasSuffix(p, "/") {
		return cleaned + "/"
	}
	return cleaned
}

// ParseOSC7 从输出中提取最后一次 OSC 7 路径。
func ParseOSC7(data []byte) (string, bool) {
	matches := osc7Pattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return "", false
	}
	raw := string(matches[len(matches)-1][1])
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	p := normalizeRemotePath(raw)
	if p == "" {
		return "", false
	}
	return p, true
}

// ParsePrompt 从输出末行匹配 Shell Prompt 提取路径。
func ParsePrompt(data []byte) (string, bool) {
	tail := stripANSI(tailWindow(data, 512))
	line := lastLine(tail)
	for _, pat := range promptPatterns {
		m := pat.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := normalizeRemotePath(m[1])
		if p != "" {
			return p, true
		}
	}
	return "", false
}

func firstArg(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"'`)
}

// ParseCdFromOutput 从回显的 cd 命令提取目标路径（仅绝对路径）。
func ParseCdFromOutput(data []byte) (string, bool) {
	tail := stripANSI(tailWindow(data, 1024))
	matches := cdPattern.FindAllStringSubmatch(tail, -1)
	if len(matches) == 0 {
		return "", false
	}
	p := normalizeRemotePath(firstArg(matches[len(matches)-1][1]))
	if p == "" {
		return "", false
	}
	return p, true
}

// ParseDirFromOutput 综合解析终端输出中的目录变更。
// source 为 prompt / cd / osc7，便于日志排查。
func ParseDirFromOutput(data []byte) (string, string, bool) {
	if p, ok := ParsePrompt(data); ok {
		return p, "prompt", true
	}
	if p, ok := ParseCdFromOutput(data); ok {
		return p, "cd", true
	}
	if p, ok := ParseOSC7(data); ok {
		return p, "osc7", true
	}
	return "", "", false
}

func containsNewline(b []byte) bool {
	return strings.ContainsRune(string(b), '\n') || strings.ContainsRune(string(b), '\r')
}

func previewTail(data []byte, n int) string {
	s := stripANSI(tailWindow(data, n*2))
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return strings.ReplaceAll(s, "\n", "\\n")
}
