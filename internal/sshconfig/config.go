// Package sshconfig imports bounded, inert snapshots of OpenSSH configuration.
// No configuration directive is ever executed.
package sshconfig

import (
	"bufio"
	"ding-ssh/internal/i18n"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

type directive struct {
	patterns []string
	key      string
	values   []string
}
type Preview struct {
	Alias    string            `json:"alias"`
	Node     models.ServerNode `json:"node"`
	Jumps    []string          `json:"jumps"`
	Warnings []string          `json:"warnings"`
}

func expand(path string) string {
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}

// words handles quotes, escapes, comments and key=value syntax without shell
// evaluation. Unterminated quoting fails the entire preview.
func words(line string) ([]string, error) {
	result := []string{}
	var word strings.Builder
	var quote rune
	escaped := false
	started := false
	flush := func() {
		if started {
			result = append(result, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range line {
		if escaped {
			word.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if r == '#' {
			break
		}
		if r == ' ' || r == '\t' || r == '=' && len(result) == 0 {
			flush()
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if quote != 0 || escaped {
		logx.Debugf("[sshconfig] 解析配置词法失败: quote=%q escaped=%v", quote, escaped)
		return nil, fmt.Errorf("%s", i18n.T("配置引号或转义不完整"))
	}
	flush()
	return result, nil
}

func Parse(path string) ([]Preview, error) {
	start := time.Now()
	path = expand(path)
	logx.Infof("[sshconfig] 开始解析 SSH 配置: path=%q", path)
	directives := []directive{}
	aliases := []string{}
	known := map[string]bool{}
	stack := map[string]bool{}
	count := 0
	var read func(string, []string, int) ([]string, error)
	read = func(file string, patterns []string, depth int) ([]string, error) {
		if depth > 16 {
			logx.Warnf("[sshconfig] Include 嵌套过深，中止解析: file=%q depth=%d", file, depth)
			return patterns, fmt.Errorf("%s", i18n.T("Include 深度超过 16"))
		}
		abs, err := filepath.Abs(file)
		if err != nil {
			return patterns, err
		}
		canonical, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return patterns, err
		}
		if stack[canonical] {
			logx.Warnf("[sshconfig] 检测到 Include 循环引用: file=%q", file)
			return patterns, fmt.Errorf(i18n.T("Include 循环引用：%s"), file)
		}
		stack[canonical] = true
		defer delete(stack, canonical)
		f, err := os.Open(abs)
		if err != nil {
			logx.Warnf("[sshconfig] 打开配置文件失败: path=%q err=%v", abs, err)
			return patterns, err
		}
		defer f.Close()
		logx.Debugf("[sshconfig] 读取配置文件: path=%q depth=%d patterns=%d", canonical, depth, len(patterns))
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			count++
			if count > 100000 {
				logx.Warnf("[sshconfig] 配置文件行数超限，中止解析: path=%q lines=%d", file, count)
				return patterns, fmt.Errorf("%s", i18n.T("配置文件过大"))
			}
			fields, err := words(scanner.Text())
			if err != nil {
				logx.Warnf("[sshconfig] 解析配置行失败: path=%q line=%d err=%v", file, count, err)
				return patterns, fmt.Errorf("%s: %w", file, err)
			}
			if len(fields) == 0 {
				continue
			}
			key := strings.ToLower(fields[0])
			values := fields[1:]
			if len(values) == 0 {
				logx.Warnf("[sshconfig] 配置项缺少取值: path=%q line=%d key=%q", file, count, fields[0])
				return patterns, fmt.Errorf(i18n.T("%s 缺少配置值"), fields[0])
			}
			if key == "include" {
				for _, pattern := range values {
					pattern = expand(pattern)
					if !filepath.IsAbs(pattern) {
						home, err := os.UserHomeDir()
						if err != nil {
							return patterns, err
						}
						pattern = filepath.Join(home, ".ssh", pattern)
					}
					matches, err := filepath.Glob(pattern)
					if err != nil {
						logx.Warnf("[sshconfig] Include 通配符非法: pattern=%q err=%v", pattern, err)
						return patterns, err
					}
					if len(matches) == 0 {
						logx.Debugf("[sshconfig] Include 未匹配任何文件: pattern=%q", pattern)
						continue
					}
					logx.Debugf("[sshconfig] Include 匹配到配置文件: pattern=%q matches=%d", pattern, len(matches))
					for _, match := range matches {
						patterns, err = read(match, patterns, depth+1)
						if err != nil {
							return patterns, err
						}
					}
				}
				continue
			}
			if key == "host" {
				patterns = values
				for _, alias := range values {
					if !strings.ContainsAny(alias, "*?![]") && !known[alias] {
						known[alias] = true
						aliases = append(aliases, alias)
					}
				}
				logx.Debugf("[sshconfig] 解析到 Host 段: path=%q aliases=%d", file, len(values))
				continue
			}
			// Match changes scope in ways this importer cannot safely evaluate.
			if key == "match" {
				logx.Warnf("[sshconfig] 配置包含不支持的 Match 段，中止解析: path=%q line=%d", file, count)
				return patterns, fmt.Errorf("%s", i18n.T("不支持 Match，无法安全生成配置快照"))
			}
			directives = append(directives, directive{append([]string{}, patterns...), key, values})
		}
		return patterns, scanner.Err()
	}
	if _, err := read(path, []string{"*"}, 0); err != nil {
		logx.Errorf("[sshconfig] SSH 配置解析失败: path=%q err=%v", path, err)
		return nil, err
	}
	logx.Debugf("[sshconfig] SSH 配置文件读取完成: path=%q lines=%d aliases=%d directives=%d elapsed=%s",
		path, count, len(aliases), len(directives), time.Since(start))
	defaultUser := os.Getenv("USER")
	if current, err := user.Current(); err == nil {
		defaultUser = current.Username
		if index := strings.LastIndex(defaultUser, "\\"); index >= 0 {
			defaultUser = defaultUser[index+1:]
		}
	}
	results := []Preview{}
	for _, alias := range aliases {
		values := map[string][]string{}
		warnings := []string{}
		for _, d := range directives {
			if !matchesHost(d.patterns, alias) {
				continue
			}
			switch d.key {
			case "hostname", "user", "port", "identityagent", "identitiesonly", "proxyjump", "hostkeyalias", "userknownhostsfile":
				if _, set := values[d.key]; !set {
					values[d.key] = d.values
				}
			case "identityfile":
				values[d.key] = append(values[d.key], d.values...)
			default:
				warnings = append(warnings, i18n.T("不支持配置项：")+d.key)
			}
		}
		first := func(key, fallback string) string {
			if len(values[key]) > 0 {
				return values[key][0]
			}
			return fallback
		}
		node := models.ServerNode{Name: alias, Host: first("hostname", alias), User: first("user", defaultUser), Port: 22, AuthType: "auto", EnvVars: map[string]string{}}
		port, err := strconv.Atoi(first("port", "22"))
		if err != nil || port < 1 || port > 65535 {
			warnings = append(warnings, i18n.T("无效 SSH 端口"))
		} else {
			node.Port = port
		}
		token := func(value string) string {
			return strings.NewReplacer("%h", node.Host, "%n", alias, "%r", node.User, "%p", strconv.Itoa(node.Port), "%%", "%").Replace(value)
		}
		node.HostKeyAlias = token(first("hostkeyalias", ""))
		node.IdentityAgent = expand(token(first("identityagent", "")))
		only := strings.ToLower(first("identitiesonly", "no"))
		if only != "yes" && only != "no" {
			warnings = append(warnings, i18n.T("无效 IdentitiesOnly 值"))
		}
		node.IdentitiesOnly = only == "yes"
		for _, file := range values["identityfile"] {
			if file != "none" {
				node.IdentityFiles = append(node.IdentityFiles, expand(token(file)))
			}
		}
		for _, file := range values["userknownhostsfile"] {
			if file == "none" {
				warnings = append(warnings, i18n.T("不支持禁用主机信任库"))
			} else {
				node.KnownHostsSources = append(node.KnownHostsSources, expand(token(file)))
			}
		}
		node.ImportAlias = alias
		node.ImportSource = path
		jumps := []string{}
		jump := first("proxyjump", "")
		if jump != "" && jump != "none" {
			jumps = strings.Split(jump, ",")
		}
		logx.Debugf("[sshconfig] 解析主机: alias=%s host=%s user=%s port=%d auth=%s jumps=%d",
			alias, node.Host, node.User, node.Port, node.AuthType, len(jumps))
		if strings.Contains(node.Host, "%") || strings.Contains(node.IdentityAgent, "%") {
			warnings = append(warnings, i18n.T("包含不支持的配置 token"))
		}
		for _, path := range append(append([]string{}, node.IdentityFiles...), node.KnownHostsSources...) {
			if strings.Contains(path, "%") {
				warnings = append(warnings, i18n.T("路径包含不支持的 token：")+path)
			}
		}
		results = append(results, Preview{alias, node, jumps, warnings})
	}
	for i := range results {
		for _, jump := range results[i].Jumps {
			if !known[jump] {
				results[i].Warnings = append(results[i].Warnings, i18n.T("ProxyJump 必须引用配置中的明确 Host 别名：")+jump)
			}
		}
	}
	// 每个存在告警的主机记一条 WARN（导入后功能降级），告警明细用 DEBUG。
	totalWarnings := 0
	for i := range results {
		if len(results[i].Warnings) == 0 {
			continue
		}
		logx.Warnf("[sshconfig] 主机配置存在告警: alias=%s warnings=%d", results[i].Alias, len(results[i].Warnings))
		for _, warning := range results[i].Warnings {
			logx.Debugf("[sshconfig] 配置告警详情: alias=%s warning=%q", results[i].Alias, warning)
		}
		totalWarnings += len(results[i].Warnings)
	}
	if len(results) == 0 {
		logx.Warnf("[sshconfig] 未解析到可导入的 Host 别名: path=%q lines=%d", path, count)
	}
	logx.Infof("[sshconfig] SSH 配置解析完成: path=%q aliases=%d directives=%d warnings=%d elapsed=%s",
		path, len(results), len(directives), totalWarnings, time.Since(start))
	return results, nil
}

func matchesHost(patterns []string, host string) bool {
	matched := false
	for _, pattern := range patterns {
		negate := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		ok, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(host))
		if err != nil {
			continue
		}
		if ok && negate {
			return false
		}
		if ok {
			matched = true
		}
	}
	return matched
}
