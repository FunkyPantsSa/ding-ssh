package localterm

import (
	"ding-ssh/internal/i18n"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"ding-ssh/internal/logx"
)

// ShellOption 设置页可选的本地 Shell。
type ShellOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// OptionsForPlatform 返回当前平台可选的 Shell 列表。
func OptionsForPlatform() []ShellOption {
	switch runtime.GOOS {
	case "darwin":
		logx.Debugf("[localterm] 枚举可选 Shell: platform=darwin count=2")
		return []ShellOption{
			{Value: "zsh", Label: "zsh"},
			{Value: "bash", Label: "bash"},
		}
	case "windows":
		logx.Debugf("[localterm] 枚举可选 Shell: platform=windows count=2")
		return []ShellOption{
			{Value: "powershell", Label: "PowerShell"},
			{Value: "cmd", Label: "Command Prompt (cmd)"},
		}
	default: // linux 等
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		label := i18n.T("系统默认 ($SHELL)")
		if shell != "" {
			label = fmt.Sprintf(i18n.T("系统默认 (%s)"), filepath.Base(shell))
		}
		logx.Debugf("[localterm] 枚举可选 Shell: platform=%s count=1 shellEnv=%q", runtime.GOOS, shell)
		return []ShellOption{{Value: "default", Label: label}}
	}
}

// DefaultShell 返回当前平台的默认 Shell 标识。
func DefaultShell() string {
	switch runtime.GOOS {
	case "darwin":
		return "zsh"
	case "windows":
		return "powershell"
	default:
		return "default"
	}
}

// Normalize 将设置值规范为当前平台有效标识。
func Normalize(pref string) string {
	raw := pref
	pref = strings.TrimSpace(strings.ToLower(pref))
	valid := map[string]bool{}
	for _, o := range OptionsForPlatform() {
		valid[o.Value] = true
	}
	if pref != "" && valid[pref] {
		return pref
	}
	if pref == "" {
		logx.Debugf("[localterm] 本地 Shell 偏好为空，使用平台默认: default=%s", DefaultShell())
	} else {
		logx.Warnf("[localterm] 本地 Shell 偏好非法，回落平台默认: value=%q default=%s", raw, DefaultShell())
	}
	return DefaultShell()
}

// Resolve 根据偏好解析可执行路径、参数与展示名。
func Resolve(pref string) (path string, args []string, label string, err error) {
	pref = Normalize(pref)
	switch runtime.GOOS {
	case "darwin":
		switch pref {
		case "bash":
			path, args, label = lookupOr("bash", "/bin/bash"), []string{"-l"}, "bash"
		default:
			path, args, label = lookupOr("zsh", "/bin/zsh"), []string{"-l"}, "zsh"
		}
	case "windows":
		switch pref {
		case "cmd":
			p := lookupOr("cmd.exe", filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
			path, args, label = p, nil, "cmd"
		default:
			p := lookupOr("powershell.exe", filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
			path, args, label = p, []string{"-NoLogo"}, "PowerShell"
		}
	default:
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		if shell == "" {
			logx.Warnf("[localterm] 环境变量 SHELL 为空，回退到 bash")
			shell = lookupOr("bash", "/bin/bash")
		}
		path, args, label = shell, []string{"-l"}, filepath.Base(shell)
	}
	if err != nil {
		logx.Warnf("[localterm] 解析本地 Shell 可执行文件失败: pref=%s err=%v", pref, err)
		return "", nil, "", err
	}
	logx.Debugf("[localterm] 本地 Shell 已解析: pref=%s path=%q label=%s args=%d", pref, path, label, len(args))
	return path, args, label, nil
}

func lookupOr(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil && p != "" {
		return p
	}
	logx.Warnf("[localterm] 未在 PATH 中找到 Shell 命令，使用回退路径: name=%s fallback=%q", name, fallback)
	return fallback
}
