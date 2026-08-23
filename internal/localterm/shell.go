package localterm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		return []ShellOption{
			{Value: "zsh", Label: "zsh"},
			{Value: "bash", Label: "bash"},
		}
	case "windows":
		return []ShellOption{
			{Value: "powershell", Label: "PowerShell"},
			{Value: "cmd", Label: "Command Prompt (cmd)"},
		}
	default: // linux 等
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		label := "系统默认 ($SHELL)"
		if shell != "" {
			label = fmt.Sprintf("系统默认 (%s)", filepath.Base(shell))
		}
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
	pref = strings.TrimSpace(strings.ToLower(pref))
	valid := map[string]bool{}
	for _, o := range OptionsForPlatform() {
		valid[o.Value] = true
	}
	if pref != "" && valid[pref] {
		return pref
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
			return lookupOr("bash", "/bin/bash"), []string{"-l"}, "bash", nil
		default:
			return lookupOr("zsh", "/bin/zsh"), []string{"-l"}, "zsh", nil
		}
	case "windows":
		switch pref {
		case "cmd":
			p := lookupOr("cmd.exe", filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
			return p, nil, "cmd", nil
		default:
			p := lookupOr("powershell.exe", filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
			return p, []string{"-NoLogo"}, "PowerShell", nil
		}
	default:
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		if shell == "" {
			shell = lookupOr("bash", "/bin/bash")
		}
		return shell, []string{"-l"}, filepath.Base(shell), nil
	}
}

func lookupOr(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil && p != "" {
		return p
	}
	return fallback
}
