package apphost

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/sshx"
)

var localEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate and construct without starting a process. Native cwd/env avoid shell
// interpolation and work for Windows paths containing spaces and Chinese.
func localJobCommand(ctx context.Context, req debugsrv.JobRequest) (*exec.Cmd, error) {
	command, err := sshx.ExecCommand(sshx.ExecRequest{Command: req.Command})
	if err != nil {
		return nil, err
	}
	if req.Mode != "" && req.Mode != "raw" && req.Mode != "posix" {
		return nil, errors.New("mode 必须是 raw 或 posix")
	}
	if runtime.GOOS == "windows" && req.Mode == "posix" {
		return nil, errors.New("Windows 本机执行不支持 POSIX 模式；使用 raw 和 shell=cmd/powershell/pwsh")
	}
	if len(req.Cwd) > 8192 || strings.ContainsRune(req.Cwd, 0) || len(req.Env) > 128 {
		return nil, errors.New("cwd/env 超出限制或包含 NUL")
	}
	if req.Cwd != "" {
		if !filepath.IsAbs(req.Cwd) {
			return nil, errors.New("本机 cwd 必须是绝对路径")
		}
		info, err := os.Stat(req.Cwd)
		if err != nil || !info.IsDir() {
			return nil, errors.New("本机 cwd 不存在或不是目录")
		}
	}
	shell := req.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows" && shell == "cmd":
		cmd = exec.CommandContext(ctx, "cmd.exe", "/D", "/S", "/C", command)
	case runtime.GOOS == "windows" && (shell == "powershell" || shell == "pwsh"):
		// EncodedCommand passes Unicode exactly and avoids nested quoting.
		script := "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $ProgressPreference = 'SilentlyContinue'; " + command
		units := utf16.Encode([]rune(script))
		buf := make([]byte, 2*len(units))
		for i, u := range units {
			binary.LittleEndian.PutUint16(buf[2*i:], u)
		}
		cmd = exec.CommandContext(ctx, shell+".exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-EncodedCommand", base64.StdEncoding.EncodeToString(buf))
	case runtime.GOOS != "windows" && shell == "sh":
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	default:
		return nil, fmt.Errorf("当前平台不支持本机 shell=%q", shell)
	}
	if cmd.Err != nil {
		return nil, fmt.Errorf("本机 shell 不可用: %w", cmd.Err)
	}
	cmd.Dir = req.Cwd
	cmd.Env = os.Environ()
	keys := make([]string, 0, len(req.Env))
	for key, value := range req.Env {
		if !localEnvName.MatchString(key) || len(value) > 32768 || strings.ContainsRune(value, 0) {
			return nil, errors.New("env 包含无效变量名或值")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+req.Env[key])
	}
	return cmd, nil
}
