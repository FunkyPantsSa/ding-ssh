// Package sshconfig imports bounded, inert snapshots of OpenSSH configuration.
// No configuration directive is ever executed.
package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

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
		return nil, fmt.Errorf("配置引号或转义不完整")
	}
	flush()
	return result, nil
}

func Parse(path string) ([]Preview, error) {
	path = expand(path)
	directives := []directive{}
	aliases := []string{}
	known := map[string]bool{}
	stack := map[string]bool{}
	count := 0
	var read func(string, []string, int) ([]string, error)
	read = func(file string, patterns []string, depth int) ([]string, error) {
		if depth > 16 {
			return patterns, fmt.Errorf("Include 深度超过 16")
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
			return patterns, fmt.Errorf("Include 循环引用：%s", file)
		}
		stack[canonical] = true
		defer delete(stack, canonical)
		f, err := os.Open(abs)
		if err != nil {
			return patterns, err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			count++
			if count > 100000 {
				return patterns, fmt.Errorf("配置文件过大")
			}
			fields, err := words(scanner.Text())
			if err != nil {
				return patterns, fmt.Errorf("%s: %w", file, err)
			}
			if len(fields) == 0 {
				continue
			}
			key := strings.ToLower(fields[0])
			values := fields[1:]
			if len(values) == 0 {
				return patterns, fmt.Errorf("%s 缺少配置值", fields[0])
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
						return patterns, err
					}
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
				continue
			}
			// Match changes scope in ways this importer cannot safely evaluate.
			if key == "match" {
				return patterns, fmt.Errorf("不支持 Match，无法安全生成配置快照")
			}
			directives = append(directives, directive{append([]string{}, patterns...), key, values})
		}
		return patterns, scanner.Err()
	}
	if _, err := read(path, []string{"*"}, 0); err != nil {
		return nil, err
	}
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
				warnings = append(warnings, "不支持配置项："+d.key)
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
			warnings = append(warnings, "无效 SSH 端口")
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
			warnings = append(warnings, "无效 IdentitiesOnly 值")
		}
		node.IdentitiesOnly = only == "yes"
		for _, file := range values["identityfile"] {
			if file != "none" {
				node.IdentityFiles = append(node.IdentityFiles, expand(token(file)))
			}
		}
		for _, file := range values["userknownhostsfile"] {
			if file == "none" {
				warnings = append(warnings, "不支持禁用主机信任库")
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
		if strings.Contains(node.Host, "%") || strings.Contains(node.IdentityAgent, "%") {
			warnings = append(warnings, "包含不支持的配置 token")
		}
		for _, path := range append(append([]string{}, node.IdentityFiles...), node.KnownHostsSources...) {
			if strings.Contains(path, "%") {
				warnings = append(warnings, "路径包含不支持的 token："+path)
			}
		}
		results = append(results, Preview{alias, node, jumps, warnings})
	}
	for i := range results {
		for _, jump := range results[i].Jumps {
			if !known[jump] {
				results[i].Warnings = append(results[i].Warnings, "ProxyJump 必须引用配置中的明确 Host 别名："+jump)
			}
		}
	}
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
