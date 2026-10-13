//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// revealLocalPath 在系统文件管理器中打开 path：
//   - macOS：open 打开目录，open -R 在访达中选中文件；
//   - 其他（Linux 等）：xdg-open 打开目录，文件则打开其所在目录。
//
// 命令同步执行：open / xdg-open 会立即返回（文件管理器在后台接管），因此这里等待退出
// 可以拿到真实的启动错误并回传给用户。
func revealLocalPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(abs)
	isDir := statErr == nil && info.IsDir()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if isDir {
			cmd = exec.Command("open", abs)
		} else {
			cmd = exec.Command("open", "-R", abs)
		}
	default:
		target := abs
		if !isDir {
			target = filepath.Dir(abs)
		}
		cmd = exec.Command("xdg-open", target)
	}
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("%w（%s）", runErr, string(out))
	}
	return nil
}
